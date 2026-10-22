package host

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/config"
	"github.com/oronbz/visor/internal/layout"
	"github.com/oronbz/visor/internal/render"
	"github.com/oronbz/visor/internal/shell"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

const toastLife = 5 * time.Second

type toast struct {
	text string
	tok  theme.Token
	// seq tells an expiring toast from the one that replaced it; zero never expires.
	seq int
}

type toastExpiredMsg struct{ seq int }

func (m Model) say(text string, tok theme.Token) (Model, tea.Cmd) {
	seq := m.toast.seq + 1
	m.toast = toast{text: text, tok: tok, seq: seq}
	return m, m.opts.Ports.Clock.After(toastLife, func(time.Time) tea.Msg { return toastExpiredMsg{seq} })
}

// listState is a list Widget's selection, kept by Item id so it survives a
// refresh, and its scroll offset, clamped here rather than when drawing.
type listState struct {
	sel      string
	idx, top int
}

func (s listState) render() render.List { return render.List{Selected: s.idx, Top: s.top} }

type editedMsg struct {
	id, file string
	err      error
	// then is the Action to run now, on the Item the edit began on.
	then *widget.Action
	item string
}

type actedMsg struct {
	id, item string
	action   widget.Action
	result   widget.Result
	err      error
	// byHost marks an Action the host performed, which leaves the Widget as it was.
	byHost bool
}

// clampList keeps the selection on an Item and in view, its group header
// too when there's room, and the scroll offset inside the list.
func (m Model) clampList(id string) {
	r, ok := m.rects()[id]
	if !ok {
		return
	}
	p := m.payloadIn(id, r)
	st := m.lists[id]
	if p.Wrap && !p.IsList() {
		rows, h := m.textRows(id, p, r)
		m.lists[id] = listState{top: min(max(st.top, 0), max(0, rows-max(1, h)))}
		return
	}
	if len(p.Items) == 0 {
		m.lists[id] = listState{}
		return
	}
	h := itemRows(p, r)
	st.idx = min(max(st.idx, 0), len(p.Items)-1)
	st.sel = p.Items[st.idx].ID
	rows := render.Rows(p)
	line := slices.Index(rows, st.idx)
	want := line
	if line > 0 && rows[line-1] == render.Header && h > 1 {
		want = line - 1
	}
	if want < st.top {
		st.top = want
	}
	if line >= st.top+h {
		st.top = line - h + 1
	}
	st.top = min(max(st.top, 0), max(0, len(rows)-h))
	m.lists[id] = st
}

// itemRows is how many rows of list Widget p, in r, show Items: the body
// below its Summary.
func itemRows(p widget.Payload, r layout.Rect) int {
	body := r.H - 2
	return max(1, body-render.SummaryRows(p, body))
}

// textRows is how many lines wrapped Widget id's Payload p takes in r, and
// how many of them show beside or around its Image.
func (m Model) textRows(id string, p widget.Payload, r layout.Rect) (rows, shown int) {
	var pic *render.Image
	if shown, ok := m.images.shown[id]; ok {
		pic = &shown
	}
	return render.TextRows(p, pic, r.W-2, r.H-2)
}

// moveSelection moves the focused list's selection to index i, clamped. A
// wrapped text Widget scrolls instead, i being its top line out of n.
func (m Model) moveSelection(i func(st listState, n int) int) {
	id := m.focus
	r, ok := m.rects()[id]
	if !ok {
		return
	}
	p := m.payloadIn(id, r)
	if p.Wrap && !p.IsList() {
		rows, _ := m.textRows(id, p, r)
		m.lists[id] = listState{top: i(listState{idx: m.lists[id].top}, rows)}
		m.clampList(id)
		return
	}
	if len(p.Items) == 0 {
		return
	}
	st := m.lists[id]
	st.idx = min(max(i(st, len(p.Items)), 0), len(p.Items)-1)
	st.sel = p.Items[st.idx].ID
	m.lists[id] = st
	m.clampList(id)
}

// bound is an Action as it applies now: to the selected Item, or to the Widget.
type bound struct {
	widget.Action
	item string
	// subject names the Item in confirm questions.
	subject string
	// by is none for the Widget's own Actions.
	by hostAction
	// typ is what an addWidget Action places.
	typ    string
	choice *widget.Choice
	// command is what a runCommand Action runs, its template filled.
	command string
}

type hostAction int

const (
	none hostAction = iota
	refreshNow
	hide
	editSettings
	removeWidget
	addWidget
	saveSettings
	installSkills
	runCommand
)

