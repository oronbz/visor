// Package plugin is the external plugin host (ADR 0001): it reads Plugin
// manifests, runs one long-lived process per Plugin speaking JSON-RPC 2.0
// over stdio, and adapts that protocol to the Widget contract, so the host
// app treats a Plugin's Widgets like built-in ones.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/version"
	"github.com/oronbz/visor/internal/widget"
)

const Protocol = 1

const (
	// Each crash in a row doubles the wait before a restart, up to maxRestart.
	firstRestart = time.Second
	maxRestart   = time.Minute
	// steady is how long a process must run for its crash to count as a first one again.
	steady           = time.Minute
	handshakeTimeout = 10 * time.Second
	invokeTimeout    = time.Minute
	exitWait         = time.Second
)

// Registry is every Plugin visor found, keyed by directory, so finding them
// again after a config change keeps the processes already running.
type Registry struct {
	mu      sync.Mutex
	plugins map[string]*Plugin
	// gone are stand-ins for the types of Plugins whose directories vanished.
	gone      map[string]widget.Type
	restarted []string
}

func NewRegistry() *Registry {
	return &Registry{plugins: map[string]*Plugin{}, gone: map[string]widget.Type{}}
}

// Dirs are the directories under pluginsDir holding a manifest, then paths.
func Dirs(pluginsDir string, paths []string) []string {
	var dirs []string
	entries, _ := os.ReadDir(pluginsDir)
	for _, e := range entries {
		// Hidden entries are the installer's staging area.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(pluginsDir, e.Name())
		if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err == nil {
			dirs = append(dirs, dir)
		}
	}
	return append(dirs, paths...)
}

// Discover returns the Widget types the manifests in dirs provide, leaving
// out type names in taken and any a Plugin read earlier provides.
func (r *Registry) Discover(dirs []string, taken []string) ([]widget.Type, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var types []widget.Type
	var problems, names []string
	taken = slices.Clone(taken)
	r.restarted = nil
	read := map[string]bool{}
	for _, dir := range dirs {
		m, err := ReadManifest(dir)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		read[dir] = true
		file := filepath.Join(dir, ManifestFile)
		if slices.Contains(names, m.Name) {
			problems = append(problems, fmt.Sprintf("%s: another Plugin is already called `%s`", file, m.Name))
			continue
		}
		names = append(names, m.Name)
		rec, _ := ReadRecord(dir)
		types = append(types, r.provide(dir, Identity(dir, rec), m, rec, &taken, func(typ string) {
			problems = append(problems, fmt.Sprintf("%s: Widget type `%s` is taken; it's left out", file, typ))
		})...)
	}
	for dir, p := range r.plugins {
		if _, err := os.Stat(dir); read[dir] || !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		delete(r.plugins, dir)
		for _, wt := range p.m.Widgets {
			if !slices.Contains(taken, wt.Type) {
				r.gone[wt.Type] = widget.Type{Name: wt.Type, Title: wt.Title, OptIn: true, Missing: p.missing()}
			}
		}
	}
	return types, problems
}

// Bundled returns the Widget types of the Plugins visor ships with. A type
// name in taken is left out quietly, so an installed or path-listed Plugin
// of the same name takes its place.
func (r *Registry) Bundled(ms []Manifest, taken []string) []widget.Type {
	r.mu.Lock()
	defer r.mu.Unlock()
	taken = slices.Clone(taken)
	var types []widget.Type
	for _, m := range ms {
		key := "bundled:" + m.Name
		types = append(types, r.provide(key, key, m, Record{}, &taken, func(string) {})...)
	}
	return types
}

// provide registers m's Plugin under key, restarting the one already there
// when its manifest or record changed.
func (r *Registry) provide(key, identity string, m Manifest, rec Record, taken *[]string, isTaken func(typ string)) []widget.Type {
	p := r.plugins[key]
	switch {
	case p == nil || p.name != m.Name:
		p = &Plugin{name: m.Name, identity: identity, m: m, record: rec, widgets: map[string]*pluginWidget{}}
		r.plugins[key] = p
	case !reflect.DeepEqual(p.m, m) || p.record != rec:
		// Widgets keep their Plugin across a reload, so an update restarts it in place.
		p.restart(identity, m, rec)
		for _, wt := range m.Widgets {
			r.restarted = append(r.restarted, wt.Type)
		}
	}
	var types []widget.Type
	for _, wt := range m.Widgets {
		if slices.Contains(*taken, wt.Type) {
			isTaken(wt.Type)
			continue
		}
		*taken = append(*taken, wt.Type)
		delete(r.gone, wt.Type)
		t, _ := wt.widgetType()
		t.New = func(c widget.Config) widget.Widget { return p.widget(t.Name, t.Settings, c) }
		types = append(types, t)
	}
	return types
}

