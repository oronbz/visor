package host

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/layout"
	"github.com/oronbz/visor/internal/render"
	"github.com/oronbz/visor/internal/widget"
)

// graphicsQuery asks whether the terminal speaks the kitty graphics
// protocol; only one that does answers it, ahead of its device attributes.
const graphicsQuery = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"

// cellSizeQueries ask for a cell's size in pixels, and the window's for
// terminals that report only that.
const cellSizeQueries = "\x1b[16t\x1b[14t"

type graphicsMode int

const (
	graphicsAsking graphicsMode = iota
	graphicsHalfBlocks
	graphicsKitty
)

// images are the Payload images the host draws and what the terminal holds
// of them.
type images struct {
	mode graphicsMode
	// answered is whether the graphics query was answered; the mode waits
	// for the device attributes, by when the cell size is in too.
	answered bool
	// cell and window are the sizes the terminal reported, in pixels.
	cell, window image.Point
	// asked is the window size, in cells, the cell size was last asked at.
	asked  image.Point
	loaded map[string]*decoded
	// shown are the images drawn this frame, by Widget id.
	shown  map[string]render.Image
	shrunk map[shrinkKey]image.Image
	// uploads are the images the terminal holds, by content hash.
	uploads map[[32]byte]*upload
	// synced is what the last sync drew from, so a frame with nothing new
	// for images skips it.
	synced imageInputs
}

// imageInputs are what decides how images draw: the graphics mode, the
// cell size and every Payload with an image.
type imageInputs struct {
	mode graphicsMode
	cell image.Point
	refs []imageRef
}

func (a imageInputs) equal(b imageInputs) bool {
	return a.mode == b.mode && a.cell == b.cell && slices.Equal(a.refs, b.refs)
}

// imageRef is a Widget whose Payload has an image, and the tile it's drawn
// in, zero when it isn't. A Payload's Lines and Wrap decide the image's box
// too; a Plugin sends a new Payload, so a new Image, when they change.
type imageRef struct {
	id    string
	img   *widget.Image
	tile  layout.Rect
	wrap  bool
	lines int
}

// upload is an image the terminal holds, and its virtual placements by
// their size in cells.
type upload struct {
	n          int
	placements map[image.Point]uint32
}

// id is the kitty image id of the nth upload: 1 to 255 in its low byte, as
// a 256-colour foreground carries it, and what's over in its high byte.
func (u *upload) id() uint32 { return uint32((u.n-1)/255)<<24 | uint32((u.n-1)%255+1) }

type decoded struct {
	hash [32]byte
	img  image.Image
	// png is what's uploaded: the file itself when it's a PNG.
	png []byte
	err error
	// mod and size tell a changed file from the one loaded, and from the
	// Image they were last checked for.
	mod  time.Time
	size int64
	from *widget.Image
}

type shrinkKey struct {
	hash [32]byte
	size image.Point
}

func newImages() *images {
	return &images{loaded: map[string]*decoded{}, shown: map[string]render.Image{}, shrunk: map[shrinkKey]image.Image{}, uploads: map[[32]byte]*upload{}}
}

// maxChunk is the most base64 a kitty graphics escape carries.
const maxChunk = 4096

// upload sends l to the terminal, unless it holds it already.
func (g *images) upload(l *decoded, out *strings.Builder) *upload {
	if u := g.uploads[l.hash]; u != nil {
		return u
	}
	u := &upload{n: 1, placements: map[image.Point]uint32{}}
	for g.taken(u.n) {
		u.n++
	}
	g.uploads[l.hash] = u
	data := base64.StdEncoding.EncodeToString(l.png)
	keys := fmt.Sprintf("a=t,f=100,i=%d,q=2", u.id())
	if len(data) <= maxChunk {
		out.WriteString(ansi.KittyGraphics([]byte(data), keys))
		return u
	}
	for len(data) > 0 {
		n := min(maxChunk, len(data))
		more := 0
		if n < len(data) {
			more = 1
		}
		out.WriteString(ansi.KittyGraphics([]byte(data[:n]), keys, fmt.Sprintf("m=%d", more)))
		data, keys = data[n:], "q=2"
	}
	return u
}

func (g *images) taken(n int) bool {
	for _, u := range g.uploads {
		if u.n == n {
			return true
		}
	}
	return false
}

