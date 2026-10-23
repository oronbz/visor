package host_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/upgrade"
	"github.com/oronbz/visor/internal/widget"
	"github.com/oronbz/visor/internal/widgets/calendar"
)

// releases is a fake GitHub serving one latest release of visor.
type releases struct {
	*httptest.Server
	mu       sync.Mutex
	tag      string
	requests int
}

const notes = "## Changelog\n* 1a2b3c4 Jira: an issue's glyph is its type\n* 5d6e7f8 PRs: a token missing scopes asks for setup\n* 9a8b7c6 Third\n* 1b2c3d4 Fourth\n* 5e6f7a8 Fifth\n* 9b8c7d6 Sixth, never shown\n"

func newReleases(t *testing.T, tag string) *releases {
	t.Helper()
	r := &releases{tag: tag}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/repos/oronbz/visor/releases/latest" {
			t.Errorf("visor asked GitHub for %s", req.URL.Path)
			http.NotFound(w, req)
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.requests++
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name": r.tag, "body": notes, "html_url": "https://github.com/oronbz/visor/releases/tag/" + r.tag,
		})
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *releases) publish(tag string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tag = tag
}

func (r *releases) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests
}

// running starts visor 0.6.2 against GitHub r.
func running(t *testing.T, r *releases, o hosttest.Options) *hosttest.Host {
	t.Helper()
	o.Version = "0.6.2"
	return runningAs(t, r, o)
}

// hosttestPaths gives o a Root, so a test can lay files out before visor starts.
func hosttestPaths(t *testing.T, o *hosttest.Options) paths.Paths {
	if o.Root == "" {
		o.Root = t.TempDir()
	}
	return paths.Under(o.Root)
}

// runningAs starts visor at o.Version against GitHub r.
func runningAs(t *testing.T, r *releases, o hosttest.Options) *hosttest.Host {
	t.Helper()
	// Calendar alone, which ticks at midnight, so advancing 12h stays quick.
	o.Types = []widget.Type{calendar.Type}
	o.Endpoints = ports.Endpoints{GitHubREST: r.URL}
	if o.Width == 0 {
		o.Width, o.Height = 140, 30
	}
	return hosttest.New(t, o)
}

func TestANewerReleaseOpensTheUpgradePopupAtLaunchWithItsNotes(t *testing.T) {
	h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{})

	screen := h.Screen()
	for _, want := range []string{
		"visor v0.7.0",
		"A new release of visor is out. You have v0.6.2.",
		"What's new",
		"• Jira: an issue's glyph is its type",
		"• Fifth",
		"⏎ opens the release page",
		"⏎ open release page", "o release notes", "esc later",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	for _, unwanted := range []string{"Changelog", "Sixth", "1a2b3c4"} {
		if strings.Contains(screen, unwanted) {
			t.Errorf("popup shows %q, want only the first 5 entries without hashes:\n%s", unwanted, screen)
		}
	}
}

func TestTheUpgradePillSitsAtTheFootersRightEdgeAtEveryWidth(t *testing.T) {
	for _, w := range []int{140, 80, 64} {
		h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{Width: w, Height: 24})
		h.Press("esc")

		footer := h.Line(23)
		if !strings.HasSuffix(footer, " ↑ v0.7.0") {
			t.Errorf("at %d columns the footer is %q, want it to end in the pill", w, footer)
		}
		if !strings.HasPrefix(footer, " NORMAL ") {
			t.Errorf("at %d columns the footer is %q, want the mode badge kept", w, footer)
		}
		pill := len([]rune(" ↑ v0.7.0 "))
		for i := range pill {
			x := w - pill + i
			if bg, ok := h.Bg(x, 23); !ok || bg != theme.Accent {
				t.Errorf("at %d columns pill cell %d has background %v, want accent", w, i, bg)
			}
		}
		if w == 64 && !strings.Contains(footer, "…") {
			t.Errorf("at %d columns the footer is %q, want the hints cut with … before the pill", w, footer)
		}
	}
}

func popupOpen(h *hosttest.Host) bool {
	_, _, ok := h.Find("A new release of visor is out")
	return ok
}

