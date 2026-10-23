package host

import (
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/closekey"
	"github.com/oronbz/visor/internal/layout"
)

type keyMap struct {
	Quit, Unfocus, NextRow, PrevRow, Left, Right key.Binding
	MoveLeft, MoveDown, MoveUp, MoveRight        key.Binding
	Arrange, Widgets, Jump, Help, Palette, Menu  key.Binding
	Refresh, Upgrade                             key.Binding
	Down, Up, First, Last, Primary               key.Binding
	Shift, Grow, Keep, Revert                    key.Binding
}

var keys = keyMap{
	Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	Unfocus:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "unfocus")),
	NextRow:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next row")),
	PrevRow:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("S-tab", "previous row")),
	Left:      key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("← h", "focus the Widget to the left")),
	Right:     key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("→ l", "focus the Widget to the right")),
	MoveLeft:  key.NewBinding(key.WithKeys("H", "shift+left"), key.WithHelp("S-← H", "focus left")),
	MoveDown:  key.NewBinding(key.WithKeys("J", "shift+down"), key.WithHelp("S-↓ J", "focus down")),
	MoveUp:    key.NewBinding(key.WithKeys("K", "shift+up"), key.WithHelp("S-↑ K", "focus up")),
	MoveRight: key.NewBinding(key.WithKeys("L", "shift+right"), key.WithHelp("S-→ L", "focus right")),
	Arrange:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "arrange the focused Widget (unfocused: focus the last one)")),
	Widgets:   key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "show or hide Widgets")),
	Jump:      key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1–9", "jump to the Nth Widget")),
	Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "key map")),
	Palette:   key.NewBinding(key.WithKeys("ctrl+k", ":"), key.WithHelp("^K :", "Action palette: every Widget's Actions")),
	Refresh:   key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^R", "refresh every Widget now")),
	// Upgrade isn't a host key: a focused Widget's own U Action wins.
	Upgrade: key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "upgrade visor, while a new release is out")),
	Menu:    key.NewBinding(key.WithKeys("."), key.WithHelp(".", "Action menu of the selected Item (right-click)")),
	Down:    key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("↓ j", "next Item")),
	Up:      key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("↑ k", "previous Item")),
	First:   key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "first Item")),
	Last:    key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "last Item")),
	Primary: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "run the Item's primary Action")),

	Shift:  key.NewBinding(key.WithKeys("h", "j", "k", "l", "left", "down", "up", "right"), key.WithHelp("←↓↑→ hjkl", "arrange: move one place (past an edge opens a row or column)")),
	Grow:   key.NewBinding(key.WithKeys("H", "J", "K", "L", "shift+left", "shift+down", "shift+up", "shift+right"), key.WithHelp("S-←↓↑→ HJKL", "arrange: resize")),
	Keep:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "arrange: keep")),
	Revert: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "arrange: revert")),
}

// Bindings lists every host key binding, in key map order.
func Bindings() []key.Binding {
	return slices.Concat(normalBindings(), []key.Binding{keys.Upgrade}, arrangeBindings())
}

func normalBindings() []key.Binding {
	k := keys
	return []key.Binding{
		k.NextRow, k.PrevRow, k.Left, k.Right, k.MoveLeft, k.MoveDown, k.MoveUp, k.MoveRight,
		k.Jump, k.Arrange, k.Widgets, k.Unfocus, k.Down, k.Up, k.First, k.Last, k.Primary, k.Palette, k.Menu, k.Refresh, k.Help, k.Quit,
	}
}

func arrangeBindings() []key.Binding {
	return []key.Binding{keys.Shift, keys.Grow, keys.Keep, keys.Revert}
}

// hostKey reports whether a NORMAL-mode host binding owns key, which then
// shadows any Action on it.
func hostKey(key string) bool {
	for _, b := range normalBindings() {
		if slices.Contains(b.Keys(), key) {
			return true
		}
	}
	return false
}

