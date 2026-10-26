package host_test

import (
	"strings"
	"testing"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/theme"
)

func TestFooterShowsTheNormalBadgeAndGlobalHintsBelowTheGrid(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 100, Height: 21})

	if got := h.Line(19); !strings.HasPrefix(got, "╰") {
		t.Errorf("the grid's last line is %q, want bottom borders right above the footer", got)
	}
	footer := h.Line(20)
	if !strings.HasPrefix(footer, " NORMAL ") {
		t.Fatalf("footer %q does not start with the NORMAL badge", footer)
	}
	for x := range len(" NORMAL ") {
		if bg, ok := h.Bg(x, 20); !ok || bg != theme.Accent {
			t.Errorf("badge cell %d background %v (theme colour %v), want accent", x, bg, ok)
		}
	}
	if fg, ok := h.Fg(1, 20); !ok || fg != theme.Bg || !h.Bold(1, 20) {
		t.Errorf("badge text drawn in %v (theme colour %v, bold %v), want bold bg", fg, ok, h.Bold(1, 20))
	}
	for _, hint := range []string{"Tab", "1–9", "? keys", "q quit"} {
		if !strings.Contains(footer, hint) {
			t.Errorf("unfocused footer %q lacks the hint %q", footer, hint)
		}
	}

	h.Press("tab")
	if footer := h.Line(20); !strings.Contains(footer, "esc unfocus") {
		t.Errorf("focused footer %q lacks the esc hint", footer)
	}
}

func TestHoldingShiftAloneSwapsTheFooterToShiftKeys(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 140, Height: 21})

	h.Press("leftshift")
	footer := h.Line(20)
	for _, want := range []string{" NORMAL ", "Shift", "focus left", "focus down", "previous row"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer while Shift is held %q lacks %q", footer, want)
		}
	}
	if strings.Contains(footer, "q quit") {
		t.Errorf("footer while Shift is held %q still shows the unmodified hints", footer)
	}

	h.Release("leftshift")
	if footer := h.Line(20); !strings.Contains(footer, "q quit") {
		t.Errorf("footer after releasing Shift %q, want the global hints back", footer)
	}
}

func TestHoldingCtrlAloneSwapsTheFooterToCtrlKeys(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 140, Height: 21})
	h.Press("tab")

	h.Press("rightctrl")
	footer := h.Line(20)
	if !strings.Contains(footer, "Ctrl:") || strings.Contains(footer, "esc unfocus") {
		t.Errorf("footer while Ctrl is held %q, want the Ctrl keys in place of the focused hints", footer)
	}

	h.Release("rightctrl")
	if footer := h.Line(20); !strings.Contains(footer, "esc unfocus") {
		t.Errorf("footer after releasing Ctrl %q, want the focused hints back", footer)
	}
}

func TestModifiedKeysActNormallyAndLeaveTheFooterUnmodified(t *testing.T) {
	h := phonetic(t, `{"default":{"rows":[
		{"weight":1,"widgets":[{"id":"a","weight":1}]},
		{"weight":1,"widgets":[{"id":"b","weight":1}]}]}}`)
	footer := func() string { return h.Line(30) }
	h.Press("2")

	h.Press("leftshift", "K")
	if got := focused(t, h, phoneticTitles); got != "Alpha" {
		t.Errorf("shift+K focuses %q, want Alpha above", got)
	}
	if !strings.Contains(footer(), "Tab rows") {
		t.Errorf("footer after shift+K %q, want the focused hints", footer())
	}
	h.Release("K", "leftshift")

	h.Press("leftctrl", "ctrl+j")
	if got := focused(t, h, phoneticTitles); got != "Alpha" {
		t.Errorf("ctrl+j moved focus to %q", got)
	}
	h.Release("ctrl+j")
	if !strings.Contains(footer(), "Tab rows") {
		t.Errorf("footer after ctrl+j %q, want the focused hints", footer())
	}
}

func TestTheNormalFooterLeadsWithThePaletteKeyAtEveryWidth(t *testing.T) {
	for _, w := range []int{140, 80, 64} {
		h := hosttest.New(t, hosttest.Options{Width: w, Height: 21})

		if footer := h.Line(20); !strings.HasPrefix(footer, " NORMAL  ^K actions · Tab/←→ focus") {
			t.Errorf("at %d columns the unfocused footer is %q, want ^K actions first", w, footer)
		}
		h.Press("tab")
		if footer := h.Line(20); !strings.HasPrefix(footer, " NORMAL  ^K actions · Tab rows") {
			t.Errorf("at %d columns the focused footer is %q, want ^K actions first", w, footer)
		}
	}
}
