package host_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/layout"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

func TestVisorReportsMouseMotionUnlessMouseIsFalse(t *testing.T) {
	on := hosttest.New(t, hosttest.Options{Types: liveViews, Width: 80, Height: 12})
	if got := on.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Errorf("mouse mode %v by default, want all motion (1003)", got)
	}

	off := hosttest.New(t, hosttest.Options{Types: liveViews, Width: 80, Height: 12, Config: "mouse = false\n"})
	if got := off.View().MouseMode; got != tea.MouseModeNone {
		t.Errorf("mouse mode %v with mouse = false, want none", got)
	}
}

const twoOverOne = `{"default": {"rows": [
	{"weight": 1, "widgets": [{"id": "a", "weight": 1}, {"id": "b", "weight": 1}]},
	{"weight": 1, "widgets": [{"id": "c", "weight": 1}]}
]}}`

func TestADraggedTitleShowsASmallGhostWithTheTitleAndABarOnTheDropSide(t *testing.T) {
	h := abc(t, twoOverOne)
	x, y, _ := h.Find("─ Charlie ")
	h.Grab(x+3, y)
	h.DragTo(2, 7)

	left, top, right, bottom := 1<<30, 1<<30, -1, -1
	for gy, line := range strings.Split(h.Screen(), "\n") {
		for gx, r := range []rune(line) {
			if r == '┄' || r == '┆' {
				left, top, right, bottom = min(left, gx), min(top, gy), max(right, gx), max(bottom, gy)
			}
		}
	}
	if right < 0 {
		t.Fatalf("no dashed ghost while dragging Charlie:\n%s", h.Screen())
	}
	if w, gh := right-left+1, bottom-top+1; w > 34 || gh > 6 {
		t.Errorf("ghost is %d×%d, want at most 34×6:\n%s", w, gh, h.Screen())
	}
	if !strings.Contains(h.Line(top), "Charlie") {
		t.Errorf("ghost's top edge %q doesn't hold the title", h.Line(top))
	}
	for by := 0; by < 15; by++ {
		if c := h.Cell(0, by); c == nil || c.Content != "┃" {
			t.Fatalf("no heavy bar down Alpha's left side at row %d:\n%s", by, h.Screen())
		}
	}
	if tok, _ := h.Fg(0, 7); tok != theme.Accent {
		t.Errorf("drop bar drawn in %v, want accent", tok)
	}
	if saved, _ := layout.Load(h.Paths.Config); shapeOf(saved) != "a b / c" {
		t.Errorf("layout.json reads %q mid-drag, want it untouched until the drop", shapeOf(saved))
	}

	h.Drop(2, 7)
	if strings.ContainsAny(h.Screen(), "┄┆┃") {
		t.Errorf("ghost or bar left on screen after the drop:\n%s", h.Screen())
	}
}

func TestHoveringATitleShowsTheGrabHandle(t *testing.T) {
	h := abc(t, twoOverOne)
	x, y, _ := h.Find("─ Bravo ")
	h.Hover(x+3, y)
	if _, _, ok := h.Find("─ ⠿ Bravo "); !ok {
		t.Fatalf("no grab handle on the hovered title:\n%s", h.Line(y))
	}
	if strings.Count(h.Screen(), "⠿") != 1 {
		t.Errorf("grab handle on more than the hovered title:\n%s", h.Screen())
	}
	h.Hover(x+3, y+5)
	if strings.Contains(h.Screen(), "⠿") {
		t.Errorf("grab handle stays after the pointer leaves the title:\n%s", h.Line(y))
	}
}

func TestHoveringASeamLightsOnlyItsTwoFacingEdges(t *testing.T) {
	type cell struct{ x, y int }
	for _, c := range []struct {
		name     string
		at       cell
		lit, not []cell
	}{
		{
			name: "widget seam",
			at:   cell{47, 7},
			lit:  []cell{{47, 1}, {48, 1}, {47, 7}, {48, 7}, {47, 13}, {48, 13}},
			not:  []cell{{47, 0}, {48, 0}, {47, 14}, {48, 14}, {0, 7}, {95, 7}, {20, 0}, {20, 14}, {60, 14}, {60, 15}},
		},
		{
			name: "row seam",
			at:   cell{60, 14},
			lit:  []cell{{1, 14}, {46, 14}, {49, 14}, {94, 14}, {20, 15}, {94, 15}},
			not:  []cell{{0, 14}, {47, 14}, {48, 14}, {95, 14}, {0, 15}, {95, 15}, {0, 7}, {47, 7}, {20, 0}, {20, 29}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := abc(t, twoOverOne)
			h.Hover(c.at.x, c.at.y)
			for _, p := range c.lit {
				if tok, _ := h.Fg(p.x, p.y); tok != theme.Accent {
					t.Errorf("seam edge %d,%d drawn in %v, want accent", p.x, p.y, tok)
				}
			}
			for _, p := range c.not {
				if tok, _ := h.Fg(p.x, p.y); tok != theme.Line {
					t.Errorf("border %d,%d drawn in %v, want line: only the seam's facing edges light up", p.x, p.y, tok)
				}
			}
			if x, y, _ := h.Find("Charlie"); !isFg(h, x, y, theme.Fg) {
				t.Errorf("Charlie's title recoloured by the seam tint")
			}
			h.Hover(20, 7)
			if tok, _ := h.Fg(c.lit[0].x, c.lit[0].y); tok != theme.Line {
				t.Errorf("seam still lit (%v) after the pointer left it", tok)
			}
		})
	}
}

