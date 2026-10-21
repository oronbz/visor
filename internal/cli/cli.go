// Package cli is visor's single entry point: the TUI and every subcommand run
// through Run, so tests drive them with a temp home and captured output.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/closekey"
	"github.com/oronbz/visor/internal/config"
	"github.com/oronbz/visor/internal/host"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/version"
)

// System is everything Run takes from the process.
type System struct {
	Args   []string
	Getenv func(string) string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Ports defaults to ports.System(Getenv).
	Ports *ports.Ports
	// ProgramOptions are extra Bubble Tea options, such as a fixed window size in tests.
	ProgramOptions []tea.ProgramOption
	// Executable is visor's own path; the Plugins visor ships with sit next
	// to it. Empty ships none.
	Executable string
}

const usage = `Usage: visor [--config <path>] [--close-key <keys>]... [--version]
       visor config default   print the full commented config reference
       visor config init      write the reference to the config path, once
       visor auth jira        set up Jira: site, email and an API token,
                             which goes only to the keyring
       visor plugin install <name | owner/repo[/subdir]> [--ref <ref>] [-y]
                             fetch a Plugin from GitHub, show what it
                             runs, ask for trust, then build it; a bare
                             name is a folder of oronbz/visor-plugins
       visor plugin update [name...] [-y]
                             rebuild installed Plugins that have new commits
       visor plugin uninstall <name>
       visor plugin list      installed and [[plugin]] path Plugins
       visor plugin schema    print the JSON Schema of Plugin manifests and
                             the protocol
       visor plugin check [--timeout 10s] <dir>
                             test the Plugin in <dir> against the protocol
       visor skills install   install the visor-configure and visor-make-plugin
                             agent skills to ~/.agents/skills, linked into
                             Claude Code's skills when it's set up
       visor skills uninstall remove the skills visor installed

visor draws a full-screen dashboard of your day. Press q to quit.
The config is $XDG_CONFIG_HOME/visor/config.toml (default ~/.config/visor),
or VISOR_CONFIG, or --config.

--close-key quits visor on a key in herdr's syntax, a chord or a sequence
of two: --close-key cmd+shift+g --close-key 'ctrl+b d'.
`

