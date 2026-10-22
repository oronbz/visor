package host_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oronbz/visor/internal/plugin/plugintest"
)

// ran is what custom Actions appended to the file they write to in dir.
func ran(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "ran"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func TestACustomActionOnAPluginWidgetRunsWithTheItemsValuesQuotedThenRefreshes(t *testing.T) {
	dir := t.TempDir()
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+fmt.Sprintf(
		`actions = [{ key = "f", label = "fix", run = "cd %s && printf '%%s\n' {{key}} >> ran" }]`+"\n", dir))
	shows(t, h, "refresh 1")

	h.Press("tab", "f")
	if got, want := ran(t, dir), "FX-1; touch pwned\n"; got != want {
		t.Errorf("the Action wrote %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("an Item value ran as shell code")
	}
	shows(t, h, "refresh 2")
}

func TestEveryItemNamesItsIdTitleAndTheUrlItOpens(t *testing.T) {
	dir := t.TempDir()
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+fmt.Sprintf(
		`actions = [{ key = "i", label = "which", run = "cd %s && printf '%%s|%%s|%%s\n' {{id}} {{title}} {{url}} >> ran" }]`+"\n", dir))

	h.Press("tab", "j", "i")
	if got, want := ran(t, dir), "bravo|hello bravo|https://example.com/bravo\n"; got != want {
		t.Errorf("the Action wrote %q, want %q", got, want)
	}
}

func TestAnItemWithoutAValueTheTemplateNamesHasNoSuchAction(t *testing.T) {
	dir := t.TempDir()
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+fmt.Sprintf(
		`actions = [{ key = "f", label = "fix", run = "cd %s && echo {{key}} >> ran" }]`+"\n", dir))

	h.Press("tab")
	if bottom := h.Line(10); !strings.Contains(bottom, "f fix") {
		t.Errorf("bottom border %q on alpha, want the f fix hint", bottom)
	}
	h.Press("j")
	if bottom := h.Line(10); strings.Contains(bottom, "f fix") {
		t.Errorf("bottom border %q on bravo, which has no key, has the f fix hint", bottom)
	}
	h.Press("f")
	if got := ran(t, dir); got != "" {
		t.Errorf("the Action ran on an Item without its value: %q", got)
	}
}

func TestACustomActionNamingNoValueActsOnTheWholeWidgetAndCanAsk(t *testing.T) {
	dir := t.TempDir()
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+fmt.Sprintf(
		`actions = [{ label = "sweep up", run = "cd %s && echo swept >> ran && echo all clean", confirm = true }]`+"\n", dir))

	h.Press("tab", "ctrl+k", "s", "w", "e", "e", "p", "enter")
	if _, _, ok := h.Find("Sweep up?"); !ok {
		t.Fatalf("a confirm Action didn't ask:\n%s", h.Screen())
	}
	h.Press("y")
	if got := ran(t, dir); got != "swept\n" {
		t.Errorf("the Widget Action wrote %q, want swept", got)
	}
	shows(t, h, "all clean")
}

func TestACustomActionsKeyAndEnterWinOverTheWidgetsOwn(t *testing.T) {
	dir := t.TempDir()
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+fmt.Sprintf(`actions = [
  { key = "d", label = "mine", run = "cd %s && echo mine {{id}} >> ran" },
  { label = "go", run = "cd %s && echo go {{id}} >> ran", primary = true },
]`+"\n", dir, dir))

	h.Press("tab")
	if bottom := h.Line(10); !strings.Contains(bottom, "d mine") || strings.Contains(bottom, "d done") {
		t.Errorf("bottom border %q, want d mine and not d done", bottom)
	}
	h.Press("d", "enter")
	if got, want := ran(t, dir), "mine alpha\ngo alpha\n"; got != want {
		t.Errorf("the Actions wrote %q, want %q", got, want)
	}
	if len(h.Opener.Opened) > 0 {
		t.Errorf("enter opened %v, want the custom primary Action instead", h.Opener.Opened)
	}
	hides(t, h, "done alpha")
}

func TestACustomKeyVisorOwnsIsAProblemAndTheActionStaysInThePalette(t *testing.T) {
	dir := t.TempDir()
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+fmt.Sprintf(
		`actions = [{ key = "w", label = "which", run = "cd %s && echo {{id}} >> ran" }]`+"\n", dir))
	if footer := h.Line(11); !strings.Contains(footer, "config.toml:1: key `w` of `which` in Widget") {
		t.Errorf("footer %q, want the shadowed key named", footer)
	}

	h.Press("tab", "ctrl+k", "w", "h", "i", "c", "h", "enter")
	if got := ran(t, dir); got != "alpha\n" {
		t.Errorf("the palette ran %q, want alpha", got)
	}
}

func TestTheActionMenuIsStillTitledWithTheItemWhenAWidgetWideCustomActionLeads(t *testing.T) {
	h := withPlugin(t, plugintest.Options{}, fixtureWidget+`actions = [{ label = "sweep up", run = "true" }]`+"\n")
	h.Press("tab", ".")
	shows(t, h, "hello alpha ─", "sweep up")
}
