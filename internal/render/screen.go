package render

import (
	"image"
	"reflect"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/theme"
)

// Screen composes frames the way a lipgloss Compositor on a Canvas does, but
// keeps the last frame's cells, so a frame that changes only some layers
// parses those alone and renders only the lines they cover. A dashboard
// redraws on every message, and most messages change one tile.
type Screen struct {
	buf    uv.ScreenBuffer
	w, h   int
	layers []layer
	lines  []string
	tinted bool
	tiles  map[string]drawnTile
	// used are the tiles drawn since the last frame; the others are dropped.
	used map[string]bool
}

type layer struct {
	content string
	x, y, z int
	bounds  image.Rectangle
}

type drawnTile struct {
	th   theme.Theme
	tile Tile
	out  string
}

func NewScreen() *Screen {
	return &Screen{tiles: map[string]drawnTile{}, used: map[string]bool{}}
}

// Tile is Draw(th, t) for Widget id's tile, reused from the last frame when
// nothing it shows changed.
func (s *Screen) Tile(id string, th theme.Theme, t Tile) string {
	s.used[id] = true
	if d, ok := s.tiles[id]; ok && reflect.DeepEqual(d.tile, t) && reflect.DeepEqual(d.th, th) {
		return d.out
	}
	out := Draw(th, t)
	s.tiles[id] = drawnTile{th: th, tile: t, out: out}
	return out
}

// Frame draws layers, lowest z first, on a w×h screen, then tint over them,
// and renders it as lipgloss.Canvas.Render does. tint may be nil. Layers'
// children aren't drawn: lipgloss gives no way to walk them.
func (s *Screen) Frame(w, h int, layers []*lipgloss.Layer, tint func(uv.Screen)) string {
	// A frame drawing no tiles, too small or with them all hidden, keeps them for the next.
	if len(s.used) > 0 {
		for id := range s.tiles {
			if !s.used[id] {
				delete(s.tiles, id)
			}
		}
		clear(s.used)
	}

	next := make([]layer, len(layers))
	for i, l := range layers {
		next[i] = layer{content: l.GetContent(), x: l.GetX(), y: l.GetY(), z: l.GetZ()}
	}
	slices.SortStableFunc(next, func(a, b layer) int { return a.z - b.z })

	changed, ok := s.changed(w, h, next)
	if !ok || tint != nil || s.tinted {
		s.redraw(w, h, next, tint)
		return strings.Join(s.lines, "\n")
	}
	rows := map[int]bool{}
	for _, i := range changed {
		area := s.layers[i].bounds.Union(next[i].bounds).Intersect(s.whole())
		for y := area.Min.Y; y < area.Max.Y; y++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				s.buf.SetCell(x, y, nil)
			}
			rows[y] = true
		}
		s.draw(next[i])
	}
	for y := range rows {
		s.renderLine(y)
	}
	s.layers = next
	return strings.Join(s.lines, "\n")
}

// changed gives the indexes of the layers whose content changed since the
// last frame, filling in every layer's bounds; ok is false when they can't
// be drawn alone: the screen was resized, layers came, went or moved, or a
// changed one touches another, which would lose what's under or over it.
func (s *Screen) changed(w, h int, next []layer) (changed []int, ok bool) {
	if w != s.w || h != s.h || len(next) != len(s.layers) {
		return nil, false
	}
	for i := range next {
		was := s.layers[i]
		if next[i].x != was.x || next[i].y != was.y || next[i].z != was.z {
			return nil, false
		}
		if next[i].content == was.content {
			next[i].bounds = was.bounds
			continue
		}
		next[i].bounds = bounds(next[i])
		changed = append(changed, i)
	}
	for _, i := range changed {
		area := s.layers[i].bounds.Union(next[i].bounds)
		for j := range next {
			if j != i && area.Overlaps(next[j].bounds) {
				return nil, false
			}
		}
	}
	return changed, true
}

func (s *Screen) redraw(w, h int, next []layer, tint func(uv.Screen)) {
	s.buf = uv.NewScreenBuffer(w, h)
	s.buf.Method = ansi.GraphemeWidth
	s.w, s.h = w, h
	for i := range next {
		next[i].bounds = bounds(next[i])
		s.draw(next[i])
	}
	if tint != nil {
		tint(s.buf)
	}
	s.tinted = tint != nil
	s.layers = next
	s.lines = make([]string, h)
	for y := range h {
		s.renderLine(y)
	}
}

func (s *Screen) whole() image.Rectangle { return image.Rect(0, 0, s.w, s.h) }

func (s *Screen) draw(l layer) {
	if l.bounds.Overlaps(s.whole()) {
		uv.NewStyledString(l.content).Draw(&s.buf, l.bounds)
	}
}

func (s *Screen) renderLine(y int) {
	s.lines[y] = strings.TrimRight(s.buf.Lines[y].Render(), " ")
}

func bounds(l layer) image.Rectangle {
	return image.Rect(l.x, l.y, l.x+lipgloss.Width(l.content), l.y+lipgloss.Height(l.content))
}
