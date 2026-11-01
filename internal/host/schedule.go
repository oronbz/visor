package host

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/cache"
	"github.com/oronbz/visor/internal/widget"
)

const (
	firstBackoff = 15 * time.Second
	maxBackoff   = 15 * time.Minute
	// beat is how often the host checks the wall clock for a jump, such as a wake from sleep.
	beat = 10 * time.Second
	// wakeSlack is how late a timer may fire before the host takes it for a wake from sleep.
	wakeSlack = 30 * time.Second
	stagger   = 3 * time.Second
	// jitter is the most a fetching Widget's schedule moves either way, as a share of it.
	jitter = 0.1
)

// sched is a Source's refresh schedule and the outcome of its last refresh,
// from which its Status follows.
type sched struct {
	// interval zero refreshes only on demand (and at midnight for types that ask).
	interval time.Duration
	due      time.Time
	// timer tells the armed timer from ones a newer plan, or a Widget
	// rebuilt under the same id, replaced.
	timer int
	// again asks for another refresh once the one in flight lands.
	busy, again bool
	fails       int
	// fetched is zero while there's no data yet.
	fetched time.Time
	err     error
	setup   *widget.SetupError
	// key is "" for a Source that isn't cached; checked is when its last
	// refresh started, here or in another instance.
	key     string
	checked time.Time
	// cached is data read from the cache at start that no refresh has replaced yet.
	cached bool
	// waiting is a refresh another instance is making, spun here too until
	// its entry lands or this instance tries again.
	waiting bool
	// written is when the newest entry this instance knows of was checked,
	// whichever instance wrote it; pending is the push held back until
	// pushWrite after it, and flush its armed timer.
	written   time.Time
	pending   *widget.Push
	pendingAt time.Time
	flush     int
}

type refreshedMsg struct {
	id      string
	payload widget.Payload
	err     error
	at      time.Time
	// skipped is a refresh another instance's fetch lock held off.
	skipped bool
	// entry is what another instance fetched meanwhile, taken instead of fetching.
	entry *cache.Entry
}

// dueMsg's want is when the timer was meant to fire.
type dueMsg struct {
	id       string
	seq      int
	want, at time.Time
}

type beatMsg struct{ want, at time.Time }

type spinMsg struct{ at time.Time }

type startMsg struct{}

const spinEvery = 100 * time.Millisecond

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m Model) spinner() string {
	return spinFrames[int(m.now.UnixMilli()/spinEvery.Milliseconds())%len(spinFrames)]
}

func (m Model) source(id string) (widget.Source, bool) {
	src, ok := m.widgets[id].widget.(widget.Source)
	return src, ok
}

// start refreshes Source id now, whatever its schedule; a refresh already
// in flight is followed by another.
func (m Model) start(id string) (Model, tea.Cmd) { return m.begin(id, true) }

// whenDue refreshes Source id unless its cached data, which another
// instance may have fetched, isn't due yet; then it plans the refresh.
func (m Model) whenDue(id string) (Model, tea.Cmd) { return m.begin(id, false) }

func (m Model) begin(id string, force bool) (Model, tea.Cmd) {
	src, ok := m.source(id)
	if !ok {
		return m, nil
	}
	s := m.sched[id]
	if s.busy {
		s.again = s.again || force
		m.sched[id] = s
		return m, nil
	}
	now := m.opts.Ports.Clock.Now()
	if !force && m.fresh(id, now) {
		return m, m.plan(id, m.next(id, s.checked))
	}
	s.busy, s.waiting, s.due = true, false, time.Time{}
	s.timer = m.nextTimer()
	m.sched[id] = s
	fetch := func() tea.Msg {
		p, err := src.Refresh(context.Background(), now)
		return refreshedMsg{id: id, payload: p, err: err, at: now}
	}
	if s.key != "" {
		fetch = m.fetchCached(id, src, now, force)
	}
	if m.spinning {
		return m, fetch
	}
	m.spinning = true
	return m, tea.Batch(fetch, m.opts.Ports.Clock.After(spinEvery, func(t time.Time) tea.Msg { return spinMsg{t} }))
}

