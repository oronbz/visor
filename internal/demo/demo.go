// Package demo is the made-up world `visor demo` runs the dashboard in: a
// temp root holding a demo config.toml, Layout, Tasks and Notes, plus
// in-memory adapters for EventKit, the keyring and other CLIs, and a
// loopback server playing GitHub and Jira. The dashboard looks like a busy
// workday without reading or writing anything of the user's, but for
// copying their Now Playing and Stocks Plugins, if installed.
package demo

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oronbz/visor/internal/config"
	"github.com/oronbz/visor/internal/host"
	"github.com/oronbz/visor/internal/layout"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/plugin"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/widget"
	"github.com/oronbz/visor/internal/widgets/jira"
	"github.com/oronbz/visor/internal/widgets/prs"
)

type Options struct {
	// Now is the real time the demo starts at; its clock reads morning then.
	Now time.Time
	// TempDir is where the root is made; "" is os.TempDir().
	TempDir string
	// BundleDir is where visor's executable and the Plugins it ships with sit.
	BundleDir string
	// Plugins is the user's plugins dir, which the Plugins in borrowed are
	// copied from. Nothing in it is written.
	Plugins string
	Locale  config.Locale
}

// borrowed are the installed Plugins the demo shows, as they show what's
// public anyway: what the Mac plays and Yahoo quotes. Others, such as
// Bitrise, would show real work.
var borrowed = []string{"nowplaying", "stocks"}

// Demo is a running demo. Close removes its root.
type Demo struct {
	Root  string
	Paths paths.Paths
	// Hints are for the user, about the Now Playing and Stocks Plugins when
	// the demo leaves them out.
	Hints []string

	offset   time.Duration
	keyring  *keyring
	eventKit *eventKit
	server   *http.Server
	url      string
}

const (
	jiraSite  = "acme.atlassian.net"
	jiraEmail = "sam@acme.example"
	// viewer is the demo user's GitHub login.
	viewer = "sam-rivera"
	// keyService is the keyring service visor keeps its own credentials under.
	keyService = "visor"
)

// Lisbon's coordinates spare the Weather Plugin geocoding a city, and it
// never falls back to the user's IP address.
var weatherSettings = widget.Settings{"city": "Lisbon", "lat": "38.72", "lon": "-9.14"}

// dashboard is the author's own Layout, with Tasks and Notes where Bitrise is.
var dashboard = layout.Layout{Rows: []layout.Row{
	{Weight: 14, Cells: []layout.Cell{{ID: "clock", Weight: 4}, {ID: "calendar", Weight: 2}, {ID: "nowplaying", Weight: 4.5}}},
	{Weight: 12.5, Cells: []layout.Cell{{ID: "reminders", Weight: 3}, {ID: "meetings", Weight: 3.5}, {ID: "stocks", Weight: 3.5}, {ID: "weather", Weight: 3.5}}},
	{Weight: 11, Cells: []layout.Cell{{ID: "jira", Weight: 1}, {ID: "prs", Weight: 1}}},
	{Weight: 12, Cells: []layout.Cell{{ID: "tasks", Weight: 2.5}, {ID: "notes", Weight: 2.5}, {ID: "cpu", Weight: 3}}},
}}

func New(o Options) (*Demo, error) {
	prune(o.TempDir)
	root, err := os.MkdirTemp(o.TempDir, "visor-demo-")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, pidFile), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	offset := shift(o.Now)
	o.Now = o.Now.Add(offset)
	d := &Demo{Root: root, Paths: paths.Under(root), offset: offset, keyring: &keyring{}, eventKit: newEventKit(o.Now)}
	if err := d.lay(o); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	if err := d.serve(newWorld(o.Now)); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	return d, nil
}

// lay writes the root's files and signs the demo in to GitHub and Jira.
func (d *Demo) lay(o Options) error {
	for _, name := range borrowed {
		src := filepath.Join(o.Plugins, name)
		if _, err := os.Stat(filepath.Join(src, plugin.ManifestFile)); err != nil {
			d.Hints = append(d.Hints, fmt.Sprintf("%s isn't installed, so the demo leaves it out; `visor plugin install %s` adds it", name, name))
			continue
		}
		if err := os.CopyFS(filepath.Join(d.Paths.Plugins(), name), os.DirFS(src)); err != nil {
			d.Hints = append(d.Hints, fmt.Sprintf("the demo leaves %s out: %v", name, err))
			_ = os.RemoveAll(filepath.Join(d.Paths.Plugins(), name))
		}
	}
	file := filepath.Join(d.Paths.Config, config.FileName)
	types, _ := host.Types(d.Paths, file, o.BundleDir, o.Locale, d.getenv(nil))
	jiraSettings := widget.Settings{"site": jiraSite, "email": jiraEmail}
	files := map[string]string{
		file:                                    configText(types, o.Locale, map[string]widget.Settings{"jira": jiraSettings, "weather": weatherSettings}),
		filepath.Join(d.Paths.Data, "tasks.md"): tasks(o.Now),
		filepath.Join(d.Paths.Data, "notes.md"): notes,
	}
	for path, text := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return err
		}
	}
	if err := layout.Save(d.Paths.Config, dashboard); err != nil {
		return err
	}
	_ = d.keyring.Set(keyService, credential(withSettings(jira.Type, jiraSettings)), "demo-jira-token")
	_ = d.keyring.Set(keyService, credential(withSettings(prs.Type, nil)), "ghp_demo")
	return nil
}

