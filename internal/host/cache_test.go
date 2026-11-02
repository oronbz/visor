package host_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oronbz/visor/internal/cache"
	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
	"github.com/oronbz/visor/internal/widgets/meetings"
	"github.com/oronbz/visor/internal/widgets/prs"
	"github.com/oronbz/visor/internal/widgets/reminders"
)

// upstream is an API that several hosts' News Widgets fetch from; every
// fetch gets the next edition.
type upstream struct {
	mu      sync.Mutex
	fetches int
	// during, when set, runs inside a fetch, while it's in flight.
	during func()
	// down fails every fetch with it.
	down error
}

func (u *upstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.fetches
}

type news struct {
	up    *upstream
	topic string
}

func (n news) Refresh(context.Context, time.Time) (widget.Payload, error) {
	u := n.up
	u.mu.Lock()
	u.fetches++
	edition, during, down := u.fetches, u.during, u.down
	u.mu.Unlock()
	if during != nil {
		during()
	}
	if down != nil {
		return widget.Payload{}, down
	}
	return widget.Payload{Lines: []widget.Line{widget.Text(theme.Fg, fmt.Sprintf("%s edition %d", n.topic, edition))}}, nil
}

// newsType is a fetching Widget type refreshing every 2m.
func newsType(u *upstream) widget.Type {
	return widget.Type{
		Name: "news", Title: "News", Refresh: 2 * time.Minute, Floor: 30 * time.Second, Fetches: true,
		Settings: []widget.Setting{{Name: "topic", Kind: widget.String, Default: "world", Description: "What the news is about."}},
		New: func(c widget.Config) widget.Widget {
			return news{up: u, topic: c.Settings.String("topic")}
		},
	}
}

// newsHost is a host showing one News Widget, its cache under root.
func newsHost(t *testing.T, u *upstream, root string, now time.Time) *hosttest.Host {
	t.Helper()
	return hosttest.New(t, hosttest.Options{Root: root, Now: now, Width: 60, Height: 8, Types: []widget.Type{newsType(u)}})
}

func TestTheFirstFrameDrawsTheCachedEntryWithItsAgeUntilFreshDataLands(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	newsHost(t, u, root, hosttest.DefaultNow)

	h := newsHost(t, u, root, hosttest.DefaultNow.Add(time.Minute))
	if u.count() != 1 {
		t.Fatalf("fetched %d times, want 1: a second instance a minute later draws the cached entry", u.count())
	}
	if top, body := h.Line(0), h.Screen(); !strings.Contains(body, "world edition 1") || !strings.HasSuffix(top, " 1m ─╮") {
		t.Errorf("first frame:\n%s\nwant the cached edition 1 and its age 1m at the right of the top border", body)
	}

	h.Advance(2 * time.Minute)
	if top, body := h.Line(0), h.Screen(); !strings.Contains(body, "world edition 2") || strings.Contains(top, "m ─╮") {
		t.Errorf("after the next refresh:\n%s\nwant edition 2 and no age", body)
	}
}

// entryFile is the one cache entry under root.
func entryFile(t *testing.T, root string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(paths.Under(root).Cache, "widgets", "*", "entry.json"))
	if len(files) != 1 {
		t.Fatalf("cache entries %v, want one", files)
	}
	return files[0]
}

func TestTwoInstancesOnOneCacheFetchADueWidgetOnceAndBothShowIt(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	a := newsHost(t, u, root, hosttest.DefaultNow)
	b := newsHost(t, u, root, hosttest.DefaultNow)
	if u.count() != 1 {
		t.Fatalf("two instances starting together fetched %d times, want 1", u.count())
	}
	for name, h := range map[string]*hosttest.Host{"a": a, "b": b} {
		if !strings.Contains(h.Screen(), "world edition 1") {
			t.Errorf("%s shows:\n%s\nwant edition 1", name, h.Screen())
		}
	}

	a.Advance(3 * time.Minute)
	b.Changed(entryFile(t, root))
	if !strings.Contains(b.Screen(), "world edition 2") {
		t.Errorf("b shows:\n%s\nwant edition 2 adopted from a's fetch", b.Screen())
	}
	b.Advance(3 * time.Minute)
	if u.count() != 2 {
		t.Errorf("fetched %d times once due again, want 2", u.count())
	}
	if !strings.Contains(a.Screen(), "world edition 2") {
		t.Errorf("a shows:\n%s\nwant edition 2", a.Screen())
	}
}

