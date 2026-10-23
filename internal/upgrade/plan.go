package upgrade

import (
	"fmt"
	"strings"
)

// Repo is visor's GitHub repository, whose releases are checked.
const Repo = "oronbz/visor"

// Release is a visor release on GitHub.
type Release struct {
	Tag   string `json:"tag"`
	Notes string `json:"notes,omitempty"`
	URL   string `json:"url,omitempty"`
}

// Page is the release's page on GitHub.
func (r Release) Page() string {
	if r.URL != "" {
		return r.URL
	}
	return "https://github.com/" + Repo + "/releases/tag/" + r.Tag
}

// Plan is what an Upgrade to one release does.
type Plan struct {
	Method Method
	// Command is what Brew runs, or what Package and GoInstall copy; "" for Download.
	Command string
	// Page is the release page, which Download opens.
	Page string
}

// Argv runs Command through sh, which Brew's two steps need.
func (p Plan) Argv() (string, []string) {
	return "sh", []string{"-c", p.Command}
}

// Plan is the Upgrade to r for this Install. A Package with no package
// manager found falls back to the release page.
func (in Install) Plan(r Release) Plan {
	p := Plan{Method: in.Method, Page: r.Page()}
	switch in.Method {
	case Brew:
		// brew refreshes a tap on its own only a day after the last update, so
		// without its own update a release out since then reads as installed.
		p.Command = "brew update && brew upgrade --cask visor"
	case GoInstall:
		p.Command = "go install github.com/oronbz/visor/cmd/visor@latest github.com/oronbz/visor/cmd/visor-weather@latest"
	case Package:
		p.Command = in.packageCommand(r)
		if p.Command == "" {
			p.Method = Download
		}
	}
	return p
}

// packageCommand downloads and installs the release's package, named the
// way GoReleaser's nfpm names it for the manager.
func (in Install) packageCommand(r Release) string {
	v := strings.TrimPrefix(r.Tag, "v")
	dl := fmt.Sprintf("https://github.com/%s/releases/download/%s/", Repo, r.Tag)
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[in.Arch]
	switch in.Manager {
	case "dpkg":
		f := fmt.Sprintf("visor_%s_%s.deb", v, in.Arch)
		return "curl -fLO " + dl + f + " && sudo dpkg -i " + f
	case "rpm":
		return fmt.Sprintf("sudo rpm -U %svisor-%s-1.%s.rpm", dl, v, arch)
	case "apk":
		f := fmt.Sprintf("visor_%s_%s.apk", v, arch)
		return "curl -fLO " + dl + f + " && sudo apk add --allow-untrusted " + f
	}
	return ""
}