// startAll refreshes every visible Source, except those waiting out a rate
// limit; unless forced, cached data that isn't due yet is kept.
func (m Model) startAll(force bool) (Model, tea.Cmd) {
	now := m.opts.Ports.Clock.Now()
	var cmds []tea.Cmd
	for _, id := range m.ids() {
		if _, ok := m.source(id); !ok || m.layout.IsHidden(id) || m.pausedUntil(id).After(now) {
			continue
		}
		var cmd tea.Cmd
		m, cmd = m.begin(id, force)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) spun(msg spinMsg) (Model, tea.Cmd) {
	m.now = msg.at
	again := func() tea.Cmd {
		return m.opts.Ports.Clock.After(spinEvery, func(t time.Time) tea.Msg { return spinMsg{t} })
	}
	if m.menu != nil && m.menu.loading {
		return m, again()
	}
	for _, s := range m.sched {
		if s.busy || s.waiting {
			return m, again()
		}
	}
	m.spinning = false
	return m, nil
}

func (m Model) plan(id string, at time.Time) tea.Cmd {
	s := m.sched[id]
	s.due = at
	s.timer = m.nextTimer()
	m.sched[id] = s
	seq := s.timer
	return m.opts.Ports.Clock.After(at.Sub(m.opts.Ports.Clock.Now()), func(t time.Time) tea.Msg {
		return dueMsg{id: id, seq: seq, want: at, at: t}
	})
}

func (m Model) due(msg dueMsg) (Model, tea.Cmd) {
	m.now = msg.at
	if m.sched[msg.id].timer != msg.seq {
		return m, nil
	}
	if msg.at.Sub(msg.want) > wakeSlack {
		return m.woke()
	}
	// A hidden Widget stays overdue, and refreshes once it's shown.
	if m.layout.IsHidden(msg.id) {
		return m, nil
	}
	return m.whenDue(msg.id)
}

// shown refreshes Source id if it fell due while hidden.
func (m Model) shown(id string) (Model, tea.Cmd) {
	s, ok := m.sched[id]
	switch {
	case !ok || s.busy:
		return m, nil
	case s.key != "":
		return m.whenDue(id)
	case !s.fetched.IsZero() && (s.due.IsZero() || s.due.After(m.opts.Ports.Clock.Now())):
		return m, nil
	}
	return m.start(id)
}

func (m Model) beat() (Model, tea.Cmd) {
	if m.checkingWallClock {
		return m, nil
	}
	for id, s := range m.sched {
		if s.interval > 0 || m.widgets[id].typ.Midnight {
			m.checkingWallClock = true
			return m, m.nextBeat(m.opts.Ports.Clock.Now())
		}
	}
	return m, nil
}

func (m Model) nextBeat(now time.Time) tea.Cmd {
	want := now.Add(beat)
	return m.opts.Ports.Clock.After(beat, func(t time.Time) tea.Msg { return beatMsg{want: want, at: t} })
}

func (m Model) beaten(msg beatMsg) (Model, tea.Cmd) {
	m.now = msg.at
	if msg.at.Sub(msg.want) <= wakeSlack {
		return m, m.nextBeat(msg.at)
	}
	m, cmd := m.woke()
	return m, tea.Batch(cmd, m.nextBeat(msg.at))
}

// woke handles a jump of the wall clock, such as a wake from sleep: every
// Widget that fell due meanwhile refreshes once, a few seconds apart, and
// the others keep their plans.
func (m Model) woke() (Model, tea.Cmd) {
	now := m.opts.Ports.Clock.Now()
	var overdue []string
	var cmds []tea.Cmd
	for _, id := range m.ids() {
		s := m.sched[id]
		switch {
		case s.busy || s.due.IsZero():
		case !s.due.After(now) && !m.layout.IsHidden(id):
			overdue = append(overdue, id)
		default:
			cmds = append(cmds, m.plan(id, s.due))
		}
	}
	slices.SortStableFunc(overdue, func(a, b string) int { return m.sched[a].due.Compare(m.sched[b].due) })
	for i, id := range overdue {
		if i == 0 {
			var cmd tea.Cmd
			m, cmd = m.whenDue(id)
			cmds = append(cmds, cmd)
			continue
		}
		cmds = append(cmds, m.plan(id, now.Add(time.Duration(i)*stagger)))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) refreshed(msg refreshedMsg) (Model, tea.Cmd) {
	if _, ok := m.sched[msg.id]; !ok {
		// The Widget was removed while it refreshed.
		return m, nil
	}
	now := m.opts.Ports.Clock.Now()
	m.now = now
	s := m.sched[msg.id]
	s.busy = false
	var limited *widget.RateLimitError
	switch {
	case msg.skipped:
		s.waiting = true
	case msg.entry != nil:
		m.sched[msg.id] = s
		m.adopt(msg.id, *msg.entry)
		s = m.sched[msg.id]
		limited = s.limited()
	default:
		var fetched time.Time
		if msg.err == nil {
			fetched = now
		}
		s, limited = m.settle(msg.id, s, msg.payload, fetched, msg.err)
		s.checked = msg.at
		if s.key != "" && !local(msg.err) {
			s.written = msg.at
		}
	}
	m.sched[msg.id] = s
	m.clampList(msg.id)
	if s.again {
		s.again = false
		m.sched[msg.id] = s
		return m.start(msg.id)
	}
	var cmds []tea.Cmd
	switch {
	case msg.skipped:
		// Another instance is fetching; its entry arrives as a file change, and
		// should it never come, the lock is abandoned by the time this fires.
		cmds = append(cmds, m.plan(msg.id, now.Add(fetchTimeout)))
	case msg.entry != nil:
		cmds = append(cmds, m.plan(msg.id, m.next(msg.id, msg.entry.CheckedAt)))
	default:
		if at := m.next(msg.id, now); !at.IsZero() {
			cmds = append(cmds, m.plan(msg.id, at))
		}
	}
	if limited != nil {
		// The pause covers every Widget on the same credential.
		for _, id := range m.ids() {
			if o := m.sched[id]; id != msg.id && !o.due.IsZero() && o.due.Before(m.pausedUntil(id)) {
				cmds = append(cmds, m.plan(id, m.pausedUntil(id)))
			}
		}
	}
	return m, tea.Batch(cmds...)
}

// settle records in s a refresh's outcome: data fetched at fetched, zero
// when there is none new, and err. A rate limit pauses s's credential, and
// is returned so the other Widgets on it can be replanned.
func (m Model) settle(id string, s sched, p widget.Payload, fetched time.Time, err error) (sched, *widget.RateLimitError) {
	var setup *widget.SetupError
	var limited *widget.RateLimitError
	switch {
	case err == nil:
		s.err, s.fails, s.setup = nil, 0, nil
	case errors.As(err, &setup):
		s.err, s.fails, s.setup = nil, 0, setup
	case errors.As(err, &limited):
		s.err, s.setup = err, nil
		if c, ok := m.widgets[id].widget.(widget.Credentialed); ok && limited.Until.After(m.paused[c.Credential()]) {
			m.paused[c.Credential()] = limited.Until
		}
	default:
		s.err, s.setup = err, nil
		s.fails++
	}
	if !fetched.IsZero() {
		s.fetched, s.cached = fetched, false
		m.fetched[id] = p
		st := m.lists[id]
		if i := slices.IndexFunc(p.Items, func(it widget.Item) bool { return it.ID == st.sel }); i >= 0 {
			st.idx = i
		}
		m.lists[id] = st
	}
	return s, limited
}

func (s sched) limited() *widget.RateLimitError {
	var limited *widget.RateLimitError
	errors.As(s.err, &limited)
	return limited
}

// next is when Source id refreshes after a refresh at now: an interval on,
// sooner after a failure, never before a rate limit lifts, and at midnight
// for types that ask.
func (m Model) next(id string, now time.Time) time.Time {
	s, typ := m.sched[id], m.widgets[id].typ
	var at time.Time
	switch {
	case s.err != nil:
		at = now.Add(m.jittered(typ, backoff(s.fails)))
	case s.interval > 0:
		at = now.Add(m.jittered(typ, s.interval))
	}
	if typ.Midnight {
		y, mo, d := now.Date()
		if mid := time.Date(y, mo, d+1, 0, 0, 0, 0, now.Location()); at.IsZero() || mid.Before(at) {
			at = mid
		}
	}
	p := m.pausedUntil(id)
	var limited *widget.RateLimitError
	if errors.As(s.err, &limited) && limited.Until.After(p) {
		p = limited.Until
	}
	if !at.IsZero() && p.After(at) {
		at = p
	}
	return at
}

func backoff(fails int) time.Duration {
	d := firstBackoff
	for i := 1; i < fails && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

// jittered spreads fetching Widgets' schedules so instances and Widgets
// started together drift apart.
func (m Model) jittered(typ widget.Type, d time.Duration) time.Duration {
	if !typ.Fetches {
		return d
	}
	return time.Duration(float64(d) * (1 - jitter + 2*jitter*m.rng.Float64()))
}

func (m Model) pausedUntil(id string) time.Time {
	if c, ok := m.widgets[id].widget.(widget.Credentialed); ok {
		return m.paused[c.Credential()]
	}
	return time.Time{}
}

func newRand(now time.Time) *rand.Rand {
	return rand.New(rand.NewPCG(uint64(now.UnixNano()), 0x9e3779b97f4a7c15))
}

// schedule sets up Source id's refresh schedule; problem says when its
// `refresh` was clamped. Other Widgets have none.
func (m Model) schedule(id string, refresh time.Duration) (problem string) {
	p := m.widgets[id]
	if _, ok := p.widget.(widget.Source); !ok {
		return ""
	}
	iv, problem := interval(p, refresh)
	s := sched{interval: iv, key: m.cacheKey(id, p)}
	m.sched[id] = s
	if e, ok := m.cache.Load(s.key); ok && s.key != "" {
		m.adopt(id, e)
		s = m.sched[id]
		s.cached = !s.fetched.IsZero()
		m.sched[id] = s
	}
	return problem
}

func (m Model) nextTimer() int {
	*m.timers++
	return *m.timers
}

// interval is how often Source p refreshes: its `refresh` setting, else its
// own interval, else its type's, clamped to the type's floor; problem says
// when it was clamped.
func interval(p placed, refresh time.Duration) (d time.Duration, problem string) {
	d = p.typ.Refresh
	if s, ok := p.widget.(widget.Scheduled); ok && s.Interval() > 0 {
		d = s.Interval()
	}
	if refresh > 0 {
		d = refresh
	}
	if d > 0 && d < p.typ.Floor {
		return p.typ.Floor, fmt.Sprintf("%s: refresh %s is below the %s minimum; using %s", p.title, widget.ShortDuration(d), p.typ.Name, widget.ShortDuration(p.typ.Floor))
	}
	return d, ""
}
