package host

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/oronbz/visor/internal/render"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

// palette is the ^K fuzzy list of every visible Widget's Actions.
type palette struct {
	query    []rune
	sel, top int
}

type entry struct {
	widget, title string
	action        bound
}

const paletteRows = 12

// entries are the Actions matching the query, best first; with no query,
// every Action in Layout order.
func (m Model) entries() []entry {
	var all []entry
	for _, t := range m.laidOut().Tiles {
		for _, a := range m.actions(t.ID) {
			all = append(all, entry{widget: t.ID, title: m.widgets[t.ID].title, action: a})
		}
	}
	all = append(all, m.addEntries()...)
	all = append(all, entry{title: dashboard, action: bound{Action: widget.Action{ID: "install-skills", Label: "install agent skills"}, by: installSkills}})
	if a, ok := m.upgradeAction(); ok {
		all = append(all, entry{title: dashboard, action: a})
	}
	if a, ok := m.checkAction(); ok {
		all = append(all, entry{title: dashboard, action: a})
	}
	q := strings.TrimSpace(string(m.palette.query))
	if q == "" {
		return all
	}
	names := make([]string, len(all))
	for i, e := range all {
		names[i] = e.title + " " + e.action.Label
	}
	var out []entry
	for _, match := range fuzzy.Find(q, names) {
		out = append(out, all[match.Index])
	}
	return out
}

func (m Model) paletteKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	p := *m.palette
	es := m.entries()
	switch k := msg.String(); k {
	case "esc", "ctrl+k":
		m.palette = nil
		return m, nil
	case "enter":
		m.palette = nil
		if p.sel < len(es) {
			return m.run(es[p.sel].widget, es[p.sel].action)
		}
		return m, nil
	case "down", "ctrl+n":
		p.sel = min(p.sel+1, max(0, len(es)-1))
	case "up", "ctrl+p":
		p.sel = max(p.sel-1, 0)
	case "backspace":
		if len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			p.sel, p.top = 0, 0
		}
	case "ctrl+u":
		p.query, p.sel, p.top = nil, 0, 0
	default:
		if msg.Text != "" {
			p.query = append(append([]rune{}, p.query...), []rune(msg.Text)...)
			p.sel, p.top = 0, 0
		}
	}
	p.top = min(p.top, p.sel)
	if p.sel >= p.top+paletteRows {
		p.top = p.sel - paletteRows + 1
	}
	m.palette = &p
	return m, nil
}

func (m Model) paletteLayer() *lipgloss.Layer {
	p := m.palette
	es := m.entries()
	w := min(76, m.w-6, m.w)
	inner := w - 4
	titleW := 0
	for _, e := range es {
		titleW = max(titleW, ansi.StringWidth(e.title))
	}
	lines := []widget.Line{queryLine(p.query, w-6)}
	if len(es) == 0 {
		lines = append(lines, widget.Text(theme.Faint, " no matching Actions"))
	}
	for i := p.top; i < min(len(es), p.top+paletteRows); i++ {
		lines = append(lines, entryLine(es[i], titleW, inner, i == p.sel))
	}
	h := min(m.h, len(lines)+2)
	box := render.Draw(m.opts.Theme, render.Tile{
		Title: "Actions", W: w, H: h, Focused: true,
		Payload: widget.Payload{Lines: lines},
		Hints:   []render.Hint{{Key: "enter", Label: "run"}, {Key: "esc", Label: "close"}},
	})
	return lipgloss.NewLayer(box).X((m.w - w) / 2).Y(max(0, (m.h-h)/3)).Z(1)
}

func queryLine(q []rune, w int) widget.Line {
	if over := len(q) - (w - 1); over > 0 {
		q = q[over:]
	}
	return widget.Line{Spans: []widget.Span{
		{Text: " › ", Token: theme.Accent},
		{Text: string(q), Token: theme.Bright},
		{Text: " ", Token: theme.Bg, Bg: theme.Accent},
	}}
}

func entryLine(e entry, titleW, w int, selected bool) widget.Line {
	bg, label := theme.Bg, widget.Span{Text: e.action.Label, Token: theme.Fg}
	if selected {
		bg, label = theme.Sel, widget.Span{Text: e.action.Label, Token: theme.Bright, Bold: true}
	}
	key := render.KeyLabel(shownKey(e.action))
	title := e.title + strings.Repeat(" ", titleW-ansi.StringWidth(e.title)) + "  "
	room := max(0, w-ansi.StringWidth(title)-ansi.StringWidth(key)-1)
	label.Text = ansi.Truncate(label.Text, room, "…")
	label.Bg = bg
	gap := strings.Repeat(" ", room-ansi.StringWidth(label.Text)+1)
	return widget.Line{Spans: []widget.Span{
		{Text: " ", Bg: bg},
		{Text: title, Token: theme.Dim, Bg: bg},
		label,
		{Text: gap, Bg: bg},
		{Text: key, Token: theme.Accent, Bold: true, Bg: bg},
		{Text: " ", Bg: bg},
	}}
}