// Gone are stand-ins for the Widget types of Plugins whose directories
// vanished since they were found, such as uninstalled ones.
func (r *Registry) Gone() []widget.Type {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.SortedFunc(maps.Values(r.gone), func(a, b widget.Type) int { return strings.Compare(a.Name, b.Name) })
}

// Restarted are the Widget types of the Plugins the last Discover
// restarted because they changed on disk.
func (r *Registry) Restarted() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.restarted)
}

func (r *Registry) Close() {
	r.mu.Lock()
	plugins := slices.Collect(maps.Values(r.plugins))
	r.mu.Unlock()
	for _, p := range plugins {
		p.shutdown()
	}
}

// Plugin is one Plugin and its process, started when a Widget first needs
// it and restarted with backoff after a crash.
type Plugin struct {
	name string

	// identity, m and record change only under mu, when the Plugin is restarted.
	identity string
	m        Manifest
	record   Record

	mu      sync.Mutex
	clock   ports.Clock
	keyring ports.Keyring
	proc    *process
	// fails counts crashes in a row; retry is when the process may start again.
	fails   int
	retry   time.Time
	stopped error
	closed  bool
	// tap, when set, sees every message in (out false) and out, as sent.
	tap func(out bool, raw []byte)

	wmu     sync.Mutex
	widgets map[string]*pluginWidget
}

type process struct {
	cmd     *exec.Cmd
	conn    *conn
	stdin   io.Closer
	started time.Time
	stderr  *tail
	// stopping marks a process visor stopped on purpose, which isn't a crash.
	stopping atomic.Bool
	exited   chan struct{}
}

func (p *Plugin) widget(typ string, schema []widget.Setting, c widget.Config) *pluginWidget {
	w := &pluginWidget{p: p, id: c.ID, typ: typ, schema: schema, settings: settingsJSON(c.Settings)}
	p.mu.Lock()
	if p.clock == nil {
		p.clock, p.keyring = c.Ports.Clock, c.Ports.Keyring
	}
	p.mu.Unlock()
	p.wmu.Lock()
	p.widgets[w.id] = w
	p.wmu.Unlock()
	return w
}

