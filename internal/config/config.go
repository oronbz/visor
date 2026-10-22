// Package config reads the user-owned config.toml (ADR 0003): global
// options, [[widget]] blocks validated against each Widget type's settings
// schema, and [[plugin]] entries. It never writes the file back.
package config

import (
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

const FileName = "config.toml"

// Config is a parsed config.toml, or the default when there's none.
type Config struct {
	Theme      string
	TimeFormat string
	WeekStart  string
	Mouse      bool
	Colors     theme.Overrides
	Widgets    []Widget
	Plugins    []Plugin
	// Problems are toast messages, each naming file:line, for options that
	// were ignored and Widgets that went to needs setup.
	Problems []string
}

type Widget struct {
	ID    string
	Type  string
	Title string
	// Refresh is zero when the block leaves it to the Widget type.
	Refresh  time.Duration
	Actions  []Action
	Settings widget.Settings
	// Setup, when set, is the needs-setup hint; the Widget isn't built.
	Setup string
	line  int
}

// Line is the 1-based line of the Widget's [[widget]] header, or 0 for a
// Widget of the default set.
func (w Widget) Line() int { return w.line }

// Equal reports whether two blocks configure the same Widget, wherever in
// the file they are.
func (w Widget) Equal(o Widget) bool {
	w.line, o.line = 0, 0
	return reflect.DeepEqual(w, o)
}

// Plugin is a [[plugin]] entry: a local Plugin under development.
type Plugin struct {
	Path string
}

// Common are the keys every [[widget]] block takes, whatever its type.
var Common = []string{"type", "id", "title", "refresh", "actions"}

// Action is a custom Action from a block's `actions`: a command the host
// runs itself, whatever the Widget type.
type Action struct {
	Run, Label, Key  string
	Confirm, Primary bool
}

// ActionsSetting is the `actions` key every [[widget]] block takes.
var ActionsSetting = widget.Setting{
	Name: "actions", Kind: widget.Tables, Default: []widget.Settings{},
	Description: "Each is a command template: {{name}} is the selected Item's value for name, " +
		"shell-quoted; a template without one acts on the whole Widget. Every Item has {{id}}, {{title}} and, " +
		"when it opens a page, {{url}}; script Items have each key they print, and Plugins may name more.",
	Why: `Act on what any Widget lists, such as [{ key = "f", label = "fix bug", run = "~/bin/fix-bug {{id}}", confirm = true }].`,
	Fields: []widget.Setting{
		{Name: "run", Kind: widget.String, Default: "", Required: true, Description: "The command template, run by sh (cmd on Windows)."},
		{Name: "label", Kind: widget.String, Default: "", Required: true, Description: "What the Action is called in hints and the palette."},
		{Name: "key", Kind: widget.String, Default: "", Description: "Runs it while the Widget is focused, over the Widget's own key."},
		{Name: "confirm", Kind: widget.Bool, Default: false, Description: "Ask before running it."},
		{Name: "primary", Kind: widget.Bool, Default: false, Description: "Run it on enter."},
	},
}

// Default is the config used with no config.toml: global defaults and one
// Widget of every type but the opt-in ones, with the type name as its id.
func Default(types []widget.Type, loc Locale) Config {
	c := globalsOnly(loc)
	for _, t := range types {
		if t.OptIn || t.Missing != "" {
			continue
		}
		c.Widgets = append(c.Widgets, Widget{ID: t.Name, Type: t.Name, Title: t.Title, Settings: widget.Defaults(t.Settings)})
	}
	return c
}

func globalsOnly(loc Locale) Config {
	g := widget.Defaults(Globals(loc))
	return Config{Theme: g.String("theme"), TimeFormat: g.String("time_format"), WeekStart: g.String("week_start"), Mouse: g.Bool("mouse")}
}

// Load reads the config at path, validating it against types. A missing
// file gives Default. A file that doesn't parse gives Default and an error
// naming file:line; problems that don't stop parsing land in Problems.
func Load(path string, types []widget.Type, loc Locale) (Config, error) {
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(types, loc), nil
	}
	name := filepath.Base(path)
	if err != nil {
		return Default(types, loc), fmt.Errorf("%s: %w", name, err)
	}
	return parse(name, src, types, loc)
}

