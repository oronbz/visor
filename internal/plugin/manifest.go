package plugin

import (
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/oronbz/visor/internal/widget"
)

// ManifestFile is the file that makes a directory a Plugin.
const ManifestFile = "visor-plugin.toml"

// Manifest is a Plugin's visor-plugin.toml (schema/manifest.json).
type Manifest struct {
	Name        string       `toml:"name"`
	Description string       `toml:"description"`
	Protocol    int          `toml:"protocol"`
	Command     []string     `toml:"-"`
	Build       []Build      `toml:"build"`
	Widgets     []WidgetType `toml:"widget"`
	// Dir is also the command's working directory.
	Dir string `toml:"-"`
	// Env is added to the environment the process inherits from visor.
	Env []string `toml:"-"`
}

// Build is a step `visor plugin install` runs.
type Build struct {
	Run []string `toml:"run"`
}

// WidgetType is a [[widget]] table: a Widget type the Plugin provides.
type WidgetType struct {
	Type        string    `toml:"type"`
	Title       string    `toml:"title"`
	Description string    `toml:"description"`
	Refresh     string    `toml:"refresh"`
	Floor       string    `toml:"floor"`
	Default     bool      `toml:"default"`
	Settings    []Setting `toml:"setting"`
}

// Setting is a [[widget.setting]] table.
type Setting struct {
	Name        string   `toml:"name"`
	Kind        string   `toml:"kind"`
	Default     any      `toml:"default"`
	Description string   `toml:"description"`
	Why         string   `toml:"why"`
	Choices     []string `toml:"choices"`
	Required    bool     `toml:"required"`
	Target      string   `toml:"target"`
}

var kinds = map[string]widget.Kind{"string": widget.String, "bool": widget.Bool, "int": widget.Int, "duration": widget.Duration, "secret": widget.Secret}

// reserved are the keys every [[widget]] block takes, so no setting may use them.
var reserved = []string{"type", "id", "title", "refresh"}

func ReadManifest(dir string) (Manifest, error) {
	file := filepath.Join(dir, ManifestFile)
	src, err := os.ReadFile(file)
	if err != nil {
		return Manifest{}, err
	}
	m, err := parseManifest(src)
	if err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", file, err)
	}
	m.Dir = dir
	return m, nil
}

// Shipped reads a manifest built into visor for a Plugin it ships with, whose
// executable is exe: the command runs exe, wherever the manifest says.
func Shipped(src []byte, exe string, env []string) (Manifest, error) {
	m, err := parseManifest(src)
	if err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", exe, err)
	}
	m.Command[0] = exe
	m.Dir = filepath.Dir(exe)
	m.Env = env
	return m, nil
}

func parseManifest(src []byte) (Manifest, error) {
	var doc map[string]any
	if err := toml.Unmarshal(src, &doc); err != nil {
		var de *toml.DecodeError
		if errors.As(err, &de) {
			row, _ := de.Position()
			return Manifest{}, fmt.Errorf("line %d: %s", row, de.Error())
		}
		return Manifest{}, err
	}
	if err := Validate("manifest.json", doc); err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := toml.Unmarshal(src, &m); err != nil {
		return Manifest{}, err
	}
	switch c := doc["command"].(type) {
	case string:
		m.Command = []string{c}
	case []any:
		for _, a := range c {
			m.Command = append(m.Command, a.(string))
		}
	}
	var names []string
	for i, w := range m.Widgets {
		if slices.Contains(names, w.Type) {
			return Manifest{}, fmt.Errorf("widget.%d: Widget type `%s` is declared twice", i, w.Type)
		}
		names = append(names, w.Type)
		if _, err := w.widgetType(); err != nil {
			return Manifest{}, fmt.Errorf("widget.%d: %w", i, err)
		}
	}
	return m, nil
}

// secrets are the secret settings m's Widget types declare.
func (m Manifest) secrets() iter.Seq[Setting] {
	return func(yield func(Setting) bool) {
		for _, wt := range m.Widgets {
			for _, s := range wt.Settings {
				if kinds[s.Kind] == widget.Secret && !yield(s) {
					return
				}
			}
		}
	}
}

// secretSetting is the secret setting called name in schema.
func secretSetting(schema []widget.Setting, name string) (widget.Setting, bool) {
	i := slices.IndexFunc(schema, func(s widget.Setting) bool { return s.Kind == widget.Secret && s.Name == name })
	if i < 0 {
		return widget.Setting{}, false
	}
	return schema[i], true
}

// widgetType is w as the Widget contract declares types, New aside.
func (w WidgetType) widgetType() (widget.Type, error) {
	t := widget.Type{Name: w.Type, Title: w.Title, Description: w.Description, Fetches: true, OptIn: !w.Default}
	var err error
	if t.Refresh, err = duration(w.Refresh); err != nil {
		return t, fmt.Errorf("refresh: %w", err)
	}
	if t.Floor, err = duration(w.Floor); err != nil {
		return t, fmt.Errorf("floor: %w", err)
	}
	for i, s := range w.Settings {
		if slices.Contains(reserved, s.Name) {
			return t, fmt.Errorf("setting.%d: `%s` is a key every Widget takes", i, s.Name)
		}
		if slices.ContainsFunc(t.Settings, func(o widget.Setting) bool { return o.Name == s.Name }) {
			return t, fmt.Errorf("setting.%d: `%s` is declared twice", i, s.Name)
		}
		ws := widget.Setting{Name: s.Name, Kind: kinds[s.Kind], Description: s.Description, Why: s.Why, Choices: s.Choices, Required: s.Required, Target: s.Target}
		if ws.Kind != widget.Secret {
			if ws.Default, err = ws.Check(s.Default); err != nil {
				return t, fmt.Errorf("setting.%d: default %w", i, err)
			}
		}
		t.Settings = append(t.Settings, ws)
	}
	return t, nil
}

func duration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}