// place gives u a virtual placement the size of box, unless it has one.
func (g *images) place(u *upload, box image.Point, out *strings.Builder) uint32 {
	if p := u.placements[box]; p != 0 {
		return p
	}
	p, taken := uint32(1), slices.Collect(maps.Values(u.placements))
	for slices.Contains(taken, p) {
		p++
	}
	u.placements[box] = p
	out.WriteString(ansi.KittyGraphics(nil, "a=p,U=1", fmt.Sprintf("i=%d,p=%d,c=%d,r=%d,q=2", u.id(), p, box.X, box.Y)))
	return p
}

// prune deletes the placements this frame didn't use, and the images no
// Payload has any more, from the terminal. used holds every image a Payload
// has, with the placements drawn.
func (g *images) prune(used map[*upload]map[image.Point]bool, out *strings.Builder) {
	for _, hash := range g.byID() {
		u := g.uploads[hash]
		if used[u] == nil {
			out.WriteString(ansi.KittyGraphics(nil, fmt.Sprintf("a=d,d=I,i=%d,q=2", u.id())))
			delete(g.uploads, hash)
			continue
		}
		for box, p := range u.placements {
			if !used[u][box] {
				out.WriteString(ansi.KittyGraphics(nil, fmt.Sprintf("a=d,d=i,i=%d,p=%d,q=2", u.id(), p)))
				delete(u.placements, box)
			}
		}
	}
}

// byID is the uploads' hashes in id order, so escapes come out the same
// every time.
func (g *images) byID() [][32]byte {
	hashes := slices.Collect(maps.Keys(g.uploads))
	slices.SortFunc(hashes, func(a, b [32]byte) int { return g.uploads[a].n - g.uploads[b].n })
	return hashes
}

// quit writes the pushes held back from the cache and deletes every image
// visor uploaded from the terminal, then quits.
func (m Model) quit() tea.Cmd {
	cmds := []tea.Cmd{m.flushPushes()}
	var out strings.Builder
	m.images.prune(nil, &out)
	if out.Len() > 0 {
		cmds = append(cmds, tea.Raw(out.String()))
	}
	return tea.Sequence(append(cmds, tea.Quit)...)
}

// answer reads the terminal's answers to the graphics and cell size queries.
func (g *images) answer(msg tea.Msg) {
	switch msg := msg.(type) {
	case uv.KittyGraphicsEvent:
		g.answered = g.answered || msg.Options.ID == 31
	case uv.PrimaryDeviceAttributesEvent, queryWaitMsg:
		if g.mode == graphicsAsking {
			g.mode = graphicsHalfBlocks
			if g.answered {
				g.mode = graphicsKitty
			}
		}
	case uv.CellSizeEvent:
		g.cell = image.Pt(msg.Width, msg.Height)
	case uv.PixelSizeEvent:
		g.window = image.Pt(msg.Width, msg.Height)
	}
}

// cellSize is a cell's size in pixels in a window w×h cells, or a guess of
// 1:2 when the terminal hasn't said.
func (g *images) cellSize(w, h int) image.Point {
	switch {
	case g.cell.X > 0 && g.cell.Y > 0:
		return g.cell
	case g.window.X > 0 && g.window.Y > 0 && w > 0 && h > 0:
		return image.Pt(g.window.X/w, g.window.Y/h)
	}
	return image.Pt(1, 2)
}

// imageInputs gathers what images draw from: every Widget whose Payload has
// an image, visible or not, and the tile it's drawn in. A hidden Widget's
// Payload counts only when it doesn't depend on a size, as a Source's.
// payloads are the refs' Payloads.
func (m Model) imageInputs() (in imageInputs, payloads []widget.Payload) {
	in = imageInputs{mode: m.images.mode, cell: m.images.cellSize(m.w, m.h)}
	f := layout.Compute(m.layout, nil, m.w, m.h-footerHeight, layout.Pointer{})
	laid := f
	if f.TooSmall {
		laid = m.laidOut()
	}
	add := func(id string, p widget.Payload, tile layout.Rect) {
		if p.HasImage() {
			in.refs = append(in.refs, imageRef{id: id, img: p.Image, tile: tile, wrap: p.Wrap, lines: len(p.Lines)})
			payloads = append(payloads, p)
		}
	}
	for _, t := range laid.Tiles {
		var tile layout.Rect
		if !f.TooSmall && (m.editing == nil || m.editing.widget != t.ID) {
			tile = t.Rect
		}
		add(t.ID, m.payloadIn(t.ID, t.Rect), tile)
	}
	for _, id := range m.ids() {
		if _, ok := laid.Tile(id); ok {
			continue
		}
		if _, ok := m.widgets[id].widget.(widget.Source); ok {
			add(id, m.payloadIn(id, layout.Rect{}), layout.Rect{})
		}
	}
	return in, payloads
}

