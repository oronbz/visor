// Package host is the visor Bubble Tea program: it owns the Widgets, input
// modes and focus, and draws the dashboard. Everything outside the process
// goes through Options.Ports.
package host

import (
	"fmt"
	"image"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/cache"
	"github.com/oronbz/visor/internal/closekey"
	"github.com/oronbz/visor/internal/config"
	"github.com/oronbz/visor/internal/layout"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/plugin"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/render"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
	"github.com/oronbz/visor/internal/widgets/calendar"
	"github.com/oronbz/visor/internal/widgets/clock"
	"github.com/oronbz/visor/internal/widgets/cpu"
	"github.com/oronbz/visor/internal/widgets/jira"
	"github.com/oronbz/visor/internal/widgets/notes"
	"github.com/oronbz/visor/internal/widgets/prs"
	"github.com/oronbz/visor/internal/widgets/script"
	"github.com/oronbz/visor/internal/widgets/tasks"
)

type Options struct {
	Paths paths.Paths
	// ConfigFile overrides Paths.Config/config.toml (--config, VISOR_CONFIG).
	ConfigFile string
	// Locale gives time_format and week_start their defaults.
	Locale config.Locale
	Ports  ports.Ports
	// Theme, when set, pins the palette (tests use sentinel colours); zero
	// follows config.toml and the terminal's colours.
	Theme theme.Theme
	// Types replaces the built-in Widget types, so tests can stage Layouts
	// the built-ins can't fill.
	Types     []widget.Type
	CloseKeys []closekey.Seq
	// Version stamps agent skills; "" is the version this binary was built as.
	Version string
	// BundleDir is where visor's executable and the Plugins it ships with sit.
	BundleDir string
}

// Model is the root tea.Model. Its maps are shared by Model copies, so only
// Update writes them.
type Model struct {
	opts    Options
	w, h    int
	now     time.Time
	widgets map[string]placed
	layout  layout.Layout
	// focus is "" when visor is unfocused; last survives unfocusing.
	focus, last string
	keyMap      bool
	arranging   bool
	// before is the Layout arrange mode reverts to.
	before  layout.Layout
	via     layout.Via
	picking bool
	cursor  int
	toast   toast
	fetched map[string]widget.Payload
	lists   map[string]listState
	sched   map[string]sched
	// paused is when each rate-limited credential may be used again.
	paused map[string]time.Time
	cache  cache.Store
	rng    *rand.Rand
	// timers numbers the refresh timers app-wide, so a rebuilt Widget's old
	// timers never match.
	timers *int
	// Each timer chain is kept to one.
	spinning, checkingWallClock bool
	overlay                     *overlay
	editing                     *editing
	palette                     *palette
	menu                        *menu
	menus                       int
	// heldModifier is only reported by kitty-protocol terminals.
	heldModifier tea.KeyMod
	colors       colors
	mouse        bool
	hover        *layout.Zone
	drag         *drag
	click        click
	configEditor config.Editor
	// widgetBase is what every Widget is built with, besides its settings.
	widgetBase widget.Config
	// configProblem shows whenever no toast does, until config.toml is fixed.
	configProblem string
	// ticks tells the current redraw timer from ones a config reload replaced.
	ticks    int
	closing  closekey.Matcher
	plugins  *plugin.Registry
	pushes   chan tea.Msg
	observed map[string]observation
	images   *images
}

type placed struct {
	typ    widget.Type
	title  string
	widget widget.Widget
	// setup, when set, is the needs-setup hint drawn instead of the Widget.
	setup string
	ref   config.Ref
	// block is what config.toml said when the Widget was built.
	block config.Widget
}

type tickMsg struct {
	at  time.Time
	seq int
}

// Builtins are the Widget types compiled into visor, in reference order.
var Builtins = slices.Concat([]widget.Type{clock.Type, calendar.Type}, macTypes,
	[]widget.Type{tasks.Type, cpu.Type, notes.Type, prs.Type, jira.Type, script.Type})

