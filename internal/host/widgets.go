package host

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/config"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

// visorTitle is the palette title of visor's own Actions, which belong to no Widget.
const visorTitle = "visor"

// configEditedMsg reports a host Action's config.toml edit, made off the UI thread.
type configEditedMsg struct {
	id     string
	action bound
	added  config.Widget
	// line is the block an edit settings Action opens the editor at.
	line int
	// toast is the Action's own, shown once its settings are saved.
	toast string
	err   error
}

func (m Model) addEntries() []entry {
	var es []entry
	for _, t := range m.configEditor.Types {
		a := bound{Action: widget.Action{ID: "add", Label: "add " + t.Title + " Widget"}, by: addWidget, typ: t.Name}
		es = append(es, entry{title: visorTitle, action: a})
	}
	return es
}

func (m Model) editConfig(id string, a bound) tea.Cmd {
	edits, ref := m.configEditor, m.widgets[id].ref
	return func() tea.Msg {
		msg := configEditedMsg{id: id, action: a}
		switch a.by {
		case addWidget:
			msg.added, msg.err = edits.AddWidget(a.typ)
		case removeWidget:
			msg.err = edits.RemoveWidget(ref)
		case editSettings:
			msg.line, msg.err = edits.Locate(ref)
		}
		return msg
	}
}

func (m Model) saveSettings(id string, a widget.Action, settings widget.Settings, toast string) tea.Cmd {
	edits, p := m.configEditor, m.widgets[id]
	return func() tea.Msg {
		_, err := edits.SetSettings(p.typ.Name, p.ref, settings)
		return configEditedMsg{id: id, action: bound{Action: a, by: saveSettings}, toast: toast, err: err}
	}
}

func (m Model) configEdited(msg configEditedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.say(capitalise(msg.action.Label)+" failed: "+msg.err.Error(), theme.Bad)
	}
	switch msg.action.by {
	case saveSettings:
		return m.settingsSaved(msg.id, msg.toast)
	case editSettings:
		return m, m.opts.Ports.Editor.Edit(m.configEditor.Path, msg.line, func(err error) tea.Msg { return editedMsg{id: msg.id, err: err} })
	case removeWidget:
		return m.removed(msg.id)
	}
	return m.added(msg.added)
}

// settingsSaved rebuilds the Widgets whose blocks changed and refreshes
// Widget id, whose Action may have changed more than its settings, such as
// a token in the keyring.
func (m Model) settingsSaved(id, toast string) (Model, tea.Cmd) {
	m, fresh, reload := m.reloadConfig()
	cmds := []tea.Cmd{reload}
	for _, other := range fresh {
		if other != id {
			var cmd tea.Cmd
			m, cmd = m.whenDue(other)
			cmds = append(cmds, cmd)
		}
	}
	m, cmd := m.after(id, toast)
	m, beat := m.beat()
	return m, tea.Batch(append(cmds, cmd, beat)...)
}

func (m Model) added(wc config.Widget) (Model, tea.Cmd) {
	ticking := slices.ContainsFunc(m.ids(), func(id string) bool {
		_, ok := m.widgets[id].widget.(widget.Ticker)
		return ok
	})
	p := m.place(wc, config.Ref{ID: wc.ID})
	m.widgets[wc.ID] = p
	m.schedule(wc.ID, wc.Refresh)
	m.layout = m.layout.Join(m.ids())
	m, toast := m.say("Added "+p.title, theme.Fg)
	m, fetch := m.whenDue(wc.ID)
	m, beat := m.beat()
	cmds := []tea.Cmd{toast, fetch, beat, m.listen([]string{wc.ID})}
	// One tick chain serves every live view; start it only if none is running.
	if !ticking {
		cmds = append(cmds, m.nextTick())
	}
	return m, tea.Batch(cmds...)
}

func (m Model) removed(id string) (Model, tea.Cmd) {
	gone := m.widgets[id]
	delete(m.widgets, id)
	m.closeDropped(map[string]placed{id: gone})
	delete(m.fetched, id)
	delete(m.lists, id)
	delete(m.sched, id)
	for other, p := range m.widgets {
		if p.ref.ID == gone.ref.ID && p.ref.N > gone.ref.N {
			p.ref.N--
			m.widgets[other] = p
		}
	}
	if m.focus == id {
		m.focus = ""
	}
	if m.last == id {
		m.last = ""
	}
	m.layout = m.layout.Join(m.ids())
	return m.say("Removed "+gone.title, theme.Fg)
}
