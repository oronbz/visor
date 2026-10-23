package host

import (
	"reflect"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/layout"
	"github.com/oronbz/visor/internal/render"
	"github.com/oronbz/visor/internal/theme"
)

const doubleClick = 400 * time.Millisecond

// drag is a left-button drag of a title (moving its Widget) or of a seam
// (resizing the Widgets either side).
type drag struct {
	zone layout.Zone
	// dx, dy is where a title was grabbed from its tile's corner; for a
	// seam, dx (or dy) is the grab's offset from the seam's first cell.
	dx, dy int
	x, y   int
	moved  bool
	drop   *layout.Drop
	// before is the Layout a seam drag reflows from.
	before layout.Layout
}

// click is the last left click, to tell a double-click.
type click struct {
	zone layout.Zone
	at   time.Time
}

func (m Model) mouseMsg(msg tea.MouseMsg) (Model, tea.Cmd) {
	if !m.mouse || m.showsPopup() {
		return m, nil
	}
	if m.menu != nil {
		return m.menuMouse(msg)
	}
	if m.modal() {
		return m, nil
	}
	ms := msg.Mouse()
	s := m.scene()
	z, hit := s.Hit(ms.X, ms.Y)
	switch msg.(type) {
	case tea.MouseMotionMsg:
		if m.drag != nil {
			return m.dragTo(s.Frame, ms.X, ms.Y), nil
		}
		m.hover = nil
		if hit && (z.Kind == layout.Title || z.Kind == layout.WidgetSeam || z.Kind == layout.RowSeam) {
			m.hover = &z
		}
	case tea.MouseClickMsg:
		switch {
		case ms.Button == tea.MouseLeft && m.onPill(ms.X, ms.Y):
			return m.askUpgrade()
		case !hit:
		case ms.Button == tea.MouseLeft:
			return m.press(s, z, ms.X, ms.Y)
		case ms.Button == tea.MouseRight && z.ID != "":
			if z.Kind == layout.Item {
				m = m.selectItem(z.ID, z.Index)
			}
			m.focus, m.last = z.ID, z.ID
			return m.openMenu(z.ID, ms.X, ms.Y), nil
		}
	case tea.MouseReleaseMsg:
		return m.release(), nil
	case tea.MouseWheelMsg:
		switch {
		case !hit || z.ID == "":
		case ms.Button == tea.MouseWheelDown:
			m.scroll(z.ID, 1)
		case ms.Button == tea.MouseWheelUp:
			m.scroll(z.ID, -1)
		}
	}
	return m, nil
}

// scroll moves list id's view by lines, focus and all else staying put. A
// selection scrolled out of view moves to the nearest Item still in it, so
// the next refresh doesn't scroll back to it.
func (m Model) scroll(id string, lines int) {
	r, ok := m.rects()[id]
	if !ok {
		return
	}
	p := m.payloadIn(id, r)
	if p.Wrap && !p.IsList() {
		m.lists[id] = listState{top: m.lists[id].top + lines}
		m.clampList(id)
		return
	}
	if len(p.Items) == 0 {
		return
	}
	rows := render.Rows(p)
	h := itemRows(p, r)
	st := m.lists[id]
	st.top = min(max(st.top+lines, 0), max(0, len(rows)-h))
	if line := slices.Index(rows, st.idx); line < st.top || line >= st.top+h {
		for j := st.top; j < min(len(rows), st.top+h); j++ {
			if rows[j] < 0 {
				continue
			}
			st.idx = rows[j]
			if line < st.top {
				break
			}
		}
		st.sel = p.Items[st.idx].ID
	}
	m.lists[id] = st
}

// selectItem focuses Widget id and selects its Item i where it's drawn,
// without scrolling.
func (m Model) selectItem(id string, i int) Model {
	m.focus, m.last = id, id
	r := m.rects()[id]
	p := m.payloadIn(id, r)
	if i >= len(p.Items) {
		return m
	}
	st := m.lists[id]
	st.idx, st.sel = i, p.Items[i].ID
	m.lists[id] = st
	return m
}

// pointer is the hover or drag the next frame draws; none while a modal
// view has the input.
func (m Model) pointer() layout.Pointer {
	var p layout.Pointer
	if m.upgrades.popup || m.modal() {
		return p
	}
	if d := m.drag; d != nil {
		if d.zone.Kind != layout.Title {
			p.Seam = &d.zone
			return p
		}
		p.Handle = d.zone.ID
		if d.moved {
			p.Drag = &layout.Drag{ID: d.zone.ID, X: d.x, Y: d.y, DX: d.dx, DY: d.dy, Drop: d.drop}
		}
		return p
	}
	if h := m.hover; h != nil {
		if h.Kind == layout.Title {
			p.Handle = h.ID
		} else {
			p.Seam = h
		}
	}
	return p
}

