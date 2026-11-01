package host_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/plugin/plugintest"
	"github.com/oronbz/visor/internal/widget"
)

// withPlugin starts visor with the fixture Plugin installed in the plugins
// directory and config placing its Widgets; no built-in types.
func withPlugin(t *testing.T, o plugintest.Options, config string) *hosttest.Host {
	t.Helper()
	root := t.TempDir()
	plugintest.Write(t, filepath.Join(paths.Under(root).Data, "plugins", "fixture"), o)
	return hosttest.New(t, hosttest.Options{Root: root, Width: 60, Height: 12, Types: []widget.Type{}, Config: config})
}

func shows(t *testing.T, h *hosttest.Host, want ...string) {
	t.Helper()
	screen := h.Screen()
	for _, w := range want {
		if !strings.Contains(screen, w) {
			t.Errorf("no %q on screen:\n%s", w, screen)
		}
	}
}

func hides(t *testing.T, h *hosttest.Host, unwanted ...string) {
	t.Helper()
	screen := h.Screen()
	for _, w := range unwanted {
		if strings.Contains(screen, w) {
			t.Errorf("%q on screen:\n%s", w, screen)
		}
	}
}

const fixtureWidget = "[[widget]]\ntype = \"fixture\"\n"

func TestAPluginWidgetIsPlacedAndShowsItsItems(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+"greeting = \"hi\"\n")
	shows(t, h, "Fixture 2", "hi alpha", "hi bravo", "refresh 1")
}

func TestAPluginWidgetShowsItsLines(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+"mode = \"lines\"\n")
	shows(t, h, "hello from lines", "▄", "refresh 1")
	if _, y, _ := h.Find("hello from lines"); h.Line(y+1) == "│"+strings.Repeat(" ", 58)+"│" {
		t.Errorf("no half-block digits under the first line:\n%s", h.Screen())
	}
}

func TestPluginSettingsAreValidatedLikeBuiltIns(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+"colour = 1\n")
	shows(t, h, "needs setup", "config.toml:3: unknown key `colour`")
}

func TestTheHostRefreshesAPluginWidgetOnSchedule(t *testing.T) {
	h := withPlugin(t, plugintest.Options{Refresh: "1m"}, fixtureWidget)
	shows(t, h, "refresh 1")
	h.Advance(50 * time.Second)
	shows(t, h, "refresh 1")
	h.Advance(20 * time.Second)
	shows(t, h, "refresh 2")
}

func TestAPluginsPushesShowWithoutARefresh(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget)
	h.Press("1")
	h.Receive()
	shows(t, h, "pushed: focused at 58×9 after 1 refreshes")
	hides(t, h, "hello alpha")
}

func TestPluginActionsRoundTripTheirInput(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget)
	h.Press("1", "j", "d")
	shows(t, h, "done bravo", "refresh 2")
	h.Press("t")
	h.Type("buy milk\n")
	shows(t, h, "noted buy milk", "refresh 3")
	h.Press("s")
	h.Type("hunter2")
	hides(t, h, "hunter2")
	h.Press("enter")
	shows(t, h, "stored ••••", "refresh 4")
	hides(t, h, "hunter2")
}

func TestAPluginChoiceActionLoadsItsChoicesAndRunsThePickedOne(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget)
	h.Press("1", "c")
	shows(t, h, "Move alpha", "To do", "Done", "Elsewhere")
	h.Press("j", "enter")
	shows(t, h, "moved alpha to done")
}

func TestAPluginActionCanSaveItsWidgetsSettings(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget)
	h.Press("1", "e")
	h.Type("howdy\n")
	shows(t, h, "howdy alpha")
	b, err := os.ReadFile(filepath.Join(h.Paths.Config, "config.toml"))
	if err != nil || !strings.Contains(string(b), `greeting = "howdy"`) {
		t.Errorf("config.toml = %s, %v", b, err)
	}
}

func TestAPluginActionCanLeadIntoAnother(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget)
	h.Press("1", "n")
	shows(t, h, "Note")
	h.Type("chained\n")
	shows(t, h, "noted chained")
}

func TestARawColourPayloadIsAnErrorNamingIt(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+"mode = \"raw-colour\"\n")
	shows(t, h, "Plugin `fixture` sent a payload visor can't", `"#ff0000" is not a theme token`)
}

