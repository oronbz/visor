package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	toml "github.com/pelletier/go-toml/v2"

	"github.com/oronbz/visor/internal/cli"
	"github.com/oronbz/visor/internal/config"
	"github.com/oronbz/visor/internal/host"
	"github.com/oronbz/visor/internal/plugin/plugintest"
	"github.com/oronbz/visor/internal/weather"
	"github.com/oronbz/visor/internal/weather/weathertest"
)

// sandbox is a temp home with env on top of HOME. The cache is kept outside
// the home: the default Layout's fetching Widgets write it by design, and may
// still be writing it when the test ends.
func sandbox(t *testing.T, env map[string]string, stdin string, args ...string) (cli.System, string, *screen, *bytes.Buffer) {
	home := t.TempDir()
	full := map[string]string{"HOME": home, "XDG_CACHE_HOME": scratch(t)}
	for k, v := range env {
		full[k] = strings.ReplaceAll(v, "$HOME", home)
	}
	getenv := func(k string) string { return full[k] }
	pp, _ := hermetic(t, getenv)
	stdout, stderr := newScreen(), &bytes.Buffer{}
	return cli.System{
		Args:   args,
		Getenv: getenv,
		Ports:  pp,
		Stdin:  keys{r: strings.NewReader(stdin), screen: stdout},
		Stdout: stdout,
		Stderr: stderr,
		ProgramOptions: []tea.ProgramOption{
			tea.WithWindowSize(40, 9),
			tea.WithoutSignals(),
		},
	}, home, stdout, stderr
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func titledClock(title string) string {
	return "[[widget]]\ntype = \"clock\"\ntitle = \"" + title + "\"\n"
}

func TestConfigDefaultDocumentsInstalledPluginTypes(t *testing.T) {
	s, home, stdout, stderr := sandbox(t, nil, "", "config", "default")
	plugintest.Write(t, filepath.Join(home, ".local", "share", "visor", "plugins", "fixture"), plugintest.Options{})
	if code := cli.Run(s); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"Fixture Widget. Shows what the fixture Plugin is told to", `type = "fixture"`, `greeting = "hello"`, "Words in front of every Item"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("reference has no %q", want)
		}
	}
}

// Package managers install visor and visor-weather side by side and link
// visor into a shared bin, which may hold no visor-weather link of its own.
func TestConfigDefaultDocumentsTheWeatherPluginInstalledBesideVisor(t *testing.T) {
	cellar, bin := t.TempDir(), t.TempDir()
	if err := os.Symlink(filepath.Join(weathertest.Dir(t), weather.Binary), filepath.Join(cellar, weather.Binary)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cellar, "visor"), "")
	if err := os.Symlink(filepath.Join(cellar, "visor"), filepath.Join(bin, "visor")); err != nil {
		t.Fatal(err)
	}
	s, _, stdout, stderr := sandbox(t, nil, "", "config", "default")
	s.Executable = filepath.Join(bin, "visor")
	if code := cli.Run(s); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{`type = "weather"`, `units = "metric"`, "An IP address can place you in the wrong city"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("reference has no %q", want)
		}
	}
}

func TestConfigDefaultHasNoWeatherWithoutTheBundledPlugin(t *testing.T) {
	s, _, stdout, stderr := sandbox(t, nil, "", "config", "default")
	s.Executable = filepath.Join(t.TempDir(), "visor")
	if code := cli.Run(s); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(stdout.String(), `type = "weather"`) {
		t.Error("reference documents weather with no visor-weather installed")
	}
}

func TestConfigIsReadFromXDGConfigHome(t *testing.T) {
	s, home, stdout, stderr := sandbox(t, map[string]string{"XDG_CONFIG_HOME": "$HOME/xdg"}, "q")
	writeFile(t, filepath.Join(home, "xdg", "visor", "config.toml"), titledClock("From XDG"))
	writeFile(t, filepath.Join(home, "Library", "Application Support", "visor", "config.toml"), titledClock("From Library"))
	writeFile(t, filepath.Join(home, ".config", "visor", "config.toml"), titledClock("From dot config"))

	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if out := ansi.Strip(stdout.String()); !strings.Contains(out, "From XDG") {
		t.Errorf("config not read from XDG_CONFIG_HOME:\n%s", out)
	}
}

