package cli_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/cli"
	"github.com/oronbz/visor/internal/plugin/plugintest"
)

// started is stdin typed once visor has drawn a frame and the file exists.
type started struct {
	keys
	file string
}

func (s started) Read(p []byte) (int, error) {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(s.file); err == nil {
			break
		}
	}
	return s.keys.Read(p)
}

// files is every path under dir with its contents.
func files(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		b, _ := os.ReadFile(path)
		out[path] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheDemoLeavesTheUsersHomeUntouchedAndRunsTheirPluginInItsOwnRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture's wrapper is a shell script")
	}
	home, tmp, seen := scratch(t), scratch(t), filepath.Join(scratch(t), "env")
	xdg := filepath.Join(home, "xdg")
	env := map[string]string{
		"HOME": home, "TMPDIR": tmp,
		"XDG_CONFIG_HOME": filepath.Join(xdg, "config"), "XDG_DATA_HOME": filepath.Join(xdg, "data"), "XDG_CACHE_HOME": filepath.Join(xdg, "cache"),
		"GH_TOKEN": "ghp_users_own", "JIRA_API_TOKEN": "users-own",
	}
	getenv := func(k string) string { return env[k] }
	plugintest.Write(t, filepath.Join(xdg, "data", "visor", "plugins", "nowplaying"), plugintest.Options{
		Name:    "nowplaying",
		Command: []string{"sh", "-c", `printf '%s\n%s\n' "$HOME" "$XDG_CACHE_HOME" > "$0"; exec "$1"`, seen, plugintest.Binary(t)},
	})
	for _, dir := range []string{"config", "cache"} {
		if err := os.MkdirAll(filepath.Join(xdg, dir, "visor"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	before := files(t, home)
	pp, cmds := hermetic(t, getenv)
	stdout, stderr := newScreen(), &bytes.Buffer{}
	s := cli.System{
		Args: []string{"demo", "--close-key", "ctrl+b d"}, Getenv: getenv, Ports: pp,
		Stdin: started{keys{strings.NewReader("q"), stdout}, seen}, Stdout: stdout, Stderr: stderr,
		ProgramOptions: []tea.ProgramOption{tea.WithWindowSize(160, 50), tea.WithoutSignals()},
	}

	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	screen := ansi.Strip(stdout.String())
	for _, want := range []string{"Review requests", "Fixture"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the demo didn't draw %s:\n%s", want, screen)
		}
	}
	if got, want := stderr.String(), "visor demo: stocks isn't installed, so the demo leaves it out; `visor plugin install stocks` adds it\n"; got != want {
		t.Errorf("stderr %q, want %q", got, want)
	}
	after := files(t, home)
	if len(after) != len(before) {
		t.Errorf("the demo changed HOME or the XDG dirs:\n%v\nwas\n%v", after, before)
	}
	for path, text := range before {
		if after[path] != text {
			t.Errorf("the demo changed %s", path)
		}
	}
	if ran := cmds.Ran(); len(ran) != 0 {
		t.Errorf("the demo ran %v", ran)
	}
	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("the Now Playing Plugin never started: %v", err)
	}
	for _, dir := range strings.Fields(string(b)) {
		if !strings.HasPrefix(dir, tmp) {
			t.Errorf("the Now Playing Plugin ran with %s, outside the demo's root", dir)
		}
	}
	_ = filepath.WalkDir(tmp, func(path string, _ os.DirEntry, _ error) error {
		if filepath.Base(path) == "config.toml" {
			t.Errorf("the demo left %s behind", path)
		}
		return nil
	})
}

// idle is stdin no key is ever typed on: reads wait until it's closed.
type idle chan struct{}

func (i idle) Read([]byte) (int, error) {
	<-i
	return 0, io.EOF
}

func TestClosingTheTerminalWindowQuitsTheDemoAndRemovesItsRoot(t *testing.T) {
	home, tmp := scratch(t), scratch(t)
	getenv := func(k string) string { return map[string]string{"HOME": home, "TMPDIR": tmp}[k] }
	pp, _ := hermetic(t, getenv)
	stdout, stderr := newScreen(), &bytes.Buffer{}
	stdin := make(idle)
	defer close(stdin)
	listening := make(chan chan<- os.Signal, 1)
	s := cli.System{
		Args: []string{"demo"}, Getenv: getenv, Ports: pp, Stdin: stdin, Stdout: stdout, Stderr: stderr,
		ProgramOptions: []tea.ProgramOption{tea.WithWindowSize(120, 40), tea.WithoutSignals()},
		Notify: func(c chan<- os.Signal, sig ...os.Signal) {
			if slices.Contains(sig, os.Signal(syscall.SIGHUP)) {
				listening <- c
			}
		},
	}
	roots := make(chan []string, 1)
	go func() {
		c := <-listening
		<-stdout.drawn
		found, _ := filepath.Glob(filepath.Join(tmp, "visor-demo-*"))
		roots <- found
		c <- syscall.SIGHUP
	}()

	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if found := <-roots; len(found) != 1 {
		t.Fatalf("roots while the demo ran: %v, want one", found)
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, "visor-demo-*")); len(left) != 0 {
		t.Errorf("the demo left %v behind", left)
	}
}
