// Package hosttest is the headless host: the full visor app built in-process
// with a temp root, an injected clock, a fixed terminal size and fake
// outside-world adapters. Tests drive it with key, mouse and resize messages
// and read the rendered screen back as text and styled cells.
package hosttest

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/closekey"
	"github.com/oronbz/visor/internal/config"
	"github.com/oronbz/visor/internal/host"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

type Options struct {
	// Width and Height default to 140×38.
	Width, Height int
	// Now defaults to Sunday 18 October 2026, 14:05:09 UTC.
	Now time.Time
	// Root defaults to a fresh t.TempDir(). Two hosts given the same Root share config, data and cache.
	Root string
	// Endpoints overrides HTTP base URLs, typically with httptest servers. Unset fields keep the real URLs.
	Endpoints ports.Endpoints
	// Theme pins the palette. Left nil, the app picks it from config.toml and
	// the terminal colour answers the test sends.
	Theme *theme.Theme
	// Config, when set, is written to config.toml in the config dir before the app starts.
	Config string
	// Types replaces the built-in Widget types (see host.Options.Types).
	Types []widget.Type
	// CloseKeys are --close-key values.
	CloseKeys []string
	// EventKit, when set, is the EventKit fake the app starts with, so its
	// first refresh already sees the access and events.
	EventKit *EventKit
	// Keyring, when set, is the keyring fake the app starts with, so its
	// first refresh already sees the stored secrets (or Err).
	Keyring *Keyring
	// Commands, when set, is the command fake the app starts with, so its
	// first refresh already sees the scripted outputs.
	Commands *Commands
	// Env is the process environment the app sees; it's empty by default.
	Env map[string]string
	// Version is the visor version the app runs as (see host.Options.Version).
	Version string
	// Executable is the path visor was launched by (see host.Options.Executable).
	Executable string
	// BundleDir stands in for the directory visor is installed in, where it
	// finds the Plugins it ships with; empty ships none.
	BundleDir string
	// Ports, when set, turns the fakes into the adapters the app runs on, so
	// a test can swap in others, such as the demo's.
	Ports func(fakes ports.Ports) ports.Ports
}

var DefaultNow = time.Date(2026, 10, 18, 14, 5, 9, 0, time.UTC)

type Host struct {
	t     testing.TB
	Root  string
	Paths paths.Paths

	Clock      *Clock
	Opener     *Opener
	Clipboard  *Clipboard
	Editor     *Editor
	Keyring    *Keyring
	Commands   *Commands
	Foreground *Foreground
	EventKit   *EventKit
	Machine    *Machine
	Watcher    *Watcher
	Inbox      *Inbox

	model  tea.Model
	w, h   int
	quit   bool
	clears int
	raw    strings.Builder
}

func New(t testing.TB, o Options) *Host {
	t.Helper()
	if o.Width == 0 {
		o.Width = 140
	}
	if o.Height == 0 {
		o.Height = 38
	}
	if o.Now.IsZero() {
		o.Now = DefaultNow
	}
	if o.Root == "" {
		o.Root = t.TempDir()
	}
	if o.EventKit == nil {
		o.EventKit = &EventKit{}
	}
	if o.Keyring == nil {
		o.Keyring = &Keyring{}
	}
	if o.Commands == nil {
		o.Commands = &Commands{}
	}
	var th theme.Theme
	if o.Theme != nil {
		th = *o.Theme
	}
	h := &Host{
		t:          t,
		Root:       o.Root,
		Paths:      paths.Under(o.Root),
		Clock:      newClock(o.Now),
		Opener:     &Opener{},
		Clipboard:  &Clipboard{},
		Editor:     &Editor{},
		Keyring:    o.Keyring,
		Commands:   o.Commands,
		Foreground: &Foreground{},
		EventKit:   o.EventKit,
		Machine:    &Machine{},
		Watcher:    &Watcher{},
		Inbox:      &Inbox{},
	}
	if o.Config != "" {
		if err := os.MkdirAll(h.Paths.Config, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.Paths.Config, config.FileName), []byte(o.Config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var closeKeys []closekey.Seq
	for _, k := range o.CloseKeys {
		seq, err := closekey.Parse(k)
		if err != nil {
			t.Fatal(err)
		}
		closeKeys = append(closeKeys, seq)
	}
	pp := ports.Ports{
		Clock:      h.Clock,
		Opener:     h.Opener,
		Clipboard:  h.Clipboard,
		Editor:     h.Editor,
		Keyring:    h.Keyring,
		Commands:   h.Commands,
		Foreground: h.Foreground,
		EventKit:   h.EventKit,
		Machine:    h.Machine,
		Watcher:    h.Watcher,
		Inbox:      h.Inbox,
		HTTP:       &http.Client{Timeout: 5 * time.Second},
		Endpoints:  mergeEndpoints(ports.DefaultEndpoints(), o.Endpoints),
		Getenv:     func(k string) string { return o.Env[k] },
	}
	if o.Ports != nil {
		pp = o.Ports(pp)
	}
	m := host.New(host.Options{
		CloseKeys:  closeKeys,
		Paths:      h.Paths,
		Theme:      th,
		Types:      o.Types,
		Version:    o.Version,
		BundleDir:  o.BundleDir,
		Executable: o.Executable,
		Ports:      pp,
	})
	h.model = m
	t.Cleanup(func() {
		if c, ok := h.model.(interface{ Close() }); ok {
			c.Close()
		}
	})
	h.run(m.Init())
	h.Resize(o.Width, o.Height)
	return h
}

func mergeEndpoints(base, over ports.Endpoints) ports.Endpoints {
	b, o := reflect.ValueOf(&base).Elem(), reflect.ValueOf(over)
	for i := range b.NumField() {
		if s := o.Field(i).String(); s != "" {
			b.Field(i).SetString(s)
		}
	}
	return base
}

// Send delivers msg to the app and runs every Cmd it returns inline, feeding
// their messages back in until the app is idle.
func (h *Host) Send(msg tea.Msg) {
	h.t.Helper()
	if h.quit {
		h.t.Fatalf("hosttest: Send(%T) after the app quit", msg)
	}
	var cmd tea.Cmd
	h.model, cmd = h.model.Update(msg)
	h.run(cmd)
}

// Press sends one key press per argument, in Key syntax ("q", "ctrl+k", "H", "shift+tab").
func (h *Host) Press(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.Send(Key(k))
	}
}

// Release sends one key release per argument, in Key syntax, the way a
// terminal reporting event types does. A released modifier no longer sets its own Mod.
func (h *Host) Release(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		r := tea.KeyReleaseMsg(Key(k))
		if m, ok := loneModifiers[k]; ok {
			r.Mod &^= m.Mod
		}
		h.Send(r)
	}
}

