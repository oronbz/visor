package render_test

import (
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/oronbz/visor/internal/render"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

func TestAGradientChartWarmsFromItsFirstTokenAtTheBottomToItsLastAtTheTop(t *testing.T) {
	th := theme.Dark()
	full := widget.Line{Chart: &widget.Chart{Values: []float64{1, 1}, Gradient: []theme.Token{theme.Ok, theme.Bad}}}
	// 30×6: a 4-row body, all chart.
	out := render.Draw(th, render.Tile{W: 30, H: 6, Payload: widget.Payload{Lines: []widget.Line{full}}})
	buf := uv.NewScreenBuffer(30, 6)
	uv.NewStyledString(out).Draw(buf, buf.Bounds())
	ok, bad := th.Color(theme.Ok), th.Color(theme.Bad)
	for _, tc := range []struct {
		row        int
		near, far  color.Color
		name, side string
	}{
		{4, ok, bad, "bottom", "ok"},
		{1, bad, ok, "top", "bad"},
	} {
		c := buf.CellAt(28, tc.row)
		if c.Content != "⣿" {
			t.Fatalf("%s row cell %q, want chart", tc.name, c.Content)
		}
		if distance(c.Style.Fg, tc.near) >= distance(c.Style.Fg, tc.far) {
			t.Errorf("%s row fg %v isn't nearer %s", tc.name, c.Style.Fg, tc.side)
		}
	}
	mid := buf.CellAt(28, 2).Style.Fg
	if same(mid, ok) || same(mid, bad) {
		t.Errorf("middle row fg %v is an end token, want a blend", mid)
	}
}

func distance(a, b color.Color) float64 {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	d := func(x, y uint32) float64 { return (float64(x) - float64(y)) * (float64(x) - float64(y)) }
	return d(ar, br) + d(ag, bg) + d(ab, bb)
}
