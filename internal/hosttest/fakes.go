package hosttest

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/ports"
)

// Clock is a fake clock. Timers fire only when the test calls Host.Advance.
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	timers []timer
	seq    int
}

type timer struct {
	due time.Time
	seq int
	msg func(time.Time) tea.Msg
}

func newClock(now time.Time) *Clock { return &Clock{now: now} }

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After arms its timer when the Cmd runs, as tea.Tick does.
func (c *Clock) After(d time.Duration, msg func(time.Time) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.seq++
		c.timers = append(c.timers, timer{due: c.now.Add(d), seq: c.seq, msg: msg})
		return nil
	}
}

func (c *Clock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

func (c *Clock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func (c *Clock) sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for i := range c.timers {
		c.timers[i].due = c.timers[i].due.Add(d)
	}
}

func (c *Clock) fireNext(limit time.Time) (tea.Msg, bool) {
	c.mu.Lock()
	if len(c.timers) == 0 {
		c.mu.Unlock()
		return nil, false
	}
	i := 0
	for j, t := range c.timers {
		if t.due.Before(c.timers[i].due) || (t.due.Equal(c.timers[i].due) && t.seq < c.timers[i].seq) {
			i = j
		}
	}
	t := c.timers[i]
	if t.due.After(limit) {
		c.mu.Unlock()
		return nil, false
	}
	c.timers = slices.Delete(c.timers, i, i+1)
	c.now = t.due
	c.mu.Unlock()
	return t.msg(t.due), true
}

// Machine reports the Stats the test last Set, zero until then.
type Machine struct {
	mu    sync.Mutex
	stats ports.Stats
	Err   error
}

func (m *Machine) Set(s ports.Stats) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats = s
}

func (m *Machine) Stats() (ports.Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats, m.Err
}

// Watcher is a fake file watcher. Next arms a waiter when its Cmd runs; the
// test reports changes with Host.Changed.
type Watcher struct {
	mu      sync.Mutex
	files   []string
	waiting []func([]string) tea.Msg
}

func (w *Watcher) Watch(files []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.files = slices.Clone(files)
	return nil
}

func (w *Watcher) Next(msg func([]string) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.waiting = append(w.waiting, msg)
		return nil
	}
}

func (w *Watcher) Files() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.files)
}

// fire returns the message the app is waiting for, for the watched ones
// among files; ok is false when none is watched or nothing waits.
func (w *Watcher) fire(files []string) (msg tea.Msg, watched, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var hit []string
	for _, f := range files {
		if slices.Contains(w.files, f) {
			hit = append(hit, f)
		}
	}
	if len(hit) == 0 {
		return nil, false, false
	}
	if len(w.waiting) == 0 {
		return nil, true, false
	}
	next := w.waiting[0]
	w.waiting = w.waiting[1:]
	return next(hit), true, true
}

// Inbox is a fake inbox. Next arms a waiter on its channel when its Cmd
// runs, instead of blocking; the test delivers with Host.Receive.
type Inbox struct {
	mu      sync.Mutex
	waiting []<-chan tea.Msg
}

func (i *Inbox) Next(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		i.mu.Lock()
		defer i.mu.Unlock()
		i.waiting = append(i.waiting, ch)
		return nil
	}
}

func (i *Inbox) take() (<-chan tea.Msg, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.waiting) == 0 {
		return nil, false
	}
	ch := i.waiting[0]
	i.waiting = i.waiting[1:]
	return ch, true
}

// Opener records every target handed to the system opener.
type Opener struct {
	Opened []string
	Err    error
}

func (o *Opener) Open(target string) error {
	o.Opened = append(o.Opened, target)
	return o.Err
}

// Clipboard records every OSC 52 copy.
type Clipboard struct{ Copied []string }

func (c *Clipboard) Copy(text string) tea.Cmd {
	return func() tea.Msg {
		c.Copied = append(c.Copied, text)
		return nil
	}
}

// Editor records every $EDITOR handoff. OnEdit, when set, plays the user's
// edit of the file before the TUI resumes.
type Editor struct {
	Edited []string
	// Lines are the line each Edited file was opened at, 0 for none.
	Lines  []int
	OnEdit func(path string) error
}

func (e *Editor) Edit(path string, line int, done func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		e.Edited = append(e.Edited, path)
		e.Lines = append(e.Lines, line)
		var err error
		if e.OnEdit != nil {
			err = e.OnEdit(path)
		}
		return done(err)
	}
}

// Keyring is an in-memory keyring; the zero value is empty. A non-nil Err
// makes every call fail, as when no keyring exists.
type Keyring struct {
	mu      sync.Mutex
	secrets map[string]string
	Err     error
}

