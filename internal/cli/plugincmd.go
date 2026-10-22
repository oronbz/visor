package cli

import (
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/oronbz/visor/internal/config"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/plugin"
	"github.com/oronbz/visor/internal/ports"
)

func pluginCommand(s System, pp ports.Ports, p paths.Paths, configFile string, locale config.Locale, editor func() config.Editor, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(s.Stderr, "visor: plugin needs a command\n\n", usage)
		return 2
	}
	switch args[0] {
	case "schema":
		s.Stdout.Write(plugin.Bundle())
		return 0
	case "check":
		return pluginCheck(s, pp, args[1:])
	case "install", "update", "uninstall", "list":
		return pluginManage(s, pp, p, configFile, locale, editor, args)
	}
	fmt.Fprintf(s.Stderr, "visor: unknown command \"plugin %s\"\n\n%s", args[0], usage)
	return 2
}

func pluginCheck(s System, pp ports.Ports, args []string) int {
	fs := flag.NewFlagSet("visor plugin check", flag.ContinueOnError)
	fs.SetOutput(s.Stderr)
	fs.Usage = func() { fmt.Fprint(s.Stderr, usage) }
	timeout := fs.Duration("timeout", 10*time.Second, "how long each step waits on the Plugin")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprint(s.Stderr, "visor: plugin check needs the Plugin's directory\n\n", usage)
		return 2
	}
	if !plugin.Check(fs.Arg(0), plugin.CheckOptions{Clock: pp.Clock, Timeout: *timeout}, s.Stdout) {
		return 1
	}
	return 0
}
