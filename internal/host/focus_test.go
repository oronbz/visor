package host_test

import (
	"strings"
	"testing"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

func TestVisorStartsUnfocusedAndTabReturnsToTheLastFocusedWidget(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 100, Height: 21})
	titles := []string{"Clock", "Calendar"}

	if got := focused(t, h, titles); got != "" {
		t.Fatalf("fresh start focuses %q, want nothing", got)
	}
	h.Press("tab")
	if got := focused(t, h, titles); got != "Clock" {
		t.Fatalf("tab on a fresh start focuses %q, want the first Widget", got)
	}
	h.Press("l")
	h.Press("esc")
	if got := focused(t, h, titles); got != "" {
		t.Fatalf("esc leaves %q focused", got)
	}
	h.Press("tab")
	if got := focused(t, h, titles); got != "Calendar" {
		t.Errorf("tab after esc focuses %q, want the last-focused Calendar", got)
	}
}

func TestTabMovesAcrossRowsWrappingToTheHorizontallyNearestWidget(t *testing.T) {
	h := phonetic(t, `{"default": {"rows": [
		{"weight": 1, "widgets": [{"id": "a", "weight": 1}, {"id": "b", "weight": 1}, {"id": "c", "weight": 1}]},
		{"weight": 1, "widgets": [{"id": "d", "weight": 1}, {"id": "e", "weight": 3}]},
		{"weight": 1, "widgets": [{"id": "f", "weight": 1}]}
	]}}`)

	for _, c := range []struct {
		from  string
		press string
		want  string
	}{
		{"1", "tab", "Delta"},
		{"2", "tab", "Echo"},
		{"3", "tab", "Echo"},
		{"4", "shift+tab", "Alpha"},
		{"5", "shift+tab", "Bravo"},
		{"5", "tab", "Foxtrot"},
		{"6", "tab", "Bravo"},
		{"1", "shift+tab", "Foxtrot"},
	} {
		h.Press(c.from, c.press)
		if got := focused(t, h, phoneticTitles); got != c.want {
			t.Errorf("%s from Widget %s focuses %q, want %q", c.press, c.from, got, c.want)
		}
	}
}

func TestTabMovesAcrossTheRowsOfAStackBesideAFullHeightColumn(t *testing.T) {
	h := phonetic(t, `{"default": {"rows": [
		{"weight": 1, "widgets": [
			{"weight": 1, "rows": [
				{"weight": 1, "widgets": [{"id": "a", "weight": 1}]},
				{"weight": 1, "widgets": [{"id": "b", "weight": 1}]}
			]},
			{"weight": 3, "rows": [
				{"weight": 1, "widgets": [{"id": "c", "weight": 1}, {"id": "d", "weight": 3}]},
				{"weight": 1, "widgets": [{"id": "e", "weight": 1}]},
				{"weight": 1, "widgets": [{"id": "f", "weight": 1}]}
			]}
		]}
	]}}`)

	for _, c := range []struct {
		from  string
		press string
		want  string
	}{
		{"1", "tab", "Bravo"},
		{"2", "tab", "Alpha"},
		{"3", "tab", "Echo"},
		{"5", "tab", "Foxtrot"},
		{"6", "tab", "Delta"},
		{"5", "shift+tab", "Delta"},
		{"3", "shift+tab", "Foxtrot"},
		{"1", "shift+tab", "Bravo"},
	} {
		h.Press(c.from, c.press)
		if got := focused(t, h, phoneticTitles); got != c.want {
			t.Errorf("%s from Widget %s focuses %q, want %q", c.press, c.from, got, c.want)
		}
	}
}

func TestHLStopAtRowEndsAndShiftedKeysMoveInAnyDirection(t *testing.T) {
	h := phonetic(t, `{"default": {"rows": [
		{"weight": 1, "widgets": [{"id": "a", "weight": 1}, {"id": "b", "weight": 1}, {"id": "c", "weight": 1}]},
		{"weight": 1, "widgets": [{"id": "d", "weight": 1}, {"id": "e", "weight": 3}]},
		{"weight": 1, "widgets": [{"id": "f", "weight": 1}]}
	]}}`)

	for _, c := range []struct {
		keys []string
		want string
	}{
		{[]string{"1", "h"}, "Alpha"},
		{[]string{"1", "l", "l", "l"}, "Charlie"},
		{[]string{"1", "right", "left"}, "Alpha"},
		{[]string{"4", "l", "l"}, "Echo"},
		{[]string{"6", "l"}, "Foxtrot"},
		{[]string{"3", "J"}, "Echo"},
		{[]string{"3", "J", "J", "J"}, "Foxtrot"},
		{[]string{"6", "K"}, "Echo"},
		{[]string{"1", "K"}, "Alpha"},
		{[]string{"5", "H", "H"}, "Delta"},
		{[]string{"1", "L", "L", "L"}, "Charlie"},
		{[]string{"1", "shift+down", "shift+right", "shift+up"}, "Bravo"},
		{[]string{"1", "shift+right", "shift+left"}, "Alpha"},
	} {
		h.Press(c.keys...)
		if got := focused(t, h, phoneticTitles); got != c.want {
			t.Errorf("%v focuses %q, want %q", c.keys, got, c.want)
		}
	}
}

