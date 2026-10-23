package host

import (
	"path/filepath"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/layout"
	"github.com/oronbz/visor/internal/theme"
)

type filesChangedMsg struct{ files []string }

// watch points the file watcher at every file visor reads: config.toml,
// layout.json, the plugins directory, the Upgrade state and each Watcher
// Widget's files.
func (m Model) watch() Model {
	files := []string{m.configEditor.Path, m.layoutFile(), m.pluginsDir(), m.upgradeStore().File}
	for _, id := range m.ids() {
		if key := m.sched[id].key; key != "" {
			files = append(files, m.cache.File(key))
		}
		for _, f := range watched(m.widgets[id].widget) {
			if !slices.Contains(files, f) {
				files = append(files, f)
			}
		}
	}
	if err := m.opts.Ports.Watcher.Watch(files); err != nil {
		m.toast = toast{text: "Watching files: " + err.Error(), tok: theme.Bad}
	}
	return m
}

func (m Model) nextChange() tea.Cmd {
	return m.opts.Ports.Watcher.Next(func(files []string) tea.Msg { return filesChangedMsg{files} })
}

func (m Model) filesChanged(msg filesChangedMsg) (Model, tea.Cmd) {
	cmds := []tea.Cmd{m.nextChange()}
	var fresh, restarted []string
	// Installing, updating or uninstalling a Plugin changes which Widget types exist.
	if slices.Contains(msg.files, m.configEditor.Path) || slices.Contains(msg.files, m.pluginsDir()) {
		var cmd tea.Cmd
		m, fresh, cmd = m.reloadConfig()
		cmds = append(cmds, cmd)
		restarted = m.plugins.Restarted()
	}
	if slices.Contains(msg.files, m.layoutFile()) {
		m = m.reloadLayout()
	}
	if slices.Contains(msg.files, m.upgradeStore().File) {
		m = m.adoptUpgrade(m.upgradeStore().Load())
	}
	for _, id := range m.ids() {
		var cmd tea.Cmd
		switch {
		case slices.ContainsFunc(watched(m.widgets[id].widget), func(f string) bool { return slices.Contains(msg.files, f) }):
			m, cmd = m.start(id)
		case slices.Contains(fresh, id):
			m, cmd = m.whenDue(id)
		case slices.Contains(restarted, m.widgets[id].typ.Name) && !m.layout.IsHidden(id):
			m, cmd = m.start(id)
		}
		cmds = append(cmds, cmd)
	}
	m, adopted := m.adoptChanged(msg.files)
	cmds = append(cmds, adopted...)
	m, beat := m.beat()
	return m, tea.Batch(append(cmds, beat)...)
}

// reloadConfig applies config.toml as it now is, returning the Widgets it
// built afresh, which need a refresh. A config that doesn't parse leaves
// everything as it was and says why until it parses again.
func (m Model) reloadConfig() (Model, []string, tea.Cmd) {
	cfg, err, types, problems := m.loadConfig(m.configEditor.Path, m.baseTypes())
	if err != nil {
		m.configProblem = err.Error()
		return m, nil, nil
	}
	m.configEditor.Types = types
	var cmds []tea.Cmd
	if cfg.Theme != m.colors.name && !m.colors.pinned {
		m.colors.asking = true
		cmds = append(cmds, m.queryColors())
	}
	m.colors.name, m.colors.over = cfg.Theme, cfg.Colors
	m = m.resolveTheme()

	checked := m.upgrades.on
	m, ids, fresh, clamped := m.placeAll(cfg)
	if m.upgrades.on && !checked {
		m.upgrades.chain++
		cmds = append(cmds, m.upgradeDue())
	}
	m.configProblem = toastOf(slices.Concat(cfg.Problems, problems, clamped))
	m.layout = m.layout.Join(ids)
	if m.arranging {
		m.before = m.before.Join(ids)
	}
	for id := range m.fetched {
		if _, ok := m.widgets[id]; !ok {
			delete(m.fetched, id)
			delete(m.lists, id)
		}
	}
	if _, ok := m.rects()[m.focus]; !ok {
		m.focus, m.arranging = "", false
	}
	if _, ok := m.widgets[m.last]; !ok {
		m.last = ""
	}
	if m.editing != nil && !slices.Contains(ids, m.editing.widget) {
		m.editing = nil
	}
	if m.overlay != nil && !slices.Contains(ids, m.overlay.widget) {
		m.overlay = nil
	}
	if m.menu != nil && !slices.Contains(ids, m.menu.widget) {
		m.menu = nil
	}
	for _, id := range m.ids() {
		m.clampList(id)
	}
	m.sizeEditor()
	m = m.watch()
	m.ticks++
	return m, fresh, tea.Batch(append(cmds, m.nextTick(), m.listen(fresh))...)
}

func (m Model) layoutFile() string { return filepath.Join(m.opts.Paths.Config, layout.FileName) }

func (m Model) pluginsDir() string { return filepath.Join(m.opts.Paths.Data, "plugins") }

// reloadLayout adopts the Layout another instance saved: the last writer
// wins. Mid-arrangement it becomes what esc reverts to; a Layout that
// doesn't load leaves the current one.
func (m Model) reloadLayout() Model {
	saved, err := layout.Load(m.opts.Paths.Config)
	if err != nil {
		return m
	}
	l := saved.Join(m.ids())
	if m.arranging {
		m.before = l
		return m
	}
	m.layout = l
	if _, ok := m.rects()[m.focus]; !ok {
		m.focus = ""
	}
	for _, id := range m.ids() {
		m.clampList(id)
	}
	m.sizeEditor()
	return m
}
