package install_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/plugin"
	"github.com/oronbz/visor/internal/plugin/install"
	"github.com/oronbz/visor/internal/plugin/plugintest"
)

func TestParseSource(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want install.Source
	}{
		{"bitrise", install.Source{Owner: "oronbz", Repo: "visor-plugins", Subdir: "bitrise"}},
		{"app-store-2", install.Source{Owner: "oronbz", Repo: "visor-plugins", Subdir: "app-store-2"}},
		{"freak4pc/visor-plugins", install.Source{Owner: "freak4pc", Repo: "visor-plugins"}},
		{"owner/repo/plugins/greet", install.Source{Owner: "owner", Repo: "repo", Subdir: "plugins/greet"}},
		{"/owner/repo/", install.Source{Owner: "owner", Repo: "repo"}},
	} {
		got, err := install.ParseSource(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseSource(%q) = %+v, %v, want %+v", tc.in, got, err, tc.want)
		}
	}
}

func TestParseSourceRefusesWhatIsNeitherANameNorARepository(t *testing.T) {
	for _, in := range []string{"", "/", "Bitrise", "-bitrise", "bit_rise", ".", "..", "owner//repo", "owner/repo/../x", "owner/./repo"} {
		if got, err := install.ParseSource(in); err == nil {
			t.Errorf("ParseSource(%q) = %+v, want an error", in, got)
		}
	}
}

// stuckKeyring holds secrets it won't delete.
type stuckKeyring struct{ *hosttest.Keyring }

func (stuckKeyring) Delete(service, account string) error { return errors.New("the keyring is locked") }

func TestUninstallThatCantDeleteASecretLeavesThePluginInstalled(t *testing.T) {
	plugins := t.TempDir()
	dir := plugintest.Write(t, filepath.Join(plugins, "greet"), plugintest.Options{Name: "greet", Command: []string{"true"}})
	rec := plugin.Record{Source: "octo/greet", Ref: "main", Commit: "abc"}
	if err := rec.Write(dir); err != nil {
		t.Fatal(err)
	}
	k := stuckKeyring{&hosttest.Keyring{}}
	if err := (plugin.Secrets{Keyring: k, Identity: "octo/greet"}).Set("token", "s3cr3t"); err != nil {
		t.Fatal(err)
	}

	if _, err := (install.Installer{Keyring: k, Dir: plugins}).Uninstall("greet"); err == nil {
		t.Fatal("Uninstall succeeded, want the keyring's error")
	}
	if _, err := os.Stat(filepath.Join(dir, plugin.ManifestFile)); err != nil {
		t.Errorf("the Plugin's folder is gone: %v", err)
	}
	if buried := plugin.Buried(plugins, nil); len(buried) > 0 {
		t.Errorf("a tombstone was kept for %v", buried)
	}
}
