package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oronbz/visor/internal/cli"
	"github.com/oronbz/visor/internal/hosttest"
)

// upgradeSystem is `visor upgrade` run as visor 0.6.2, launched by exe,
// with GitHub's latest release at tag.
func upgradeSystem(t *testing.T, tag, exe string, env map[string]string) (cli.System, *hosttest.Foreground, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag, "html_url": "https://github.com/oronbz/visor/releases/tag/" + tag})
	}))
	t.Cleanup(srv.Close)
	home := scratch(t)
	getenv := func(k string) string {
		if k == "HOME" {
			return home
		}
		return env[k]
	}
	pp, _ := hermetic(t, getenv)
	pp.Endpoints.GitHubREST = srv.URL
	fg := &hosttest.Foreground{}
	pp.Foreground = fg
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	return cli.System{Args: []string{"upgrade"}, Getenv: getenv, Ports: pp, Stdout: stdout, Stderr: stderr, Executable: exe, Version: "0.6.2"}, fg, stdout, stderr
}

func TestUpgradeOnTheLatestReleaseSaysSo(t *testing.T) {
	s, fg, stdout, stderr := upgradeSystem(t, "v0.6.2", "", nil)
	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if got := stdout.String(); !strings.Contains(got, "visor v0.6.2 is up to date") {
		t.Errorf("stdout %q, want up to date", got)
	}
	if len(fg.Ran()) > 0 {
		t.Errorf("ran %v with nothing newer", fg.Ran())
	}
}

func TestUpgradeRunsBrewForACaskInstall(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "Caskroom", "visor", "0.6.2", "visor")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	s, fg, stdout, stderr := upgradeSystem(t, "v0.7.0", exe, nil)
	fg.Output = "==> Upgrading visor\n"

	if code := run(t, s); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if !slices.EqualFunc(fg.Ran(), [][]string{{"sh", "-c", "brew update && brew upgrade --cask visor"}}, slices.Equal) {
		t.Errorf("ran %v, want brew update && brew upgrade --cask visor", fg.Ran())
	}
	if got := stdout.String(); !strings.Contains(got, "==> Upgrading visor") {
		t.Errorf("stdout %q, want brew's output", got)
	}
}

func TestUpgradeFailsWhenBrewDoes(t *testing.T) {
	s, fg, _, stderr := upgradeSystem(t, "v0.7.0", filepath.Join(t.TempDir(), "Caskroom", "visor", "0.6.2", "visor"), nil)
	fg.Err = errors.New("exit status 1")
	if code := run(t, s); code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "upgrade failed: exit status 1") {
		t.Errorf("stderr %q, want brew's error", stderr)
	}
}

func TestUpgradePrintsWhatToRunForOtherInstalls(t *testing.T) {
	gobin := t.TempDir()
	cases := map[string]struct {
		exe  string
		env  map[string]string
		want string
	}{
		"go install": {filepath.Join(gobin, "visor"), map[string]string{"GOBIN": gobin},
			"go install github.com/oronbz/visor/cmd/visor@latest github.com/oronbz/visor/cmd/visor-weather@latest"},
		"a download": {filepath.Join(t.TempDir(), "visor"), nil, "https://github.com/oronbz/visor/releases/tag/v0.7.0"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, fg, stdout, stderr := upgradeSystem(t, "v0.7.0", c.exe, c.env)
			if code := run(t, s); code != 0 {
				t.Fatalf("exit code %d, stderr: %s", code, stderr)
			}
			if got := stdout.String(); !strings.Contains(got, "visor v0.7.0 is out; you have v0.6.2") || !strings.Contains(got, c.want) {
				t.Errorf("stdout %q, want the new release and %q", got, c.want)
			}
			if len(fg.Ran()) > 0 {
				t.Errorf("ran %v, want it only printed", fg.Ran())
			}
		})
	}
}
