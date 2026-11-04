package host_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/layout"
)

// sixFlat is the phonetic Widgets in three rows of two: on 96×31 each tile
// is 48 wide and 10 tall.
const sixFlat = `{"default": {"rows": [
	{"weight": 1, "widgets": [{"id": "a", "weight": 1}, {"id": "b", "weight": 1}]},
	{"weight": 1, "widgets": [{"id": "c", "weight": 1}, {"id": "d", "weight": 1}]},
	{"weight": 1, "widgets": [{"id": "e", "weight": 1}, {"id": "f", "weight": 1}]}
]}}`

// alphaColumn is Alpha as a full-height column beside Bravo, Charlie and
// Delta stacked, over a row of Echo and Foxtrot. On 96×31 Alpha is x 0–28
// and y 0–19, and the stack x 29–95 with Bravo at y 0, Charlie 7, Delta 14.
const alphaColumn = `{"default": {"rows": [
	{"weight": 2, "widgets": [{"id": "a", "weight": 3}, {"weight": 7, "rows": [
		{"weight": 1, "widgets": [{"id": "b", "weight": 1}]},
		{"weight": 1, "widgets": [{"id": "c", "weight": 1}]},
		{"weight": 1, "widgets": [{"id": "d", "weight": 1}]}
	]}]},
	{"weight": 1, "widgets": [{"id": "e", "weight": 1}, {"id": "f", "weight": 1}]}
]}}`

func saved(t *testing.T, h *hosttest.Host) layout.Layout {
	t.Helper()
	l, err := layout.Load(h.Paths.Config)
	if err != nil {
		t.Fatalf("layout.json not written: %v", err)
	}
	return l
}

func at(t *testing.T, h *hosttest.Host, text string) (int, int) {
	t.Helper()
	x, y, ok := h.Find(text)
	if !ok {
		t.Fatalf("%q not on screen:\n%s", text, h.Screen())
	}
	return x, y
}

func TestDraggingATitleToTheLeftEdgeMakesAFullHeightColumnAsWideAsItWas(t *testing.T) {
	h := phonetic(t, sixFlat)
	x, y := at(t, h, "─ Charlie ")
	h.Grab(x+2, y)
	h.DragTo(0, 15)
	for by := range 30 {
		if c := h.Cell(0, by); c == nil || c.Content != "┃" {
			t.Fatalf("no drop bar down the dashboard's whole left edge at row %d:\n%s", by, h.Screen())
		}
	}
	h.Drop(0, 15)

	if x, y := at(t, h, "╭─ Charlie "); x != 0 || y != 0 {
		t.Errorf("Charlie's tile starts at %d,%d, want the top-left corner:\n%s", x, y, h.Screen())
	}
	if c := h.Cell(0, 29); c == nil || c.Content != "╰" {
		t.Errorf("Charlie's tile doesn't reach the bottom:\n%s", h.Screen())
	}
	if x, _ := at(t, h, "╭─ Alpha "); x != 48 {
		t.Errorf("Alpha starts at x %d, want 48 beside a half-width column:\n%s", x, h.Screen())
	}
	dragged := saved(t, h)
	if got, want := shapeOf(dragged), "c [a b / d / e f]"; got != want {
		t.Errorf("saved %q, want %q", got, want)
	}

	keyed := phonetic(t, sixFlat)
	keyed.Press("3", "m", "h", "enter")
	if got := saved(t, keyed); !reflect.DeepEqual(got, dragged) {
		t.Errorf("← past the left edge saved %q %+v, want the same as the edge drop %+v", shapeOf(got), got, dragged)
	}
}

func TestDraggingATitleToTheTopEdgeOpensAFullWidthRow(t *testing.T) {
	h := phonetic(t, sixFlat)
	x, y := at(t, h, "─ Echo ")
	h.Drag(x+2, y, 40, 0)
	if got, want := shapeOf(saved(t, h)), "e / a b / c d / f"; got != want {
		t.Errorf("saved %q, want %q", got, want)
	}
}

func TestByKeyboardAWidgetGoesIntoAFullHeightColumnAndBack(t *testing.T) {
	h := phonetic(t, `{"default": {"rows": [
		{"weight": 1, "widgets": [{"id": "a", "weight": 1}, {"weight": 1, "rows": [
			{"weight": 1, "widgets": [{"id": "b", "weight": 1}]},
			{"weight": 1, "widgets": [{"id": "c", "weight": 1}]}
		]}]},
		{"weight": 1, "widgets": [{"id": "d", "weight": 1}, {"id": "e", "weight": 1}, {"id": "f", "weight": 1}]}
	]}}`)
	h.Press("2", "m", "h", "h", "enter")
	if got, want := shapeOf(saved(t, h)), "[b / a] c / d e f"; got != want {
		t.Errorf("← ← saved %q, want %q", got, want)
	}
	h.Press("m", "l", "l", "enter")
	if got, want := shapeOf(saved(t, h)), "a [b / c] / d e f"; got != want {
		t.Errorf("→ → saved %q, want %q back where it started", got, want)
	}
}