func (p *Plugin) connect(ctx context.Context, w *pluginWidget) (*process, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m.Protocol != Protocol {
		return nil, p.versionErr("declares", p.m.Protocol)
	}
	if p.proc == nil {
		if p.closed {
			return nil, &widget.LocalError{Err: p.withName(errClosed)}
		}
		if now := p.clock.Now(); now.Before(p.retry) {
			return nil, p.stoppedErr(now)
		}
		proc, err := p.start(ctx)
		if err != nil {
			return nil, err
		}
		p.proc = proc
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.on != p.proc {
		settings := Secrets{p.keyring, p.identity}.withSecrets(w.settings, w.schema)
		open := openParams{frameParams: frameOf(w.id, w.shown), Type: w.typ, Settings: settings}
		if err := p.proc.conn.Notify("widget/open", open); err != nil {
			return nil, p.callErr(err)
		}
		w.on = p.proc
	}
	return p.proc, nil
}

func (p *Plugin) start(ctx context.Context) (*process, error) {
	proc, err := p.spawn()
	if err != nil {
		return nil, err
	}
	if err := p.handshake(ctx, proc); err != nil {
		return nil, err
	}
	return proc, nil
}

func (p *Plugin) spawn() (*process, error) {
	name := p.m.Command[0]
	if strings.ContainsAny(name, `/\`) && !filepath.IsAbs(name) {
		name = filepath.Join(p.m.Dir, name)
	}
	cmd := exec.Command(name, p.m.Command[1:]...)
	cmd.Dir = p.m.Dir
	if len(p.m.Env) > 0 {
		cmd.Env = append(os.Environ(), p.m.Env...)
	}
	cmd.WaitDelay = exitWait
	proc := &process{cmd: cmd, started: p.clock.Now(), stderr: &tail{}, exited: make(chan struct{})}
	cmd.Stderr = proc.stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, p.withName(fmt.Errorf("didn't start: %w", err))
	}
	proc.stdin = stdin
	proc.conn = newConn(stdout, stdin, p.tap, p.notified, func(err error) { p.ended(proc, err) })
	return proc, nil
}

// handshake sends initialize; a Plugin that fails it is stopped.
func (p *Plugin) handshake(ctx context.Context, proc *process) error {
	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	var res initializeResult
	if err := proc.conn.Call(ctx, "initialize", initializeParams{Protocol: Protocol, Visor: version.Version}, &res); err != nil {
		proc.stop()
		<-proc.exited
		return p.withName(fmt.Errorf("failed its handshake: %w", proc.exitReason(err)))
	}
	if res.Protocol != Protocol {
		proc.stop()
		return p.versionErr("speaks", res.Protocol)
	}
	return nil
}

func (p *Plugin) versionErr(verb string, v int) error {
	return &widget.SetupError{Hint: fmt.Sprintf("Plugin `%s` %s protocol %d, but this visor speaks %d. Update visor or the Plugin.", p.name, verb, v, Protocol)}
}

// restart takes the Plugin as it now is on disk: the running process stops,
// and the next call starts the new one, reopening each Widget on it.
func (p *Plugin) restart(identity string, m Manifest, rec Record) {
	p.mu.Lock()
	p.identity, p.m, p.record = identity, m, rec
	proc := p.proc
	p.proc, p.fails, p.retry = nil, 0, time.Time{}
	p.mu.Unlock()
	if proc != nil {
		proc.stop()
	}
}

func (p *Plugin) missing() string { return missing(p.name, p.record) }

// ended handles the end of proc's session: a crash fails its Widgets and
// sets when the process may start again.
func (p *Plugin) ended(proc *process, err error) {
	if proc.cmd.Process != nil {
		_ = proc.cmd.Process.Kill()
	}
	waitErr := proc.cmd.Wait()
	close(proc.exited)
	p.mu.Lock()
	if p.proc != proc || proc.stopping.Load() {
		p.mu.Unlock()
		return
	}
	p.proc = nil
	now := p.clock.Now()
	if now.Sub(proc.started) >= steady {
		p.fails = 0
	}
	p.fails++
	p.retry = now.Add(restartDelay(p.fails))
	if errors.Is(err, io.EOF) {
		err = waitErr
	}
	if err == nil {
		err = errors.New("exited")
	}
	p.stopped = proc.stderr.withLastLine(err)
	retry, crash := p.retry, p.stoppedErr(now)
	p.mu.Unlock()
	for _, w := range p.listed() {
		w.deliver(widget.Push{Err: crash, Lost: true, Retry: retry})
	}
}

func restartDelay(fails int) time.Duration {
	d := firstRestart
	for i := 1; i < fails && d < maxRestart; i++ {
		d *= 2
	}
	return min(d, maxRestart)
}

// stoppedErr is what a stopped Plugin's Widgets show until it restarts.
func (p *Plugin) stoppedErr(now time.Time) error {
	hint := "Restarting it in " + widget.ShortDuration(p.retry.Sub(now).Round(time.Second))
	return &widget.LocalError{Err: failure{msg: fmt.Sprintf("Plugin `%s` stopped: %v", p.name, p.stopped), reason: "plugin stopped", hint: hint}}
}

func (p *Plugin) listed() []*pluginWidget {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	ws := make([]*pluginWidget, 0, len(p.widgets))
	for _, w := range p.widgets {
		ws = append(ws, w)
	}
	return ws
}

func (p *Plugin) notified(method string, params json.RawMessage) {
	if method != "widget/update" {
		return
	}
	var u update
	if err := json.Unmarshal(params, &u); err != nil {
		return
	}
	p.wmu.Lock()
	w := p.widgets[u.Widget]
	p.wmu.Unlock()
	if w == nil {
		return
	}
	payload, err := w.read(u)
	w.deliver(widget.Push{Payload: payload, Err: err})
}

func (p *Plugin) secrets() Secrets {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Secrets{p.keyring, p.identity}
}

// reopen sends every Widget of the Plugin widget/open again, so each gets
// the secrets as now stored, and refreshes all but by, which its Action's
// host refreshes.
func (p *Plugin) reopen(by *pluginWidget) {
	for _, w := range p.listed() {
		w.mu.Lock()
		w.on = nil
		w.mu.Unlock()
		if w != by {
			go w.repush()
		}
	}
}

// withName puts the Plugin's name in front of errors visor found; ones the
// Plugin reported itself already read right.
func (p *Plugin) withName(err error) error {
	var f failure
	var setup *widget.SetupError
	var limited *widget.RateLimitError
	var rpc *RPCError
	if err == nil || errors.As(err, &f) || errors.As(err, &setup) || errors.As(err, &limited) || errors.As(err, &rpc) {
		return err
	}
	return fmt.Errorf("Plugin `%s` %w", p.name, err)
}

func (p *Plugin) callErr(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, errClosed) {
		return &widget.LocalError{Err: p.withName(errors.New("stopped"))}
	}
	return p.withName(err)
}

// release lets go of w, stopping the process once no Widget needs it.
func (p *Plugin) release(w *pluginWidget, on *process) {
	p.wmu.Lock()
	// A Widget rebuilt under the same id has taken w's place, so the Plugin keeps it.
	replaced := p.widgets[w.id] != w
	if !replaced {
		delete(p.widgets, w.id)
	}
	left := len(p.widgets)
	p.wmu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if on != nil && on == p.proc && !replaced {
		_ = on.conn.Notify("widget/close", widgetParams{Widget: w.id})
	}
	if left == 0 && p.proc != nil {
		p.proc.stop()
		p.proc = nil
	}
}

func (p *Plugin) shutdown() {
	p.mu.Lock()
	proc := p.proc
	p.proc, p.closed = nil, true
	p.mu.Unlock()
	if proc == nil {
		return
	}
	proc.stop()
	select {
	case <-proc.exited:
	case <-time.After(exitWait):
	}
}

func (proc *process) stop() {
	proc.stopping.Store(true)
	proc.conn.Close()
	_ = proc.stdin.Close()
	_ = proc.cmd.Process.Kill()
}

// exitReason is how the process ended, when that's why a call failed.
func (proc *process) exitReason(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, errClosed) {
		<-proc.exited
		return proc.stderr.withLastLine(proc.cmd.ProcessState)
	}
	return err
}

// tail keeps the end of a Plugin's stderr, to say why it stopped.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(b), nil
}

func (t *tail) withLastLine(err any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.buf)), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return fmt.Errorf("%v (%s)", err, last)
	}
	return fmt.Errorf("%v", err)
}

// pluginWidget adapts one Widget of a Plugin's type to the Widget contract.
type pluginWidget struct {
	p        *Plugin
	id, typ  string
	schema   []widget.Setting
	settings map[string]any

	mu     sync.Mutex
	shown  widget.Shown
	push   func(widget.Push)
	closed bool
	on     *process
}

func (w *pluginWidget) Refresh(ctx context.Context, _ time.Time) (widget.Payload, error) {
	if err := w.missingSecret(); err != nil {
		return widget.Payload{}, err
	}
	proc, err := w.p.connect(ctx, w)
	if err != nil {
		return widget.Payload{}, err
	}
	var u update
	if err := proc.conn.Call(ctx, "widget/refresh", widgetParams{Widget: w.id}, &u); err != nil {
		return widget.Payload{}, w.p.callErr(err)
	}
	p, err := w.read(u)
	switch {
	case err != nil:
		return widget.Payload{}, err
	case p == nil:
		return widget.Payload{}, w.p.withName(errors.New("answered widget/refresh with no payload"))
	}
	return *p, nil
}

// read is u as w shows it, with the host's own Action for each of w's
// secret settings added to its payload.
func (w *pluginWidget) read(u update) (*widget.Payload, error) {
	p, err := u.read(w.p.m.Dir, w.typ, w.schema)
	if err != nil {
		return nil, w.p.withName(err)
	}
	w.offerSecrets(p)
	return p, nil
}

// repush refreshes w unasked and pushes what it gets.
func (w *pluginWidget) repush() {
	p, err := w.Refresh(context.Background(), time.Time{})
	if err != nil {
		w.deliver(widget.Push{Err: err})
		return
	}
	w.deliver(widget.Push{Payload: &p})
}

// missingSecret is the setup a required secret setting with no stored
// value puts w in, which the Plugin is never refreshed through.
func (w *pluginWidget) missingSecret() error {
	secrets := w.p.secrets()
	for _, s := range w.schema {
		if s.Kind != widget.Secret || !s.Required {
			continue
		}
		if _, ok := secrets.Get(s.Name); !ok {
			fix := s.SetAction()
			return &widget.SetupError{Hint: "No " + s.Label() + " set", Action: &fix}
		}
	}
	return nil
}

// offerSecrets adds the host's own Action for each of w's secret settings.
func (w *pluginWidget) offerSecrets(p *widget.Payload) {
	if p == nil {
		return
	}
	for _, s := range w.schema {
		if s.Kind == widget.Secret {
			p.Actions = append(p.Actions, s.SetAction())
		}
	}
}

// setSecret keeps value as secret setting name, which never reaches the
// Plugin as an Action, and reopens the Plugin's Widgets with it.
func (w *pluginWidget) setSecret(name, value string) (widget.Result, error) {
	i := slices.IndexFunc(w.schema, func(s widget.Setting) bool { return s.Kind == widget.Secret && s.Name == name })
	if i < 0 {
		return widget.Result{}, fmt.Errorf("Widget type `%s` has no secret setting `%s`", w.typ, name)
	}
	if err := w.p.secrets().store(w.schema[i], value); err != nil {
		return widget.Result{}, err
	}
	w.p.reopen(w)
	return widget.Result{Toast: "Saved the " + w.schema[i].Label() + " to the keyring"}, nil
}

func (w *pluginWidget) Invoke(ctx context.Context, inv widget.Invocation) (widget.Result, error) {
	if name, ok := strings.CutPrefix(inv.Action, widget.SecretPrefix); ok {
		return w.setSecret(name, inv.Text)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, invokeTimeout)
		defer cancel()
	}
	proc, err := w.p.connect(ctx, w)
	if err != nil {
		return widget.Result{}, err
	}
	var res invokeResult
	err = proc.conn.Call(ctx, "action/invoke", invokeParams{Widget: w.id, Action: inv.Action, Item: inv.Item, Text: inv.Text, Choice: inv.Choice, Base: inv.Base}, &res)
	if err != nil {
		return widget.Result{}, w.p.callErr(err)
	}
	if res.Then != nil {
		if err := ownsNoSecret(*res.Then); err != nil {
			return widget.Result{}, w.p.withName(err)
		}
	}
	settings, err := res.settings(w.schema)
	if err != nil {
		return widget.Result{}, w.p.withName(err)
	}
	res.Result.Settings = settings
	return res.Result, nil
}

func (w *pluginWidget) Choices(ctx context.Context, inv widget.Invocation) ([]widget.Choice, error) {
	proc, err := w.p.connect(ctx, w)
	if err != nil {
		return nil, err
	}
	var res choicesResult
	if err := proc.conn.Call(ctx, "action/choices", choicesParams{Widget: w.id, Action: inv.Action, Item: inv.Item}, &res); err != nil {
		return nil, w.p.callErr(err)
	}
	choices := make([]widget.Choice, len(res.Choices))
	for i, c := range res.Choices {
		choices[i] = widget.Choice{ID: c.ID, Label: c.Label, Target: c.Target}
	}
	return choices, nil
}

func (w *pluginWidget) Listen(push func(widget.Push)) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.push = push
	w.mu.Unlock()
	if _, err := w.p.connect(context.Background(), w); err != nil {
		w.deliver(widget.Push{Err: err})
	}
}

func (w *pluginWidget) Observe(s widget.Shown) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.shown = s
	if w.on != nil {
		_ = w.on.conn.Notify("widget/frame", frameOf(w.id, s))
	}
}

func (w *pluginWidget) Close() {
	w.mu.Lock()
	w.closed, w.push = true, nil
	on := w.on
	w.mu.Unlock()
	go w.p.release(w, on)
}

func (w *pluginWidget) deliver(push widget.Push) {
	w.mu.Lock()
	fn := w.push
	w.mu.Unlock()
	if fn != nil {
		fn(push)
	}
}
