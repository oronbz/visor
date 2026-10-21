// Package install installs, updates and uninstalls Plugins from GitHub,
// the way `herdr plugin` does: fetch a repository at a ref, show what it
// provides and what its build runs, ask for trust, build, then move it
// into the plugins directory in one step, so running instances never see a
// half-installed Plugin.
package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oronbz/visor/internal/plugin"
	"github.com/oronbz/visor/internal/ports"
)

type Fetcher interface {
	// Resolve returns the commit ref names; an empty ref is the default branch.
	Resolve(ctx context.Context, owner, repo, ref string) (string, error)
	// Fetch writes the files under subdir ("" for all) at commit into dir.
	Fetch(ctx context.Context, owner, repo, commit, subdir, dir string) error
}

var (
	// ErrDeclined is a Plugin the user didn't trust.
	ErrDeclined = errors.New("not trusted, so nothing was installed")
	validName   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// Source is where on GitHub a Plugin lives.
type Source struct {
	Owner, Repo string
	// Subdir is the Plugin's directory in the repository; empty is its root.
	Subdir string
}

func ParseSource(s string) (Source, error) {
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return Source{}, fmt.Errorf("%q isn't owner/repo or owner/repo/subdir", s)
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return Source{}, fmt.Errorf("%q isn't owner/repo or owner/repo/subdir", s)
		}
	}
	return Source{Owner: parts[0], Repo: parts[1], Subdir: strings.Join(parts[2:], "/")}, nil
}

func (s Source) String() string {
	if s.Subdir == "" {
		return s.Owner + "/" + s.Repo
	}
	return s.Owner + "/" + s.Repo + "/" + s.Subdir
}

// Plan is what a Plugin about to be built will be: the user trusts it or not.
type Plan struct {
	Manifest plugin.Manifest
	Record   plugin.Record
	Dir      string
}

type Installed struct {
	Dir      string
	Manifest plugin.Manifest
	// Record is zero for a Plugin put there by hand.
	Record plugin.Record
	// Err is why its manifest can't be read.
	Err error
}

type Installer struct {
	// Keyring holds the secrets Uninstall deletes; nil leaves them.
	Keyring ports.Keyring
	// Dir is the plugins directory.
	Dir     string
	Fetcher Fetcher
	Log     io.Writer
}

func (i Installer) Install(ctx context.Context, src Source, ref string, trust func(Plan) bool) (Plan, error) {
	commit, err := i.Fetcher.Resolve(ctx, src.Owner, src.Repo, ref)
	if err != nil {
		return Plan{}, err
	}
	plan, staged, err := i.stage(ctx, src, ref, commit)
	if err != nil {
		return plan, err
	}
	defer os.RemoveAll(staged)
	if _, err := os.Stat(plan.Dir); err == nil {
		name := plan.Manifest.Name
		return plan, fmt.Errorf("Plugin `%s` is already installed; run `visor plugin update %s`, or `visor plugin uninstall %s` first", name, name, name)
	}
	if err := i.build(ctx, plan, staged, trust); err != nil {
		return plan, err
	}
	if err := os.Rename(staged, plan.Dir); err != nil {
		return plan, err
	}
	return plan, plugin.Unbury(i.Dir, plan.Manifest.Name)
}

// Update fetches an installed Plugin's recorded ref again and, when that
// moved to a new commit, asks trust and rebuilds it. It returns the Plugin
// as it was and as it is now; they're the same when nothing changed.
func (i Installer) Update(ctx context.Context, name string, trust func(Plan) bool) (was, now plugin.Record, err error) {
	dir, err := i.dir(name)
	if err != nil {
		return was, now, err
	}
	was, err = plugin.ReadRecord(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return was, now, fmt.Errorf("Plugin `%s` wasn't installed from GitHub, so there's nothing to update it from", name)
	}
	if err != nil {
		return was, now, err
	}
	src, err := ParseSource(was.Source)
	if err != nil {
		return was, now, err
	}
	commit, err := i.Fetcher.Resolve(ctx, src.Owner, src.Repo, was.Ref)
	if err != nil || commit == was.Commit {
		return was, was, err
	}
	plan, staged, err := i.stage(ctx, src, was.Ref, commit)
	if err != nil {
		return was, now, err
	}
	defer os.RemoveAll(staged)
	if plan.Manifest.Name != name {
		return was, now, fmt.Errorf("%s now calls its Plugin `%s`; uninstall `%s` and install it again", src, plan.Manifest.Name, name)
	}
	if err := i.build(ctx, plan, staged, trust); err != nil {
		return was, now, err
	}
	trash, err := i.trash(dir)
	if err != nil {
		return was, now, err
	}
	defer os.RemoveAll(filepath.Dir(trash))
	if err := os.Rename(staged, dir); err != nil {
		return was, now, errors.Join(err, os.Rename(trash, dir))
	}
	return was, plan.Record, nil
}