func TestArrowFocusMovesByPositionThroughStacks(t *testing.T) {
	h := phonetic(t, alphaColumn)
	for _, c := range []struct {
		keys []string
		want string
	}{
		{[]string{"1", "l"}, "Charlie"},
		{[]string{"1", "l", "h"}, "Alpha"},
		{[]string{"2", "J"}, "Charlie"},
		{[]string{"2", "J", "J"}, "Delta"},
		{[]string{"4", "h"}, "Alpha"},
		{[]string{"4", "J"}, "Foxtrot"},
		{[]string{"5", "K"}, "Alpha"},
		{[]string{"3", "tab"}, "Delta"},
		{[]string{"4", "tab"}, "Foxtrot"},
		{[]string{"6", "shift+tab"}, "Delta"},
		{[]string{"2", "shift+tab"}, "Foxtrot"},
		{[]string{"5", "tab"}, "Alpha"},
	} {
		h.Press(c.keys...)
		if got := focused(t, h, phoneticTitles); got != c.want {
			t.Errorf("%v focuses %q, want %q", c.keys, got, c.want)
		}
	}
}

func TestDigitsCountTilesInReadingOrderThroughStacks(t *testing.T) {
	h := phonetic(t, alphaColumn)
	for i, want := range phoneticTitles {
		h.Press(string(rune('1' + i)))
		if got := focused(t, h, phoneticTitles); got != want {
			t.Errorf("%d focuses %q, want %q", i+1, got, want)
		}
	}
}

func TestShiftArrowsResizeInTheNearestContainerWithASiblingThatWay(t *testing.T) {
	h := phonetic(t, alphaColumn)
	bx, _ := at(t, h, "╭─ Bravo ")
	h.Press("3", "m", "L")
	if x, _ := at(t, h, "┏━ Charlie "); x >= bx {
		t.Errorf("⇧→ on Charlie left the stack at x %d, want it wider than from %d:\n%s", x, bx, h.Screen())
	}
	tall := func() int {
		_, cy := at(t, h, "┏━ Charlie ")
		_, dy := at(t, h, "╭─ Delta ")
		return dy - cy
	}
	before := tall()
	h.Press("J")
	if got := tall(); got <= before {
		t.Errorf("⇧↓ on Charlie left it %d tall, want taller than %d:\n%s", got, before, h.Screen())
	}
	if c := h.Cell(0, 19); c == nil || c.Content != "╰" {
		t.Errorf("⇧↓ on Charlie changed Alpha's height:\n%s", h.Screen())
	}
}

func TestASeamInsideAStackDragsOnlyItsTwoRows(t *testing.T) {
	h := phonetic(t, alphaColumn)
	_, cy := at(t, h, "╭─ Charlie ")
	h.Drag(70, cy, 70, cy-2)
	if _, y := at(t, h, "╭─ Charlie "); y != cy-2 {
		t.Errorf("Charlie starts at y %d after dragging its top seam up 2, want %d:\n%s", y, cy-2, h.Screen())
	}
	if c := h.Cell(0, 19); c == nil || c.Content != "╰" {
		t.Errorf("Alpha's height changed:\n%s", h.Screen())
	}
	l := saved(t, h)
	top := l.Rows[0]
	stack := top.Cells[1].Rows
	if top.Weight != 2 || top.Cells[0].Weight != 3 || top.Cells[1].Weight != 7 || stack[2].Weight != 1 {
		t.Errorf("saved %+v, want only Bravo's and Charlie's weights changed", l)
	}
	if sum := stack[0].Weight + stack[1].Weight; sum != 2 || stack[0].Weight >= 1 {
		t.Errorf("Bravo and Charlie weigh %v and %v, want Bravo less and the pair still 2", stack[0].Weight, stack[1].Weight)
	}
}

func TestTheTooSmallNoticeCountsEveryRowOfAStack(t *testing.T) {
	h := phonetic(t, alphaColumn)
	h.Resize(40, 20)
	if _, _, ok := h.Find("too small (needs 32×21)"); !ok {
		t.Errorf("a three-row stack over a row at 40×20, want the notice to ask for 32×21:\n%s", h.Screen())
	}
	h.Resize(40, 21)
	if _, _, ok := h.Find("too small"); ok {
		t.Errorf("notice shown at the minimum size:\n%s", h.Screen())
	}
}

func TestHidingEveryWidgetInAStackHidesItAndShowingOneBringsItBack(t *testing.T) {
	h := phonetic(t, `{"default": {"rows": [
		{"weight": 1, "widgets": [{"id": "a", "weight": 1}, {"weight": 1, "rows": [
			{"weight": 1, "widgets": [{"id": "b", "weight": 1}]},
			{"weight": 1, "widgets": [{"id": "c", "weight": 1}]}
		]}]},
		{"weight": 1, "widgets": [{"id": "d", "weight": 1}, {"id": "e", "weight": 1}, {"id": "f", "weight": 1}]}
	]}}`)
	h.Press("w", "j", "space", "j", "space", "esc")
	if got := cornersOn(h.Line(0)); !slices.Equal(got, []int{0}) {
		t.Errorf("with Bravo and Charlie hidden the top row has tiles at %v, want Alpha alone:\n%s", got, h.Screen())
	}
	h.Press("w", "j", "j", "space", "esc")
	if x, y := at(t, h, "╭─ Charlie "); x != 48 || y != 0 {
		t.Errorf("Charlie shown again at %d,%d, want back in its stack beside Alpha at 48,0:\n%s", x, y, h.Screen())
	}
	if got := shapeOf(saved(t, h)); got != "a [b / c] / d e f" {
		t.Errorf("saved %q, want the stack kept", got)
	}
}