func TestEscClosesTheUpgradePopupForThatReleaseOnly(t *testing.T) {
	gh := newReleases(t, "v0.7.0")
	h := running(t, gh, hosttest.Options{})
	h.Press("esc")
	if popupOpen(h) {
		t.Fatalf("esc left the popup open:\n%s", h.Screen())
	}

	again := running(t, gh, hosttest.Options{Root: h.Root})
	if popupOpen(again) {
		t.Errorf("a new visor opened the popup of a release already closed:\n%s", again.Screen())
	}
	if !strings.HasSuffix(again.Line(29), "↑ v0.7.0") {
		t.Errorf("footer %q, want the pill kept after closing the popup", again.Line(29))
	}

	gh.publish("v0.8.0")
	again.Advance(upgrade.Interval)
	if !popupOpen(again) || !strings.Contains(again.Screen(), "visor v0.8.0") {
		t.Errorf("a newer release didn't open the popup again:\n%s", again.Screen())
	}
}

func TestACheckFinishingWhileTypingWaitsToOpenThePopup(t *testing.T) {
	gh := newReleases(t, "v0.6.2")
	h := running(t, gh, hosttest.Options{})
	if popupOpen(h) {
		t.Fatalf("popup open with no newer release:\n%s", h.Screen())
	}

	gh.publish("v0.7.0")
	h.Press("ctrl+k")
	h.Type("ref")
	h.Advance(upgrade.Interval)
	if gh.count() != 2 {
		t.Fatalf("GitHub asked %d times, want a second check after 12h", gh.count())
	}
	if popupOpen(h) {
		t.Errorf("the popup opened over the palette:\n%s", h.Screen())
	}
	h.Press("esc")
	if !popupOpen(h) {
		t.Errorf("closing the palette didn't open the waiting popup:\n%s", h.Screen())
	}
}

func TestClosingThePopupInOneVisorClosesItInAnother(t *testing.T) {
	gh := newReleases(t, "v0.7.0")
	a := running(t, gh, hosttest.Options{})
	b := running(t, gh, hosttest.Options{Root: a.Root})
	if !popupOpen(a) || !popupOpen(b) {
		t.Fatalf("both visors should show the popup")
	}

	a.Press("esc")
	b.Changed(upgrade.StoreIn(b.Paths.Cache).File)
	if popupOpen(b) {
		t.Errorf("the other visor kept its popup open:\n%s", b.Screen())
	}
}

func TestVisorsSharingARootCheckGitHubOnceEvery12Hours(t *testing.T) {
	gh := newReleases(t, "v0.7.0")
	a := running(t, gh, hosttest.Options{})
	if gh.count() != 1 {
		t.Fatalf("GitHub asked %d times at launch, want 1", gh.count())
	}
	b := running(t, gh, hosttest.Options{Root: a.Root})
	if gh.count() != 1 {
		t.Errorf("a second visor within 12h asked GitHub again (%d requests)", gh.count())
	}
	b.Advance(upgrade.Interval - time.Minute)
	if gh.count() != 1 {
		t.Errorf("asked GitHub again before 12h passed (%d requests)", gh.count())
	}
	b.Advance(time.Minute)
	if gh.count() != 2 {
		t.Errorf("GitHub asked %d times after 12h, want a second check", gh.count())
	}
}

func TestUpgradeChecksTurnOff(t *testing.T) {
	cases := []struct {
		name string
		o    hosttest.Options
	}{
		{"upgrade_check = false", hosttest.Options{Config: "upgrade_check = false\n"}},
		{"VISOR_NO_UPGRADE_CHECK", hosttest.Options{Env: map[string]string{"VISOR_NO_UPGRADE_CHECK": "1"}}},
		{"a dev build", hosttest.Options{Version: "dev"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gh := newReleases(t, "v0.7.0")
			if _, err := upgrade.StoreIn(hosttestPaths(t, &c.o).Cache).Update(func(s *upgrade.State) {
				s.Latest = upgrade.Release{Tag: "v0.7.0"}
			}); err != nil {
				t.Fatal(err)
			}
			h := runningAs(t, gh, c.o)
			h.Advance(upgrade.Interval)
			if gh.count() != 0 {
				t.Errorf("asked GitHub %d times, want none", gh.count())
			}
			if popupOpen(h) || strings.Contains(h.Screen(), "↑ v0.7.0") {
				t.Errorf("shows the Upgrade with checks off:\n%s", h.Screen())
			}
		})
	}
}

