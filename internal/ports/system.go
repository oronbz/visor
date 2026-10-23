package ports

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/zalando/go-keyring"
)

// ErrSecretNotFound is what Keyring.Get and Delete return for a missing secret.
var ErrSecretNotFound = keyring.ErrNotFound

// System returns the real adapters. getenv is the process environment.
func System(getenv func(string) string) Ports {
	return Ports{
		Clock:      systemClock{},
		Opener:     systemOpener{},
		Clipboard:  osc52{},
		Editor:     execEditor{getenv: getenv},
		Keyring:    osKeyring{},
		Commands:   execCommands{},
		Foreground: execForeground{},
		EventKit:   newEventKit(),
		Machine:    newMachine(),
		Watcher:    newFSWatcher(),
		Inbox:      channelInbox{},
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		Endpoints:  DefaultEndpoints(),
		Getenv:     getenv,
		Locale:     systemLocale,
	}
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) After(d time.Duration, msg func(time.Time) tea.Msg) tea.Cmd {
	return tea.Tick(d, msg)
}

type systemOpener struct{}

func (systemOpener) Open(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

type osc52 struct{}

func (osc52) Copy(text string) tea.Cmd { return tea.SetClipboard(text) }

type execEditor struct{ getenv func(string) string }

func (e execEditor) Edit(path string, line int, done func(error) tea.Msg) tea.Cmd {
	return tea.ExecProcess(editorCommand(e.getenv, runtime.GOOS, path, line), func(err error) tea.Msg { return done(err) })
}

// editorCommand opens path in $VISUAL, else $EDITOR, else vi, at line when
// it's above zero.
func editorCommand(getenv func(string) string, goos, path string, line int) *exec.Cmd {
	editor := getenv("VISUAL")
	if editor == "" {
		editor = getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	if goos == "windows" {
		args := []string{path}
		if line > 0 {
			args = []string{"+" + strconv.Itoa(line), path}
		}
		return exec.Command(editor, args...)
	}
	// Through the shell so that EDITOR="code -w" works; the path stays out of the script as $0.
	script := editor + ` "$0"`
	if line > 0 {
		switch filepath.Base(strings.Fields(editor)[0]) {
		case "code", "code-insiders", "codium", "cursor", "windsurf":
			script = fmt.Sprintf(`%s -g "$0:%d"`, editor, line)
		case "subl", "zed", "hx", "helix":
			script = fmt.Sprintf(`%s "$0:%d"`, editor, line)
		default:
			script = fmt.Sprintf(`%s +%d "$0"`, editor, line)
		}
	}
	return exec.Command("sh", "-c", script, path)
}

type execForeground struct{}

func (execForeground) Run(name string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	return cmd.Run()
}

func (execForeground) Suspend(name string, args []string, done func(error) tea.Msg) tea.Cmd {
	return tea.ExecProcess(exec.Command(name, args...), func(err error) tea.Msg { return done(err) })
}

type osKeyring struct{}

func (osKeyring) Get(service, account string) (string, error) { return keyring.Get(service, account) }
func (osKeyring) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}
func (osKeyring) Delete(service, account string) error { return keyring.Delete(service, account) }

type execCommands struct{}

func (execCommands) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

type channelInbox struct{}

func (channelInbox) Next(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}