func TestAWrongProtocolVersionNeedsSetupNamingBothVersions(t *testing.T) {
	h := withPlugin(t, plugintest.Options{Args: []string{"-protocol", "2"}}, fixtureWidget)
	h.Resize(120, 12)
	shows(t, h, "needs setup", "Plugin `fixture` speaks protocol 2, but this visor speaks 1.")
}

func TestACrashedPluginShowsAnErrorAndRestartsWithGrowingDelays(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget)
	h.Press("1")
	h.Receive()
	crash := func() {
		t.Helper()
		h.Press("x")
		shows(t, h, "Crash failed: Plugin `fixture` stopped")
		h.ReceiveUntil(func() bool { return strings.Contains(h.Screen(), "stopped:") })
		hides(t, h, "pushed:", "hello alpha")
		shows(t, h, "Plugin `fixture` stopped: exit status 3 (fixture: boom)")
	}
	crash()
	shows(t, h, "Restarting it in 1s")
	h.Advance(time.Second - time.Millisecond)
	hides(t, h, "hello alpha")
	h.Advance(time.Millisecond)
	shows(t, h, "hello alpha")

	crash()
	shows(t, h, "Restarting it in 2s")
	h.Advance(time.Second)
	hides(t, h, "hello alpha")
	h.Advance(time.Second)
	shows(t, h, "hello alpha")
}

func TestAPluginCrashStaysWithTheInstanceWhoseProcessStopped(t *testing.T) {
	a := withPlugin(t, plugintest.Options{}, fixtureWidget)
	a.Press("1")
	a.Receive()
	a.Advance(10 * time.Second)
	entry := entryFile(t, a.Root)
	before, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	a.Press("x")
	a.ReceiveUntil(func() bool { return strings.Contains(a.Screen(), "stopped:") })
	a.Press("ctrl+r")
	shows(t, a, "Restarting it in 1s")

	if after, _ := os.ReadFile(entry); string(after) != string(before) {
		t.Errorf("the crash changed the shared cache entry:\n%s\nwant it as before:\n%s", after, before)
	}
	b := hosttest.New(t, hosttest.Options{Root: a.Root, Width: 60, Height: 12, Types: []widget.Type{}, Config: fixtureWidget})
	hides(t, b, "stopped")
	shows(t, b, "pushed: focused")
}

func TestAnAddedPluginWidgetGetsItsPushes(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget)
	h.Resize(60, 24)
	runFromPalette(t, h, "add fixture")
	shows(t, h, "Added Fixture")
	h.Press("2")
	h.Receive()
	_, y, _ := h.Find("pushed: focused")
	if _, top, _ := h.Find("refresh 1"); y < top {
		t.Errorf("the push landed in the first Widget, not the one added:\n%s", h.Screen())
	}
}

func TestAPluginListedInAReloadedConfigProvidesItsTypes(t *testing.T) {
	root := t.TempDir()
	cfgDir := paths.Under(root).Config
	plugintest.Write(t, filepath.Join(cfgDir, "dev", "fixture"), plugintest.Options{})
	h := hosttest.New(t, hosttest.Options{Root: root, Width: 60, Height: 12, Types: []widget.Type{}, Config: fixtureWidget})
	shows(t, h, "unknown Widget type `fixture`")
	file := filepath.Join(cfgDir, "config.toml")
	if err := os.WriteFile(file, []byte("[[plugin]]\npath = \"dev/fixture\"\n\n"+fixtureWidget), 0o644); err != nil {
		t.Fatal(err)
	}
	h.Changed(file)
	shows(t, h, "hello alpha")
	hides(t, h, "unknown Widget type")
}

func TestPluginsListedInConfigAreFoundRelativeToIt(t *testing.T) {
	root := t.TempDir()
	cfgDir := paths.Under(root).Config
	plugintest.Write(t, filepath.Join(cfgDir, "dev", "fixture"), plugintest.Options{})
	h := hosttest.New(t, hosttest.Options{Root: root, Width: 60, Height: 12, Types: []widget.Type{},
		Config: "[[plugin]]\npath = \"dev/fixture\"\n\n" + fixtureWidget})
	shows(t, h, "hello alpha")
}