func (m Model) key(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if mod, ok := loneModifier(msg.Code); ok {
		if !msg.IsRepeat {
			m.heldModifier = mod
		}
		return m, nil
	}
	// Any other key means the modifier is no longer held alone, and a lost
	// release can't leave the footer stuck.
	m.heldModifier = 0
	var close closekey.Outcome
	switch m.closing, close = m.closing.Press(msg, m.opts.Ports.Clock.Now()); close {
	case closekey.Close:
		if m.editing != nil {
			m, save := m.saveEdit()
			return m, tea.Sequence(save, m.quit())
		}
		return m, m.quit()
	case closekey.Swallow:
		return m, nil
	}
	if m.keyMap {
		if key.Matches(msg, keys.Help, keys.Unfocus, keys.Quit) {
			m.keyMap = false
		}
		return m, nil
	}
	if m.showsPopup() {
		return m.popupKey(msg)
	}
	if m.overlay != nil {
		return m.overlayKey(msg)
	}
	if m.palette != nil {
		return m.paletteKey(msg)
	}
	if m.menu != nil {
		return m.menuKey(msg)
	}
	if m.editing != nil && editsKey(msg) {
		return m.editKey(msg)
	}
	if m.picking {
		return m.pickerKey(msg)
	}
	if m.arranging {
		return m.arrangeKey(msg), nil
	}
	switch {
	case key.Matches(msg, keys.Help):
		m.keyMap = true
	case key.Matches(msg, keys.Palette):
		m.palette = &palette{}
	case key.Matches(msg, keys.Menu):
		if m.focus != "" {
			x, y := m.selectedAt(m.focus)
			m = m.openMenu(m.focus, x, y)
		}
	case key.Matches(msg, keys.Quit):
		return m, m.quit()
	case key.Matches(msg, keys.Refresh):
		return m.startAll(true)
	case key.Matches(msg, keys.Unfocus):
		m.focus = ""
	case key.Matches(msg, keys.Arrange):
		if m.focus == "" {
			m.focus = m.lastFocus()
		} else {
			m.arranging, m.before, m.via = true, m.layout, layout.Via{}
		}
	case key.Matches(msg, keys.Widgets):
		m.picking, m.cursor = true, 0
	case key.Matches(msg, keys.Down):
		m.moveSelection(func(st listState, _ int) int { return st.idx + 1 })
	case key.Matches(msg, keys.Up):
		m.moveSelection(func(st listState, _ int) int { return st.idx - 1 })
	case key.Matches(msg, keys.First):
		m.moveSelection(func(listState, int) int { return 0 })
	case key.Matches(msg, keys.Last):
		m.moveSelection(func(_ listState, n int) int { return n - 1 })
	case key.Matches(msg, keys.Primary):
		if a, ok := m.actionFor("enter"); ok {
			return m.run(m.focus, a)
		}
	case key.Matches(msg, keys.Jump):
		if id := m.nth(int(msg.Code - '1')); id != "" {
			m.focus = id
		}
	case key.Matches(msg, keys.NextRow, keys.PrevRow, keys.Left, keys.Right, keys.MoveLeft, keys.MoveDown, keys.MoveUp, keys.MoveRight):
		if m.focus == "" {
			m.focus = m.lastFocus()
			break
		}
		f := m.laidOut()
		switch {
		case key.Matches(msg, keys.Left, keys.MoveLeft):
			m.focus = f.Toward(m.focus, layout.Left)
		case key.Matches(msg, keys.Right, keys.MoveRight):
			m.focus = f.Toward(m.focus, layout.Right)
		case key.Matches(msg, keys.MoveDown):
			m.focus = f.Toward(m.focus, layout.Down)
		case key.Matches(msg, keys.MoveUp):
			m.focus = f.Toward(m.focus, layout.Up)
		case key.Matches(msg, keys.NextRow):
			m.focus = f.NextRow(m.focus, 1)
		case key.Matches(msg, keys.PrevRow):
			m.focus = f.NextRow(m.focus, -1)
		}
	default:
		if a, ok := m.actionFor(msg.String()); ok && m.focus != "" {
			return m.run(m.focus, a)
		}
		if key.Matches(msg, keys.Upgrade) {
			return m.askUpgrade()
		}
	}
	if m.focus != "" {
		m.last = m.focus
	}
	return m, nil
}

// modal reports whether a view other than the dashboard has the input.
func (m Model) modal() bool {
	return m.keyMap || m.overlay != nil || m.palette != nil || m.menu != nil || m.editing != nil || m.picking || m.arranging
}

// laidOut lays every visible Widget out. While the terminal is too small to
// draw the grid, it lays out at the minimum size so focus still moves
// sensibly.
func (m Model) laidOut() layout.Frame {
	f := m.frame()
	if f.TooSmall {
		f = layout.Compute(m.layout, m.titles(), f.Need.W, f.Need.H, layout.Pointer{})
	}
	return f
}

func (m Model) rects() map[string]layout.Rect {
	f := m.laidOut()
	out := make(map[string]layout.Rect, len(f.Tiles))
	for _, t := range f.Tiles {
		out[t.ID] = t.Rect
	}
	return out
}

// nth is the id of the Nth Widget in reading order from 0, or "".
func (m Model) nth(n int) string {
	if ts := m.laidOut().Tiles; n < len(ts) {
		return ts[n].ID
	}
	return ""
}

// lastFocus is the Widget focus returns to: the last one focused, or the
// first on a fresh start or once that one has left the Layout.
func (m Model) lastFocus() string {
	if _, ok := m.rects()[m.last]; ok {
		return m.last
	}
	return m.nth(0)
}
