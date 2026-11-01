package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/cache"
	"github.com/oronbz/visor/internal/widget"
)

const (
	// fetchTimeout bounds a fetching Widget's refresh; a fetch lock older than it was abandoned.
	fetchTimeout = time.Minute
	// keepOrphans is how long the entries of Widgets no longer configured are kept.
	keepOrphans = 30 * 24 * time.Hour
)

func cacheStore(o Options) cache.Store {
	return cache.Store{Dir: filepath.Join(o.Paths.Cache, "widgets")}
}

// cacheKey is Widget p's cache key, or "" for Widgets that aren't cached:
// only those that fetch are.
func (m Model) cacheKey(id string, p placed) string {
	if _, ok := p.widget.(widget.Source); !ok || !p.typ.Fetches {
		return ""
	}
	return cache.Key(id, p.typ.Name, m.version(), p.block.Settings)
}

// freshUntil is when an entry last checked at checked, after fails failed
// fetches, falls due. It allows for the jitter on schedules, so an instance
// whose timer fires early still finds the entry another one just fetched fresh.
func freshUntil(checked time.Time, fails int, interval time.Duration, midnight bool) time.Time {
	if checked.IsZero() || interval == 0 && fails == 0 {
		return time.Time{}
	}
	wait := interval
	if fails > 0 {
		wait = backoff(fails)
	}
	at := checked.Add(time.Duration(float64(wait)*(1-jitter)) - time.Second)
	if midnight {
		y, mo, d := checked.Date()
		if mid := time.Date(y, mo, d+1, 0, 0, 0, 0, checked.Location()); mid.Before(at) {
			at = mid
		}
	}
	return at
}

func (m Model) fresh(id string, now time.Time) bool {
	s := m.sched[id]
	return s.key != "" && now.Before(freshUntil(s.checked, s.fails, s.interval, m.widgets[id].typ.Midnight))
}

// fetchCached refreshes a cached Source unless another instance holds its
// fetch lock, or fetched it meanwhile, and records the outcome in its entry.
func (m Model) fetchCached(id string, src widget.Source, now time.Time, force bool) tea.Cmd {
	s, store := m.sched[id], m.cache
	key, interval, midnight := s.key, s.interval, m.widgets[id].typ.Midnight
	return func() tea.Msg {
		unlock, ok := store.Lock(key, now, fetchTimeout)
		if !ok {
			return refreshedMsg{id: id, at: now, skipped: true}
		}
		defer unlock()
		e, _ := store.Load(key)
		if !force && now.Before(freshUntil(e.CheckedAt, e.Fails, interval, midnight)) {
			return refreshedMsg{id: id, at: now, entry: &e}
		}
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		p, err := src.Refresh(ctx, now)
		if !local(err) {
			// The cache only saves a fetch; failing to write it is no reason to drop the data.
			_ = store.Save(key, record(e, p, err, now))
		}
		return refreshedMsg{id: id, payload: p, err: err, at: now}
	}
}

func record(e cache.Entry, p widget.Payload, err error, now time.Time) cache.Entry {
	e.CheckedAt, e.Status = now, statusOf(err)
	var setup *widget.SetupError
	var limited *widget.RateLimitError
	switch {
	case err == nil:
		e.Payload, e.FetchedAt, e.Fails = p, now, 0
	case errors.As(err, &setup):
		e.Fails = 0
	case !errors.As(err, &limited):
		e.Fails++
	}
	return e
}

// pushWrite is the least time between writes of one Widget's pushes to its
// entry, across instances. Every instance gets the pushes itself; the entry
// only has to be recent enough for an instance starting up.
const pushWrite = 10 * time.Second

type pushFlushMsg struct {
	id  string
	seq int
}

// cachePush leaves out updates about this instance alone, and ones that
// change nothing, and holds back a push due within pushWrite of the entry's
// last write until then, writing only the latest.
func (m Model) cachePush(id string, s sched, push widget.Push, now time.Time) (sched, tea.Cmd) {
	if s.key == "" || push.Lost || local(push.Err) || push.Payload == nil && push.Err == nil {
		return s, nil
	}
	s.checked = now
	s.pending, s.pendingAt = &push, now
	if s.flush != 0 {
		return s, nil
	}
	if wait := s.written.Add(pushWrite).Sub(now); wait > 0 {
		return m.flushIn(id, s, wait)
	}
	return m.writePush(s)
}

func (m Model) flushIn(id string, s sched, wait time.Duration) (sched, tea.Cmd) {
	s.flush = m.nextTimer()
	seq := s.flush
	return s, m.opts.Ports.Clock.After(wait, func(time.Time) tea.Msg { return pushFlushMsg{id: id, seq: seq} })
}

