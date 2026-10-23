package upgrade_test

import (
	"testing"

	"github.com/oronbz/visor/internal/upgrade"
)

func TestDetectTellsTheInstallMethodFromWhereTheExecutableLives(t *testing.T) {
	env := map[string]string{"HOME": "/home/me", "GOBIN": "/opt/gobin", "GOPATH": "/work/a:/work/b"}
	noGobin := map[string]string{"HOME": "/home/me"}
	cases := []struct {
		name, exe, goos string
		env             map[string]string
		want            upgrade.Method
	}{
		{"a Homebrew cask on Apple silicon", "/opt/homebrew/Caskroom/visor/0.6.2/visor", "darwin", env, upgrade.Brew},
		{"a Homebrew cask on Intel", "/usr/local/Caskroom/visor/0.6.2/visor", "darwin", env, upgrade.Brew},
		{"a Homebrew cask on Linux", "/home/linuxbrew/.linuxbrew/Caskroom/visor/0.6.2/visor", "linux", env, upgrade.Brew},
		{"a deb, rpm or apk package", "/usr/bin/visor", "linux", env, upgrade.Package},
		{"/usr/bin on macOS isn't a package", "/usr/bin/visor", "darwin", env, upgrade.Download},
		{"go install into GOBIN", "/opt/gobin/visor", "darwin", env, upgrade.GoInstall},
		{"go install into a GOPATH's bin", "/work/b/bin/visor", "linux", map[string]string{"GOPATH": "/work/a:/work/b"}, upgrade.GoInstall},
		{"go install into the default GOPATH", "/home/me/go/bin/visor", "darwin", noGobin, upgrade.GoInstall},
		{"an unpacked archive", "/home/me/bin/visor", "linux", env, upgrade.Download},
		{"Windows", `C:\Users\me\AppData\Local\Programs\visor\visor.exe`, "windows", env, upgrade.Download},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := upgrade.Detect(c.exe, c.goos, func(k string) string { return c.env[k] }); got != c.want {
				t.Errorf("Detect(%q, %s) = %v, want %v", c.exe, c.goos, got, c.want)
			}
		})
	}
}