func New(o Options) Model {
	m := Model{opts: o, now: o.Ports.Clock.Now(), widgets: map[string]placed{},
		fetched: map[string]widget.Payload{}, lists: map[string]listState{},
		sched: map[string]sched{}, paused: map[string]time.Time{}, cache: cacheStore(o), rng: newRand(o.Ports.Clock.Now()), timers: new(int),
		closing: closekey.NewMatcher(o.CloseKeys), plugins: plugin.NewRegistry(), pushes: make(chan tea.Msg, 64), observed: map[string]observation{}, images: newImages()}
	file := o.ConfigFile
	if file == "" {
		file = filepath.Join(o.Paths.Config, config.FileName)
	}
	cfg, err, types, problems := m.loadConfig(file, m.baseTypes())
	if err != nil {
		problems = append([]string{err.Error()}, problems...)
	}
	m.colors = colors{pinned: !o.Theme.IsZero(), name: cfg.Theme, over: cfg.Colors, asking: o.Theme.IsZero()}
	m = m.resolveTheme()
	m.configEditor = config.NewEditor(file, o.Paths.Data, types, o.Locale)
	m, ids, _, clamped := m.placeAll(cfg)
	m.configProblem = toastOf(append(append(problems, cfg.Problems...), clamped...))
	saved, err := layout.Load(o.Paths.Config)
	if err != nil {
		saved = layout.Default()
	}
	m.layout = saved.Join(ids)
	return m.watch()
}

// placeAll builds cfg's Widgets and returns their ids in config order, the
// ids of those it built afresh, and toasts for refresh settings it clamped
// and custom Action keys visor owns.
// A Widget whose block, and the globals Widgets are built from, haven't
// changed is kept as it was, state and all.
func (m Model) placeAll(cfg config.Config) (Model, []string, []string, []string) {
	old := m.widgets
	sameGlobals := cfg.TimeFormat == m.widgetBase.TimeFormat && cfg.WeekStart == m.widgetBase.WeekStart
	m.mouse = cfg.Mouse
	m.widgetBase = widget.Config{TimeFormat: cfg.TimeFormat, WeekStart: cfg.WeekStart, DataDir: m.opts.Paths.Data, Ports: m.opts.Ports}
	m.widgets = make(map[string]placed, len(cfg.Widgets))
	var ids, fresh, problems []string
	for _, wc := range cfg.Widgets {
		problems = append(problems, m.shadowed(wc)...)
		id, ref := wc.ID, config.Ref{ID: wc.ID}
		// Widgets sharing an id are all in needs setup; suffixes keep them apart on screen only.
		for n := 2; m.widgets[id].title != ""; n++ {
			id, ref.N = fmt.Sprintf("%s~%d", wc.ID, n), n-1
		}
		ids = append(ids, id)
		if p, ok := old[id]; ok && sameGlobals && p.block.Equal(wc) {
			p.ref = ref
			m.widgets[id] = p
			continue
		}
		m.widgets[id] = m.place(wc, ref)
		fresh = append(fresh, id)
		if problem := m.schedule(id, wc.Refresh); problem != "" {
			problems = append(problems, problem)
		}
	}
	for id := range m.sched {
		if _, ok := m.widgets[id]; !ok {
			delete(m.sched, id)
		}
	}
	m.closeDropped(old)
	return m, ids, fresh, problems
}

// shadowed are problems naming wc's custom Actions whose keys a host key
// owns, which leaves them only in the palette.
func (m Model) shadowed(wc config.Widget) []string {
	var out []string
	for _, a := range wc.Actions {
		if a.Key != "" && hostKey(a.Key) {
			out = append(out, fmt.Sprintf("%s:%d: key `%s` of `%s` in Widget `%s` is visor's own; pick another",
				filepath.Base(m.configEditor.Path), wc.Line(), a.Key, a.Label, wc.ID))
		}
	}
	return out
}

func (m Model) place(wc config.Widget, ref config.Ref) placed {
	p := placed{title: wc.Title, setup: wc.Setup, ref: ref, block: wc}
	if i := slices.IndexFunc(m.configEditor.Types, func(t widget.Type) bool { return t.Name == wc.Type }); i >= 0 {
		p.typ = m.configEditor.Types[i]
	}
	if p.setup == "" {
		c := m.widgetBase
		c.ID, c.Settings = wc.ID, wc.Settings
		p.widget = p.typ.New(c)
	}
	return p
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.nextTick(), m.queryTerminal(), m.nextChange(), m.nextPush(), m.listen(m.ids()), func() tea.Msg { return startMsg{} }, m.cleanCache(), m.refreshSkills())
}