func TestDraggingASeamResizesLiveAndWritesTheLayoutOnRelease(t *testing.T) {
	h := abc(t, twoOverOne)
	h.Grab(47, 7)
	h.DragTo(30, 7)
	if x, _, _ := h.Find("╭─ Bravo "); x != 31 {
		t.Errorf("dragging the seam to column 30 starts Bravo at %d, want 31:\n%s", x, h.Line(0))
	}
	if tok, _ := h.Fg(30, 7); tok != theme.Accent {
		t.Errorf("dragged seam drawn in %v, want accent", tok)
	}
	if saved, _ := layout.Load(h.Paths.Config); saved.Rows[0].Cells[0].Weight != 1 {
		t.Errorf("layout.json changed mid-drag: %+v", saved.Rows[0])
	}
	h.DragTo(5, 7)
	if x, _, _ := h.Find("╭─ Bravo "); x != layout.MinTile.W {
		t.Errorf("dragging the seam past Alpha's minimum starts Bravo at %d, want %d", x, layout.MinTile.W)
	}
	h.DragTo(40, 7)
	h.Drop(40, 7)
	saved, err := layout.Load(h.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	a, b := saved.Rows[0].Cells[0].Weight, saved.Rows[0].Cells[1].Weight
	if share := a / (a + b); share < 40.5/96 || share > 41.5/96 {
		t.Errorf("saved Alpha:Bravo %v:%v, want Alpha 41 of 96 columns", a, b)
	}

	h.Grab(60, 14)
	h.DragTo(60, 9)
	if _, y, _ := h.Find("─ Charlie "); y != 10 {
		t.Errorf("dragging the row seam up to row 9 puts Charlie at row %d, want 10", y)
	}
	h.Drop(60, 9)
	saved, _ = layout.Load(h.Paths.Config)
	if top, bottom := saved.Rows[0].Weight, saved.Rows[1].Weight; top >= bottom {
		t.Errorf("saved row weights %v over %v, want the top row shorter", top, bottom)
	}
}

func longList() widget.Payload {
	return widget.Payload{
		Columns: []widget.Column{{Flex: true}},
		Items: append(append(items("", "a1", "a2", "a3", "a4", "a5"),
			items("Later", "b1", "b2", "b3", "b4", "b5", "b6", "b7", "b8", "b9", "b10")...),
			items("Someday", "c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8", "c9", "c10")...),
	}
}

func TestTheWheelScrollsWithoutFocusingAndAClickSelectsTheItemUnderIt(t *testing.T) {
	// 40×12: the list shows 9 of its 27 lines (25 Items, 2 group headers).
	h, _ := withInbox(t, 12, longList(), nil)
	h.Wheel(10, 5, 8)
	if got, want := strings.Trim(h.Line(1), "│▐ "), "b3"; got != want {
		t.Errorf("after 8 notches the first line is %q, want %q:\n%s", got, want, h.Screen())
	}
	if footer := h.Line(11); !strings.Contains(footer, "Tab/←→ focus") {
		t.Errorf("the wheel focused the Widget: footer %q", footer)
	}

	h.Click(10, 3)
	if x, y, ok := h.Find("b5"); !ok || y != 3 || !isSel(h, x, y) {
		t.Errorf("clicking the line showing b5 didn't select it in place:\n%s", h.Screen())
	}
	if footer := h.Line(11); strings.Contains(footer, "Tab/←→ focus") {
		t.Errorf("clicking an Item didn't focus its Widget: footer %q", footer)
	}

	h.Click(10, 9)
	if x, y, _ := h.Find("b5"); !isSel(h, x, y) {
		t.Errorf("clicking the Someday header moved the selection off b5:\n%s", h.Screen())
	}

	h.Wheel(10, 5, 40)
	if got, want := strings.Trim(h.Line(9), "│▐ "), "c10"; got != want {
		t.Errorf("wheeling far down ends on %q, want the last Item %q", got, want)
	}
	h.Click(10, 2)
	if x, y, ok := h.Find("c3"); !ok || y != 2 || !isSel(h, x, y) {
		t.Errorf("clicking the line showing c3 at the end of the list didn't select it:\n%s", h.Screen())
	}
	h.Wheel(10, 5, -40)
	if got, want := strings.Trim(h.Line(1), "│▐ "), "a1"; got != want {
		t.Errorf("wheeling far up starts on %q, want %q", got, want)
	}
}

func TestTheWheelScrollsWrappedText(t *testing.T) {
	var lines []widget.Line
	for i := 1; i <= 20; i++ {
		lines = append(lines, widget.Text(theme.Fg, "line "+strconv.Itoa(i)))
	}
	h, _ := withInbox(t, 8, widget.Payload{Wrap: true, Lines: lines}, nil)
	h.Wheel(10, 3, 3)
	if got := strings.Trim(h.Line(1), "│▐ "); got != "line 4" {
		t.Errorf("3 notches down show %q first, want line 4:\n%s", got, h.Screen())
	}
	h.Wheel(10, 3, -10)
	if got := strings.Trim(h.Line(1), "│▐ "); got != "line 1" {
		t.Errorf("wheeling back up shows %q first, want line 1", got)
	}
}

func explosive() widget.Payload {
	its := items("", "alpha", "bravo", "charlie")
	for i := range its {
		its[i].Actions = []widget.Action{
			{ID: "open", Label: "open", Primary: true},
			{ID: "explode", Key: "x", Label: "explode"},
		}
	}
	return widget.Payload{Columns: []widget.Column{{Flex: true}}, Items: its,
		Actions: []widget.Action{{ID: "sync", Key: "s", Label: "sync"}}}
}

func TestDoubleClickRunsThePrimaryActionOnTheClickedItem(t *testing.T) {
	h, ran := withInbox(t, 8, explosive(), nil)
	h.Click(10, 2)
	h.Advance(500 * time.Millisecond)
	h.Click(10, 2)
	if len(*ran) != 0 {
		t.Fatalf("two clicks 500ms apart ran %+v, want nothing", *ran)
	}
	h.Click(10, 3)
	if len(*ran) != 0 {
		t.Fatalf("a click on another Item ran %+v", *ran)
	}
	h.Advance(100 * time.Millisecond)
	h.Click(10, 3)
	if len(*ran) != 1 || (*ran)[0].Action != "open" || (*ran)[0].Item != "charlie" {
		t.Errorf("double-click on charlie ran %+v, want open on charlie", *ran)
	}
}

func TestClickingABorderHintRunsItsAction(t *testing.T) {
	h, ran := withInbox(t, 8, explosive(), nil)
	h.Click(10, 2)
	x, y, ok := h.Find("x explode")
	if !ok {
		t.Fatalf("no explode hint in the bottom border:\n%s", h.Screen())
	}
	h.Click(x+4, y)
	if len(*ran) != 1 || (*ran)[0].Action != "explode" || (*ran)[0].Item != "bravo" {
		t.Errorf("clicking the explode hint ran %+v, want explode on bravo", *ran)
	}
	x, y, _ = h.Find("s sync")
	h.Click(x, y)
	if len(*ran) != 2 || (*ran)[1].Action != "sync" {
		t.Errorf("clicking the sync hint ran %+v, want sync", *ran)
	}
}

func TestRightClickOpensTheItemsMenuAndDotOpensTheSameMenu(t *testing.T) {
	h, ran := withInbox(t, 14, explosive(), nil)
	h.RightClick(10, 3)
	mouse := menuEntries(h)
	want := "open ⏎ / explode x / sync s / refresh / hide / edit settings / remove"
	if got := strings.Join(mouse, " / "); got != want {
		t.Fatalf("right-click on charlie opened %q, want %q:\n%s", got, want, h.Screen())
	}
	if _, _, ok := h.Find("─ charlie "); !ok {
		t.Errorf("the menu isn't titled with the Item:\n%s", h.Screen())
	}
	h.Press("esc")
	if menuEntries(h) != nil {
		t.Fatalf("esc left the menu open:\n%s", h.Screen())
	}

	h.Press(".")
	if got := strings.Join(menuEntries(h), " / "); got != want {
		t.Errorf(". opened %q, want the right-click menu %q", got, want)
	}
	h.Press("j", "enter")
	if len(*ran) != 1 || (*ran)[0].Action != "explode" || (*ran)[0].Item != "charlie" {
		t.Errorf("j ⏎ in the menu ran %+v, want explode on charlie", *ran)
	}
	if menuEntries(h) != nil {
		t.Errorf("running an Action left the menu open:\n%s", h.Screen())
	}

	h.RightClick(10, 1)
	x, y, _ := h.Find("sync")
	h.Click(x, y)
	if len(*ran) != 2 || (*ran)[1].Action != "sync" {
		t.Errorf("clicking sync in alpha's menu ran %+v, want sync", *ran)
	}
	h.RightClick(10, 1)
	h.Click(30, 12)
	if menuEntries(h) != nil || len(*ran) != 2 {
		t.Errorf("a click outside the menu ran %+v or left it open:\n%s", *ran, h.Screen())
	}
}

// menuEntries are the open Action menu's rows as their words, or nil when
// no menu is open. The menu is the box drawn over the Inbox tile.
func menuEntries(h *hosttest.Host) []string {
	var out []string
	for _, l := range strings.Split(h.Screen(), "\n") {
		r := []rune(l)
		start := slices.Index(r[1:], '│') + 1
		if start < 1 || start >= len(r)-1 {
			continue
		}
		end := slices.Index(r[start+1:], '│') + start + 1
		if end <= start {
			continue
		}
		if words := strings.Fields(string(r[start+1 : end])); len(words) > 0 {
			out = append(out, strings.Join(words, " "))
		}
	}
	return out
}

func isFg(h *hosttest.Host, x, y int, want theme.Token) bool {
	tok, ok := h.Fg(x, y)
	return ok && tok == want
}

// abc is a 96×31 host of the blank Widgets Alpha, Bravo and Charlie, arranged by layoutJSON.
func abc(t *testing.T, layoutJSON string) *hosttest.Host {
	t.Helper()
	var types []widget.Type
	for _, title := range phoneticTitles[:3] {
		types = append(types, widget.Type{
			Name:  strings.ToLower(title[:1]),
			Title: title,
			New:   func(widget.Config) widget.Widget { return blank{} },
		})
	}
	return hosttest.New(t, hosttest.Options{Width: 96, Height: 31, Root: savedLayout(t, layoutJSON), Types: types})
}

func TestDraggingATitleOntoEachSideOfAWidgetRearrangesAndWritesTheLayout(t *testing.T) {
	// 96×31: Alpha and Bravo share the top row (y 0–14), Charlie fills the
	// bottom one (y 15–29).
	for _, c := range []struct {
		name, drag string
		x, y       int
		want       string
	}{
		{"left of Alpha joins its row before it", "Charlie", 2, 7, "c a b"},
		{"right of Bravo joins its row after it", "Charlie", 93, 7, "a b c"},
		{"top of Charlie opens a row above it", "Bravo", 48, 16, "a / b / c"},
		{"bottom of Charlie opens a row below it", "Alpha", 48, 28, "b / c / a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := abc(t, twoOverOne)
			x, y, ok := h.Find("─ " + c.drag + " ")
			if !ok {
				t.Fatalf("%s's title not on screen:\n%s", c.drag, h.Screen())
			}
			h.Drag(x+2, y, c.x, c.y)

			saved, err := layout.Load(h.Paths.Config)
			if err != nil {
				t.Fatalf("layout.json not written after the drop: %v", err)
			}
			if got := shapeOf(saved); got != c.want {
				t.Errorf("dropping %s at %d,%d saved %q, want %q:\n%s", c.drag, c.x, c.y, got, c.want, h.Screen())
			}
		})
	}
}

func TestClickingThePaletteHintOpensThePalette(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 80, Height: 21})
	x, y, ok := h.Find("^K actions")
	if !ok || y != 20 {
		t.Fatalf("no ^K actions hint in the footer:\n%s", h.Screen())
	}

	h.Click(x+len("^K actions")+3, y)
	if strings.Contains(h.Line(20), "type to filter") {
		t.Fatalf("a click on the footer past the hint opened the palette:\n%s", h.Screen())
	}

	h.Click(x+3, y)
	if !strings.Contains(h.Line(20), "type to filter") {
		t.Errorf("a click on the ^K actions hint left the footer at %q, want the palette open", h.Line(20))
	}
}

func TestClickingWhereThePaletteHintWouldBeDoesNothingInOtherModes(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 80, Height: 21})
	x, y, _ := h.Find("^K actions")

	h.Press("w")
	h.Click(x+3, y)
	if strings.Contains(h.Line(20), "type to filter") {
		t.Errorf("a click on the Widget picker's footer opened the palette:\n%s", h.Screen())
	}
}