// caskInstall lays visor out as the Homebrew cask does and returns the
// path brew links into its bin, which visor is launched by.
func caskInstall(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	real := filepath.Join(dir, "Caskroom", "visor", "0.6.2", "visor")
	launched := filepath.Join(dir, "bin", "visor")
	for _, d := range []string{filepath.Dir(real), filepath.Dir(launched)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(real, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, launched); err != nil {
		t.Fatal(err)
	}
	return launched
}

var brewUpgrade = []string{"brew", "upgrade", "--cask", "visor"}

func ranBrew(h *hosttest.Host) bool {
	return slices.ContainsFunc(h.Foreground.Ran(), func(c []string) bool { return slices.Equal(c, brewUpgrade) })
}

func TestEnterInThePopupRunsBrewAndRestartsThroughTheLaunchPath(t *testing.T) {
	exe := caskInstall(t)
	h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{Executable: exe})
	if !strings.Contains(h.Screen(), "⏎ runs `brew upgrade --cask visor`") || !strings.Contains(h.Screen(), "⏎ upgrade") {
		t.Fatalf("popup for a cask install doesn't name brew:\n%s", h.Screen())
	}

	h.Press("enter")
	if !ranBrew(h) {
		t.Fatalf("ran %v, want brew upgrade --cask visor", h.Foreground.Ran())
	}
	if !h.Quit() || h.Restart() != exe {
		t.Errorf("after brew, quit %v and restart %q, want visor run again through %q", h.Quit(), h.Restart(), exe)
	}
}

func TestEveryOtherWayToUpgradeAsksFirstWithTheExactCommand(t *testing.T) {
	ways := map[string]func(h *hosttest.Host){
		"U":                   func(h *hosttest.Host) { h.Press("U") },
		"a click on the pill": func(h *hosttest.Host) { h.Click(138, 29) },
		"the palette": func(h *hosttest.Host) {
			h.Press("ctrl+k")
			h.Type("upgrade visor")
			h.Press("enter")
		},
	}
	for name, start := range ways {
		t.Run(name, func(t *testing.T) {
			exe := caskInstall(t)
			h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{Executable: exe})
			h.Press("esc")

			start(h)
			screen := h.Screen()
			if !strings.Contains(screen, "Upgrade visor (v0.6.2 → v0.7.0)?") || !strings.Contains(screen, "brew upgrade --cask visor") {
				t.Fatalf("no confirm naming the command:\n%s", screen)
			}
			if ranBrew(h) {
				t.Fatal("ran brew before confirming")
			}
			h.Press("enter")
			if !ranBrew(h) || h.Restart() != exe {
				t.Errorf("confirming ran %v and restarts %q, want brew and %q", h.Foreground.Ran(), h.Restart(), exe)
			}
		})
	}
}

func TestUAndThePillDoNothingWithoutANewerRelease(t *testing.T) {
	h := running(t, newReleases(t, "v0.6.2"), hosttest.Options{Executable: caskInstall(t)})
	h.Press("U")
	h.Click(138, 29)
	if strings.Contains(h.Screen(), "Upgrade visor") || len(h.Foreground.Ran()) > 0 {
		t.Errorf("U or the pill's cell started an Upgrade with none out:\n%s", h.Screen())
	}
}

func TestAFailedBrewUpgradeToastsItsErrorAndKeepsVisorRunning(t *testing.T) {
	h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{Executable: caskInstall(t)})
	h.Foreground.Err = errors.New("exit status 1")

	h.Press("enter")
	if h.Quit() {
		t.Fatal("visor quit after brew failed")
	}
	if footer := h.Line(29); !strings.Contains(footer, "Upgrade failed: exit status 1") {
		t.Errorf("footer %q, want brew's error", footer)
	}
}

func TestAGoInstallUpgradeCopiesTheCommands(t *testing.T) {
	gobin := t.TempDir()
	h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{
		Executable: filepath.Join(gobin, "visor"), Env: map[string]string{"GOBIN": gobin},
	})
	want := "go install github.com/oronbz/visor/cmd/visor@latest github.com/oronbz/visor/cmd/visor-weather@latest"

	h.Press("enter")
	if !slices.Equal(h.Clipboard.Copied, []string{want}) {
		t.Errorf("copied %q, want %q", h.Clipboard.Copied, want)
	}
	if footer := h.Line(29); !strings.Contains(footer, "Copied `go install") {
		t.Errorf("footer %q, want a toast saying what was copied", footer)
	}
}

