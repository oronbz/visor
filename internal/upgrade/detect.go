package upgrade

import (
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/oronbz/visor/internal/paths"
)

// Method is how visor was installed, which decides what an Upgrade does.
type Method int

const (
	// Download is an unpacked archive or anything unrecognised: the Upgrade opens the release page.
	Download Method = iota
	// Brew is the Homebrew cask, which visor upgrades by running brew.
	Brew
	// Package is a deb, rpm or apk, whose install command needs root, so it's only copied.
	Package
	// GoInstall is `go install`, whose commands are copied.
	GoInstall
)

// Detect tells the install method from exe, the executable's path with
// symlinks resolved, on goos.
func Detect(exe, goos string, getenv func(string) string) Method {
	slashed := filepath.ToSlash(exe)
	dir := path.Dir(slashed)
	switch {
	case goos != "windows" && strings.Contains(slashed, "/Caskroom/visor/"):
		return Brew
	case goos == "linux" && dir == "/usr/bin":
		return Package
	case goos != "windows" && inGoBin(dir, getenv):
		return GoInstall
	}
	return Download
}

// inGoBin reports whether dir is where `go install` puts binaries: GOBIN,
// else each GOPATH's bin, else ~/go/bin.
func inGoBin(dir string, getenv func(string) string) bool {
	if gobin := getenv("GOBIN"); gobin != "" {
		return dir == filepath.ToSlash(filepath.Clean(gobin))
	}
	gopath := getenv("GOPATH")
	if gopath == "" {
		home := paths.Home(getenv)
		if home == "" {
			return false
		}
		gopath = filepath.Join(home, "go")
	}
	for _, p := range filepath.SplitList(gopath) {
		if p != "" && dir == filepath.ToSlash(filepath.Join(p, "bin")) {
			return true
		}
	}
	return false
}

// Install is how this visor was installed, with what a Package Upgrade needs.
type Install struct {
	Method Method
	// Manager is the package manager a Package came from: dpkg, rpm, apk, or "" when none is found.
	Manager string
	// Arch is GOARCH.
	Arch string
}

// Here detects the Install of the executable at exe, the path visor was launched by.
func Here(exe string, getenv func(string) string) Install {
	if exe == "" {
		return Install{Arch: runtime.GOARCH}
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	in := Install{Method: Detect(exe, runtime.GOOS, getenv), Arch: runtime.GOARCH}
	if in.Method == Package {
		for _, m := range []string{"dpkg", "rpm", "apk"} {
			if _, err := exec.LookPath(m); err == nil {
				in.Manager = m
				break
			}
		}
	}
	return in
}
