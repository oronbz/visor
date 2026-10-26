package host_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

// feed is a Source that records when it was refreshed and answers with the
// next scripted error, or with one Item when none is left.
type feed struct{ log *feedLog }

type feedLog struct {
	mu   sync.Mutex
	runs []time.Time
	errs []error
	// during, when set, runs inside Refresh, while the refresh is in flight.
	during func()
}

func (f feed) Refresh(_ context.Context, now time.Time) (widget.Payload, error) {
	f.log.mu.Lock()
	f.log.runs = append(f.log.runs, now)
	var err error
	if len(f.log.errs) > 0 {
		err, f.log.errs = f.log.errs[0], f.log.errs[1:]
	}
	during := f.log.during
	f.log.mu.Unlock()
	if during != nil {
		during()
	}
	if err != nil {
		return widget.Payload{}, err
	}
	return widget.Payload{Columns: []widget.Column{{Flex: true}}, Items: items("", "headline"), Count: 1}, nil
}

func (l *feedLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.runs)
}

// fail makes the next refreshes fail with errs, in order.
func (l *feedLog) fail(errs ...error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, errs...)
}

// feedType is a fetching Widget type refreshing every 2m, never more often than every 30s.
func feedType(name, title string, log *feedLog) widget.Type {
	return widget.Type{
		Name: name, Title: title, Refresh: 2 * time.Minute, Floor: 30 * time.Second,
		New: func(widget.Config) widget.Widget { return feed{log: log} },
	}
}

// withFeed is a host with one Feed Widget whose first refreshes fail with errs.
func withFeed(t *testing.T, o hosttest.Options, errs ...error) (*hosttest.Host, *feedLog) {
	t.Helper()
	log := &feedLog{errs: errs}
	o.Types = append(o.Types, feedType("feed", "Feed", log))
	if o.Width == 0 {
		o.Width, o.Height = 90, 10
	}
	return hosttest.New(t, o), log
}

func TestRefreshBelowTheFloorIsClampedWithAToast(t *testing.T) {
	h, log := withFeed(t, hosttest.Options{Config: "[[widget]]\ntype = \"feed\"\nrefresh = \"5s\"\n"})

	if footer := h.Line(9); !strings.Contains(footer, "Feed: refresh 5s is below the feed minimum; using 30s") {
		t.Errorf("footer %q, want the clamping toast", footer)
	}
	h.Advance(29 * time.Second)
	if log.count() != 1 {
		t.Errorf("refreshed %d times in the first 29s, want 1 (at start)", log.count())
	}
	h.Advance(time.Second)
	if log.count() != 2 {
		t.Errorf("refreshed %d times by 30s, want 2", log.count())
	}
}

