package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/oronbz/visor/internal/widget"
)

// Editor makes visor's only writes to config.toml (ADR 0003). Every edit is
// a text edit taken under a lock file: re-read the file, change it, re-parse
// the result and write it atomically only when it parses to the intended
// config, so comments and formatting elsewhere stay byte-identical.
type Editor struct {
	Path string
	// LockDir holds the lock file, outside the config dir so dotfiles
	// don't collect it.
	LockDir string
	Types   []widget.Type
	Locale  Locale
}

func NewEditor(path, dataDir string, types []widget.Type, loc Locale) Editor {
	return Editor{Path: path, LockDir: filepath.Join(dataDir, "locks"), Types: types, Locale: loc}
}

// Ref names a Widget by id; N tells apart Widgets sharing that id, 0 being
// the first in the file.
type Ref struct {
	ID string
	N  int
}

// Edit applies change to the config source and writes the result when
// check accepts the config it parses to. A config showing the default
// Widgets (no file, or no [[widget]] blocks) first gets those Widgets
// as explicit blocks, a missing file starting from the full reference, so
// the edit can't change which Widgets exist by accident.
func (e Editor) Edit(change func(src []byte, c Config) ([]byte, error), check func(before, after Config) error) error {
	unlock, err := widget.Lock(widget.LockFile(e.LockDir, "config", e.Path))
	if err != nil {
		return err
	}
	defer unlock()

	name := filepath.Base(e.Path)
	src, err := os.ReadFile(e.Path)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return fmt.Errorf("%s: %w", name, err)
	}
	before, err := parse(name, src, e.Types, e.Locale)
	if err != nil {
		return err
	}
	base, mid := src, before
	if !explicit(before) {
		if missing {
			base = []byte(Reference(e.Types, e.Locale))
		}
		for _, w := range before.Widgets {
			base = appendBlock(base, Block(w.Type, w.ID, nil))
		}
		if mid, err = e.verify(base, func(after Config) error { return sameWidgets(before, after, true) }); err != nil {
			return err
		}
	}
	out, err := change(bytes.Clone(base), mid)
	if err != nil {
		return err
	}
	if _, err := e.verify(out, func(after Config) error { return check(mid, after) }); err != nil {
		return err
	}
	if bytes.Equal(out, src) && !missing {
		return nil
	}
	return widget.WriteFile(e.Path, out)
}

// verify parses src and checks the result, wording a failure as a refusal.
func (e Editor) verify(src []byte, check func(Config) error) (Config, error) {
	name := filepath.Base(e.Path)
	after, err := parse(name, src, e.Types, e.Locale)
	if err != nil {
		return Config{}, fmt.Errorf("%s left unchanged, the edit wouldn't parse: %v", name, err)
	}
	if err := check(after); err != nil {
		return Config{}, fmt.Errorf("%s left unchanged, the edit wouldn't read back as intended: %v", name, err)
	}
	return after, nil
}

// AddWidget appends a [[widget]] block for Widget type typ with a fresh
// id and the type's required settings at their defaults.
func (e Editor) AddWidget(typ string) (Widget, error) {
	ti := slices.IndexFunc(e.Types, func(t widget.Type) bool { return t.Name == typ })
	if ti < 0 {
		return Widget{}, fmt.Errorf("unknown Widget type `%s`", typ)
	}
	var added Widget
	err := e.Edit(func(src []byte, c Config) ([]byte, error) {
		added.ID = freshID(typ, c)
		return appendBlock(src, Block(typ, added.ID, e.Types[ti].Settings)), nil
	}, func(before, after Config) error {
		if len(after.Widgets) != len(before.Widgets)+1 {
			return fmt.Errorf("%d Widgets, want %d", len(after.Widgets), len(before.Widgets)+1)
		}
		w := after.Widgets[len(after.Widgets)-1]
		if w.ID != added.ID || w.Type != typ || w.Setup != "" {
			return fmt.Errorf("the new block reads as Widget `%s` of type `%s`", w.ID, w.Type)
		}
		added = w
		after.Widgets = after.Widgets[:len(after.Widgets)-1]
		return sameWidgets(before, after, true)
	})
	return added, err
}