// actions are Widget id's Actions as they apply now: the user's own from
// its block, the selected Item's, the Widget's, then the host-provided ones,
// which always include setting each of its secrets.
func (m Model) actions(id string) []bound {
	r, ok := m.rects()[id]
	if !ok {
		return nil
	}
	host := []bound{
		{Action: widget.Action{ID: "refresh", Label: "refresh"}, by: refreshNow},
		{Action: widget.Action{ID: "hide", Label: "hide"}, by: hide},
		{Action: widget.Action{ID: "edit-settings", Label: "edit settings"}, by: editSettings},
		{Action: widget.Action{ID: "remove", Label: "remove"}, by: removeWidget},
	}
	if m.widgets[id].setup != "" {
		return host[1:]
	}
	var out []bound
	if s := m.sched[id].setup; s != nil {
		if s.Action != nil {
			a := *s.Action
			a.Primary = true
			out = append(out, bound{Action: a})
		}
	} else {
		p := m.payloadIn(id, r)
		var it *widget.Item
		if st := m.lists[id]; st.idx < len(p.Items) {
			it = &p.Items[st.idx]
		}
		mine := custom(m.widgets[id].block.Actions, p, it)
		out = append(out, mine...)
		yieldTo := func(a widget.Action) widget.Action {
			if slices.ContainsFunc(mine, func(c bound) bool { return c.Key != "" && c.Key == a.Key }) {
				a.Key = ""
			}
			if slices.ContainsFunc(mine, func(c bound) bool { return c.Primary }) {
				a.Primary = false
			}
			return a
		}
		if it != nil {
			for _, a := range it.Actions {
				out = append(out, bound{Action: yieldTo(a), item: it.ID, subject: subject(p, *it)})
			}
		}
		for _, a := range p.Actions {
			out = append(out, bound{Action: yieldTo(a)})
		}
	}
	for _, s := range m.widgets[id].typ.Settings {
		set := s.SetAction()
		if s.Kind == widget.Secret && !slices.ContainsFunc(out, func(b bound) bool { return b.ID == set.ID }) {
			out = append(out, bound{Action: set})
		}
	}
	return append(out, host...)
}

// custom are the block's own Actions that apply now: those naming values
// of the selected Item it, filled from them, and those naming none.
func custom(as []config.Action, p widget.Payload, it *widget.Item) []bound {
	var out []bound
	for i, c := range as {
		a := bound{Action: widget.Action{ID: fmt.Sprintf("config:%d", i), Key: c.Key, Label: c.Label, Confirm: c.Confirm, Primary: c.Primary}, by: runCommand}
		t := shell.Template(c.Run)
		if t.PerItem() {
			if it == nil {
				continue
			}
			a.item, a.subject = it.ID, subject(p, *it)
		}
		var ok bool
		if a.command, ok = t.Fill(values(p, it)); ok {
			out = append(out, a)
		}
	}
	return out
}

// values are what a custom Action's template can name of Item it: its id,
// its title, the page it opens, and the values it names itself, which win.
func values(p widget.Payload, it *widget.Item) map[string]string {
	if it == nil {
		return nil
	}
	v := map[string]string{"id": it.ID}
	if t := subject(p, *it); t != "" {
		v["title"] = t
	}
	if i := slices.IndexFunc(it.Actions, func(a widget.Action) bool { return a.Verb == widget.Open && a.Target != "" }); i >= 0 {
		v["url"] = it.Actions[i].Target
	}
	maps.Copy(v, it.Values)
	return v
}

// subject is the text of an Item's first flex Field, the name it goes by.
func subject(p widget.Payload, it widget.Item) string {
	for c, col := range p.Columns {
		if col.Flex && c < len(it.Fields) {
			var b strings.Builder
			for _, s := range it.Fields[c] {
				b.WriteString(s.Text)
			}
			return b.String()
		}
	}
	return ""
}

// hints are the focused Widget's Action keys for its bottom border; Actions
// a host key shadows are left out.
func (m Model) hints(id string) []render.Hint {
	var hs []render.Hint
	for _, a := range m.hinted(id) {
		hs = append(hs, render.Hint{Key: shownKey(a), Label: a.Label})
	}
	return hs
}

// hinted are the Actions behind hints(id), in the same order.
func (m Model) hinted(id string) []bound {
	var out []bound
	for _, a := range m.actions(id) {
		if shownKey(a) != "" {
			out = append(out, a)
		}
	}
	return out
}

// shownKey is the key that runs a while its Widget is focused, or "" when it
// has none or a host key shadows it.
func shownKey(a bound) string {
	k := a.Key
	if k == "" && a.Primary {
		k = "enter"
	}
	if k == "" || hostKey(k) && !(k == "enter" && a.Primary) {
		return ""
	}
	return k
}

// actionFor is the focused Widget's Action bound to key, Item Actions first.
func (m Model) actionFor(key string) (bound, bool) {
	for _, a := range m.actions(m.focus) {
		if a.Key == key || key == "enter" && a.Primary {
			return a, true
		}
	}
	return bound{}, false
}