func TestConfigFlagAndEnvOverrideThePath(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		args []string
		want string
	}{
		{"VISOR_CONFIG", map[string]string{"VISOR_CONFIG": "$HOME/env.toml"}, nil, "From env"},
		{"--config", nil, []string{"--config", "flag.toml"}, "From flag"},
		{"--config beats VISOR_CONFIG", map[string]string{"VISOR_CONFIG": "$HOME/env.toml"}, []string{"--config", "flag.toml"}, "From flag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, home, stdout, stderr := sandbox(t, tt.env, "q", tt.args...)
			for i, a := range s.Args {
				if strings.HasSuffix(a, ".toml") {
					s.Args[i] = filepath.Join(home, a)
				}
			}
			writeFile(t, filepath.Join(home, ".config", "visor", "config.toml"), titledClock("From default"))
			writeFile(t, filepath.Join(home, "env.toml"), titledClock("From env"))
			writeFile(t, filepath.Join(home, "flag.toml"), titledClock("From flag"))

			if code := run(t, s); code != 0 {
				t.Fatalf("exit code %d, stderr: %s", code, stderr)
			}
			if out := ansi.Strip(stdout.String()); !strings.Contains(out, tt.want) {
				t.Errorf("want %q drawn:\n%s", tt.want, out)
			}
		})
	}
}

func TestConfigDefaultPrintsTheFullyCommentedReference(t *testing.T) {
	s, _, stdout, stderr := sandbox(t, map[string]string{"LANG": "en_US.UTF-8"}, "", "config", "default")

	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	ref := stdout.String()
	var doc map[string]any
	if err := toml.Unmarshal([]byte(ref), &doc); err != nil {
		t.Fatalf("not valid TOML: %v\n%s", err, ref)
	}
	if len(doc) != 0 {
		t.Errorf("not everything is commented out, got keys %v", doc)
	}
	for _, want := range []string{
		`#theme = "terminal"`,
		`#time_format = "12h"`,
		`#week_start = "sunday"`,
		`#mouse = true`,
		`#[[plugin]]`,
		`#type = "clock"`,
		`#type = "calendar"`,
		`#seconds = true`,
		`#type = "tasks"`,
		`#file = ""`,
	} {
		if !strings.Contains(ref, want+"\n") {
			t.Errorf("reference lacks %q:\n%s", want, ref)
		}
	}
	for _, typ := range host.Builtins {
		for _, st := range typ.Settings {
			if !strings.Contains(ref, st.Why) {
				t.Errorf("reference lacks the why-line of %s.%s", typ.Name, st.Name)
			}
		}
	}
}

func TestConfigDefaultSaysWhichWidgetsAnEmptyConfigShows(t *testing.T) {
	s, _, stdout, stderr := sandbox(t, nil, "", "config", "default")
	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	ref := stdout.String()
	if strings.Contains(ref, "every type") {
		t.Errorf("reference claims an empty config shows every type:\n%s", ref)
	}
	i := strings.Index(ref, "the default Widgets")
	if i < 0 {
		t.Fatalf("reference doesn't say an empty config shows the default Widgets:\n%s", ref)
	}
	defaults, _, _ := strings.Cut(ref[i:], "\n# Every block")
	defaults = strings.ReplaceAll(defaults, "\n# ", " ")
	for _, want := range []string{"Clock", "Calendar", "System", "Review requests"} {
		if !strings.Contains(defaults, want) {
			t.Errorf("the default Widgets %q lack %s", defaults, want)
		}
	}
	if strings.Contains(defaults, "Script") {
		t.Errorf("the default Widgets %q name the opt-in Script", defaults)
	}
}