// Placed says config.toml places a Widget of type typ. Defaults says it
// shows the default set, which an edit writes out as blocks first.
func (e Editor) Placed(typ string) (placed, defaults bool) {
	c, err := Load(e.Path, e.Types, e.Locale)
	if err != nil {
		return false, false
	}
	return slices.ContainsFunc(c.Widgets, func(w Widget) bool { return w.Type == typ }), !explicit(c)
}

// RemoveWidget comments out Widget r's block, every line of it, so the user
// can bring it back by uncommenting.
func (e Editor) RemoveWidget(r Ref) error {
	gone := -1
	return e.Edit(func(src []byte, c Config) ([]byte, error) {
		i, err := find(c, r)
		if err != nil {
			return nil, err
		}
		if len(c.Widgets) == 1 {
			return nil, errors.New("it's the last Widget; with none, the dashboard shows the default set")
		}
		gone = i
		lines := bytes.SplitAfter(src, []byte("\n"))
		start, end := blockLines(src, c.Widgets[i].line)
		for n := start; n <= end; n++ {
			if len(bytes.TrimSpace(lines[n-1])) > 0 {
				lines[n-1] = append([]byte("# "), lines[n-1]...)
			}
		}
		return bytes.Join(lines, nil), nil
	}, func(before, after Config) error {
		before.Widgets = slices.Delete(slices.Clone(before.Widgets), gone, gone+1)
		return sameWidgets(before, after, false)
	})
}

// SetSettings writes settings into Widget r's block by commenting the block
// out and appending a copy with those keys filled in, so the old values
// stay in the file. With r.ID empty it appends a new block of type typ.
// Settings the block already has write nothing.
func (e Editor) SetSettings(typ string, r Ref, settings widget.Settings) (Widget, error) {
	ti := slices.IndexFunc(e.Types, func(t widget.Type) bool { return t.Name == typ })
	if ti < 0 {
		return Widget{}, fmt.Errorf("unknown Widget type `%s`", typ)
	}
	var keys []string
	for _, s := range e.Types[ti].Settings {
		if _, ok := settings[s.Name]; ok {
			keys = append(keys, s.Name)
		}
	}
	if len(keys) != len(settings) {
		return Widget{}, fmt.Errorf("Widget type `%s` has no such setting", typ)
	}
	var fill strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&fill, "%s = %s\n", k, widget.TOMLValue(settings[k]))
	}

	old, id, same := -1, "", false
	var set Widget
	err := e.Edit(func(src []byte, c Config) ([]byte, error) {
		if r.ID == "" {
			id = freshID(typ, c)
			return appendBlock(src, Block(typ, id, nil)+fill.String()), nil
		}
		i, err := find(c, r)
		if err != nil {
			return nil, err
		}
		if w := c.Widgets[i]; w.Type != typ {
			return nil, fmt.Errorf("Widget `%s` is a `%s` Widget, not `%s`", w.ID, w.Type, typ)
		}
		old, id = i, c.Widgets[i].ID
		if same = !slices.ContainsFunc(keys, func(k string) bool { return !reflect.DeepEqual(c.Widgets[i].Settings[k], settings[k]) }); same {
			return src, nil
		}
		lines := bytes.SplitAfter(src, []byte("\n"))
		start, end := blockLines(src, c.Widgets[i].line)
		var filled []byte
		for n := start; n <= end; n++ {
			l := lines[n-1]
			if !slices.Contains(keys, keyOf(l)) {
				filled = append(filled, l...)
			}
			if len(bytes.TrimSpace(l)) > 0 {
				lines[n-1] = append([]byte("# "), l...)
			}
		}
		if !bytes.HasSuffix(filled, []byte("\n")) {
			filled = append(filled, '\n')
		}
		return appendBlock(bytes.Join(lines, nil), string(filled)+fill.String()), nil
	}, func(before, after Config) error {
		if same {
			set = after.Widgets[old]
			return sameWidgets(before, after, true)
		}
		if old >= 0 {
			before.Widgets = slices.Delete(slices.Clone(before.Widgets), old, old+1)
		}
		if len(after.Widgets) != len(before.Widgets)+1 {
			return fmt.Errorf("%d Widgets, want %d", len(after.Widgets), len(before.Widgets)+1)
		}
		set = after.Widgets[len(after.Widgets)-1]
		if set.ID != id || set.Type != typ || set.Setup != "" {
			return fmt.Errorf("the new block reads as Widget `%s` of type `%s`", set.ID, set.Type)
		}
		for _, k := range keys {
			if !reflect.DeepEqual(set.Settings[k], settings[k]) {
				return fmt.Errorf("`%s` reads back as %v", k, set.Settings[k])
			}
		}
		after.Widgets = after.Widgets[:len(after.Widgets)-1]
		return sameWidgets(before, after, true)
	})
	return set, err
}