func TestDigitsJumpToTheNthWidget(t *testing.T) {
	h := phonetic(t, `{"default": {"rows": [
		{"weight": 1, "widgets": [{"id": "a", "weight": 1}, {"id": "b", "weight": 1}]},
		{"weight": 1, "widgets": [{"id": "c", "weight": 1}, {"id": "d", "weight": 1}]},
		{"weight": 1, "widgets": [{"id": "e", "weight": 1}, {"id": "f", "weight": 1}]}
	]}}`)

	h.Press("6")
	if got := focused(t, h, phoneticTitles); got != "Foxtrot" {
		t.Fatalf("6 from unfocused focuses %q, want Foxtrot", got)
	}
	h.Press("3")
	if got := focused(t, h, phoneticTitles); got != "Charlie" {
		t.Errorf("3 focuses %q, want Charlie", got)
	}
	h.Press("7", "9")
	if got := focused(t, h, phoneticTitles); got != "Charlie" {
		t.Errorf("digits past the last Widget moved focus to %q, want it left on Charlie", got)
	}
	h.Press("esc", "9")
	if got := focused(t, h, phoneticTitles); got != "" {
		t.Errorf("9 with six Widgets focused %q, want visor left unfocused", got)
	}
}

func TestHLAndMBringFocusBackWithoutMoving(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 100, Height: 21})
	titles := []string{"Clock", "Calendar"}
	for _, k := range []string{"h", "l", "m"} {
		h.Press("2", "esc", k)
		if got := focused(t, h, titles); got != "Calendar" {
			t.Errorf("%s while unfocused focuses %q, want the last-focused Calendar", k, got)
		}
	}
}

func TestFocusShowsOnlyAsAnAccentBorderAndBoldTitle(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 100, Height: 21, Types: liveViews})
	h.Press("2")

	at := func(text string) (int, int) {
		x, y, ok := h.Find(text)
		if !ok {
			t.Fatalf("%q not on screen:\n%s", text, h.Screen())
		}
		return x, y
	}
	cx, cy := at("╭─ Clock")
	kx, ky := at("╭─ Calendar")
	checks := []struct {
		what string
		x, y int
		fg   theme.Token
		bold bool
	}{
		{"focused corner", kx, ky, theme.Accent, false},
		{"focused title", kx + 3, ky, theme.Bright, true},
		{"focused side", kx, ky + 3, theme.Accent, false},
		{"unfocused corner", cx, cy, theme.Line, false},
		{"unfocused title", cx + 3, cy, theme.Fg, false},
	}
	for _, c := range checks {
		if got, ok := h.Fg(c.x, c.y); !ok || got != c.fg {
			t.Errorf("%s drawn in %v (theme colour %v), want %v", c.what, got, ok, c.fg)
		}
		if got := h.Bold(c.x, c.y); got != c.bold {
			t.Errorf("%s bold %v, want %v", c.what, got, c.bold)
		}
	}
	x, y := at("█▀█")
	if got, ok := h.Fg(x, y); !ok || got != theme.Bright {
		t.Errorf("unfocused clock digits drawn in %v (theme colour %v), want bright as when nothing is focused", got, ok)
	}
	x, y = at("October 2026")
	if got, ok := h.Fg(x, y); !ok || got != theme.Fg {
		t.Errorf("focused calendar month drawn in %v (theme colour %v), want its usual fg", got, ok)
	}
}

var phoneticTitles = []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}

// phonetic is a 96×31 host whose Widget types a–f are titled Alpha–Foxtrot,
// arranged by layoutJSON.
func phonetic(t *testing.T, layoutJSON string) *hosttest.Host {
	t.Helper()
	return phoneticAt(t, savedLayout(t, layoutJSON))
}

// phoneticAt is phonetic over an existing root, so a second host can restart
// on what the first one saved.
func phoneticAt(t *testing.T, root string) *hosttest.Host {
	t.Helper()
	var types []widget.Type
	for _, title := range phoneticTitles {
		types = append(types, widget.Type{
			Name:  strings.ToLower(title[:1]),
			Title: title,
			New:   func(widget.Config) widget.Widget { return blank{} },
		})
	}
	return hosttest.New(t, hosttest.Options{Width: 96, Height: 31, Root: root, Types: types})
}

type blank struct{}

func (blank) Payload(widget.Frame) widget.Payload { return widget.Payload{} }

// focused names the one Widget among titles whose border title is bold, or ""
// when none is. It fails the test if more than one is.
func focused(t *testing.T, h *hosttest.Host, titles []string) string {
	t.Helper()
	got := ""
	for _, title := range titles {
		x, y, ok := h.Find("─ " + title + " ")
		if !ok {
			t.Fatalf("title %q not on screen:\n%s", title, h.Screen())
		}
		if h.Bold(x+2, y) {
			if got != "" {
				t.Fatalf("both %q and %q are focused", got, title)
			}
			got = title
		}
	}
	return got
}