func TestARefreshShowsASpinnerInTheTopBorderOrCentredWhileLoading(t *testing.T) {
	root := savedLayout(t, `{"default": {"rows": [{"weight": 1, "widgets": [{"id": "clock", "weight": 1}]}, {"weight": 1, "widgets": [{"id": "feed", "weight": 1}]}], "hidden": ["feed"]}}`)
	h, log := withFeed(t, hosttest.Options{Root: root, Width: 40, Height: 19, Types: []widget.Type{liveViews[0]}})
	var during []string
	log.during = func() { during = strings.Split(h.Screen(), "\n") }
	spinner := regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]`)

	// Feed's tile is rows 9–17 once shown.
	h.Press("w", "j", "space", "esc")
	if during == nil {
		t.Fatal("showing the Widget didn't refresh it")
	}
	if !spinner.MatchString(during[13]) || !strings.Contains(during[9], "Feed") {
		t.Errorf("while loading, the body's middle line is %q, want a spinner:\n%s", during[13], strings.Join(during, "\n"))
	}
	if spinner.MatchString(h.Screen()) {
		t.Errorf("a spinner is left once the refresh landed:\n%s", h.Screen())
	}

	during = nil
	h.Advance(2 * time.Minute)
	if top := during[9]; !spinner.MatchString(top) || !strings.HasSuffix(top, " ─╮") {
		t.Errorf("while refreshing, the top border is %q, want a spinner at its right", top)
	}
	if !strings.Contains(strings.Join(during, "\n"), "headline") {
		t.Errorf("while refreshing, the data went away:\n%s", strings.Join(during, "\n"))
	}
}

func TestRefreshSettingOverridesTheTypesInterval(t *testing.T) {
	h, log := withFeed(t, hosttest.Options{Config: "[[widget]]\ntype = \"feed\"\nrefresh = \"5m\"\n"})

	h.Advance(4 * time.Minute)
	if log.count() != 1 {
		t.Errorf("refreshed %d times in 4m with refresh = 5m, want 1", log.count())
	}
	h.Advance(time.Minute)
	if log.count() != 2 {
		t.Errorf("refreshed %d times by 5m, want 2", log.count())
	}
}

func TestAReloadedRefreshSettingReschedulesTheWidget(t *testing.T) {
	h, log := withFeed(t, hosttest.Options{Config: "[[widget]]\ntype = \"feed\"\nrefresh = \"5m\"\n"})
	file := filepath.Join(h.Paths.Config, "config.toml")
	if err := os.WriteFile(file, []byte("[[widget]]\ntype = \"feed\"\nrefresh = \"1m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.Changed(file)
	if log.count() != 2 {
		t.Fatalf("reloading a changed block refreshed it %d times in all, want 2", log.count())
	}
	h.Advance(time.Minute)
	if log.count() != 3 {
		t.Errorf("refreshed %d times a minute after refresh became 1m, want 3", log.count())
	}
	h.Advance(4 * time.Minute)
	if log.count() != 7 {
		t.Errorf("refreshed %d times 5m after refresh became 1m, want 7 (the old 5m timer is gone)", log.count())
	}
}

func TestHiddenWidgetIsNotRefreshedUntilShown(t *testing.T) {
	root := savedLayout(t, `{"default": {"rows": [{"weight": 1, "widgets": [{"id": "clock", "weight": 1}, {"id": "feed", "weight": 1}]}], "hidden": ["feed"]}}`)
	clockType := liveViews[0]
	h, log := withFeed(t, hosttest.Options{Root: root, Types: []widget.Type{clockType}})

	h.Advance(10 * time.Minute)
	if log.count() != 0 {
		t.Fatalf("hidden Widget refreshed %d times, want none", log.count())
	}

	h.Press("w", "j", "space", "esc")
	if log.count() != 1 {
		t.Errorf("showing the Widget refreshed it %d times, want once", log.count())
	}
	if _, _, ok := h.Find("headline"); !ok {
		t.Errorf("shown Widget lacks its Item:\n%s", h.Screen())
	}
	h.Advance(2 * time.Minute)
	if log.count() != 2 {
		t.Errorf("refreshed %d times 2m after showing, want 2", log.count())
	}
}

// outage is a failed refresh with a short reason for the stale marker and a fix hint.
type outage struct{ reason, msg, hint string }

func (o outage) Error() string  { return o.msg }
func (o outage) Reason() string { return o.reason }
func (o outage) Hint() string   { return o.hint }

var offline = outage{"offline", "dial tcp 140.82.112.6:443: connect: network is unreachable", "Check your connection"}

func TestFailureWithNoDataShowsTheErrorAndHintInTheBody(t *testing.T) {
	h, _ := withFeed(t, hosttest.Options{}, offline)

	screen := h.Screen()
	for _, want := range []string{"network is unreachable", "Check your connection"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	x, y, _ := h.Find("connect")
	if tok, _ := h.Fg(x, y); tok != theme.Bad {
		t.Errorf("error drawn in %v, want bad", tok)
	}
}

func TestFailuresBackOffFrom15sDoublingTo15MinutesAndASuccessResetsIt(t *testing.T) {
	errs := make([]error, 9)
	for i := range errs {
		errs[i] = offline
	}
	h, log := withFeed(t, hosttest.Options{}, errs...)

	var gaps []time.Duration
	for log.count() < 11 {
		before := log.count()
		start := h.Clock.Now()
		for log.count() == before {
			h.Advance(time.Second)
		}
		gaps = append(gaps, h.Clock.Now().Sub(start))
	}
	want := []time.Duration{15, 30, 60, 120, 240, 480, 900, 900, 900, 120}
	for i := range want {
		want[i] *= time.Second
	}
	if len(gaps) != len(want) {
		t.Fatalf("gaps %v, want %v", gaps, want)
	}
	for i := range want {
		if gaps[i] != want[i] {
			t.Errorf("gaps between refreshes %v, want %v (the last one after a success)", gaps, want)
			break
		}
	}
}

func TestCtrlRRefreshesEveryWidgetAtOnceEvenInBackoff(t *testing.T) {
	log := &feedLog{errs: []error{offline, offline}}
	other := &feedLog{}
	h := hosttest.New(t, hosttest.Options{Width: 90, Height: 10, Types: []widget.Type{
		feedType("feed", "Feed", log), feedType("wire", "Wire", other),
	}})
	h.Advance(5 * time.Second)

	h.Press("ctrl+r")
	if log.count() != 2 || other.count() != 2 {
		t.Fatalf("^R refreshed Feed %d and Wire %d times in all, want 2 each", log.count(), other.count())
	}
	// The second failure backs off 30s from now.
	h.Advance(29 * time.Second)
	if log.count() != 2 {
		t.Errorf("Feed refreshed %d times, want 2 until its 30s backoff ends", log.count())
	}
	h.Advance(time.Second)
	if log.count() != 3 {
		t.Errorf("Feed refreshed %d times, want 3 once the backoff ends", log.count())
	}
	if _, _, ok := h.Find("headline"); !ok {
		t.Errorf("Feed lacks its Item after a successful retry:\n%s", h.Screen())
	}
}

func TestAfterWakeFromSleepOverdueWidgetsRefreshOnceStaggered(t *testing.T) {
	a, b, c := &feedLog{}, &feedLog{}, &feedLog{}
	slow := feedType("slow", "Slow", c)
	slow.Refresh = 2 * time.Hour
	h := hosttest.New(t, hosttest.Options{Width: 90, Height: 10, Types: []widget.Type{
		feedType("feed", "Feed", a), feedType("wire", "Wire", b), slow,
	}})
	h.Advance(30 * time.Second)

	h.Sleep(time.Hour)
	h.Advance(10 * time.Second)
	if n := a.count() + b.count(); n != 3 {
		t.Fatalf("Feed and Wire refreshed %d times in all right after waking, want 3 (one of them first)", n)
	}
	h.Advance(5 * time.Second)
	if a.count() != 2 || b.count() != 2 {
		t.Errorf("Feed %d and Wire %d refreshes 5s after waking, want 2 each", a.count(), b.count())
	}
	if c.count() != 1 {
		t.Errorf("Slow, not due for another hour, refreshed %d times, want 1", c.count())
	}
	h.Advance(time.Minute)
	if a.count() != 2 || b.count() != 2 {
		t.Errorf("Feed %d and Wire %d refreshes a minute after waking, want 2 each (once)", a.count(), b.count())
	}
}

// gate needs setup until its setup Action has run.
type gate struct{ granted *bool }

func (g gate) Refresh(context.Context, time.Time) (widget.Payload, error) {
	if !*g.granted {
		return widget.Payload{}, &widget.SetupError{
			Hint:   "Allow access to your calendars",
			Action: &widget.Action{ID: "grant", Label: "allow access"},
		}
	}
	return widget.Payload{Columns: []widget.Column{{Flex: true}}, Items: items("", "standup"), Count: 1}, nil
}

func (g gate) Invoke(_ context.Context, inv widget.Invocation) (widget.Result, error) {
	*g.granted = inv.Action == "grant"
	return widget.Result{}, nil
}

func TestNeedsSetupMakesTheSetupActionPrimary(t *testing.T) {
	granted := false
	typ := widget.Type{Name: "meetings", Title: "Meetings", Refresh: time.Minute,
		New: func(widget.Config) widget.Widget { return gate{granted: &granted} }}
	h := hosttest.New(t, hosttest.Options{Width: 60, Height: 10, Types: []widget.Type{typ}})

	for _, want := range []string{"needs setup", "Allow access to your calendars", "⏎ allow access"} {
		if _, _, ok := h.Find(want); !ok {
			t.Errorf("screen lacks %q:\n%s", want, h.Screen())
		}
	}
	h.Press("tab")
	if bottom := h.Line(8); !strings.Contains(bottom, "⏎ allow access") {
		t.Errorf("bottom border %q, want the setup Action as the primary hint", bottom)
	}
	h.Press("enter")
	if !granted {
		t.Fatal("⏎ didn't run the setup Action")
	}
	if _, _, ok := h.Find("standup"); !ok {
		t.Errorf("the Widget didn't refresh after its setup Action:\n%s", h.Screen())
	}
}

// tokenFeed is a feed signing in with a shared token.
type tokenFeed struct{ feed }

func (tokenFeed) Credential() string { return "github:github.com" }

func TestARateLimitPausesEveryWidgetSharingTheCredential(t *testing.T) {
	first, second, other := &feedLog{}, &feedLog{}, &feedLog{}
	typ := func(name string, log *feedLog) widget.Type {
		return widget.Type{Name: name, Title: name, Refresh: 2 * time.Minute,
			New: func(widget.Config) widget.Widget { return tokenFeed{feed{log: log}} }}
	}
	h := hosttest.New(t, hosttest.Options{Width: 90, Height: 12, Types: []widget.Type{
		typ("prs", first), typ("mine", second), feedType("feed", "Feed", other),
	}})
	h.Advance(time.Minute)
	first.fail(&widget.RateLimitError{Until: h.Clock.Now().Add(9 * time.Minute), Err: errors.New("API rate limit exceeded")})
	h.Press("ctrl+k", "p", "r", "s", " ", "r", "e", "f", "enter")
	if first.count() != 2 {
		t.Fatalf("refreshing prs from the palette ran it %d times in all, want 2", first.count())
	}
	h.Press("ctrl+r")
	if first.count() != 2 || second.count() != 1 {
		t.Errorf("^R during the pause refreshed prs %d and mine %d times, want 2 and 1", first.count(), second.count())
	}
	if top := h.Line(0); !strings.Contains(top, "rate limited") {
		t.Errorf("top border %q, want rate limited on the stale marker:\n%s", top, h.Screen())
	}

	h.Advance(8 * time.Minute)
	if first.count() != 2 || second.count() != 1 {
		t.Errorf("during the pause prs refreshed %d and mine %d times, want 2 and 1", first.count(), second.count())
	}
	if other.count() < 5 {
		t.Errorf("a Widget on another credential refreshed only %d times during the pause", other.count())
	}
	h.Advance(time.Minute)
	if first.count() != 3 || second.count() != 2 {
		t.Errorf("once the pause lifts prs refreshed %d and mine %d times, want 3 and 2", first.count(), second.count())
	}
}

func TestThePaletteListsRefreshForEachWidget(t *testing.T) {
	log := &feedLog{}
	h := hosttest.New(t, hosttest.Options{Width: 90, Height: 24, Types: []widget.Type{
		liveViews[0], feedType("feed", "Feed", log),
	}})

	h.Press("ctrl+k")
	entries := strings.Join(paletteEntries(h), " / ")
	for _, want := range []string{"Clock refresh", "Feed refresh"} {
		if !strings.Contains(entries, want) {
			t.Errorf("palette lacks %q; it lists %q", want, entries)
		}
	}
	h.Press("f", "e", "e", "d", "r", "e", "f", "enter")
	if log.count() != 2 {
		t.Errorf("Feed refreshed %d times, want 2 (start and the palette's refresh)", log.count())
	}
}

func TestFailureWithDataShowsItStaleWithReasonAndAgeAndTheFullErrorWhenFocused(t *testing.T) {
	h, log := withFeed(t, hosttest.Options{})
	log.fail(offline, offline, offline)
	h.Advance(2 * time.Minute)

	top := h.Line(0)
	if !strings.Contains(top, "offline · 2m") {
		t.Fatalf("top border %q, want the stale marker", top)
	}
	x, _, _ := h.Find("offline · 2m")
	if tok, _ := h.Fg(x, 0); tok != theme.Warn {
		t.Errorf("stale marker drawn in %v, want warn", tok)
	}
	if _, _, ok := h.Find("headline"); !ok {
		t.Errorf("stale Widget hides its data:\n%s", h.Screen())
	}
	if strings.Contains(h.Screen(), "unreachable") {
		t.Errorf("unfocused stale Widget shows the full error:\n%s", h.Screen())
	}

	h.Press("tab")
	if footer := h.Line(9); !strings.Contains(footer, "network is unreachable") || strings.Contains(footer, "^K") {
		t.Errorf("footer %q, want only the full error while the stale Widget is focused", footer)
	}

	// Retries at 2m15s and 2m45s fail too; the one at 3m45s succeeds.
	h.Advance(100 * time.Second)
	if top := h.Line(0); !strings.Contains(top, "offline · 3m") {
		t.Errorf("top border %q, want the age to grow", top)
	}
	h.Advance(10 * time.Second)
	if top := h.Line(0); strings.Contains(top, "offline") {
		t.Errorf("top border %q still stale after a successful retry", top)
	}
}
