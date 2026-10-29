package host

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/theme"
)

// queryWait is how long visor waits for the terminal to answer its colour
// and graphics queries before settling on what it has.
const queryWait = 500 * time.Millisecond

// colors tracks the theme against the terminal's colour answers.
type colors struct {
	// pinned themes come from Options.Theme and ignore the terminal.
	pinned bool
	name   string
	over   theme.Overrides
	term   theme.Terminal
	// asking is true until the terminal has answered or queryWait passed;
	// visor leaves the terminal background alone meanwhile so the query
	// doesn't read back visor's own colour.
	asking bool
}

type queryWaitMsg struct{}

// Theme is the palette in use.
func (m Model) Theme() theme.Theme { return m.opts.Theme }

// queryTerminal asks whether the terminal draws kitty graphics and how big
// its cells are, then what queryColors asks.
//
// A terminal without kitty graphics, such as Terminal.app, may print the
// graphics query's text. Asked from inside the alternate screen, that text
// is gone once the first frame clears it, instead of left on the shell's
// screen at quit. Leaving the alternate screen after it would also leave
// one visor had already entered, and draw the dashboard over the shell, so
// View enters it and draws only once Update has seen the query go out:
// Bubble Tea writes a RawMsg before it updates the model with it.
func (m Model) queryTerminal() tea.Cmd {
	graphics := ansi.SetModeAltScreenSaveCursor + graphicsQuery + ansi.ResetModeAltScreenSaveCursor
	return m.query(append([]tea.Cmd{tea.Raw(graphics + cellSizeQueries)}, m.colorQueries()...))
}

// queryColors asks for the terminal's colours, unless the theme is pinned.
func (m Model) queryColors() tea.Cmd {
	queries := m.colorQueries()
	if queries == nil {
		return nil
	}
	return m.query(queries)
}

// query sends queries, then asks for device attributes, whose answer comes
// after every other answer the terminal will give.
func (m Model) query(queries []tea.Cmd) tea.Cmd {
	return tea.Batch(
		tea.Sequence(append(queries, tea.Raw(ansi.RequestPrimaryDeviceAttributes))...),
		m.opts.Ports.Clock.After(queryWait, func(time.Time) tea.Msg { return queryWaitMsg{} }),
	)
}

func (m Model) colorQueries() []tea.Cmd {
	if m.colors.pinned {
		return nil
	}
	queries := []tea.Cmd{tea.RequestBackgroundColor}
	if m.colors.name == theme.TerminalTheme {
		var osc4 strings.Builder
		for n := 1; n <= 6; n++ {
			fmt.Fprintf(&osc4, "\x1b]4;%d;?\x07", n)
		}
		queries = append(queries, tea.RequestForegroundColor, tea.Raw(osc4.String()))
	}
	return queries
}

// background is the colour visor sets as the terminal background, nil while
// it leaves the terminal's own alone.
func (m Model) background() color.Color {
	if m.colors.asking || m.opts.Theme.FromTerminal() {
		return nil
	}
	return m.opts.Theme.Color(theme.Bg)
}

// colorAnswer handles a reply to queryColors; ok is false for other messages.
// When the answer changes the terminal background it clears the screen:
// herdr applies a new default background only to cells drawn after it, so
// without a full redraw the old colour lingers until each cell repaints.
func (m Model) colorAnswer(msg tea.Msg) (Model, tea.Cmd, bool) {
	before := m.background()
	next, ok := m.applyColorAnswer(msg)
	if !ok {
		return m, nil, false
	}
	if sameColor(before, next.background()) {
		return next, nil, true
	}
	return next, tea.ClearScreen, true
}

func (m Model) applyColorAnswer(msg tea.Msg) (Model, bool) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.colors.term.Bg = msg.Color
	case tea.ForegroundColorMsg:
		m.colors.term.Fg = msg.Color
	case uv.UnknownOscEvent:
		if !paletteAnswer(string(msg), &m.colors.term) {
			return m, false
		}
	case uv.PrimaryDeviceAttributesEvent, queryWaitMsg:
		m.colors.asking = false
		return m, true
	default:
		return m, false
	}
	return m.resolveTheme(), true
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func (m Model) resolveTheme() Model {
	if !m.colors.pinned {
		m.opts.Theme = theme.Resolve(m.colors.name, m.colors.term, m.colors.over)
	}
	return m
}

// paletteAnswer reads an OSC 4 answer ("ESC ]4;1;rgb:cccc/0000/0000 ST"),
// which may carry several number;colour pairs, into term.
func paletteAnswer(s string, term *theme.Terminal) bool {
	body, ok := strings.CutPrefix(s, "\x1b]4;")
	if !ok {
		return false
	}
	body = strings.TrimSuffix(strings.TrimSuffix(body, "\x07"), "\x1b\\")
	parts := strings.Split(body, ";")
	for i := 0; i+1 < len(parts); i += 2 {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 || n >= len(term.ANSI) {
			continue
		}
		if c := ansi.XParseColor(parts[i+1]); c != nil {
			term.ANSI[n] = c
		}
	}
	return true
}