func TestConfigDefaultDocumentsTheThemeAndEveryColourToken(t *testing.T) {
	s, _, stdout, stderr := sandbox(t, nil, "", "config", "default")
	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	ref := stdout.String()
	for _, want := range []string{`#theme = "terminal"`, "#[colors.dark]", "#[colors.light]"} {
		if !strings.Contains(ref, want+"\n") {
			t.Errorf("reference lacks %q:\n%s", want, ref)
		}
	}
	darkAt, lightAt := strings.Index(ref, "#[colors.dark]\n"), strings.Index(ref, "#[colors.light]\n")
	if darkAt < 0 || lightAt < darkAt {
		t.Fatalf("reference lacks [colors.dark] followed by [colors.light]:\n%s", ref)
	}
	dark, light := ref[darkAt:lightAt], ref[lightAt:]
	for _, tok := range []string{"bg", "sel", "line", "faint", "dim", "fg", "bright", "accent", "warn", "ok", "bad", "info", "alt"} {
		for name, table := range map[string]string{"dark": dark, "light": light} {
			if !strings.Contains(table, "\n#"+tok+` = "#`) {
				t.Errorf("[colors.%s] in the reference lacks the token %q", name, tok)
			}
		}
	}
	if !strings.Contains(dark, `#accent = "#8aa9ff"`) || !strings.Contains(light, `#accent = "#3f63d8"`) {
		t.Errorf("token defaults aren't each palette's own colours:\n%s", ref)
	}
}

func TestConfigDefaultFollowsTheLocale(t *testing.T) {
	s, _, stdout, stderr := sandbox(t, map[string]string{"LANG": "de_DE.UTF-8"}, "", "config", "default")

	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	for _, want := range []string{`#time_format = "24h"`, `#week_start = "monday"`} {
		if !strings.Contains(stdout.String(), want+"\n") {
			t.Errorf("reference lacks %q:\n%s", want, stdout)
		}
	}
}

func TestTheReferenceUncommentedIsAValidConfig(t *testing.T) {
	s, home, stdout, stderr := sandbox(t, nil, "", "config", "default")
	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	var b strings.Builder
	for line := range strings.Lines(stdout.String()) {
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "#\n") {
			line = line[1:]
		}
		b.WriteString(line)
	}
	path := filepath.Join(home, "uncommented.toml")
	writeFile(t, path, b.String())

	types := host.Builtins
	cfg, err := config.Load(path, types, config.Locale{})
	if err != nil {
		t.Fatalf("uncommented reference doesn't parse: %v\n%s", err, b.String())
	}
	if len(cfg.Problems) > 0 {
		t.Errorf("uncommented reference has problems %v\n%s", cfg.Problems, b.String())
	}
	if len(cfg.Widgets) != len(types) {
		t.Errorf("uncommented reference places %d Widgets, want one per type", len(cfg.Widgets))
	}
}

func TestConfigInitWritesTheReferenceOnce(t *testing.T) {
	s, home, stdout, stderr := sandbox(t, nil, "", "config", "init")
	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	path := filepath.Join(home, ".config", "visor", "config.toml")
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `#type = "clock"`) {
		t.Errorf("init didn't write the reference:\n%s", written)
	}
	if !strings.Contains(stdout.String(), path) {
		t.Errorf("init doesn't say where it wrote: %q", stdout)
	}

	writeFile(t, path, "# mine\n")
	stdout.Reset()
	stderr.Reset()
	if code := run(t, s); code != 1 {
		t.Errorf("second init exit code %d, want 1", code)
	}
	if got, _ := os.ReadFile(path); string(got) != "# mine\n" {
		t.Errorf("second init overwrote the config:\n%s", got)
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Errorf("second init doesn't explain: %q", stderr)
	}
}

func TestLaunchingWithNoConfigWritesNothing(t *testing.T) {
	s, home, _, stderr := sandbox(t, nil, "q")
	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("launch wrote %v into the home", entries)
	}
}
