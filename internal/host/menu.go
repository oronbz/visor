package host

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/layout"
	"github.com/oronbz/visor/internal/render"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

// menu is the Action menu of one Widget's selected Item, opened by
// right-click or ".", anchored at the pointer or the Item; or a choice
// Action's menu of choices.
type menu struct {
	widget, title string
	actions       []bound
	sel           int
	x, y          int
	// choosing is the choice Action whose choices the menu lists; loading
	// says they haven't arrived yet, and seq tells them from those of a
	// menu this one replaced.
	choosing bound
	loading  bool
	seq      int
}

// openMenu opens Widget id's Action menu with its top-left corner at x, y,
// titled with the selected Item's name, or the Widget's when there's none.
func (m Model) openMenu(id string, x, y int) Model {
	as := m.actions(id)
	if len(as) == 0 {
		return m
	}
	title := m.widgets[id].title
	if i := slices.IndexFunc(as, func(a bound) bool { return a.item != "" && a.subject != "" }); i >= 0 {
		title = as[i].subject
	}
	m.menu = &menu{widget: id, title: title, actions: as, x: x, y: y}
	return m
}

// selectedAt is where a keyboard-opened menu goes: just inside the selected
// Item's line, or the Widget's top-left inner corner.
func (m Model) selectedAt(id string) (x, y int) {
	s := m.scene()
	idx := m.lists[id].idx
	for _, z := range s.Zones {
		if z.ID == id && z.Kind == layout.Item && z.Index == idx {
			return z.X + 2, z.Y + 1
		}
	}
	for _, z := range s.Zones {
		if z.ID == id && z.Kind == layout.Body {
			return z.X + 1, z.Y
		}
	}
	return 0, 0
}

func (m Model) menuKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	mu := *m.menu
	switch msg.String() {
	case "esc", ".":
		m.menu = nil
		return m, nil
	case "enter":
		if len(mu.actions) == 0 {
			return m, nil
		}
		m.menu = nil
		return m.run(mu.widget, mu.actions[mu.sel])
	case "j", "down", "ctrl+n":
		mu.sel = min(mu.sel+1, len(mu.actions)-1)
	case "k", "up", "ctrl+p":
		mu.sel = max(mu.sel-1, 0)
	}
	m.menu = &mu
	return m, nil
}

func (m Model) menuMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	ms := msg.Mouse()
	box := m.menuBox()
	i := ms.Y - box.Y - 1
	on := ms.X > box.X && ms.X < box.X+box.W-1 && i >= 0 && i < len(m.menu.actions)
	switch msg.(type) {
	case tea.MouseMotionMsg:
		if on {
			mu := *m.menu
			mu.sel = i
			m.menu = &mu
		}
	case tea.MouseClickMsg:
		mu := *m.menu
		m.menu = nil
		if on && ms.Button == tea.MouseLeft {
			return m.run(mu.widget, mu.actions[i])
		}
	}
	return m, nil
}

// menuBox is the menu's outer rectangle: as wide as its widest entry, kept
// on screen above the footer.
func (m Model) menuBox() layout.Rect {
	mu := m.menu
	w := ansi.StringWidth(mu.title) + 6
	for _, a := range mu.actions {
		w = max(w, menuLabelW(mu)+ansi.StringWidth(render.KeyLabel(entryKey(a)))+6)
	}
	w = min(w, m.w)
	h := min(max(len(mu.actions), 1)+2, m.h-footerHeight)
	return layout.Rect{
		X: min(max(mu.x, 0), m.w-w),
		Y: min(max(mu.y, 0), m.h-footerHeight-h),
		W: w, H: h,
	}
}

func menuLabelW(mu *menu) int {
	n := 0
	for _, a := range mu.actions {
		n = max(n, ansi.StringWidth(entryLabel(a)))
	}
	return n
}

func entryLabel(a bound) string {
	if a.choice != nil {
		return a.choice.Label
	}
	return a.Label
}

func entryKey(a bound) string {
	if a.choice != nil {
		return ""
	}
	return shownKey(a)
}

func (m Model) menuLayer() *lipgloss.Layer {
	mu := m.menu
	box := m.menuBox()
	labelW := menuLabelW(mu)
	var lines []widget.Line
	if mu.loading {
		lines = append(lines, widget.Line{Spans: []widget.Span{{Text: " " + m.spinner(), Token: theme.Accent}}})
	}
	for i, a := range mu.actions {
		bg, tok := theme.Bg, theme.Fg
		if i == mu.sel {
			bg, tok = theme.Sel, theme.Bright
		}
		key := render.KeyLabel(entryKey(a))
		label := entryLabel(a)
		gap := strings.Repeat(" ", max(0, box.W-4-labelW-ansi.StringWidth(key)))
		lines = append(lines, widget.Line{Spans: []widget.Span{
			{Text: " " + label + strings.Repeat(" ", labelW-ansi.StringWidth(label)), Token: tok, Bold: i == mu.sel, Bg: bg},
			{Text: gap, Bg: bg},
			{Text: key + " ", Token: theme.Accent, Bold: true, Bg: bg},
		}})
	}
	tile := render.Draw(m.opts.Theme, render.Tile{Title: mu.title, W: box.W, H: box.H, Focused: true, Payload: widget.Payload{Lines: lines}})
	return lipgloss.NewLayer(tile).X(box.X).Y(box.Y).Z(1)
}