func (m Model) baseTypes() []widget.Type {
	if m.opts.Types != nil {
		return m.opts.Types
	}
	return Builtins
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.images.answer(msg)
	next, cmd := m.update(msg)
	n := next.(Model)
	n.observe()
	return n, tea.Batch(cmd, n.syncImages())
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, cmd, ok := m.colorAnswer(msg); ok {
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		for _, id := range m.ids() {
			m.clampList(id)
		}
		m.sizeEditor()
	case tea.PasteMsg:
		if m.overlay != nil {
			return m.overlayPaste(msg), nil
		}
		if m.editing != nil && m.palette == nil {
			return m.editKey(msg)
		}
	case refreshedMsg:
		return m.refreshed(msg)
	case pushedMsg:
		return m.pushed(msg)
	case retryMsg:
		return m.retried(msg)
	case startMsg:
		m, fetch := m.startAll(false)
		m, beat := m.beat()
		return m, tea.Batch(fetch, beat)
	case dueMsg:
		return m.due(msg)
	case beatMsg:
		return m.beaten(msg)
	case spinMsg:
		return m.spun(msg)
	case actedMsg:
		return m.acted(msg)
	case choicesMsg:
		return m.chose(msg)
	case editedMsg:
		return m.edited(msg)
	case filesChangedMsg:
		return m.filesChanged(msg)
	case skillsMsg:
		return m.skillsDone(msg)
	case configEditedMsg:
		return m.configEdited(msg)
	case toastExpiredMsg:
		if m.toast.seq == msg.seq {
			m.toast = toast{}
		}
	case tickMsg:
		if msg.seq != m.ticks {
			return m, nil
		}
		m.now = msg.at
		return m, m.nextTick()
	case tea.MouseMsg:
		return m.mouseMsg(msg)
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.KeyReleaseMsg:
		if _, ok := loneModifier(msg.Code); ok {
			m.heldModifier = 0
		}
	}
	return m, nil
}

func (m Model) nextTick() tea.Cmd {
	var next time.Time
	for _, w := range m.widgets {
		if t, ok := w.widget.(widget.Ticker); ok && w.setup == "" {
			if n := t.Next(m.now); next.IsZero() || n.Before(next) {
				next = n
			}
		}
	}
	if next.IsZero() {
		return nil
	}
	seq := m.ticks
	return m.opts.Ports.Clock.After(next.Sub(m.now), func(t time.Time) tea.Msg { return tickMsg{at: t, seq: seq} })
}

func (m Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.BackgroundColor = m.background()
	// Report-all-keys and event types make lone modifier presses and releases
	// arrive, for the modifier footer.
	v.KeyboardEnhancements = tea.KeyboardEnhancements{
		ReportEventTypes:           true,
		ReportAlternateKeys:        true,
		ReportAllKeysAsEscapeCodes: true,
		ReportAssociatedText:       true,
	}
	if m.mouse {
		v.MouseMode = tea.MouseModeAllMotion
	}
	if m.w == 0 || m.h == 0 {
		return v
	}
	var layers []*lipgloss.Layer
	s := m.scene()
	switch {
	case s.TooSmall:
		layers = append(layers, lipgloss.NewLayer(m.centred(fmt.Sprintf("too small (needs %d×%d)", s.Need.W, s.Need.H), theme.Warn)))
	case len(s.Tiles) == 0:
		layers = append(layers, lipgloss.NewLayer(m.centred("all Widgets hidden · w to show", theme.Dim)),
			lipgloss.NewLayer(m.footer()).Y(m.h-footerHeight))
	default:
		for i, t := range s.Tiles {
			if m.editing != nil && t.ID == m.editing.widget {
				layers = append(layers, m.editorLayer(t.X, t.Y))
			}
			layers = append(layers, lipgloss.NewLayer(render.Draw(m.opts.Theme, s.tiles[i])).X(t.X).Y(t.Y))
		}
		layers = append(layers, m.dragLayers(s)...)
		layers = append(layers, lipgloss.NewLayer(m.footer()).Y(m.h-footerHeight))
	}
	if m.picking {
		layers = append(layers, m.pickerLayer())
	}
	if m.keyMap {
		layers = append(layers, m.keyMapLayer())
	}
	if m.palette != nil {
		layers = append(layers, m.paletteLayer())
	}
	if m.menu != nil {
		layers = append(layers, m.menuLayer())
	}
	if m.overlay != nil {
		layers = append(layers, m.overlayLayer())
	}
	canvas := lipgloss.NewCanvas(m.w, m.h)
	lipgloss.NewCompositor(layers...).Draw(canvas, image.Rect(0, 0, m.w, m.h))
	m.tint(canvas, s.Tint)
	v.SetContent(canvas.Render())
	return v
}

