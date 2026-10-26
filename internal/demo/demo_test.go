package demo_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oronbz/visor/internal/demo"
	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/plugin/plugintest"
)

// dashboard is the demo dashboard at hosttest's DefaultNow, borrowing
// Plugins from plugins.
func dashboard(t *testing.T, plugins string) (*demo.Demo, *hosttest.Host) {
	t.Helper()
	return dashboardAt(t, hosttest.DefaultNow, plugins)
}

func dashboardAt(t *testing.T, now time.Time, plugins string) (*demo.Demo, *hosttest.Host) {
	t.Helper()
	d, err := demo.New(demo.Options{Now: now, TempDir: t.TempDir(), Plugins: plugins})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d, hosttest.New(t, hosttest.Options{Now: now, Root: d.Root, Width: 180, Height: 64, Ports: d.Ports})
}

func lacks(t *testing.T, h *hosttest.Host, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if _, _, ok := h.Find(text); ok {
			t.Errorf("screen shows %q:\n%s", text, h.Screen())
		}
	}
}

func shows(t *testing.T, h *hosttest.Host, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if _, _, ok := h.Find(text); !ok {
			t.Errorf("screen lacks %q:\n%s", text, h.Screen())
		}
	}
}

// row is the screen line text is first found on.
func row(t *testing.T, h *hosttest.Host, text string) string {
	t.Helper()
	_, y, ok := h.Find(text)
	if !ok {
		t.Fatalf("screen lacks %q:\n%s", text, h.Screen())
	}
	return h.Line(y)
}

func click(t *testing.T, h *hosttest.Host, text string) {
	t.Helper()
	x, y, ok := h.Find(text)
	if !ok {
		t.Fatalf("screen lacks %q:\n%s", text, h.Screen())
	}
	h.Click(x, y)
}

func TestTheDemoDashboardShowsTheDemoPRsIssuesTasksAndNotes(t *testing.T) {
	_, h := dashboard(t, t.TempDir())

	shows(t, h, "web#482", "api#1290", "maya-chen", "@you", "owner",
		"ACME-157", "ACME-142", "In Review", "To Do",
		"Review the rate-limit rollout plan", "Saved cards ship behind")
	lacks(t, h, "needs setup", "Reply to the design feedback thread")
}

func TestTheDemoDashboardShowsTheDemoMeetingsAndReminders(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Meetings and Reminders are macOS only")
	}
	_, h := dashboard(t, t.TempDir())

	shows(t, h, "Release freeze", "Design review: checkout flow", "1:1 with Priya", "Lunch with the API team", "Daily standup",
		"Submit the expense report", "Book the team offsite venue", "Water the plants")
	if got := row(t, h, "Design review"); !strings.Contains(got, "now") {
		t.Errorf("the design review isn't under way: %q", got)
	}
	if got := row(t, h, "Submit the expense report"); !strings.Contains(got, "overdue") {
		t.Errorf("the expense report isn't overdue: %q", got)
	}
	lacks(t, h, "Renew passport")
}

func TestTheDemoClockReadsLateMorningOnTheSameDateAtAnyHour(t *testing.T) {
	for _, real := range []time.Time{
		time.Date(2026, 10, 18, 23, 30, 0, 0, time.UTC),
		time.Date(2026, 10, 18, 0, 10, 0, 0, time.UTC),
	} {
		t.Run(real.Format("15:04"), func(t *testing.T) {
			_, h := dashboardAt(t, real, t.TempDir())

			shows(t, h, "Sunday 18 October", "web#482")
			if got := row(t, h, "web#482"); !strings.HasSuffix(strings.TrimRight(got, "│ "), " 3h") {
				t.Errorf("web#482 isn't 3h old by the demo clock: %q", got)
			}
			if runtime.GOOS != "darwin" {
				return
			}
			shows(t, h, "11:20 ───")
			if got := row(t, h, "Design review"); !strings.Contains(got, "11:00 ●") || !strings.Contains(got, "now") {
				t.Errorf("the design review isn't live at 11:00: %q", got)
			}
			_, tomorrow, _ := h.Find("Tomorrow ─")
			for _, want := range []string{"12:30   Lunch with the API team", "14:00   Incident review", "16:00   Sprint retro"} {
				if _, y, ok := h.Find(want); !ok || y > tomorrow {
					t.Errorf("%q isn't on today's list:\n%s", want, h.Screen())
				}
			}
		})
	}
}

func TestApprovingADemoPRDropsItFromTheList(t *testing.T) {
	_, h := dashboard(t, t.TempDir())

	click(t, h, "web#482")
	h.Press("a", "enter")
	h.Press("ctrl+r")
	lacks(t, h, "Checkout: show saved cards first")
	shows(t, h, "─ Review requests 6 ", "web#479")
}