func (m Model) flushPush(msg pushFlushMsg) (Model, tea.Cmd) {
	s, ok := m.sched[msg.id]
	if !ok || s.flush != msg.seq {
		return m, nil
	}
	s.flush = 0
	var cmd tea.Cmd
	// Another instance may have written the entry meanwhile.
	if wait := s.written.Add(pushWrite).Sub(m.opts.Ports.Clock.Now()); wait > 0 {
		s, cmd = m.flushIn(msg.id, s, wait)
	} else {
		s, cmd = m.writePush(s)
	}
	m.sched[msg.id] = s
	return m, cmd
}

func (m Model) writePush(s sched) (sched, tea.Cmd) {
	push, now := *s.pending, s.pendingAt
	s.pending, s.written = nil, now
	var p widget.Payload
	if push.Payload != nil {
		p = *push.Payload
	}
	store, key := m.cache, s.key
	return s, func() tea.Msg {
		// A held lock is a fetch in flight in another instance, which writes the entry.
		unlock, ok := store.Lock(key, now, fetchTimeout)
		if !ok {
			return nil
		}
		defer unlock()
		e, _ := store.Load(key)
		if e.CheckedAt.After(now) {
			return nil
		}
		if n := record(e, p, push.Err, now); !same(e, n) {
			_ = store.Save(key, n)
		}
		return nil
	}
}

// same ignores when the entries were fetched or checked.
func same(a, b cache.Entry) bool {
	a.FetchedAt, a.CheckedAt, a.Fails = b.FetchedAt, b.CheckedAt, b.Fails
	x, errX := json.Marshal(a)
	y, errY := json.Marshal(b)
	return errX == nil && errY == nil && bytes.Equal(x, y)
}

// adopt takes Source id's data and Status from its cache entry: written by
// another instance, or left from an earlier run.
func (m Model) adopt(id string, e cache.Entry) {
	s := m.sched[id]
	var fetched time.Time
	if e.FetchedAt.After(s.fetched) {
		fetched = e.FetchedAt
	}
	s, _ = m.settle(id, s, e.Payload, fetched, errOf(e.Status))
	s.checked, s.fails, s.waiting = e.CheckedAt, e.Fails, false
	s.written = later(s.written, e.CheckedAt)
	m.sched[id] = s
}

// adoptChanged adopts the newer entries other instances wrote for the
// Widgets whose cache files changed, and replans their refreshes.
func (m Model) adoptChanged(files []string) (Model, []tea.Cmd) {
	var cmds []tea.Cmd
	for _, id := range m.ids() {
		s := m.sched[id]
		if s.key == "" || s.busy || !slices.Contains(files, m.cache.File(s.key)) {
			continue
		}
		e, ok := m.cache.Load(s.key)
		if !ok {
			continue
		}
		s.written = later(s.written, e.CheckedAt)
		m.sched[id] = s
		if !e.CheckedAt.After(s.checked) {
			continue
		}
		m.adopt(id, e)
		m.clampList(id)
		cmds = append(cmds, m.plan(id, m.next(id, e.CheckedAt)))
	}
	return m, cmds
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (m Model) cleanCache() tea.Cmd {
	var keep []string
	for _, s := range m.sched {
		if s.key != "" {
			keep = append(keep, s.key)
		}
	}
	store, cutoff := m.cache, m.opts.Ports.Clock.Now().Add(-keepOrphans)
	return func() tea.Msg {
		_ = store.Clean(keep, cutoff)
		return nil
	}
}

// failure is a failed fetch read back from the cache.
type failure struct{ msg, reason, hint string }

func (f failure) Error() string { return f.msg }

func (f failure) Reason() string {
	if f.reason == "" {
		return reasonOf(errors.New(f.msg))
	}
	return f.reason
}

func (f failure) Hint() string { return f.hint }

func local(err error) bool {
	var l *widget.LocalError
	return errors.As(err, &l)
}

func statusOf(err error) cache.Status {
	if err == nil {
		return cache.Status{}
	}
	var setup *widget.SetupError
	if errors.As(err, &setup) {
		return cache.Status{Error: setup.Hint, Setup: true, Action: setup.Action}
	}
	st := cache.Status{Error: err.Error(), Reason: reasonOf(err), Hint: hintOf(err)}
	var limited *widget.RateLimitError
	if errors.As(err, &limited) {
		st.Until = limited.Until
	}
	return st
}

func errOf(st cache.Status) error {
	switch {
	case st.Setup:
		return &widget.SetupError{Hint: st.Error, Action: st.Action}
	case !st.Until.IsZero():
		return &widget.RateLimitError{Until: st.Until, Err: failure{msg: st.Error, hint: st.Hint}}
	case st.Error != "":
		return failure{msg: st.Error, reason: st.Reason, hint: st.Hint}
	}
	return nil
}