func TestAFailedFetchInOneInstanceMarksTheWidgetStaleInBoth(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	a := newsHost(t, u, root, hosttest.DefaultNow)
	b := newsHost(t, u, root, hosttest.DefaultNow)
	u.down = offline

	a.Advance(3 * time.Minute)
	tries := u.count()
	b.Changed(entryFile(t, root))
	b.Advance(3 * time.Minute)
	if u.count() != tries {
		t.Errorf("b fetched %d times, want none: it takes a's failure and backs off with it", u.count()-tries)
	}
	for name, h := range map[string]*hosttest.Host{"a": a, "b": b} {
		if top := h.Line(0); !strings.Contains(top, "offline · 3m") || !strings.Contains(h.Screen(), "world edition 1") {
			t.Errorf("%s shows:\n%s\nwant edition 1 marked offline · 3m", name, h.Screen())
		}
	}
}

func TestCtrlRFetchesEvenWhenTheCachedEntryIsFresh(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	newsHost(t, u, root, hosttest.DefaultNow)
	h := newsHost(t, u, root, hosttest.DefaultNow.Add(time.Minute))

	h.Press("ctrl+r")
	if u.count() != 2 || !strings.Contains(h.Screen(), "world edition 2") {
		t.Errorf("fetched %d times after ^R, want 2; screen:\n%s", u.count(), h.Screen())
	}
}

func TestAnInstanceWhoseTimerFiresFirstFetchesAndTheOtherTakesItsEntry(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	a := newsHost(t, u, root, hosttest.DefaultNow)
	b := newsHost(t, u, root, hosttest.DefaultNow)

	// b fetches first; a's timer then finds the entry fresh without being told it changed.
	b.Advance(3 * time.Minute)
	a.Advance(3 * time.Minute)
	if u.count() != 2 {
		t.Errorf("fetched %d times, want 2", u.count())
	}
	if !strings.Contains(a.Screen(), "world edition 2") {
		t.Errorf("a shows:\n%s\nwant edition 2", a.Screen())
	}
}

func TestChangingAWidgetsSettingsUsesANewCacheEntry(t *testing.T) {
	u := &upstream{}
	h := hosttest.New(t, hosttest.Options{Width: 60, Height: 8, Types: []widget.Type{newsType(u)},
		Config: "[[widget]]\ntype = \"news\"\ntopic = \"tech\"\n"})

	writeConfig(t, h, "[[widget]]\ntype = \"news\"\ntopic = \"sport\"\n")
	if u.count() != 2 || !strings.Contains(h.Screen(), "sport edition 2") {
		t.Errorf("fetched %d times after the topic changed, want 2; screen:\n%s", u.count(), h.Screen())
	}
	if files, _ := filepath.Glob(filepath.Join(h.Paths.Cache, "widgets", "*", "entry.json")); len(files) != 2 {
		t.Errorf("cache entries %v, want one per topic", files)
	}

	writeConfig(t, h, "[[widget]]\ntype = \"news\"\ntopic = \"tech\"\n")
	if u.count() != 2 || !strings.Contains(h.Screen(), "tech edition 1") {
		t.Errorf("fetched %d times after the topic changed back, want 2: the tech entry is fresh; screen:\n%s", u.count(), h.Screen())
	}
}

func TestCacheEntriesAreReadableOnlyByTheUser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits")
	}
	root := t.TempDir()
	newsHost(t, &upstream{}, root, hosttest.DefaultNow)

	info, err := os.Stat(entryFile(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("entry permissions %v, want 0600", perm)
	}
}