func (h *Host) Resize(w, hh int) {
	h.t.Helper()
	h.w, h.h = w, hh
	h.Send(tea.WindowSizeMsg{Width: w, Height: hh})
}

// Advance moves the fake clock forward, firing every timer that falls due on the way, in order.
func (h *Host) Advance(d time.Duration) {
	h.t.Helper()
	target := h.Clock.Now().Add(d)
	for {
		msg, ok := h.Clock.fireNext(target)
		if !ok {
			break
		}
		h.Send(msg)
	}
	h.Clock.set(target)
}

// Sleep jumps the wall clock forward by d the way sleeping the machine does:
// timers count monotonic time, so each still waits what it had left, and
// nothing fires until the next Advance.
func (h *Host) Sleep(d time.Duration) { h.Clock.sleep(d) }

// Changed reports that files changed on disk, as the file watcher would.
// Files the app doesn't watch are ignored, like a real watcher ignores them.
func (h *Host) Changed(files ...string) {
	h.t.Helper()
	msg, watched, ok := h.Watcher.fire(files)
	if !watched {
		return
	}
	if !ok {
		h.t.Fatalf("hosttest: %v changed but the app isn't waiting for file changes", files)
	}
	h.Send(msg)
}

const receiveWait = 10 * time.Second

// Receive waits for the next message that arrives on its own from outside
// the app, such as a Plugin's push, and delivers it.
func (h *Host) Receive() {
	h.t.Helper()
	ch, ok := h.Inbox.take()
	if !ok {
		h.t.Fatal("hosttest: Receive, but the app isn't waiting for messages")
	}
	select {
	case msg := <-ch:
		h.Send(msg)
	case <-time.After(receiveWait):
		h.t.Fatalf("hosttest: no message arrived within %v", receiveWait)
	}
}

// ReceiveUntil delivers messages that arrive on their own until done holds.
func (h *Host) ReceiveUntil(done func() bool) {
	h.t.Helper()
	for !done() {
		h.Receive()
	}
}

// Quit reports whether the app has asked Bubble Tea to quit.
func (h *Host) Quit() bool { return h.quit }

// Restart is the path the app asked to be run again through as it quit, after an Upgrade.
func (h *Host) Restart() string { return h.model.(host.Model).Restart() }

// Clears counts the times the app has asked Bubble Tea to clear the screen.
func (h *Host) Clears() int { return h.clears }

// Written is everything the app has written to the terminal outside its
// frames, such as queries and kitty graphics escapes, in order.
func (h *Host) Written() string { return h.raw.String() }

var (
	cmdType         = reflect.TypeFor[tea.Cmd]()
	clearScreenType = reflect.TypeOf(tea.ClearScreen())
)

func (h *Host) run(cmd tea.Cmd) {
	if cmd == nil || h.quit {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.QuitMsg:
		h.quit = true
	case tea.RawMsg:
		fmt.Fprint(&h.raw, msg.Msg)
	case tea.BatchMsg:
		for _, c := range msg {
			h.run(c)
		}
	default:
		if reflect.TypeOf(msg) == clearScreenType {
			h.clears++
			return
		}
		// tea.Sequence returns an unexported []tea.Cmd; unpack it by reflection.
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == cmdType {
			for i := range v.Len() {
				h.run(v.Index(i).Interface().(tea.Cmd))
			}
			return
		}
		h.Send(msg)
	}
}