func TestRemovingMyselfFromADemoPRDropsItFromTheList(t *testing.T) {
	_, h := dashboard(t, t.TempDir())

	click(t, h, "api#1290")
	h.Press("r", "enter")
	h.Press("ctrl+r")
	lacks(t, h, "Rate-limit the public search")
	shows(t, h, "─ Review requests 6 ")
}

func TestTransitioningADemoIssueChangesItsStatus(t *testing.T) {
	_, h := dashboard(t, t.TempDir())
	if got := row(t, h, "ACME-161"); !strings.Contains(got, "To Do") {
		t.Fatalf("ACME-161 starts %q, want To Do", got)
	}

	click(t, h, "ACME-161")
	h.Press("t")
	shows(t, h, "Start progress → In Progress", "Send to review → In Review", "Done")
	h.Press("enter")
	h.Press("ctrl+r")
	if got := row(t, h, "ACME-161"); !strings.Contains(got, "In Progress") {
		t.Errorf("ACME-161 after Start progress: %q", got)
	}

	click(t, h, "ACME-161")
	h.Press("t", "j", "j", "enter")
	h.Press("ctrl+r")
	lacks(t, h, "Settings page drops unsaved changes")
	shows(t, h, "─ Jira 5 ")
}

func TestCompletingADemoReminderRemovesItAndARecurringOneMovesOn(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Reminders is macOS only")
	}
	_, h := dashboard(t, t.TempDir())

	for i, title := range []string{"Book the team offsite venue", "Water the plants"} {
		click(t, h, title)
		h.Press("space")
		h.Press("ctrl+r")
		shows(t, h, fmt.Sprintf("─ Reminders %d ", 3-i))
	}
	shows(t, h, "Submit the expense report", "Pick up the dry cleaning")
}

// installed lays out fixture Plugins named names in a plugins dir.
func installed(t *testing.T, names ...string) string {
	dir := t.TempDir()
	for _, name := range names {
		plugintest.Write(t, filepath.Join(dir, name), plugintest.Options{Name: name})
	}
	return dir
}

// tree is every file under dir with its contents.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		files[path] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestTheDemoShowsAnInstalledNowPlayingAndHintsAtAMissingStocks(t *testing.T) {
	plugins := installed(t, "nowplaying", "bitrise")
	before := tree(t, plugins)
	d, h := dashboard(t, plugins)

	if got := strings.Count(h.Screen(), "─ Fixture "); got != 1 {
		t.Errorf("%d fixture Widgets, want only nowplaying's:\n%s", got, h.Screen())
	}
	want := []string{"stocks isn't installed, so the demo leaves it out; `visor plugin install stocks` adds it"}
	if !slices.Equal(d.Hints, want) {
		t.Errorf("hints %q, want %q", d.Hints, want)
	}
	if after := tree(t, plugins); len(after) != len(before) {
		t.Errorf("the plugins dir changed: %v, was %v", after, before)
	}
}

func TestTheDemoLeavesBothPluginsOutWhenNeitherIsInstalled(t *testing.T) {
	d, h := dashboard(t, t.TempDir())

	if len(d.Hints) != 2 || !strings.Contains(d.Hints[0], "visor plugin install nowplaying") || !strings.Contains(d.Hints[1], "visor plugin install stocks") {
		t.Errorf("hints %q, want one for each Plugin", d.Hints)
	}
	lacks(t, h, "Fixture", "needs setup")
}

func TestClosingTheDemoRemovesItsRoot(t *testing.T) {
	d, err := demo.New(demo.Options{Now: hosttest.DefaultNow, TempDir: t.TempDir(), Plugins: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.Root); !os.IsNotExist(err) {
		t.Errorf("%s is still there: %v", d.Root, err)
	}
}

func TestStartingTheDemoRemovesOnlyTheRootsOfDemosWhoseProcessIsGone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the demo can't tell a gone process on Windows, so it leaves every root")
	}
	ended := exec.Command(os.Args[0], "-test.run=^$")
	if err := ended.Run(); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	roots := map[string]string{
		"visor-demo-ended":   strconv.Itoa(ended.Process.Pid),
		"visor-demo-running": strconv.Itoa(os.Getpid()),
		"visor-demo-new":     "",
	}
	for name, pid := range roots {
		if err := os.MkdirAll(filepath.Join(tmp, name, "config"), 0o755); err != nil {
			t.Fatal(err)
		}
		if pid != "" {
			if err := os.WriteFile(filepath.Join(tmp, name, "visor-demo.pid"), []byte(pid), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	d, err := demo.New(demo.Options{Now: hosttest.DefaultNow, TempDir: tmp, Plugins: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for name := range roots {
		_, err := os.Stat(filepath.Join(tmp, name))
		if gone := os.IsNotExist(err); gone != (name == "visor-demo-ended") {
			t.Errorf("%s removed = %v", name, gone)
		}
	}
	if _, err := os.Stat(d.Root); err != nil {
		t.Errorf("the demo's own root: %v", err)
	}
}