func parse(name string, src []byte, types []widget.Type, loc Locale) (Config, error) {
	var doc map[string]any
	if err := toml.Unmarshal(src, &doc); err != nil {
		var de *toml.DecodeError
		if errors.As(err, &de) {
			row, _ := de.Position()
			return Default(types, loc), fmt.Errorf("%s:%d: %s", name, row, strings.TrimPrefix(de.Error(), "toml: "))
		}
		return Default(types, loc), fmt.Errorf("%s: %w", name, err)
	}
	p := parser{name: name, lines: keyLines(src), types: types}
	c := p.parse(doc, loc)
	if len(c.Widgets) == 0 {
		c.Widgets = Default(types, loc).Widgets
	}
	return c, nil
}

type parser struct {
	name     string
	lines    map[string]int
	types    []widget.Type
	problems []string
}

func (p *parser) problem(key, format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	line, ok := p.lines[key]
	// Entries of an inline array have no line of their own; use the array's.
	for !ok && strings.Contains(key, ".") {
		key = key[:strings.LastIndex(key, ".")]
		line, ok = p.lines[key]
	}
	p.problems = append(p.problems, fmt.Sprintf("%s:%d: %s", p.name, line, msg))
	return msg
}

func (p *parser) parse(doc map[string]any, loc Locale) Config {
	c := globalsOnly(loc)
	globals := Globals(loc)
	for _, k := range p.sorted(doc, "") {
		v := doc[k]
		switch k {
		case "widget":
			blocks, ok := v.([]any)
			if !ok {
				p.problem(k, "`widget` must be [[widget]] blocks")
				continue
			}
			for i, b := range blocks {
				if w, ok := p.widget(i, b); ok {
					c.Widgets = append(c.Widgets, w)
				}
			}
			continue
		case "plugin":
			blocks, ok := v.([]any)
			if !ok {
				p.problem(k, "`plugin` must be [[plugin]] blocks")
				continue
			}
			for i, b := range blocks {
				if pl, ok := p.plugin(i, b); ok {
					c.Plugins = append(c.Plugins, pl)
				}
			}
			continue
		case "colors":
			c.Colors = p.colors(v)
			continue
		}
		i := slices.IndexFunc(globals, func(s widget.Setting) bool { return s.Name == k })
		if i < 0 {
			p.problem(k, "unknown option `%s`", k)
			continue
		}
		val, err := globals[i].Check(v)
		if err != nil {
			p.problem(k, "`%s` %v; using %s", k, err, widget.TOMLValue(globals[i].Default))
			continue
		}
		switch k {
		case "theme":
			c.Theme = val.(string)
		case "time_format":
			c.TimeFormat = val.(string)
		case "week_start":
			c.WeekStart = val.(string)
		case "mouse":
			c.Mouse = val.(bool)
		}
	}
	c.Widgets = p.dedupe(c.Widgets)
	c.Problems = p.problems
	return c
}

func (p *parser) widget(i int, v any) (Widget, bool) {
	block, _ := v.(map[string]any)
	at := fmt.Sprintf("widget.%d", i)
	typeName, ok := block["type"].(string)
	if !ok || typeName == "" {
		p.problem(at, "[[widget]] needs a `type`")
		return Widget{}, false
	}
	w := Widget{Type: typeName, ID: typeName, Title: typeName}
	ti := slices.IndexFunc(p.types, func(t widget.Type) bool { return t.Name == typeName })
	var missing string
	if ti >= 0 && p.types[ti].Missing != "" {
		missing, ti = p.types[ti].Missing, -1
	}
	var schema []widget.Setting
	if ti >= 0 {
		schema = p.types[ti].Settings
		w.Title = p.types[ti].Title
	}
	w.Settings = widget.Defaults(schema)

	var setup []string
	if v, set := block["id"]; set {
		if id, ok := v.(string); ok && id != "" {
			w.ID = id
		} else {
			setup = append(setup, p.problem(at+".id", "`id` must be a non-empty string"))
		}
	}
	switch {
	case missing != "":
		setup = append(setup, missing)
	case ti < 0:
		setup = append(setup, p.problem(at+".type", "unknown Widget type `%s`", typeName))
	}
	for _, k := range p.sorted(block, at+".") {
		v := block[k]
		key := at + "." + k
		switch k {
		case "type", "id":
		case "title":
			if s, ok := v.(string); ok && s != "" {
				w.Title = s
			} else {
				setup = append(setup, p.problem(key, "`title` in Widget `%s` must be a non-empty string", w.ID))
			}
		case "refresh":
			d, err := widget.Setting{Kind: widget.Duration}.Check(v)
			if err != nil {
				setup = append(setup, p.problem(key, "`refresh` in Widget `%s` %v", w.ID, err))
				continue
			}
			w.Refresh = d.(time.Duration)
		case "actions":
			tables, err := ActionsSetting.Check(v)
			if err != nil {
				setup = append(setup, p.problem(key, "`actions` in Widget `%s` %v", w.ID, err))
				continue
			}
			for _, t := range tables.([]widget.Settings) {
				w.Actions = append(w.Actions, Action{
					Run: t.String("run"), Label: t.String("label"), Key: t.String("key"),
					Confirm: t.Bool("confirm"), Primary: t.Bool("primary"),
				})
			}
		default:
			if ti < 0 {
				continue
			}
			si := slices.IndexFunc(schema, func(s widget.Setting) bool { return s.Name == k })
			if si < 0 {
				setup = append(setup, p.problem(key, "unknown key `%s` in Widget `%s`", k, w.ID))
				continue
			}
			val, err := schema[si].Check(v)
			if err != nil {
				setup = append(setup, p.problem(key, "`%s` in Widget `%s` %v", k, w.ID, err))
				continue
			}
			w.Settings[k] = val
		}
	}
	if len(setup) > 0 {
		w.Setup = setup[0]
		if len(setup) > 1 {
			w.Setup += fmt.Sprintf(" (+%d more)", len(setup)-1)
		}
	}
	w.line = p.lines[at]
	return w, true
}