func Run(s System) int {
	fs := flag.NewFlagSet("visor", flag.ContinueOnError)
	fs.SetOutput(s.Stderr)
	fs.Usage = func() { fmt.Fprint(s.Stderr, usage) }
	showVersion := fs.Bool("version", false, "print the version and exit")
	configFlag := fs.String("config", "", "path to config.toml")
	var closeKeyFlags repeated
	fs.Var(&closeKeyFlags, "close-key", "a key that quits visor, in herdr's key syntax")
	if err := fs.Parse(s.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	var closeKeys []closekey.Seq
	for _, k := range closeKeyFlags {
		seq, err := closekey.Parse(k)
		if err != nil {
			fmt.Fprintf(s.Stderr, "visor: --close-key %q: %v\n", k, err)
			return 2
		}
		closeKeys = append(closeKeys, seq)
	}
	if *showVersion {
		fmt.Fprintln(s.Stdout, "visor", version.Version)
		return 0
	}

	p, err := paths.Resolve(s.Getenv, runtime.GOOS)
	if err != nil {
		fmt.Fprintln(s.Stderr, "visor:", err)
		return 1
	}
	configFile := filepath.Join(p.Config, config.FileName)
	if v := s.Getenv("VISOR_CONFIG"); v != "" {
		configFile = v
	}
	if *configFlag != "" {
		configFile = *configFlag
	}
	pp := ports.System(s.Getenv)
	if s.Ports != nil {
		pp = *s.Ports
	}
	var system config.Locale
	if pp.Locale != nil {
		sys := pp.Locale()
		system = config.Locale{TimeFormat: sys.TimeFormat, WeekStart: sys.WeekStart}
	}
	locale := config.DetectLocale(s.Getenv, system)

	bundle := bundleDir(s.Executable)
	reference := func() string {
		types, problems := host.Types(p, configFile, bundle, locale, s.Getenv)
		for _, problem := range problems {
			fmt.Fprintln(s.Stderr, "visor:", problem)
		}
		return config.Reference(types, locale)
	}
	if args := fs.Args(); len(args) > 0 && args[0] == "plugin" {
		return pluginCommand(s, pp, p, configFile, locale, args[1:])
	}
	if args := fs.Args(); len(args) > 0 && args[0] == "skills" {
		return skillsCommand(s, args[1:])
	}
	switch strings.Join(fs.Args(), " ") {
	case "":
	case "config default":
		fmt.Fprint(s.Stdout, reference())
		return 0
	case "config init":
		return configInit(s, configFile, reference())
	case "auth jira":
		types, _ := host.Types(p, configFile, bundle, locale, s.Getenv)
		return authJira(s, pp, config.NewEditor(configFile, p.Data, types, locale))
	default:
		fmt.Fprintf(s.Stderr, "visor: unknown command %q\n\n%s", strings.Join(fs.Args(), " "), usage)
		return 2
	}
	// Popups have no HERDR_PANE_ID, and right-click can't reach them anyway.
	if s.Getenv("HERDR_PANE_ID") != "" {
		herdrRightClick(s.Getenv, pp.Commands, "pane")
		// Deferred, so a crash or a signal that ends the program restores herdr's menu too.
		defer herdrRightClick(s.Getenv, pp.Commands, "herdr")
	}
	m := host.New(host.Options{Paths: p, ConfigFile: configFile, Locale: locale, Ports: pp, CloseKeys: closeKeys, BundleDir: bundle})
	var opts []tea.ProgramOption
	// Real files get Bubble Tea's defaults, which open /dev/tty when stdin is piped.
	if _, isFile := s.Stdin.(*os.File); !isFile {
		opts = append(opts, tea.WithInput(s.Stdin))
	}
	if _, isFile := s.Stdout.(*os.File); !isFile {
		opts = append(opts, tea.WithOutput(s.Stdout))
	}
	opts = append(opts, s.ProgramOptions...)
	if c, ok := pp.Watcher.(io.Closer); ok {
		defer c.Close()
	}
	// Every copy of the model shares its Plugins, so this stops them however the program ends.
	defer m.Close()
	if _, err := tea.NewProgram(m, opts...).Run(); err != nil {
		fmt.Fprintln(s.Stderr, "visor:", err)
		return 1
	}
	return 0
}

// bundleDir is the directory visor is installed in: the real file's, since
// package managers may link visor into a shared bin.
func bundleDir(exe string) string {
	if exe == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return filepath.Dir(real)
	}
	return filepath.Dir(exe)
}

// repeated collects every value of a repeatable flag.
type repeated []string

func (r *repeated) String() string { return strings.Join(*r, " ") }

func (r *repeated) Set(v string) error {
	*r = append(*r, v)
	return nil
}

// herdrRightClick routes right-clicks in visor's herdr pane to target: "pane"
// for visor, "herdr" for herdr's own menu. Failing leaves herdr as it was, so
// errors are ignored.
func herdrRightClick(getenv func(string) string, c ports.Commands, target string) {
	bin := getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = c.Output(ctx, bin, "pane", "input", "--current", "--right-click", target)
}

// configInit writes the reference to path, refusing to touch an existing file.
func configInit(s System, path, ref string) int {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(s.Stderr, "visor:", err)
		return 1
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		fmt.Fprintf(s.Stderr, "visor: %s already exists; see `visor config default` for every option\n", path)
		return 1
	}
	if err != nil {
		fmt.Fprintln(s.Stderr, "visor:", err)
		return 1
	}
	_, werr := f.WriteString(ref)
	if err := errors.Join(werr, f.Close()); err != nil {
		fmt.Fprintln(s.Stderr, "visor:", err)
		return 1
	}
	fmt.Fprintln(s.Stdout, "wrote", path)
	return 0
}
