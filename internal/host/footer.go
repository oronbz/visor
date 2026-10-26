package host

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/theme"
)

const footerHeight = 1

// paletteHint leads the NORMAL hints, as text and as a click target.
const paletteHint = "^K actions"

func (m Model) footer() string {
	th := m.opts.Theme
	mode, modeColor := " NORMAL ", theme.Accent
	bindings := normalBindings()
	hints := paletteHint + " · Tab/←→ focus · 1–9 jump · w widgets · ? keys · q quit"
	hintTok := theme.Dim
	if m.focus != "" {
		hints = paletteHint + " · Tab rows · ←→ widgets · Shift + ←↓↑→ any direction · m arrange · esc unfocus · ? keys"
		if e := m.staleError(m.focus); e != "" {
			hints, hintTok = e, theme.Warn
		}
	}
	if m.arranging {
		mode, modeColor = " ARRANGE ", theme.Warn
		bindings = arrangeBindings()
		hints = "←↓↑→ move · Shift + ←↓↑→ resize · ⏎ keep · esc revert"
	}
	if m.picking {
		hints = "↑↓ move · ␣ show/hide · esc close"
	}
	if m.editing != nil {
		mode, modeColor = " INSERT ", theme.Ok
		hints = "esc save"
	}
	if m.palette != nil {
		hints = "type to filter · ↑↓ choose · ⏎ run · esc close"
	}
	if m.menu != nil {
		hints = "↑↓ choose · ⏎ run · esc close"
		if m.menu.loading {
			hints = "loading… · esc close"
		}
	}
	if o := m.overlay; o != nil {
		mode, modeColor = " INPUT ", theme.Ok
		if o.kind == confirming {
			mode, modeColor = " CONFIRM ", theme.Warn
		}
		hints = "⏎ " + o.action.Label + " · esc cancel"
	}
	badge := th.Fg(theme.Bg).Background(th.Color(modeColor)).Bold(true).Render(mode)
	switch m.heldModifier {
	case tea.ModShift:
		hints = modifierHints("Shift", bindings, isShifted)
	case tea.ModCtrl:
		hints = modifierHints("Ctrl", bindings, func(k string) bool { return strings.HasPrefix(k, "ctrl+") })
	}
	toast := ""
	switch {
	case m.toast.text != "":
		toast = th.Fg(m.toast.tok).Render(m.toast.text) + "  "
	case m.configProblem != "":
		toast = th.Fg(theme.Warn).Render(m.configProblem) + "  "
	}
	line := badge + " " + toast + th.Fg(hintTok).Render(hints)
	pill := m.pill()
	pw := ansi.StringWidth(pill)
	if pill == "" || m.w < pw+len(mode)+2 {
		return ansi.Truncate(line, m.w, "…")
	}
	line = ansi.Truncate(line, m.w-pw-1, "…")
	return line + strings.Repeat(" ", m.w-ansi.StringWidth(line)-pw) + pill
}

// onPaletteHint reports whether cell x, y is the footer's whole ^K actions
// hint, which is drawn only in NORMAL mode and not when cut by the width.
func (m Model) onPaletteHint(x, y int) bool {
	if y != m.h-footerHeight {
		return false
	}
	plain := ansi.Strip(m.footer())
	i := strings.Index(plain, paletteHint+" · ")
	if i < 0 {
		return false
	}
	from := ansi.StringWidth(plain[:i])
	return x >= from && x < from+ansi.StringWidth(paletteHint)
}

func modifierHints(name string, bindings []key.Binding, uses func(key string) bool) string {
	var hints []string
	for _, b := range bindings {
		for _, k := range b.Keys() {
			if uses(k) {
				hints = append(hints, b.Help().Key+" "+b.Help().Desc)
				break
			}
		}
	}
	if hints == nil {
		return name + ": no keys"
	}
	return name + ": " + strings.Join(hints, " · ")
}

func isShifted(k string) bool {
	r, size := utf8.DecodeRuneInString(k)
	return strings.Contains(k, "shift+") || size == len(k) && unicode.IsUpper(r)
}

// loneModifier reports whether code is a modifier key pressed on its own,
// and the Shift or Ctrl it swaps the footer to (0 for the others).
func loneModifier(code rune) (tea.KeyMod, bool) {
	switch code {
	case tea.KeyLeftShift, tea.KeyRightShift:
		return tea.ModShift, true
	case tea.KeyLeftCtrl, tea.KeyRightCtrl:
		return tea.ModCtrl, true
	case tea.KeyLeftAlt, tea.KeyRightAlt, tea.KeyLeftSuper, tea.KeyRightSuper,
		tea.KeyLeftHyper, tea.KeyRightHyper, tea.KeyLeftMeta, tea.KeyRightMeta,
		tea.KeyIsoLevel3Shift, tea.KeyIsoLevel5Shift:
		return 0, true
	}
	return 0, false
}