func (k *Keyring) Get(service, account string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Err != nil {
		return "", k.Err
	}
	s, ok := k.secrets[service+"\x00"+account]
	if !ok {
		return "", ports.ErrSecretNotFound
	}
	return s, nil
}

func (k *Keyring) Set(service, account, secret string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Err != nil {
		return k.Err
	}
	if k.secrets == nil {
		k.secrets = map[string]string{}
	}
	k.secrets[service+"\x00"+account] = secret
	return nil
}

func (k *Keyring) Delete(service, account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Err != nil {
		return k.Err
	}
	if _, ok := k.secrets[service+"\x00"+account]; !ok {
		return ports.ErrSecretNotFound
	}
	delete(k.secrets, service+"\x00"+account)
	return nil
}

// Commands fakes other CLIs. Unscripted commands fail as if not installed.
type Commands struct {
	mu      sync.Mutex
	Calls   [][]string
	scripts map[string]result
}

type result struct {
	out string
	err error
}

// Script makes the command line `name args...` print out and return err.
func (c *Commands) Script(out string, err error, name string, args ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scripts == nil {
		c.scripts = map[string]result{}
	}
	c.scripts[strings.Join(append([]string{name}, args...), " ")] = result{out, err}
}

func (c *Commands) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	line := append([]string{name}, args...)
	c.Calls = append(c.Calls, line)
	r, ok := c.scripts[strings.Join(line, " ")]
	if !ok {
		return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	return []byte(r.out), r.err
}

// Ran is a copy of Calls, safe to take while commands may still run.
func (c *Commands) Ran() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.Calls)
}

// EventKit fakes macOS calendars and reminders. Access defaults to AccessUnsupported.
type EventKit struct {
	mu      sync.Mutex
	Access  map[ports.Entity]ports.Access
	Granted bool // what the user answers to a Request prompt
	// Unaskable is a terminal that can't show the prompt, so a Request
	// leaves undetermined or unaskable access AccessUnaskable.
	Unaskable bool
	Requests  []ports.Entity
	// Calendar is every event; Events returns the ones overlapping its range.
	Calendar []ports.Event
	// Open is every open reminder; Complete removes one and records its ID
	// in Completed.
	Open      []ports.Reminder
	Completed []string
	// Err, when set, fails every read and Complete.
	Err error
}

func (e *EventKit) Status(en ports.Entity) ports.Access {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.Access[en]
}

func (e *EventKit) Request(_ context.Context, en ports.Entity) (ports.Access, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Requests = append(e.Requests, en)
	if a := e.Access[en]; a == ports.AccessUndetermined || a == ports.AccessUnaskable {
		e.Access[en] = ports.AccessDenied
		if e.Unaskable {
			e.Access[en] = ports.AccessUnaskable
		}
		if e.Granted {
			e.Access[en] = ports.AccessGranted
		}
	}
	return e.Access[en], nil
}

func (e *EventKit) Events(_ context.Context, from, to time.Time) ([]ports.Event, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Access[ports.Events] != ports.AccessGranted {
		return nil, ports.ErrNoAccess
	}
	if e.Err != nil {
		return nil, e.Err
	}
	var out []ports.Event
	for _, ev := range e.Calendar {
		if ev.End.After(from) && ev.Start.Before(to) {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (e *EventKit) Reminders(context.Context) ([]ports.Reminder, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Access[ports.Reminders] != ports.AccessGranted {
		return nil, ports.ErrNoAccess
	}
	if e.Err != nil {
		return nil, e.Err
	}
	return slices.Clone(e.Open), nil
}

func (e *EventKit) Complete(_ context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Access[ports.Reminders] != ports.AccessGranted {
		return ports.ErrNoAccess
	}
	if e.Err != nil {
		return e.Err
	}
	i := slices.IndexFunc(e.Open, func(r ports.Reminder) bool { return r.ID == id })
	if i < 0 {
		return errors.New("reminder not found")
	}
	e.Open = slices.Delete(e.Open, i, i+1)
	e.Completed = append(e.Completed, id)
	return nil
}

// Foreground records every command run on the terminal. Err, when set, is
// how each one fails; Output is what Run prints.
type Foreground struct {
	mu     sync.Mutex
	Calls  [][]string
	Output string
	Err    error
}

func (f *Foreground) Run(name string, args []string, stdout, _ io.Writer) error {
	f.record(name, args)
	_, _ = io.WriteString(stdout, f.Output)
	return f.Err
}

func (f *Foreground) Suspend(name string, args []string, done func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		f.record(name, args)
		return done(f.Err)
	}
}

func (f *Foreground) record(name string, args []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, append([]string{name}, args...))
}

// Ran is a copy of Calls, safe to take while commands may still run.
func (f *Foreground) Ran() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Calls)
}