// configText places one Widget of each type a fresh install shows, plus
// those of the borrowed Plugins, as `visor plugin install` adds them.
func configText(types []widget.Type, loc config.Locale, settings map[string]widget.Settings) string {
	var b strings.Builder
	b.WriteString("# visor demo: made-up data in a temp dir, removed when visor quits.\n# `visor config default` prints every option.\n")
	block := func(t widget.Type, s widget.Settings) {
		b.WriteString("\n" + config.Block(t.Name, t.Name, t.Settings))
		for _, k := range slices.Sorted(maps.Keys(s)) {
			fmt.Fprintf(&b, "%s = %s\n", k, widget.TOMLValue(s[k]))
		}
	}
	byName := map[string]widget.Type{}
	for _, t := range types {
		byName[t.Name] = t
	}
	for _, w := range config.Default(types, loc).Widgets {
		block(byName[w.Type], settings[w.Type])
	}
	for _, name := range borrowed {
		if t, ok := byName[name]; ok && t.OptIn && t.Missing == "" {
			block(t, nil)
		}
	}
	return b.String()
}

func withSettings(t widget.Type, s widget.Settings) widget.Widget {
	all := widget.Defaults(t.Settings)
	for k, v := range s {
		all[k] = v
	}
	return t.New(widget.Config{Settings: all})
}

func credential(w widget.Widget) string {
	if c, ok := w.(widget.Credentialed); ok {
		return c.Credential()
	}
	return ""
}

func (d *Demo) serve(w *world) error {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	d.url = "http://" + l.Addr().String()
	d.server = &http.Server{Handler: w, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = d.server.Serve(l) }()
	return nil
}

// Ports are base with every outside source swapped for the demo's and the
// clock shifted to the demo's morning; the Machine's stats, the opener and
// the terminal stay real.
func (d *Demo) Ports(base ports.Ports) ports.Ports {
	p := base
	p.Clock = clock{base: base.Clock, offset: d.offset}
	p.Keyring = d.keyring
	p.EventKit = d.eventKit
	p.Commands = noCommands{}
	p.Endpoints.GitHubGraphQL = d.url + "/graphql"
	p.Endpoints.GitHubREST = d.url
	p.Endpoints.AtlassianAPI = d.url
	p.Endpoints.Jira = d.url
	p.Endpoints.IPAPI = d.url
	p.Getenv = d.getenv(base.Getenv)
	return p
}

// PluginEnv points the copied Plugins' home and caches into the root too.
func (d *Demo) PluginEnv() []string {
	over := d.overrides()
	var env []string
	for _, k := range slices.Sorted(maps.Keys(over)) {
		if k == "HOME" || strings.HasPrefix(k, "XDG_") {
			env = append(env, k+"="+over[k])
		}
	}
	return env
}

// overrides are the environment the demo hides or replaces: the user's
// home and XDG dirs, and tokens that would sign the Widgets in as the user.
func (d *Demo) overrides() map[string]string {
	home := filepath.Join(d.Root, "home")
	return map[string]string{
		"HOME":                   home,
		"USERPROFILE":            home,
		"XDG_CONFIG_HOME":        filepath.Dir(d.Paths.Config),
		"XDG_DATA_HOME":          filepath.Dir(d.Paths.Data),
		"XDG_CACHE_HOME":         filepath.Dir(d.Paths.Cache),
		"VISOR_CONFIG":           "",
		"GH_TOKEN":               "",
		"GITHUB_TOKEN":           "",
		"JIRA_API_TOKEN":         "",
		"VISOR_NO_UPGRADE_CHECK": "1",
	}
}

func (d *Demo) getenv(base func(string) string) func(string) string {
	over := d.overrides()
	return func(k string) string {
		if v, ok := over[k]; ok {
			return v
		}
		if base == nil {
			return ""
		}
		return base(k)
	}
}

// Close stops the loopback server and removes the root.
func (d *Demo) Close() error {
	return errors.Join(d.server.Close(), os.RemoveAll(d.Root))
}

// noCommands runs nothing, so the demo never borrows `gh auth token`.
type noCommands struct{}

func (noCommands) Output(_ context.Context, name string, _ ...string) ([]byte, error) {
	return nil, fmt.Errorf("%s: %w", name, exec.ErrNotFound)
}
