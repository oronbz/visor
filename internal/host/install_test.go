package host_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/plugin"
	"github.com/oronbz/visor/internal/plugin/install"
	"github.com/oronbz/visor/internal/plugin/plugintest"
	"github.com/oronbz/visor/internal/widget"
)

// installs is visor running from root while `visor plugin` commands change
// its plugins directory, with GitHub faked.
type installs struct {
	t   *testing.T
	gh  *plugintest.GitHub
	ins install.Installer
	h   *hosttest.Host
}

func newInstalls(t *testing.T) *installs {
	t.Helper()
	root := t.TempDir()
	gh := plugintest.NewGitHub(t)
	gh.Push("octo/greet", "main", greetRepo(t, "1"))
	h := hosttest.New(t, hosttest.Options{Root: root, Width: 100, Height: 12, Types: []widget.Type{}, Config: "[[widget]]\ntype = \"greet\"\n"})
	return &installs{t: t, gh: gh, ins: install.Installer{
		Dir:     filepath.Join(paths.Under(root).Data, "plugins"),
		Fetcher: install.GitHub{API: gh.URL},
		Keyring: h.Keyring,
	}, h: h}
}

func greetRepo(t *testing.T, version string) map[string]string {
	return map[string]string{
		"visor-plugin.toml": plugintest.Manifest(t, plugintest.Options{Name: "greet"}),
		"VERSION":           version,
	}
}

func trusted(install.Plan) bool { return true }

// changed is what the running instance's watcher reports after a command.
func (in *installs) changed() { in.h.Changed(in.ins.Dir) }

func (in *installs) install() {
	in.t.Helper()
	if _, err := in.ins.Install(context.Background(), install.Source{Owner: "octo", Repo: "greet"}, "", trusted); err != nil {
		in.t.Fatal(err)
	}
	in.changed()
}

func TestARunningInstancePicksUpAnInstalledPlugin(t *testing.T) {
	in := newInstalls(t)
	shows(t, in.h, "needs setup", "unknown Widget type `greet`")

	in.install()
	shows(t, in.h, "hello alpha", "refresh 1")
	hides(t, in.h, "needs setup")
}

func TestUninstallingPutsTheWidgetsInNeedsSetupNamingTheInstallCommand(t *testing.T) {
	in := newInstalls(t)
	in.install()

	if _, err := in.ins.Uninstall("greet"); err != nil {
		t.Fatal(err)
	}
	in.changed()
	shows(t, in.h, "needs setup", "Plugin `greet` isn't installed", "visor plugin install octo/greet")
	hides(t, in.h, "hello alpha", "unknown Widget type")
}

func TestUninstallingDeletesThePluginsSecrets(t *testing.T) {
	in := newInstalls(t)
	in.install()
	setToken(in.h, secret)
	in.h.ReceiveUntil(func() bool { return strings.Contains(in.h.Screen(), "hello alpha (signed in)") })

	stored := plugin.Secrets{Keyring: in.h.Keyring, Identity: "octo/greet"}
	if got, _ := stored.Get("token"); got != secret {
		t.Fatalf("the keyring holds %q before the uninstall, want the secret", got)
	}

	if _, err := in.ins.Uninstall("greet"); err != nil {
		t.Fatal(err)
	}
	if got, ok := stored.Get("token"); ok {
		t.Errorf("the keyring still holds %q after the uninstall", got)
	}
}

func TestAnInstanceStartedAfterAnUninstallStillNamesTheInstallCommand(t *testing.T) {
	in := newInstalls(t)
	in.install()
	if _, err := in.ins.Uninstall("greet"); err != nil {
		t.Fatal(err)
	}

	started := func() *hosttest.Host {
		return hosttest.New(t, hosttest.Options{Root: in.h.Root, Width: 100, Height: 12, Types: []widget.Type{}})
	}
	later := started()
	shows(t, later, "needs setup", "Plugin `greet` isn't installed", "visor plugin install octo/greet")
	hides(t, later, "unknown Widget type")

	in.install()
	again := started()
	shows(t, again, "hello alpha")
	hides(t, again, "needs setup")
	if got := in.ins.List(); len(got) != 1 {
		t.Errorf("installed %v, want greet alone", got)
	}
}

func TestARunningInstanceRestartsThePluginAfterAnUpdate(t *testing.T) {
	in := newInstalls(t)
	in.install()
	in.h.Press("ctrl+r")
	shows(t, in.h, "refresh 2")

	in.gh.Push("octo/greet", "main", greetRepo(t, "2"))
	if _, _, err := in.ins.Update(context.Background(), "greet", trusted); err != nil {
		t.Fatal(err)
	}
	in.changed()
	shows(t, in.h, "hello alpha", "refresh 1")
}