// syncImages loads the images the frame shows, sizing each to its tile. With
// kitty graphics it uploads and places the ones the terminal doesn't hold
// yet, and deletes placements no tile draws and images no Payload has any
// more, in escapes written outside the frame, ahead of it. It does nothing
// while what images draw from is unchanged.
func (m Model) syncImages() tea.Cmd {
	g := m.images
	if g.mode == graphicsAsking || m.w == 0 || m.h == 0 {
		return nil
	}
	in, payloads := m.imageInputs()
	if in.equal(g.synced) {
		return nil
	}
	g.synced = in
	var out strings.Builder
	if size := image.Pt(m.w, m.h); g.asked == (image.Point{}) {
		g.asked = size
	} else if size != g.asked && len(in.refs) > 0 {
		g.asked = size
		out.WriteString(cellSizeQueries)
	}
	used := map[*upload]map[image.Point]bool{}
	shown := map[string]render.Image{}
	loaded := map[string]*decoded{}
	shrunk := map[shrinkKey]image.Image{}
	for i, ref := range in.refs {
		key, l := g.load(ref.img)
		loaded[key] = l
		if l.err != nil {
			continue
		}
		if u := g.uploads[l.hash]; u != nil && used[u] == nil {
			used[u] = map[image.Point]bool{}
		}
		if ref.tile == (layout.Rect{}) {
			continue
		}
		pic := render.Image{Size: l.img.Bounds().Size(), Cell: in.cell}
		box := render.ImageBox(payloads[i], pic, ref.tile.W-2, ref.tile.H-2)
		switch {
		case box == (image.Point{}):
			continue
		case g.mode == graphicsKitty:
			u := g.upload(l, &out)
			pic.ID, pic.Placement = u.id(), g.place(u, box, &out)
			if used[u] == nil {
				used[u] = map[image.Point]bool{}
			}
			used[u][box] = true
		default:
			k := shrinkKey{l.hash, render.HalfBlocks(pic, box.X, box.Y)}
			if shrunk[k] = g.shrunk[k]; shrunk[k] == nil {
				shrunk[k] = render.Shrink(l.img, k.size)
			}
			pic.Pixels = shrunk[k]
		}
		shown[ref.id] = pic
	}
	g.shown, g.loaded, g.shrunk = shown, loaded, shrunk
	g.prune(used, &out)
	if out.Len() == 0 {
		return nil
	}
	return tea.Raw(out.String())
}

// load reads and decodes img, or returns what it read before. A file is
// checked for changes only when img is new, as it is in a new Payload.
func (g *images) load(img *widget.Image) (string, *decoded) {
	if img.Path == "" {
		key := "data:" + img.Data
		if l := g.loaded[key]; l != nil {
			return key, l
		}
		b, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			return key, &decoded{err: err}
		}
		return key, decode(b)
	}
	key := "path:" + img.Path
	l := g.loaded[key]
	if l != nil && l.from == img {
		return key, l
	}
	st, err := os.Stat(img.Path)
	if err != nil {
		return key, &decoded{err: err, from: img}
	}
	if l != nil && l.err == nil && l.mod.Equal(st.ModTime()) && l.size == st.Size() {
		l.from = img
		return key, l
	}
	b, err := os.ReadFile(img.Path)
	if err != nil {
		return key, &decoded{err: err, from: img}
	}
	l = decode(b)
	l.mod, l.size, l.from = st.ModTime(), st.Size(), img
	return key, l
}

func decode(b []byte) *decoded {
	img, format, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return &decoded{err: err}
	}
	l := &decoded{hash: sha256.Sum256(b), img: img, png: b}
	switch format {
	case "png":
	case "jpeg":
		var out bytes.Buffer
		if err := png.Encode(&out, img); err != nil {
			return &decoded{err: err}
		}
		l.png = out.Bytes()
	default:
		return &decoded{err: errors.New("not a PNG or JPEG")}
	}
	return l
}