func keyOf(line []byte) string {
	k, _, ok := bytes.Cut(line, []byte("="))
	if !ok {
		return ""
	}
	return string(bytes.Trim(bytes.TrimSpace(k), `"`))
}

// Locate is the line of Widget r's [[widget]] header, for opening the
// config in an editor there. A Widget of the default set gets its block
// written first.
func (e Editor) Locate(r Ref) (int, error) {
	line := 0
	err := e.Edit(func(src []byte, _ Config) ([]byte, error) { return src, nil }, func(before, after Config) error {
		i, err := find(after, r)
		if err != nil {
			return err
		}
		line = after.Widgets[i].line
		return SameWidgets(before, after)
	})
	return line, err
}

// SameWidgets says how after's Widgets or global options differ from
// before's, positions in the file aside: the check for an edit that should
// change neither.
func SameWidgets(before, after Config) error { return sameWidgets(before, after, true) }

func find(c Config, r Ref) (int, error) {
	n := 0
	for i, w := range c.Widgets {
		if w.ID != r.ID {
			continue
		}
		if n == r.N {
			if w.line == 0 {
				break
			}
			return i, nil
		}
		n++
	}
	return 0, fmt.Errorf("no [[widget]] block for Widget `%s`", r.ID)
}

// blockLines are the first and last line of the block whose header is on
// line header: up to the next header, less the blank and comment lines
// that lead into it.
func blockLines(src []byte, header int) (start, end int) {
	lines := bytes.Split(src, []byte("\n"))
	end = len(lines)
	for _, h := range headerLines(src) {
		if h > header {
			end = h - 1
			break
		}
	}
	for end > header {
		l := bytes.TrimSpace(lines[end-1])
		if len(l) > 0 && l[0] != '#' {
			break
		}
		end--
	}
	return header, end
}

// explicit says the Widgets come from [[widget]] blocks, not the default set.
func explicit(c Config) bool {
	return slices.ContainsFunc(c.Widgets, func(w Widget) bool { return w.line != 0 })
}

func freshID(typ string, c Config) string {
	taken := func(id string) bool {
		return slices.ContainsFunc(c.Widgets, func(w Widget) bool { return w.ID == id })
	}
	id := typ
	for n := 2; taken(id); n++ {
		id = fmt.Sprintf("%s-%d", typ, n)
	}
	return id
}

// Block is a [[widget]] block of type typ with id and the required settings
// of schema at their defaults, as Add Widget writes it.
func Block(typ, id string, schema []widget.Setting) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[[widget]]\ntype = %q\nid = %q\n", typ, id)
	for _, s := range schema {
		if s.Required && s.Kind != widget.Secret {
			fmt.Fprintf(&b, "%s = %s\n", s.Name, widget.TOMLValue(s.Default))
		}
	}
	return b.String()
}

func appendBlock(src []byte, text string) []byte {
	switch {
	case len(src) == 0:
	case !bytes.HasSuffix(src, []byte("\n")):
		src = append(src, "\n\n"...)
	case !bytes.HasSuffix(src, []byte("\n\n")):
		src = append(src, '\n')
	}
	return append(src, text...)
}

// sameWidgets says how after's Widgets differ from before's, positions in
// the file aside. Without setup, needs-setup hints may differ too, as when
// removing one of two Widgets that shared an id.
func sameWidgets(before, after Config, setup bool) error {
	if len(before.Widgets) != len(after.Widgets) {
		return fmt.Errorf("%d Widgets, want %d", len(after.Widgets), len(before.Widgets))
	}
	for i, b := range before.Widgets {
		a := after.Widgets[i]
		b.line, a.line = 0, 0
		if !setup {
			b.Setup, a.Setup = "", ""
		}
		if !reflect.DeepEqual(a, b) {
			return fmt.Errorf("Widget `%s` would change", b.ID)
		}
	}
	before.Widgets, after.Widgets, before.Problems, after.Problems = nil, nil, nil, nil
	if !reflect.DeepEqual(before, after) {
		return errors.New("global options would change")
	}
	return nil
}
