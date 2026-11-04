package layout

import (
	"math"
	"slices"
)

// Toward is the tile focus moves to from id going d: the nearest tile that
// way, preferring tiles that overlap id's across the direction of travel.
// It is id itself when there's none.
func (f Frame) Toward(id string, d Dir) string {
	t, ok := f.Tile(id)
	if !ok {
		return id
	}
	return f.nearest(id, t.Rect, d)
}

// Wrapping is Tab: Toward going Down or Up, except that past the bottom or
// top it carries on from the opposite edge, as if from a tile there as wide
// as id's.
func (f Frame) Wrapping(id string, d Dir) string {
	if next := f.Toward(id, d); next != id {
		return next
	}
	t, ok := f.Tile(id)
	if !ok {
		return id
	}
	from := t.Rect
	from.Y, from.H = -1, 1
	if d == Up {
		from.Y = f.h
	}
	return f.nearest(id, from, d)
}

func (f Frame) nearest(id string, t Rect, d Dir) string {
	best, bestScore := id, [3]float64{math.Inf(1)}
	for _, o := range f.Tiles {
		if o.ID == id {
			continue
		}
		var gap, overlap int
		switch d {
		case Right:
			gap = o.X - (t.X + t.W)
		case Left:
			gap = t.X - (o.X + o.W)
		case Down:
			gap = o.Y - (t.Y + t.H)
		default:
			gap = t.Y - (o.Y + o.H)
		}
		if gap < 0 {
			continue
		}
		tc, oc := centre(t), centre(o.Rect)
		off := math.Abs(oc.x - tc.x)
		if d.across() {
			overlap = min(t.Y+t.H, o.Y+o.H) - max(t.Y, o.Y)
			off = math.Abs(oc.y - tc.y)
		} else {
			overlap = min(t.X+t.W, o.X+o.W) - max(t.X, o.X)
		}
		apart := 1.0
		if overlap > 0 {
			apart = 0
		}
		if score := [3]float64{apart, float64(gap), off}; slices.Compare(score[:], bestScore[:]) < 0 {
			best, bestScore = o.ID, score
		}
	}
	return best
}