// payloadIn is what Widget id shows in a tile at r, border included.
func (m Model) payloadIn(id string, r layout.Rect) widget.Payload {
	w := m.widgets[id]
	if p, ok := m.statusPayload(id); ok {
		return p
	}
	switch v := w.widget.(type) {
	case widget.Source:
		return m.fetched[id]
	case widget.View:
		return v.Payload(widget.Frame{Now: m.now, Size: widget.Size{W: r.W - 2, H: r.H - 2}, Focused: id == m.focus})
	}
	return widget.Payload{}
}

func (m Model) ids() []string {
	ids := make([]string, 0, len(m.widgets))
	for id := range m.widgets {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func toastOf(msgs []string) string {
	switch len(msgs) {
	case 0:
		return ""
	case 1:
		return msgs[0]
	}
	return fmt.Sprintf("%s (+%d more)", msgs[0], len(msgs)-1)
}

func (m Model) centred(text string, tok theme.Token) string {
	text = ansi.Truncate(text, m.w, "…")
	pad := (m.w - ansi.StringWidth(text)) / 2
	lines := make([]string, m.h)
	lines[(m.h-1)/2] = strings.Repeat(" ", pad) + m.opts.Theme.Fg(tok).Render(text)
	return strings.Join(lines, "\n")
}

// Zones are the hit zones of the frame View draws.
func (m Model) Zones() []layout.Zone { return m.scene().Zones }

// scene is the one layout pass View draws and the mouse hits: the frame's
// geometry, each tile's content, and the zones of the Item rows and key
// hints placed in it.
type scene struct {
	layout.Frame
	// tiles are drawn at Frame.Tiles' places.
	tiles []render.Tile
}

func (m Model) scene() scene {
	s := scene{Frame: m.frame()}
	if s.TooSmall {
		return s
	}
	for _, t := range s.Tiles {
		tile := render.Tile{
			Title:   t.Title,
			W:       t.W,
			H:       t.H,
			Focused: t.ID == m.focus,
			Arrange: m.arranging && t.ID == m.focus,
			Handle:  t.Handle,
			Payload: m.payloadIn(t.ID, t.Rect),
			List:    m.lists[t.ID].render(),
			Status:  m.chrome(t.ID),
		}
		if pic, ok := m.images.shown[t.ID]; ok {
			tile.Image = &pic
		}
		switch {
		case m.editing != nil && t.ID == m.editing.widget:
			tile.Payload, tile.Hints = widget.Payload{}, []render.Hint{{Key: "esc", Label: "save"}}
		case tile.Focused && !m.arranging:
			tile.Hints = m.hints(t.ID)
			for i, at := range render.HintSlots(tile.Hints, t.W) {
				s.Zones = append(s.Zones, layout.Zone{Kind: layout.Hint, ID: t.ID, Index: i, Rect: layout.Rect{X: t.X + at.X, Y: t.Y + t.H - 1, W: at.W, H: 1}})
			}
		}
		if tile.Payload.IsList() {
			rows := render.Rows(tile.Payload)
			n := itemRows(tile.Payload, t.Rect)
			top := t.Y + t.H - 1 - n
			for i := range n {
				if j := tile.List.Top + i; j < len(rows) && rows[j] >= 0 {
					s.Zones = append(s.Zones, layout.Zone{Kind: layout.Item, ID: t.ID, Index: rows[j], Rect: layout.Rect{X: t.X + 1, Y: top + i, W: t.W - 2, H: 1}})
				}
			}
		}
		s.tiles = append(s.tiles, tile)
	}
	return s
}

func (m Model) frame() layout.Frame {
	f := layout.Compute(m.layout, m.titles(), m.w, m.h-footerHeight, m.pointer())
	f.Need.H += footerHeight
	return f
}

func (m Model) titles() map[string]string {
	titles := make(map[string]string, len(m.widgets))
	for id, w := range m.widgets {
		titles[id] = w.title
	}
	return titles
}
