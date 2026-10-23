package upgrade_test

import (
	"testing"

	"github.com/oronbz/visor/internal/upgrade"
)

func TestEachInstallGetsTheUpgradeItsInstallerOffers(t *testing.T) {
	r := upgrade.Release{Tag: "v0.7.0", URL: "https://github.com/oronbz/visor/releases/tag/v0.7.0"}
	dl := "https://github.com/oronbz/visor/releases/download/v0.7.0/"
	cases := []struct {
		name    string
		in      upgrade.Install
		method  upgrade.Method
		command string
	}{
		// The tap first: brew refreshes it on its own only a day after the last
		// update, so a release out since then reads as already installed.
		{"brew", upgrade.Install{Method: upgrade.Brew}, upgrade.Brew, "brew update && brew upgrade --cask visor"},
		{"go install", upgrade.Install{Method: upgrade.GoInstall}, upgrade.GoInstall,
			"go install github.com/oronbz/visor/cmd/visor@latest github.com/oronbz/visor/cmd/visor-weather@latest"},
		{"deb on amd64", upgrade.Install{Method: upgrade.Package, Manager: "dpkg", Arch: "amd64"}, upgrade.Package,
			"curl -fLO " + dl + "visor_0.7.0_amd64.deb && sudo dpkg -i visor_0.7.0_amd64.deb"},
		{"deb on arm64", upgrade.Install{Method: upgrade.Package, Manager: "dpkg", Arch: "arm64"}, upgrade.Package,
			"curl -fLO " + dl + "visor_0.7.0_arm64.deb && sudo dpkg -i visor_0.7.0_arm64.deb"},
		{"rpm on amd64", upgrade.Install{Method: upgrade.Package, Manager: "rpm", Arch: "amd64"}, upgrade.Package,
			"sudo rpm -U " + dl + "visor-0.7.0-1.x86_64.rpm"},
		{"rpm on arm64", upgrade.Install{Method: upgrade.Package, Manager: "rpm", Arch: "arm64"}, upgrade.Package,
			"sudo rpm -U " + dl + "visor-0.7.0-1.aarch64.rpm"},
		{"apk on amd64", upgrade.Install{Method: upgrade.Package, Manager: "apk", Arch: "amd64"}, upgrade.Package,
			"curl -fLO " + dl + "visor_0.7.0_x86_64.apk && sudo apk add --allow-untrusted visor_0.7.0_x86_64.apk"},
		{"apk on arm64", upgrade.Install{Method: upgrade.Package, Manager: "apk", Arch: "arm64"}, upgrade.Package,
			"curl -fLO " + dl + "visor_0.7.0_aarch64.apk && sudo apk add --allow-untrusted visor_0.7.0_aarch64.apk"},
		{"a package with no manager found", upgrade.Install{Method: upgrade.Package, Arch: "amd64"}, upgrade.Download, ""},
		{"a download", upgrade.Install{Method: upgrade.Download}, upgrade.Download, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.in.Plan(r)
			if p.Method != c.method || p.Command != c.command {
				t.Errorf("Plan = %v %q, want %v %q", p.Method, p.Command, c.method, c.command)
			}
			if p.Page != r.URL {
				t.Errorf("Page = %q, want the release's %q", p.Page, r.URL)
			}
		})
	}
}

func TestAReleaseWithNoPageLinksToItsTag(t *testing.T) {
	p := upgrade.Install{}.Plan(upgrade.Release{Tag: "v0.7.0"})
	if want := "https://github.com/oronbz/visor/releases/tag/v0.7.0"; p.Page != want {
		t.Errorf("Page = %q, want %q", p.Page, want)
	}
}