func TestAnUnknownInstallOpensTheReleasePage(t *testing.T) {
	h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{Executable: filepath.Join(t.TempDir(), "visor")})
	page := "https://github.com/oronbz/visor/releases/tag/v0.7.0"

	h.Press("o")
	if !popupOpen(h) || !slices.Equal(h.Opener.Opened, []string{page}) {
		t.Fatalf("o opened %v and left the popup open %v, want the release page and the popup kept", h.Opener.Opened, popupOpen(h))
	}
	h.Press("enter")
	if popupOpen(h) || len(h.Opener.Opened) != 2 || h.Opener.Opened[1] != page {
		t.Errorf("⏎ opened %v, want the release page and the popup closed", h.Opener.Opened)
	}
}

func TestTheUpgradePillStepsAsideForAToast(t *testing.T) {
	h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{Width: 64, Height: 24})
	h.Opener.Err = errors.New("no browser, none here")

	h.Press("o")
	footer := h.Line(23)
	if strings.Contains(footer, "↑ v0.7.0") {
		t.Errorf("footer %q shows the pill over a toast", footer)
	}
	if !strings.Contains(footer, "no browser, none here") {
		t.Errorf("footer %q cuts the toast short", footer)
	}

	h.Advance(10 * time.Second)
	if footer := h.Line(23); !strings.HasSuffix(footer, "↑ v0.7.0") {
		t.Errorf("footer %q, want the pill back once the toast expired", footer)
	}
}

func TestAFocusedWidgetsOwnUActionWinsOverUpgrading(t *testing.T) {
	dir := t.TempDir()
	h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{Config: fmt.Sprintf(
		"[[widget]]\ntype = \"calendar\"\nactions = [{ key = \"U\", label = \"mine\", run = \"cd %s && echo mine >> ran\" }]\n", dir)})
	h.Press("esc", "tab", "U")
	if got := ran(t, dir); got != "mine\n" {
		t.Errorf("U on the focused Widget ran %q, want its own Action", got)
	}
	if strings.Contains(h.Screen(), "Upgrade visor") {
		t.Errorf("U on a Widget that binds it asked to upgrade:\n%s", h.Screen())
	}

	h.Press("esc", "U")
	if !strings.Contains(h.Screen(), "Upgrade visor (v0.6.2 → v0.7.0)?") {
		t.Errorf("U with nothing focused didn't ask to upgrade:\n%s", h.Screen())
	}
}

// reconfigure rewrites config.toml and tells visor, as its watcher would.
func reconfigure(t *testing.T, h *hosttest.Host, toml string) {
	t.Helper()
	file := filepath.Join(h.Paths.Config, "config.toml")
	if err := os.MkdirAll(h.Paths.Config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	h.Changed(file)
}

func TestTurningUpgradeCheckOffClosesThePopupAndThePill(t *testing.T) {
	h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{})
	reconfigure(t, h, "upgrade_check = false\n")
	if popupOpen(h) || strings.Contains(h.Screen(), "↑ v0.7.0") {
		t.Errorf("the Upgrade still shows with checks turned off:\n%s", h.Screen())
	}
	h.Press("tab")
	if focused(t, h, []string{"Calendar"}) == "" {
		t.Error("keys still go to a popup that's gone")
	}
}

func TestTurningUpgradeCheckOnChecksRightAway(t *testing.T) {
	gh := newReleases(t, "v0.7.0")
	h := running(t, gh, hosttest.Options{Config: "upgrade_check = false\n"})
	reconfigure(t, h, "upgrade_check = true\n")
	if gh.count() != 1 || !popupOpen(h) {
		t.Errorf("after turning checks on, GitHub asked %d times and popup open %v, want 1 and true", gh.count(), popupOpen(h))
	}
	h.Advance(upgrade.Interval)
	if gh.count() != 2 {
		t.Errorf("GitHub asked %d times 12h on, want one check every 12h, not a second chain", gh.count())
	}
}

func TestKeysReachVisorWhileItsTooSmallToDrawThePopup(t *testing.T) {
	h := running(t, newReleases(t, "v0.7.0"), hosttest.Options{})
	h.Resize(12, 4)
	if !strings.Contains(h.Screen(), "too small") {
		t.Fatalf("12×4 isn't too small:\n%s", h.Screen())
	}
	h.Press("q")
	if !h.Quit() {
		t.Errorf("q while the popup can't be drawn didn't quit:\n%s", h.Screen())
	}
}