func (m Model) press(s scene, z layout.Zone, x, y int) (Model, tea.Cmd) {
	switch z.Kind {
	case layout.Title:
		t, _ := s.Tile(z.ID)
		m.focus, m.last = z.ID, z.ID
		m.drag = &drag{zone: z, dx: x - t.X, dy: y - t.Y, x: x, y: y}
	case layout.WidgetSeam, layout.RowSeam:
		m.drag = &drag{zone: z, dx: x - z.X, dy: y - z.Y, x: x, y: y, before: m.layout}
	case layout.Hint:
		if hs := m.hinted(z.ID); z.Index < len(hs) {
			return m.run(z.ID, hs[z.Index])
		}
	case layout.Item, layout.Body:
		m.focus, m.last = z.ID, z.ID
		if z.Kind == layout.Item {
			m = m.selectItem(z.ID, z.Index)
		}
		now := m.opts.Ports.Clock.Now()
		last := m.click
		m.click = click{zone: z, at: now}
		if last.zone.Kind == z.Kind && last.zone.ID == z.ID && last.zone.Index == z.Index && now.Sub(last.at) < doubleClick {
			m.click = click{}
			if a, ok := m.actionFor("enter"); ok {
				return m.run(z.ID, a)
			}
		}
	}
	return m, nil
}

// dragTo follows the pointer: a title drag tracks where it would drop, and a
// seam drag reflows the Layout live.
func (m Model) dragTo(f layout.Frame, x, y int) Model {
	d := *m.drag
	d.x, d.y, d.moved = x, y, true
	m.drag = &d
	switch d.zone.Kind {
	case layout.Title:
		d.drop = nil
		if drop, ok := f.DropAt(d.zone.ID, x, y); ok {
			d.drop = &drop
		}
	case layout.WidgetSeam, layout.RowSeam:
		from := layout.Compute(d.before, m.titles(), m.w, m.h-footerHeight, layout.Pointer{})
		at := x - d.dx
		if d.zone.Kind == layout.RowSeam {
			at = y - d.dy
		}
		m.layout = d.before.Split(from, d.zone, at)
		for _, id := range m.ids() {
			m.clampList(id)
		}
	}
	return m
}

func (m Model) release() Model {
	d := m.drag
	m.drag = nil
	if d == nil {
		return m
	}
	before := m.layout
	if d.zone.Kind == layout.Title && d.drop != nil {
		m.layout = m.layout.Drop(d.zone.ID, *d.drop)
	}
	if d.zone.Kind != layout.Title {
		before = d.before
	}
	if !reflect.DeepEqual(m.layout, before) {
		m = m.saveLayout()
	}
	return m
}

// dragLayers draw a title drag: the dashed ghost holding the title, and the
// heavy bar on the side the Widget would drop.
func (m Model) dragLayers(s scene) []*lipgloss.Layer {
	g, bar := s.Ghost, s.Bar
	if g.W < 2 || g.H < 2 {
		return nil
	}
	line := m.opts.Theme.Fg(theme.Accent)
	edge := line.Render(strings.Repeat("┄", g.W-2))
	side := line.Render(strings.TrimSuffix(strings.Repeat("┆\n", g.H-2), "\n"))
	title := ansi.Truncate(" "+m.widgets[m.drag.zone.ID].title+" ", max(0, g.W-4), "…")
	layers := []*lipgloss.Layer{
		lipgloss.NewLayer(line.Render("╭") + edge + line.Render("╮")).X(g.X).Y(g.Y).Z(2),
		lipgloss.NewLayer(line.Render("╰") + edge + line.Render("╯")).X(g.X).Y(g.Y + g.H - 1).Z(2),
		lipgloss.NewLayer(m.opts.Theme.Fg(theme.Bright).Bold(true).Render(title)).X(g.X + 2).Y(g.Y).Z(3),
	}
	if g.H > 2 {
		layers = append(layers,
			lipgloss.NewLayer(side).X(g.X).Y(g.Y+1).Z(2),
			lipgloss.NewLayer(side).X(g.X+g.W-1).Y(g.Y+1).Z(2))
	}
	if bar.W > 0 {
		heavy := m.opts.Theme.Fg(theme.Accent).Bold(true)
		s := heavy.Render(strings.Repeat("━", bar.W))
		if bar.W == 1 {
			s = heavy.Render(strings.TrimSuffix(strings.Repeat("┃\n", bar.H), "\n"))
		}
		layers = append(layers, lipgloss.NewLayer(s).X(bar.X).Y(bar.Y).Z(4))
	}
	return layers
}

// tint lights the border cells in rs in the accent colour, leaving any text
// drawn over the border (a title) as it is.
func (m Model) tint(c *lipgloss.Canvas, rs []layout.Rect) {
	accent := m.opts.Theme.Color(theme.Accent)
	for _, r := range rs {
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				cell := c.CellAt(x, y)
				if cell == nil || cell.Content != "─" && cell.Content != "│" {
					continue
				}
				lit := *cell
				lit.Style.Fg = accent
				c.SetCell(x, y, &lit)
			}
		}
	}
}
