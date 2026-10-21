// Package plugintest builds the fixture Plugin, a tiny Go program in
// testdata that plays scripted protocol behaviours, and lays out Plugin
// directories for tests.
package plugintest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	build    sync.Once
	binary   string
	buildErr error
)

// Binary is the fixture Plugin's executable, built once per test process.
func Binary(t testing.TB) string {
	t.Helper()
	build.Do(func() {
		_, here, _, _ := runtime.Caller(0)
		dir, err := os.MkdirTemp("", "visor-fixture-")
		if err != nil {
			buildErr = err
			return
		}
		binary = filepath.Join(dir, "fixture")
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		src := filepath.Join(filepath.Dir(here), "testdata", "fixture")
		// The test binary doesn't link the fixture, so reading its sources is
		// what tells go test's cache that a change to them matters.
		files, _ := os.ReadDir(src)
		for _, f := range files {
			if !f.IsDir() {
				_, _ = os.ReadFile(filepath.Join(src, f.Name()))
			}
		}
		cmd := exec.Command("go", "build", "-o", binary, ".")
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("building the fixture Plugin: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return binary
}

type Options struct {
	// Name defaults to "fixture", which is also its Widget type's name.
	Name string
	// Protocol is the version the manifest declares; zero is 1.
	Protocol int
	// Args are passed to the fixture, such as "-protocol", "2".
	Args []string
	// Refresh is the Widget type's default interval; "" is "1m".
	Refresh string
	// Default places a Widget of the type when config.toml places none.
	Default bool
	// Mode is the `mode` setting's default; "" is "items".
	Mode string
	// Command replaces the built fixture (and Args) as the command.
	Command []string
	Build   [][]string
	// TokenRequired makes the `token` secret setting required.
	TokenRequired bool
}

// Write lays out a fixture Plugin in dir: its visor-plugin.toml, whose
// command runs the built fixture. It returns dir.
func Write(t testing.TB, dir string, o Options) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "visor-plugin.toml"), []byte(Manifest(t, o)), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func Manifest(t testing.TB, o Options) string {
	t.Helper()
	if o.Name == "" {
		o.Name = "fixture"
	}
	if o.Protocol == 0 {
		o.Protocol = 1
	}
	if o.Refresh == "" {
		o.Refresh = "1m"
	}
	if o.Mode == "" {
		o.Mode = "items"
	}
	if o.Command == nil {
		o.Command = append([]string{Binary(t)}, o.Args...)
	}
	var build strings.Builder
	for _, step := range o.Build {
		fmt.Fprintf(&build, "\n[[build]]\nrun = [%s]\n", quoted(step))
	}
	return fmt.Sprintf(`name = %q
description = "Scripted protocol behaviours for tests"
protocol = %d
command = [%s]

[[widget]]
type = %q
title = "Fixture"
description = "Shows what the fixture Plugin is told to"
refresh = %q
floor = "10s"
default = %t

[[widget.setting]]
name = "greeting"
kind = "string"
default = "hello"
description = "Words in front of every Item"

[[widget.setting]]
name = "mode"
kind = "string"
default = %q
description = "What refreshes return"
choices = ["items", "lines", "raw-colour", "malformed", "unknown-field", "setup", "error", "two-images", "bad-side", "image-file", "leak", "leak-status", "rejected", "unknown-secret", "own-secret"]

[[widget.setting]]
name = "token"
kind = "secret"
description = "Your Fixture token"
target = "https://example.com/tokens/new"
required = %t
%s`, o.Name, o.Protocol, quoted(o.Command), o.Name, o.Refresh, o.Default, o.Mode, o.TokenRequired, build.String())
}

func quoted(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = fmt.Sprintf("%q", a)
	}
	return strings.Join(q, ", ")
}