func (m Model) run(id string, a bound) (Model, tea.Cmd) {
	switch {
	case a.Confirm:
		m.overlay = &overlay{kind: confirming, widget: id, action: a}
	case a.Input == widget.TextInput, a.Input == widget.SecretInput:
		o := &overlay{kind: prompting, widget: id, action: a}
		if a.Input == widget.TextInput {
			o.value, o.cursor = []rune(a.Value), utf8.RuneCountInString(a.Value)
		}
		m.overlay = o
		if a.Target != "" {
			opener, target := m.opts.Ports.Opener, a.Target
			return m, func() tea.Msg {
				return actedMsg{id: id, action: a.Action, err: opener.Open(target), byHost: true}
			}
		}
	case a.Input == widget.ChoiceInput:
		return m.choose(id, a)
	case a.Input == widget.InlineInput:
		return m.startEditing(id, a)
	default:
		return m.invoke(id, a, "")
	}
	return m, nil
}

// invoke performs host-provided Actions itself and hands the rest to the Widget.
func (m Model) invoke(id string, a bound, text string) (Model, tea.Cmd) {
	switch {
	case a.by == refreshNow:
		return m.start(id)
	case a.by == hide:
		return m.setHidden(id, true), nil
	case a.by == editSettings, a.by == removeWidget, a.by == addWidget:
		return m, m.editConfig(id, a)
	case a.by == installSkills:
		return m, m.installSkills()
	case a.by == runCommand:
		return m, func() tea.Msg {
			out, err := shell.Run(context.Background(), a.command)
			toast := shell.LastLine(out)
			if toast == "" {
				toast = a.Label + " done"
			}
			return actedMsg{id: id, item: a.item, action: a.Action, result: widget.Result{Toast: toast}, err: err}
		}
	case a.Verb == widget.Open:
		opener, target := m.opts.Ports.Opener, a.Target
		return m, func() tea.Msg {
			return actedMsg{id: id, action: a.Action, err: opener.Open(target), byHost: true}
		}
	case a.Verb == widget.Copy:
		m, toast := m.say("Copied to the clipboard", theme.Fg)
		return m, tea.Batch(m.opts.Ports.Clipboard.Copy(a.Target), toast)
	}
	actor, ok := m.widgets[id].widget.(widget.Actor)
	if !ok {
		return m, nil
	}
	inv := widget.Invocation{Action: a.ID, Item: a.item, Text: text, Base: a.Value, Now: m.opts.Ports.Clock.Now()}
	if a.choice != nil {
		inv.Choice = a.choice.ID
	}
	secret := a.Input == widget.SecretInput
	return m, func() tea.Msg {
		res, err := actor.Invoke(context.Background(), inv)
		if secret {
			res.Toast, err = widget.Redact(res.Toast, text), redactErr(err, text)
		}
		return actedMsg{id: id, item: a.item, action: a.Action, result: res, err: err}
	}
}

func redactErr(err error, secret string) error {
	if err == nil || secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return errors.New(widget.Redact(err.Error(), secret))
}

// acted toasts an Action's result and, after a success, refreshes its Widget
// and every Widget sharing a file with it, or first hands a file to $EDITOR.
func (m Model) acted(msg actedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.say(capitalise(msg.action.Label)+" failed: "+msg.err.Error(), theme.Bad)
	}
	if msg.byHost {
		return m, nil
	}
	if f := msg.result.Edit; f != "" {
		then, item := msg.result.Then, msg.item
		return m, m.opts.Ports.Editor.Edit(f, 0, func(err error) tea.Msg {
			return editedMsg{id: msg.id, file: f, err: err, then: then, item: item}
		})
	}
	if s := msg.result.Settings; len(s) > 0 {
		return m, m.saveSettings(msg.id, msg.action, s, msg.result.Toast)
	}
	if then := msg.result.Then; then != nil {
		var toast tea.Cmd
		if msg.result.Toast != "" {
			m, toast = m.say(msg.result.Toast, theme.Fg)
		}
		m, next := m.run(msg.id, bound{Action: *then, item: msg.item})
		return m, tea.Batch(toast, next)
	}
	return m.after(msg.id, msg.result.Toast)
}

// edited runs the Action that follows the editor, or refreshes the Widget
// whose file the editor just had, or says why it failed.
func (m Model) edited(msg editedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.say("$EDITOR failed: "+msg.err.Error(), theme.Bad)
	}
	if msg.then != nil {
		return m.invoke(msg.id, bound{Action: *msg.then, item: msg.item}, msg.file)
	}
	return m.after(msg.id, "")
}

// after toasts a change to Widget id and refreshes it and every Widget
// sharing a file with it.
func (m Model) after(id, toast string) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if toast != "" {
		var cmd tea.Cmd
		m, cmd = m.say(toast, theme.Fg)
		cmds = append(cmds, cmd)
	}
	files := watched(m.widgets[id].widget)
	for _, other := range m.ids() {
		if other == id || slices.ContainsFunc(watched(m.widgets[other].widget), func(f string) bool { return slices.Contains(files, f) }) {
			var cmd tea.Cmd
			m, cmd = m.start(other)
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func watched(w widget.Widget) []string {
	if wt, ok := w.(widget.Watcher); ok {
		return wt.Files()
	}
	return nil
}

func capitalise(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}