// Uninstall keeps a Tombstone of the Plugin it deletes.
func (i Installer) Uninstall(name string) (plugin.Record, error) {
	dir, err := i.dir(name)
	if err != nil {
		return plugin.Record{}, err
	}
	rec, _ := plugin.ReadRecord(dir)
	if m, err := plugin.ReadManifest(dir); err == nil {
		if err := plugin.Bury(i.Dir, m, rec); err != nil {
			return rec, err
		}
		if err := (plugin.Secrets{Keyring: i.Keyring, Identity: plugin.Identity(dir, rec)}).Forget(m); err != nil {
			return rec, err
		}
	}
	trash, err := i.trash(dir)
	if err != nil {
		return rec, err
	}
	return rec, os.RemoveAll(filepath.Dir(trash))
}

func (i Installer) List() []Installed {
	entries, _ := os.ReadDir(i.Dir)
	var list []Installed
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(i.Dir, e.Name())
		m, err := plugin.ReadManifest(dir)
		rec, _ := plugin.ReadRecord(dir)
		list = append(list, Installed{Dir: dir, Manifest: m, Record: rec, Err: err})
	}
	return list
}

func (i Installer) dir(name string) (string, error) {
	if !validName.MatchString(name) {
		return "", fmt.Errorf("%q isn't a Plugin name", name)
	}
	dir := filepath.Join(i.Dir, name)
	if _, err := os.Stat(filepath.Join(dir, plugin.ManifestFile)); err != nil {
		return "", fmt.Errorf("Plugin `%s` isn't installed", name)
	}
	return dir, nil
}

// staging is where Plugins are fetched and built, and old ones wait to be
// deleted: inside the plugins directory, so moves are renames on one disk,
// and hidden, so visor doesn't read them as Plugins.
func (i Installer) staging() (string, error) {
	dir := filepath.Join(i.Dir, ".staging")
	return dir, os.MkdirAll(dir, 0o755)
}

func (i Installer) stage(ctx context.Context, src Source, ref, commit string) (Plan, string, error) {
	root, err := i.staging()
	if err != nil {
		return Plan{}, "", err
	}
	staged, err := os.MkdirTemp(root, "new-")
	if err != nil {
		return Plan{}, "", err
	}
	plan := Plan{Record: plugin.Record{Source: src.String(), Ref: ref, Commit: commit}}
	err = i.Fetcher.Fetch(ctx, src.Owner, src.Repo, commit, src.Subdir, staged)
	if err == nil {
		plan.Manifest, err = plugin.ReadManifest(staged)
		if errors.Is(err, fs.ErrNotExist) {
			err = fmt.Errorf("%s has no %s", src, plugin.ManifestFile)
		}
	}
	if err != nil {
		os.RemoveAll(staged)
		return plan, "", err
	}
	plan.Dir = filepath.Join(i.Dir, plan.Manifest.Name)
	return plan, staged, nil
}

func (i Installer) build(ctx context.Context, plan Plan, staged string, trust func(Plan) bool) error {
	if !trust(plan) {
		return ErrDeclined
	}
	for _, step := range plan.Manifest.Build {
		name := step.Run[0]
		if strings.ContainsAny(name, `/\`) && !filepath.IsAbs(name) {
			name = filepath.Join(staged, name)
		}
		cmd := exec.CommandContext(ctx, name, step.Run[1:]...)
		cmd.Dir = staged
		cmd.Stdout, cmd.Stderr = i.Log, i.Log
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build step `%s` failed: %w; nothing was installed", strings.Join(step.Run, " "), err)
		}
	}
	return plan.Record.Write(staged)
}

func (i Installer) trash(dir string) (string, error) {
	root, err := i.staging()
	if err != nil {
		return "", err
	}
	bin, err := os.MkdirTemp(root, "old-")
	if err != nil {
		return "", err
	}
	to := filepath.Join(bin, filepath.Base(dir))
	if err := os.Rename(dir, to); err != nil {
		os.Remove(bin)
		return "", err
	}
	return to, nil
}
