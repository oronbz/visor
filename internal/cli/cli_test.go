package cli_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/cli"
	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/ports"
)

// hermetic are the real ports with every way out of the machine faked:
// other CLIs (such as gh) aren't installed, the keyring is empty, the system
// opener only records, and every HTTP endpoint is a server that fails the
// test if anything reaches it.
func hermetic(t *testing.T, getenv func(string) string) (*ports.Ports, *hosttest.Commands) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("visor reached the network: %s %s", r.Method, r.URL)
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	pp := ports.System(getenv)
	cmds := &hosttest.Commands{}
	pp.Commands = cmds
	pp.EventKit = &hosttest.EventKit{}
	pp.Keyring = &hosttest.Keyring{}
	pp.Opener = &hosttest.Opener{}
	pp.Locale = func() ports.SystemLocale { return ports.SystemLocale{} }
	e := reflect.ValueOf(&pp.Endpoints).Elem()
	for i := range e.NumField() {
		e.Field(i).SetString(srv.URL)
	}
	return &pp, cmds
}

// scratch is a temp dir that a fetch still in flight at exit may write its
// cache entry into after the test ends, so removing it is best effort.
func scratch(t *testing.T) string {
	dir, err := os.MkdirTemp("", "visor-cli")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// screen is visor's output, noting when a frame with text is first drawn
// and when the startup queries have gone out.
type screen struct {
	bytes.Buffer
	mu                sync.Mutex
	drawnOnce, asked1 sync.Once
	drawn, asked      chan struct{}
}

func newScreen() *screen { return &screen{drawn: make(chan struct{}), asked: make(chan struct{})} }

func (s *screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.Buffer.Write(p)
	if strings.TrimSpace(ansi.Strip(s.Buffer.String())) != "" {
		s.drawnOnce.Do(func() { close(s.drawn) })
	}
	// Device attributes are asked last, after every other startup query.
	if strings.Contains(s.Buffer.String(), ansi.RequestPrimaryDeviceAttributes) {
		s.asked1.Do(func() { close(s.asked) })
	}
	return n, err
}

// keys is stdin typed once visor has drawn a frame. Bubble Tea sends the
// window size from a goroutine, so a q read sooner can quit before the first
// frame is drawn.
type keys struct {
	r      *strings.Reader
	screen *screen
	// afterQueries holds the keys back until the startup queries have gone out too.
	afterQueries bool
}

func (k keys) Read(p []byte) (int, error) {
	if k.r.Len() == 0 {
		return 0, io.EOF
	}
	<-k.screen.drawn
	if k.afterQueries {
		<-k.screen.asked
	}
	return k.r.Read(p)
}

func system(t *testing.T, stdin string, args ...string) (cli.System, *screen, *bytes.Buffer) {
	home := scratch(t)
	getenv := func(k string) string { return map[string]string{"HOME": home}[k] }
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
			tea.WithWindowSize(80, 24),
			tea.WithoutSignals(),
		},
	}, stdout, stderr
}

func run(t *testing.T, s cli.System) int {
	t.Helper()
	done := make(chan int, 1)
	go func() { done <- cli.Run(s) }()
	select {
	case code := <-done:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("visor did not exit")
		return -1
	}
}

func TestVisorDrawsTheClockAndQuitsOnQ(t *testing.T) {
	s, stdout, stderr := system(t, "q")

	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	out := stdout.String()
	if !strings.Contains(ansi.Strip(out), "Clock") {
		t.Errorf("no Clock tile drawn:\n%q", out)
	}
	enter, leave := ansi.SetModeAltScreenSaveCursor, ansi.ResetModeAltScreenSaveCursor
	if !strings.Contains(out, enter) || strings.LastIndex(out, leave) < strings.LastIndex(out, enter) {
		t.Errorf("alt screen not entered and then restored:\n%q", out)
	}
}

func TestVisorDrawsEveryFrameOnTheAlternateScreen(t *testing.T) {
	s, stdout, stderr := system(t, "q")
	s.Stdin = keys{r: strings.NewReader("q"), screen: stdout, afterQueries: true}

	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	out := stdout.String()
	enter, leave := ansi.SetModeAltScreenSaveCursor, ansi.ResetModeAltScreenSaveCursor
	drawn := strings.Index(out, "Clock")
	if drawn < 0 {
		t.Fatalf("no Clock tile drawn:\n%q", out)
	}
	before, after := out[:drawn], out[drawn:]
	if strings.LastIndex(before, enter) < strings.LastIndex(before, leave) {
		t.Errorf("first frame drawn on the main screen:\n%q", out)
	}
	if n := strings.Count(after, leave); n != 1 {
		t.Errorf("left the alt screen %d times after the first frame, want once at exit:\n%q", n, out)
	}
}

func TestVisorEnablesKittyKeyboardFlagsAndRestoresThemOnExit(t *testing.T) {
	// Shift press and release, then q, all kitty-encoded.
	s, stdout, stderr := system(t, "\x1b[57441;2u\x1b[57441;1:3u\x1b[113u")

	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	out := stdout.String()
	flags := ansi.KittyDisambiguateEscapeCodes | ansi.KittyReportEventTypes | ansi.KittyReportAlternateKeys |
		ansi.KittyReportAllKeysAsEscapeCodes | ansi.KittyReportAssociatedKeys
	push, pop := ansi.PushKittyKeyboard(flags), ansi.PopKittyKeyboard(1)
	if !strings.Contains(out, push) || strings.LastIndex(out, pop) < strings.Index(out, push) {
		t.Errorf("kitty flags %d not pushed and then popped:\n%q", flags, out)
	}
}

func TestVersionFlagPrintsTheVersionWithoutStartingTheTUI(t *testing.T) {
	s, stdout, stderr := system(t, "", "--version")

	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if got, want := stdout.String(), "visor dev\n"; got != want {
		t.Errorf("stdout %q, want %q", got, want)
	}
}

func TestUnknownArgumentIsAUsageError(t *testing.T) {
	s, _, stderr := system(t, "", "--nope")

	if code := run(t, s); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage: visor") {
		t.Errorf("stderr has no usage:\n%s", stderr)
	}
}
