// Package skills installs visor's bundled agent skills where agents look for
// them, and keeps the copies visor installed in step with the binary.
//
// Each installed SKILL.md carries a stamp in its frontmatter metadata: the
// visor version that wrote it and a digest of what it wrote. Only stamped
// folders are visor's to update or remove, and the digest tells an untouched
// copy from one the user edited.
package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/version"
	bundled "github.com/oronbz/visor/skills"
)

// Installer writes the bundled skills for one home directory.
type Installer struct {
	// Agents is the folder every skill is written to, ~/.agents/skills.
	Agents string
	// Claude is Claude Code's skills folder, which gets a symlink per skill;
	// "" when Claude Code isn't set up.
	Claude string
	// Version is the visor version stamped into what Install writes.
	Version string
	// Bundle holds one folder per skill; nil means the skills built into visor.
	Bundle fs.FS
}

// For finds where skills go from the environment: HOME (USERPROFILE on
// Windows) and CLAUDE_CONFIG_DIR, whose skills folder is used only when the
// Claude config folder exists.
func For(getenv func(string) string, version string) (Installer, error) {
	home := paths.Home(getenv)
	if home == "" {
		return Installer{}, errors.New("can't find your home folder: HOME is not set")
	}
	in := Installer{Agents: filepath.Join(home, ".agents", "skills"), Version: version}
	claude := getenv("CLAUDE_CONFIG_DIR")
	if claude == "" {
		claude = filepath.Join(home, ".claude")
	}
	if fi, err := os.Stat(claude); err == nil && fi.IsDir() {
		in.Claude = filepath.Join(claude, "skills")
	}
	return in, nil
}

// Report says what Install or Uninstall changed.
type Report struct {
	// Installed are the skill folders written; Linked, the symlinks to them.
	Installed, Linked []string
	Removed           []string
	// Kept are notes on paths left alone because visor didn't put them there.
	Kept []string
}

// Install writes every bundled skill to Agents, replacing each folder whole,
// and links it into Claude's skills folder.
func (in Installer) Install() (Report, error) {
	var r Report
	set, err := in.skills()
	if err != nil {
		return r, err
	}
	for _, name := range names(set) {
		dest, err := in.write(name, set[name])
		if err != nil {
			return r, err
		}
		r.Installed = append(r.Installed, dest)
		if in.Claude == "" {
			continue
		}
		link, err := in.link(name, dest)
		switch {
		case err != nil:
			return r, err
		case link == "":
			r.Kept = append(r.Kept, fmt.Sprintf("left %s alone: visor didn't put it there", filepath.Join(in.Claude, name)))
		default:
			r.Linked = append(r.Linked, link)
		}
	}
	return r, nil
}

// Uninstall removes every folder in Agents that visor stamped, and the
// symlinks in Claude's skills folder that point at them.
func (in Installer) Uninstall() (Report, error) {
	var r Report
	entries, err := os.ReadDir(in.Agents)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return r, err
	}
	for _, e := range entries {
		dir := filepath.Join(in.Agents, e.Name())
		if !e.IsDir() || !isStamped(dir) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return r, err
		}
		r.Removed = append(r.Removed, dir)
	}
	if in.Claude == "" || len(r.Removed) == 0 {
		return r, nil
	}
	links, err := os.ReadDir(in.Claude)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return r, err
	}
	for _, e := range links {
		link := filepath.Join(in.Claude, e.Name())
		target, err := os.Readlink(link)
		if err != nil || !slices.Contains(r.Removed, target) {
			continue
		}
		if err := os.Remove(link); err != nil {
			return r, err
		}
		r.Removed = append(r.Removed, link)
	}
	return r, nil
}

// Refresh brings the skills an older visor installed up to this version,
// silently when nobody edited them since. For edited ones it returns a hint
// to run `visor skills install` instead of overwriting the edits.
func (in Installer) Refresh() (hint string, err error) {
	set, err := in.skills()
	if err != nil {
		return "", err
	}
	var edited []string
	for _, name := range names(set) {
		dir := filepath.Join(in.Agents, name)
		md, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil {
			continue
		}
		st, ok := readStamp(md)
		if !ok || !older(st.version, in.Version) {
			continue
		}
		files, err := readFolder(dir)
		if err != nil {
			return "", err
		}
		if digest(files) != st.digest {
			edited = append(edited, name)
			continue
		}
		if _, err := in.write(name, set[name]); err != nil {
			return "", err
		}
	}
	switch len(edited) {
	case 0:
		return "", nil
	case 1:
		return "Run `visor skills install` to update the edited skill " + edited[0], nil
	}
	return "Run `visor skills install` to update the edited skills " + strings.Join(edited, ", "), nil
}