func (p *parser) plugin(i int, v any) (Plugin, bool) {
	block, _ := v.(map[string]any)
	at := fmt.Sprintf("plugin.%d", i)
	path, ok := block["path"].(string)
	if !ok || path == "" {
		p.problem(at, "[[plugin]] needs a `path`")
		return Plugin{}, false
	}
	for _, k := range p.sorted(block, at+".") {
		if k != "path" {
			p.problem(at+"."+k, "unknown key `%s` in [[plugin]]", k)
		}
	}
	return Plugin{Path: path}, true
}

// colors reads the [colors.dark] and [colors.light] override tables; bad
// entries are reported and left out.
func (p *parser) colors(v any) theme.Overrides {
	var o theme.Overrides
	tables, ok := v.(map[string]any)
	if !ok {
		p.problem("colors", "`colors` must be [colors.dark] and [colors.light] tables")
		return o
	}
	for _, mode := range p.sorted(tables, "colors.") {
		at := "colors." + mode
		var dst *map[theme.Token]color.Color
		switch mode {
		case "dark":
			dst = &o.Dark
		case "light":
			dst = &o.Light
		default:
			p.problem(at, "unknown table [%s], use [colors.dark] or [colors.light]", at)
			continue
		}
		table, ok := tables[mode].(map[string]any)
		if !ok {
			p.problem(at, "`%s` must be a [%s] table", at, at)
			continue
		}
		*dst = map[theme.Token]color.Color{}
		for _, k := range p.sorted(table, at+".") {
			tok, ok := theme.Lookup(k)
			if !ok {
				p.problem(at+"."+k, "unknown colour token `%s` in [%s]", k, at)
				continue
			}
			s, _ := table[k].(string)
			c, ok := theme.ParseColor(s)
			if !ok {
				p.problem(at+"."+k, "`%s` in [%s] must be a colour like \"#ff8800\"; ignored", k, at)
				continue
			}
			(*dst)[tok] = c
		}
	}
	return o
}

// dedupe puts every Widget sharing an id in needs setup, since the Layout
// couldn't tell them apart.
func (p *parser) dedupe(ws []Widget) []Widget {
	count := map[string]int{}
	for _, w := range ws {
		count[w.ID]++
	}
	reported := map[string]bool{}
	for i, w := range ws {
		n := count[w.ID]
		if n < 2 {
			continue
		}
		hint := fmt.Sprintf("%s Widgets with id `%s`, give each its own `id`", spell(n), w.ID)
		if w.ID == w.Type {
			hint = fmt.Sprintf("%s `%s` Widgets, add `id`", spell(n), w.Type)
		}
		if !reported[w.ID] {
			reported[w.ID] = true
			p.problems = append(p.problems, fmt.Sprintf("%s:%d: %s", p.name, w.line, hint))
		}
		ws[i].Setup = hint
	}
	return ws
}

func spell(n int) string {
	if n == 2 {
		return "two"
	}
	return fmt.Sprint(n)
}

// sorted lists m's keys in file order, using their recorded lines under prefix.
func (p *parser) sorted(m map[string]any, prefix string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if d := p.lines[prefix+a] - p.lines[prefix+b]; d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	return keys
}
