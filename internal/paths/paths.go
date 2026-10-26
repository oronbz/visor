// Package paths resolves visor's config, data and cache directories. visor does
// XDG itself on every OS because os.UserConfigDir ignores XDG_CONFIG_HOME on macOS.
package paths

import (
	"errors"
	"path/filepath"
	"strings"
)

type Paths struct {
	Config string
	Data   string
	Cache  string
}

// Under puts all three directories inside root, for tests and sandboxes.
func Under(root string) Paths {
	return Paths{
		Config: filepath.Join(root, "config", "visor"),
		Data:   filepath.Join(root, "data", "visor"),
		Cache:  filepath.Join(root, "cache", "visor"),
	}
}

// Plugins is where installed Plugins live, one directory each.
func (p Paths) Plugins() string { return filepath.Join(p.Data, "plugins") }

// Home is HOME, else USERPROFILE (Windows sets only that).
func Home(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	if h := getenv("HOME"); h != "" {
		return h
	}
	return getenv("USERPROFILE")
}

// Expand resolves only a leading `~` or `~/`, and nothing with no home.
func Expand(p, home string) string {
	if home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return p
}

// Resolve applies XDG base directories, then the OS default: ~/.config,
// ~/.local/share and ~/.cache, or %AppData% and %LocalAppData% on Windows.
func Resolve(getenv func(string) string, goos string) (Paths, error) {
	dir := func(xdg, unixDefault, winVar, winSub string) (string, error) {
		if v := getenv(xdg); filepath.IsAbs(v) {
			return filepath.Join(v, "visor"), nil
		}
		if goos == "windows" {
			base := getenv(winVar)
			if base == "" {
				base = getenv("AppData")
			}
			if base == "" {
				return "", errors.New("paths: %AppData% is not set")
			}
			return filepath.Join(base, "visor", winSub), nil
		}
		home := Home(getenv)
		if home == "" {
			return "", errors.New("paths: $HOME is not set")
		}
		return filepath.Join(home, unixDefault, "visor"), nil
	}
	var p Paths
	var err error
	if p.Config, err = dir("XDG_CONFIG_HOME", ".config", "AppData", ""); err != nil {
		return Paths{}, err
	}
	if p.Data, err = dir("XDG_DATA_HOME", filepath.Join(".local", "share"), "LocalAppData", "data"); err != nil {
		return Paths{}, err
	}
	if p.Cache, err = dir("XDG_CACHE_HOME", ".cache", "LocalAppData", "cache"); err != nil {
		return Paths{}, err
	}
	return p, nil
}
