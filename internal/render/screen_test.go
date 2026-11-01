package render_test

import (
	"fmt"
	"image"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/oronbz/visor/internal/render"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

// composed is a frame as a lipgloss Compositor draws it on a fresh Canvas,
// which Screen must match.
func composed(w, h int, layers []*lipgloss.Layer, tint func(uv.Screen)) string {
	c := lipgloss.NewCanvas(w, h)
	lipgloss.NewCompositor(layers...).Draw(c, image.Rect(0, 0, w, h))
	if tint != nil {
		tint(c)
	}
	return c.Render()
}

func tile(title, text string, w, h int) render.Tile {
	return render.Tile{Title: title, W: w, H: h, Payload: widget.Payload{Lines: []widget.Line{
		widget.Text(theme.Accent, text), {Spans: []widget.Span{{Text: "bold", Token: theme.Warn, Bold: true}}},
	}}}
}

// grid is four tiles side by side and a footer, the dashboard at rest.
func grid(s *render.Screen, texts [4]string, w, h int) []*lipgloss.Layer {
	th := theme.Dark()
	tw := w / 4
	var layers []*lipgloss.Layer
	for i, text := range texts {
		id := fmt.Sprint("w", i)
		layers = append(layers, lipgloss.NewLayer(s.Tile(id, th, tile(id, text, tw, h-1))).X(i*tw))
	}
	return append(layers, lipgloss.NewLayer(th.Fg(theme.Dim).Render("footer")).Y(h-1))
}

func popup(text string) *lipgloss.Layer {
	return lipgloss.NewLayer(theme.Dark().Fg(theme.Bright).Render(text)).X(10).Y(3).Z(1)
}

func lit(scr uv.Screen) {
	for x := range 20 {
		if c := scr.CellAt(x, 0); c != nil {
			l := *c
			l.Style.Fg = theme.Dark().Color(theme.Accent)
			scr.SetCell(x, 0, &l)
		}
	}
}

func TestAScreenDrawsEachFrameAsACompositorOnAFreshCanvasWould(t *testing.T) {
	s := render.NewScreen()
	frames := []struct {
		name   string
		w, h   int
		texts  [4]string
		popup  string
		tinted bool
	}{
		{name: "first frame", w: 80, h: 10, texts: [4]string{"a", "b", "c", "d"}},
		{name: "nothing changed", w: 80, h: 10, texts: [4]string{"a", "b", "c", "d"}},
		{name: "one tile changed", w: 80, h: 10, texts: [4]string{"a", "b", "clock 12:01", "d"}},
		{name: "two tiles changed", w: 80, h: 10, texts: [4]string{"a much longer line than fits", "b", "clock 12:02", "d"}},
		{name: "a popup opened over the tiles", w: 80, h: 10, texts: [4]string{"a", "b", "clock 12:02", "d"}, popup: "popup"},
		{name: "a tile under the popup changed", w: 80, h: 10, texts: [4]string{"a", "b", "clock 12:03", "d"}, popup: "popup"},
		{name: "the popup changed", w: 80, h: 10, texts: [4]string{"a", "b", "clock 12:03", "d"}, popup: "pop"},
		{name: "the popup closed", w: 80, h: 10, texts: [4]string{"a", "b", "clock 12:03", "d"}},
		{name: "a seam lit", w: 80, h: 10, texts: [4]string{"a", "b", "clock 12:03", "d"}, tinted: true},
		{name: "the seam went dark", w: 80, h: 10, texts: [4]string{"a", "b", "clock 12:04", "d"}},
		{name: "the terminal grew", w: 100, h: 12, texts: [4]string{"a", "b", "clock 12:04", "d"}},
		{name: "after the resize, one tile changed", w: 100, h: 12, texts: [4]string{"a", "β wide 漢字", "clock 12:04", "d"}},
	}
	for _, f := range frames {
		layers := grid(s, f.texts, f.w, f.h)
		if f.popup != "" {
			layers = append(layers, popup(f.popup))
		}
		var tint func(uv.Screen)
		if f.tinted {
			tint = lit
		}
		if got, want := s.Frame(f.w, f.h, layers, tint), composed(f.w, f.h, layers, tint); got != want {
			t.Errorf("%s: drew\n%q\nwant\n%q", f.name, got, want)
		}
	}
}

func TestAScreenDrawsATileAgainOnlyWhenWhatItShowsChanged(t *testing.T) {
	s := render.NewScreen()
	th := theme.Dark()
	first := s.Tile("clock", th, tile("Clock", "12:00", 20, 5))
	if again := s.Tile("clock", th, tile("Clock", "12:00", 20, 5)); again != first {
		t.Error("an unchanged tile drew differently")
	}
	if next := s.Tile("clock", th, tile("Clock", "12:01", 20, 5)); next == first || next != render.Draw(th, tile("Clock", "12:01", 20, 5)) {
		t.Errorf("a changed tile drew\n%s\nwant it drawn afresh", next)
	}
	light := theme.Light()
	if relit := s.Tile("clock", light, tile("Clock", "12:01", 20, 5)); relit != render.Draw(light, tile("Clock", "12:01", 20, 5)) {
		t.Error("a theme change kept the old colours")
	}
}