func TestStartRemovesEntriesOfNoConfiguredWidgetAndLeftoverWritesOnceTheyreOver30DaysOld(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	newsHost(t, u, root, hosttest.DefaultNow)
	configured := filepath.Dir(entryFile(t, root))
	dir := filepath.Dir(configured)
	old, recent := filepath.Join(dir, "gone-0123456789ab"), filepath.Join(dir, "moved-0123456789ab")
	flat := filepath.Join(dir, "news-0123456789ab.json")
	staleWrite, liveWrite := filepath.Join(configured, "entry.1.tmp"), filepath.Join(configured, "entry.2.tmp")
	ages := map[string]time.Duration{configured: 40 * 24 * time.Hour, old: 31 * 24 * time.Hour, recent: 29 * 24 * time.Hour,
		flat: 31 * 24 * time.Hour, staleWrite: 31 * 24 * time.Hour, liveWrite: time.Second}
	for f, age := range ages {
		switch f {
		case old, recent:
			if err := os.MkdirAll(f, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f, "entry.json"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		case flat, staleWrite, liveWrite:
			if err := os.WriteFile(f, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chtimes(f, hosttest.DefaultNow.Add(-age), hosttest.DefaultNow.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}

	newsHost(t, u, root, hosttest.DefaultNow.Add(time.Minute))
	for f, want := range map[string]bool{configured: true, old: false, recent: true, flat: false, staleWrite: false, liveWrite: true} {
		if _, err := os.Stat(f); (err == nil) != want {
			t.Errorf("%s exists: %v, want %v", filepath.Base(f), err == nil, want)
		}
	}
}

// lockEntry takes root's one cache entry's fetch lock at at, as an instance
// that is still running but never finishes its fetch would.
func lockEntry(t *testing.T, root string, at time.Time) {
	t.Helper()
	dir := filepath.Dir(entryFile(t, root))
	store := cache.Store{Dir: filepath.Dir(dir)}
	if _, ok := store.Lock(filepath.Base(dir), at, time.Minute); !ok {
		t.Fatal("the entry's lock is taken")
	}
}

// lockEntryByExited leaves root's one cache entry's fetch lock taken at at by
// a process that has exited, as an instance killed mid-fetch does.
func lockEntryByExited(t *testing.T, root string, at time.Time) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("visor can't tell an exited lock owner on Windows")
	}
	exited := exec.Command("true")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(filepath.Dir(entryFile(t, root)), "lock")
	content := at.Format(time.RFC3339Nano) + " " + strconv.Itoa(exited.Process.Pid)
	if err := os.WriteFile(lock, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestALockLeftByAnExitedInstanceIsTakenOverAtOnce(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	newsHost(t, u, root, hosttest.DefaultNow)
	at := hosttest.DefaultNow.Add(2*time.Minute + 30*time.Second)
	lockEntryByExited(t, root, at.Add(-time.Second))

	h := newsHost(t, u, root, at)
	if u.count() != 2 || !strings.Contains(h.Screen(), "world edition 2") {
		t.Errorf("fetched %d times past a second-old lock whose owner exited, want 2; screen:\n%s", u.count(), h.Screen())
	}
}

func TestAWidgetSpinsWhileAnotherInstanceFetchesIt(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	h := newsHost(t, u, root, hosttest.DefaultNow)
	lockEntry(t, root, hosttest.DefaultNow.Add(2*time.Minute))
	spinner := regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏] ─╮$`)

	h.Press("ctrl+r")
	if !spinner.MatchString(h.Line(0)) {
		t.Errorf("while another instance holds the lock, the top border is %q, want a spinner", h.Line(0))
	}
}

func TestALockHeldByAnotherInstanceHoldsOffTheFetchUntilItsAbandoned(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	h := newsHost(t, u, root, hosttest.DefaultNow)
	lockEntry(t, root, hosttest.DefaultNow.Add(90*time.Second))

	h.Advance(2*time.Minute + 30*time.Second)
	if u.count() != 1 {
		t.Errorf("fetched %d times while another instance held the lock, want 1", u.count())
	}
	h.Advance(2 * time.Minute)
	if u.count() != 2 || !strings.Contains(h.Screen(), "world edition 2") {
		t.Errorf("fetched %d times once the lock was over a minute old, want 2; screen:\n%s", u.count(), h.Screen())
	}
}

func TestAnAbandonedLockDoesntBlockFetching(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	h := newsHost(t, u, root, hosttest.DefaultNow)
	lockEntry(t, root, hosttest.DefaultNow)

	h.Advance(2*time.Minute + 30*time.Second)
	if u.count() != 2 || !strings.Contains(h.Screen(), "world edition 2") {
		t.Errorf("fetched %d times past a lock older than the fetch timeout, want 2; screen:\n%s", u.count(), h.Screen())
	}
}

func TestAnInstanceSkipsTheFetchAnotherIsMakingAndShowsItsResult(t *testing.T) {
	root, u := t.TempDir(), &upstream{}
	var b *hosttest.Host
	u.during = func() {
		u.during = nil
		b = newsHost(t, u, root, hosttest.DefaultNow)
	}
	newsHost(t, u, root, hosttest.DefaultNow)
	if u.count() != 1 {
		t.Fatalf("fetched %d times, want 1: b started while a's fetch held the lock", u.count())
	}
	b.Changed(entryFile(t, root))
	if !strings.Contains(b.Screen(), "world edition 1") {
		t.Errorf("b shows:\n%s\nwant edition 1 from a's fetch", b.Screen())
	}
}

// wire is the News Widgets' push channel; each Widget built listens on it.
type wire struct {
	mu   sync.Mutex
	push []func(widget.Push)
}

func (w *wire) send(text string) {
	p := widget.Payload{Lines: []widget.Line{widget.Text(theme.Fg, text)}}
	w.deliver(widget.Push{Payload: &p})
}

func (w *wire) deliver(p widget.Push) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, push := range w.push {
		push(p)
	}
}

type liveNews struct {
	news
	w *wire
}

func (n liveNews) Listen(push func(widget.Push)) {
	n.w.mu.Lock()
	defer n.w.mu.Unlock()
	n.w.push = append(n.w.push, push)
}

// liveNewsHost is newsHost with a News Widget that also takes pushes from w.
func liveNewsHost(t *testing.T, u *upstream, w *wire, root string, now time.Time) *hosttest.Host {
	t.Helper()
	return hosttest.New(t, hosttest.Options{Root: root, Now: now, Width: 60, Height: 8, Types: []widget.Type{liveNewsType(u, w)}})
}

func liveNewsType(u *upstream, w *wire) widget.Type {
	typ := newsType(u)
	typ.New = func(c widget.Config) widget.Widget {
		return liveNews{news{up: u, topic: c.Settings.String("topic")}, w}
	}
	return typ
}

func TestAPushIsWrittenToTheCacheForOtherInstances(t *testing.T) {
	root, u, w := t.TempDir(), &upstream{}, &wire{}
	a := liveNewsHost(t, u, w, root, hosttest.DefaultNow)
	w.send("breaking news")
	a.Receive()
	if !strings.Contains(a.Screen(), "breaking news") {
		t.Fatalf("a shows:\n%s\nwant the pushed payload", a.Screen())
	}
	a.Advance(10 * time.Second)

	b := liveNewsHost(t, u, w, root, hosttest.DefaultNow.Add(time.Minute))
	if !strings.Contains(b.Screen(), "breaking news") {
		t.Errorf("an instance started after the push shows:\n%s\nwant the pushed payload from the cache", b.Screen())
	}
	if u.count() != 1 {
		t.Errorf("fetched %d times, want 1: the push counts as fresh data", u.count())
	}
}

// untouched backdates entry's mtime and returns a check that fails the test
// if entry was written since.
func untouched(t *testing.T, entry string) func(why string) {
	t.Helper()
	old := hosttest.DefaultNow.Add(-time.Hour)
	if err := os.Chtimes(entry, old, old); err != nil {
		t.Fatal(err)
	}
	return func(why string) {
		t.Helper()
		if fi, err := os.Stat(entry); err != nil || !fi.ModTime().Equal(old) {
			t.Errorf("the cache entry was written: %s", why)
		}
	}
}

func TestAPushWhileAnotherInstanceFetchesLeavesTheEntryToThatFetch(t *testing.T) {
	root, u, w := t.TempDir(), &upstream{}, &wire{}
	a := liveNewsHost(t, u, w, root, hosttest.DefaultNow)
	check := untouched(t, entryFile(t, root))
	lockEntry(t, root, hosttest.DefaultNow)

	w.send("breaking news")
	a.Receive()
	if !strings.Contains(a.Screen(), "breaking news") {
		t.Fatalf("a shows:\n%s\nwant the pushed payload", a.Screen())
	}
	check("another instance holds the entry's lock, and its fetch writes it")
}

func TestAPushThatChangesNothingLeavesTheEntryAlone(t *testing.T) {
	root, u, w := t.TempDir(), &upstream{}, &wire{}
	a := liveNewsHost(t, u, w, root, hosttest.DefaultNow)
	w.send("breaking news")
	a.Receive()
	check := untouched(t, entryFile(t, root))

	a.Advance(time.Second)
	w.send("breaking news")
	a.Receive()
	check("the push repeated the payload")
	w.deliver(widget.Push{Retry: hosttest.DefaultNow.Add(time.Hour)})
	a.Receive()
	check("the push only asked for a retry")
}

func entryHas(t *testing.T, root, text, why string) {
	t.Helper()
	if b, err := os.ReadFile(entryFile(t, root)); err != nil || !strings.Contains(string(b), text) {
		t.Errorf("the cache entry lacks %q: %s", text, why)
	}
}

func TestPushesWithinTenSecondsOfTheLastWriteLandInTheCacheTogetherAsTheLatest(t *testing.T) {
	root, u, w := t.TempDir(), &upstream{}, &wire{}
	a := liveNewsHost(t, u, w, root, hosttest.DefaultNow)
	check := untouched(t, entryFile(t, root))

	w.send("first take")
	a.Receive()
	a.Advance(2 * time.Second)
	w.send("second take")
	a.Receive()
	check("the pushes came within 10s of the fetch's write")

	a.Advance(8 * time.Second)
	entryHas(t, root, "second take", "10s after the fetch's write, the latest push lands")
	check = untouched(t, entryFile(t, root))
	a.Advance(time.Second)
	w.send("third take")
	a.Receive()
	a.Advance(8 * time.Second)
	check("the push came within 10s of the last write, however long before it that push came")
	a.Advance(time.Second)
	entryHas(t, root, "third take", "10s after the last write, the next lands")
}

func TestAPushHeldOffByTheEntrysLockLandsOnceTheLockIsFree(t *testing.T) {
	root, u, w := t.TempDir(), &upstream{}, &wire{}
	a := liveNewsHost(t, u, w, root, hosttest.DefaultNow)
	check := untouched(t, entryFile(t, root))
	lockEntry(t, root, hosttest.DefaultNow)

	w.send("breaking news")
	a.Receive()
	a.Advance(10 * time.Second)
	check("another instance held the entry's lock when the push was due")
	if err := os.Remove(filepath.Join(filepath.Dir(entryFile(t, root)), "lock")); err != nil {
		t.Fatal(err)
	}
	a.Advance(10 * time.Second)
	entryHas(t, root, "breaking news", "the lock was free 10s later and no fetch had written the entry")
}

func TestQuittingWritesThePushHeldBack(t *testing.T) {
	root, u, w := t.TempDir(), &upstream{}, &wire{}
	a := liveNewsHost(t, u, w, root, hosttest.DefaultNow)
	w.send("breaking news")
	a.Receive()
	a.Press("q")
	if !a.Quit() {
		t.Fatal("q didn't quit")
	}
	entryHas(t, root, "breaking news", "the push was held back when visor quit")
}

func TestAWidgetRebuiltOntoTheSameEntryStillWritesThePushItHeld(t *testing.T) {
	root, u, w := t.TempDir(), &upstream{}, &wire{}
	a := hosttest.New(t, hosttest.Options{Root: root, Width: 60, Height: 8, Types: []widget.Type{liveNewsType(u, w)},
		Config: "[[widget]]\ntype = \"news\"\n"})
	w.send("breaking news")
	a.Receive()
	writeConfig(t, a, "[[widget]]\ntype = \"news\"\ntitle = \"Headlines\"\n")
	if !strings.Contains(a.Screen(), "Headlines") {
		t.Fatalf("a shows:\n%s\nwant the Widget retitled", a.Screen())
	}
	a.Advance(10 * time.Second)
	entryHas(t, root, "breaking news", "the retitled Widget keeps its entry")
}

func TestAnInstanceHoldsBackItsPushesWithinTenSecondsOfAnotherInstancesWrite(t *testing.T) {
	root, u, wa, wb := t.TempDir(), &upstream{}, &wire{}, &wire{}
	a := liveNewsHost(t, u, wa, root, hosttest.DefaultNow)
	b := liveNewsHost(t, u, wb, root, hosttest.DefaultNow)
	a.Advance(10 * time.Second)
	b.Advance(10 * time.Second)

	wa.send("from a")
	a.Receive()
	entryHas(t, root, "from a", "a's push came 10s after the fetch's write")
	b.Changed(entryFile(t, root))
	check := untouched(t, entryFile(t, root))
	b.Advance(3 * time.Second)
	wb.send("from b")
	b.Receive()
	check("b's push came within 10s of a's write")

	b.Advance(7 * time.Second)
	entryHas(t, root, "from b", "10s after a's write, b's push lands")
}

func TestPRsMeetingsAndRemindersAreCachedWidgets(t *testing.T) {
	granted := map[ports.Entity]ports.Access{ports.Events: ports.AccessGranted, ports.Reminders: ports.AccessGranted}
	for _, typ := range []widget.Type{prs.Type, meetings.Type, reminders.Type} {
		t.Run(typ.Name, func(t *testing.T) {
			h := hosttest.New(t, hosttest.Options{Width: 60, Height: 10, Types: []widget.Type{typ},
				Config:   "[[widget]]\ntype = \"" + typ.Name + "\"\n",
				EventKit: &hosttest.EventKit{Access: granted}})
			entryFile(t, h.Root)
		})
	}
}

// Access to calendars and reminders is granted per terminal app, so an
// instance in another terminal, or the next one in this, checks for itself.
func TestMeetingsAndRemindersLeaveTheirAccessSetupOutOfTheCache(t *testing.T) {
	for _, typ := range []widget.Type{meetings.Type, reminders.Type} {
		t.Run(typ.Name, func(t *testing.T) {
			h := hosttest.New(t, hosttest.Options{Width: 60, Height: 10, Types: []widget.Type{typ},
				Config:   "[[widget]]\ntype = \"" + typ.Name + "\"\n",
				EventKit: &hosttest.EventKit{Access: map[ports.Entity]ports.Access{ports.Events: ports.AccessUndetermined, ports.Reminders: ports.AccessUndetermined}}})
			if !strings.Contains(h.Screen(), "allow access") {
				t.Fatalf("screen:\n%s\nwant the allow access setup", h.Screen())
			}
			files, _ := filepath.Glob(filepath.Join(paths.Under(h.Root).Cache, "widgets", "*.json"))
			if len(files) != 0 {
				t.Errorf("cache entries %v, want none", files)
			}
		})
	}
}
