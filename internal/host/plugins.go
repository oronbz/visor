package host

import (
	"path/filepath"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/config"
	"github.com/oronbz/visor/internal/layout"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/plugin"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/widget"
)

// pushedMsg is an update Widget from sent unasked; from tells it from a
// Widget since rebuilt under the same id.
type pushedMsg struct {
	id   string
	from widget.Widget
	push widget.Push
}

type retryMsg struct {
	id   string
	from widget.Widget
}

// observation is the Shown an Observer was last told.
type observation struct {
	w     widget.Widget
	shown widget.Shown
}

// loadConfig reads config.toml against the Widget types base plus those of
// the Plugins it finds, which may be listed in config.toml itself. It
// returns the config, why it didn't parse, every type and the Plugins' problems.
func (m Model) loadConfig(file string, base []widget.Type) (config.Config, error, []widget.Type, []string) {
	cfg, err := config.Load(file, base, m.opts.Locale)
	var paths []string
	for _, p := range cfg.Plugins {
		paths = append(paths, m.pluginPath(file, p.Path))
	}
	found, problems := m.plugins.Discover(plugin.Dirs(m.pluginsDir(), paths), names(base))
	found = append(found, m.plugins.Bundled(m.bundled(), names(base, found))...)
	gone := m.plugins.Gone()
	gone = append(gone, plugin.Buried(m.pluginsDir(), names(base, found, gone))...)
	gone = append(gone, unavailable(names(base, found, gone))...)
	if len(found) == 0 && len(gone) == 0 {
		return cfg, err, base, problems
	}
	types := append(append([]widget.Type{}, base...), found...)
	// The stand-ins for missing types only tell config what their Widgets need.
	cfg, err = config.Load(file, append(slices.Clip(types), gone...), m.opts.Locale)
	return cfg, err, types, problems
}

func names(lists ...[]widget.Type) []string {
	var out []string
	for _, l := range lists {
		for _, t := range l {
			out = append(out, t.Name)
		}
	}
	return out
}

// Types are the built-in Widget types plus those of the Plugins installed in
// p's data dir, listed in configFile or shipped in bundleDir, with the
// Plugins' problems. No Plugin is started.
func Types(p paths.Paths, configFile, bundleDir string, loc config.Locale, getenv func(string) string) ([]widget.Type, []string) {
	m := Model{opts: Options{Paths: p, Locale: loc, BundleDir: bundleDir, Ports: ports.Ports{Getenv: getenv}}, plugins: plugin.NewRegistry()}
	_, _, types, problems := m.loadConfig(configFile, Builtins)
	return types, problems
}

// PluginPaths are the Plugin directories configFile lists with [[plugin]] path.
func PluginPaths(configFile string, loc config.Locale, getenv func(string) string) []string {
	m := Model{opts: Options{Locale: loc, Ports: ports.Ports{Getenv: getenv}}}
	cfg, _ := config.Load(configFile, Builtins, loc)
	var dirs []string
	for _, p := range cfg.Plugins {
		dirs = append(dirs, m.pluginPath(configFile, p.Path))
	}
	return dirs
}

// pluginPath resolves a [[plugin]] path: ~/ is the home directory and a
// relative path is relative to config.toml.
func (m Model) pluginPath(configFile, p string) string {
	if p = paths.Expand(p, paths.Home(m.opts.Ports.Getenv)); filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(filepath.Dir(configFile), p)
}

// Close stops every Plugin's process.
func (m Model) Close() { m.plugins.Close() }

func (m Model) listen(ids []string) tea.Cmd {
	var cmds []tea.Cmd
	for _, id := range ids {
		w := m.widgets[id].widget
		p, ok := w.(widget.Pusher)
		if !ok {
			continue
		}
		ch := m.pushes
		cmds = append(cmds, func() tea.Msg {
			p.Listen(func(push widget.Push) { ch <- pushedMsg{id: id, from: w, push: push} })
			return nil
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) nextPush() tea.Cmd { return m.opts.Ports.Inbox.Next(m.pushes) }

// pushed takes an update a Widget sent unasked as if a refresh had fetched it.
func (m Model) pushed(msg pushedMsg) (Model, tea.Cmd) {
	cmds := []tea.Cmd{m.nextPush()}
	s, ok := m.sched[msg.id]
	if !ok || m.widgets[msg.id].widget != msg.from {
		return m, cmds[0]
	}
	now := m.opts.Ports.Clock.Now()
	m.now = now
	if msg.push.Lost {
		delete(m.fetched, msg.id)
		s.fetched, s.cached = time.Time{}, false
	}
	var fetched time.Time
	var p widget.Payload
	if msg.push.Payload != nil && msg.push.Err == nil {
		fetched, p = now, *msg.push.Payload
	}
	s, _ = m.settle(msg.id, s, p, fetched, msg.push.Err)
	s, write := m.cachePush(msg.id, s, msg.push, now)
	m.sched[msg.id] = s
	cmds = append(cmds, write)
	m.clampList(msg.id)
	if at := msg.push.Retry; !at.IsZero() {
		id, from := msg.id, msg.from
		cmds = append(cmds, m.opts.Ports.Clock.After(at.Sub(now), func(time.Time) tea.Msg { return retryMsg{id: id, from: from} }))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) retried(msg retryMsg) (Model, tea.Cmd) {
	if m.widgets[msg.id].widget != msg.from || m.layout.IsHidden(msg.id) {
		return m, nil
	}
	return m.start(msg.id)
}

func (m Model) observe() {
	// Until the terminal reports its size, any size would be a guess, and a
	// Plugin would fit its first payload to it.
	if m.w == 0 || m.h == 0 {
		return
	}
	var rects map[string]layout.Rect
	for _, id := range m.ids() {
		w := m.widgets[id].widget
		o, ok := w.(widget.Observer)
		if !ok {
			continue
		}
		if rects == nil {
			rects = m.rects()
		}
		var s widget.Shown
		if r, visible := rects[id]; visible {
			s = widget.Shown{Size: widget.Size{W: r.W - 2, H: r.H - 2}, Focused: id == m.focus, Visible: true}
		}
		if last, ok := m.observed[id]; ok && last.w == w && last.shown == s {
			continue
		}
		m.observed[id] = observation{w: w, shown: s}
		o.Observe(s)
	}
}

func (m Model) closeDropped(old map[string]placed) {
	for id, p := range old {
		if c, ok := p.widget.(widget.Closer); ok && m.widgets[id].widget != p.widget {
			c.Close()
			delete(m.observed, id)
		}
	}
}
