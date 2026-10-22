// Package shell runs the user's own commands, a script Widget's and custom
// Actions', through sh (cmd on Windows), and fills command templates.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// timeout stops a hung command so the user can see why.
const timeout = time.Minute

// Run runs command and returns its stdout; a failure is a Failure carrying stderr.
func Run(ctx context.Context, command string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", command)
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", command)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, Failure{Reason: "timed out", Msg: fmt.Sprintf("the command ran over %s", timeout)}
	}
	if err != nil {
		return nil, failed(err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// Failure is a command that failed; Reason is the short form, such as
// stderr's last line.
type Failure struct{ Reason, Msg string }

func (f Failure) Error() string { return f.Msg }

func failed(err error, stderr string) Failure {
	reason := err.Error()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		reason = fmt.Sprintf("exit %d", exit.ExitCode())
	}
	msg := reason
	if s := strings.TrimSpace(ansi.Strip(stderr)); s != "" {
		lines := strings.Split(s, "\n")
		reason = strings.TrimSpace(lines[len(lines)-1])
		msg += ": " + strings.Join(lines, " ")
	}
	return Failure{Reason: reason, Msg: msg}
}

// LastLine is out's last non-blank line, ANSI stripped, or "" when it has none.
func LastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(ansi.Strip(string(out))), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// Template is a command whose {{name}} placeholders are filled with values.
type Template string

var placeholder = regexp.MustCompile(`\{\{\s*([^{}\s]+)\s*\}\}`)

// PerItem reports whether t has a placeholder, so it needs an Item's values.
func (t Template) PerItem() bool { return placeholder.MatchString(string(t)) }

// Fill is t with each placeholder replaced by its value, shell-quoted so a
// value can't run as code; ok is false when values lack one.
func (t Template) Fill(values map[string]string) (command string, ok bool) {
	ok = true
	command = placeholder.ReplaceAllStringFunc(string(t), func(m string) string {
		v, has := values[placeholder.FindStringSubmatch(m)[1]]
		ok = ok && has
		return quote(v)
	})
	return command, ok
}

func quote(s string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