// skills reads the bundle as skill name → path in its folder → content.
func (in Installer) skills() (map[string]map[string][]byte, error) {
	b := in.Bundle
	if b == nil {
		b = bundled.FS
	}
	tops, err := fs.ReadDir(b, ".")
	if err != nil {
		return nil, err
	}
	set := map[string]map[string][]byte{}
	for _, top := range tops {
		if !top.IsDir() {
			continue
		}
		files := map[string][]byte{}
		err := fs.WalkDir(b, top.Name(), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := fs.ReadFile(b, p)
			files[strings.TrimPrefix(p, top.Name()+"/")] = data
			return err
		})
		if err != nil {
			return nil, err
		}
		if _, ok := files["SKILL.md"]; ok {
			set[top.Name()] = files
		}
	}
	return set, nil
}

func names(set map[string]map[string][]byte) []string {
	var out []string
	for n := range set {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// write puts skill name's files in a fresh folder under Agents, stamped, and
// swaps it in for whatever was there.
func (in Installer) write(name string, files map[string][]byte) (string, error) {
	if err := os.MkdirAll(in.Agents, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(in.Agents, "."+name+"-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	sum := digest(files)
	for p, data := range files {
		if p == "SKILL.md" {
			if data, err = stamp(data, in.Version, sum); err != nil {
				return "", fmt.Errorf("skill %s: %w", name, err)
			}
		}
		full := filepath.Join(tmp, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return "", err
		}
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(in.Agents, name)
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	return dest, os.Rename(tmp, dest)
}

// link points Claude's skills folder entry for name at dest. It replaces an
// older symlink or a folder visor stamped, and returns "" for anything else,
// which it leaves alone.
func (in Installer) link(name, dest string) (string, error) {
	if err := os.MkdirAll(in.Claude, 0o755); err != nil {
		return "", err
	}
	l := filepath.Join(in.Claude, name)
	fi, err := os.Lstat(l)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", err
	case fi.Mode()&fs.ModeSymlink != 0:
		if target, _ := os.Readlink(l); target == dest {
			return l, nil
		}
		if err := os.Remove(l); err != nil {
			return "", err
		}
	case fi.IsDir() && isStamped(l):
		if err := os.RemoveAll(l); err != nil {
			return "", err
		}
	default:
		return "", nil
	}
	return l, os.Symlink(dest, l)
}

// readFolder reads an installed skill folder the way the bundle is read,
// with the stamp taken back out of SKILL.md.
func readFolder(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if rel == "SKILL.md" {
			st, _ := readStamp(data)
			data = st.rest
		}
		files[filepath.ToSlash(rel)] = data
		return err
	})
	return files, err
}

// digest identifies a skill's files, unstamped.
func digest(files map[string][]byte) string {
	h := sha256.New()
	ps := make([]string, 0, len(files))
	for p := range files {
		ps = append(ps, p)
	}
	slices.Sort(ps)
	for _, p := range ps {
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(files[p]))
		h.Write(files[p])
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// stampRe matches the stamp exactly as stamp writes it, last in the frontmatter.
var stampRe = regexp.MustCompile(`\nmetadata:\n  visor-version: "([^"\n]*)"\n  visor-digest: "([^"\n]*)"\n---\n`)

func stamp(md []byte, version, digest string) ([]byte, error) {
	end := frontmatterEnd(md)
	if end < 0 {
		return nil, errors.New("SKILL.md has no frontmatter")
	}
	s := fmt.Sprintf("metadata:\n  visor-version: %s\n  visor-digest: %s\n", strconv.Quote(version), strconv.Quote(digest))
	return slices.Concat(md[:end], []byte(s), md[end:]), nil
}

type stampInfo struct {
	version, digest string
	// rest is the SKILL.md without the stamp.
	rest []byte
}

func readStamp(md []byte) (stampInfo, bool) {
	end := frontmatterEnd(md)
	if end < 0 {
		return stampInfo{rest: md}, false
	}
	front := md[:end+len("---\n")]
	loc := stampRe.FindSubmatchIndex(front)
	if loc == nil {
		return stampInfo{rest: md}, false
	}
	return stampInfo{
		version: string(front[loc[2]:loc[3]]),
		digest:  string(front[loc[4]:loc[5]]),
		rest:    slices.Concat(md[:loc[0]+1], md[loc[1]-len("---\n"):]),
	}, true
}

// frontmatterEnd is the offset of the frontmatter's closing "---" line, or -1.
func frontmatterEnd(md []byte) int {
	if !bytes.HasPrefix(md, []byte("---\n")) {
		return -1
	}
	i := bytes.Index(md[3:], []byte("\n---\n"))
	if i < 0 {
		return -1
	}
	return 3 + i + 1
}

func isStamped(dir string) bool {
	md, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return false
	}
	_, ok := readStamp(md)
	return ok
}

// older says whether a stamp from version was is behind version now. A dev
// build is never ahead of anything; a stamp that isn't a release is behind
// every release.
func older(was, now string) bool {
	if _, ok := version.Parse(now); !ok {
		return false
	}
	if _, ok := version.Parse(was); !ok {
		return true
	}
	return version.Behind(was, now)
}
