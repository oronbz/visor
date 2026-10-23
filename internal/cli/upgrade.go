package cli

import (
	"fmt"

	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/upgrade"
	"github.com/oronbz/visor/internal/version"
)

// upgradeCommand checks for a new release now, whatever the last check
// said, and runs brew or says what to run.
func upgradeCommand(s System, pp ports.Ports, p paths.Paths, running string) int {
	c := upgrade.For(p.Cache, pp)
	st, err := c.Check(pp.Clock.Now(), true)
	if err != nil {
		fmt.Fprintln(s.Stderr, "visor:", err)
		return 1
	}
	_, release := version.Parse(running)
	if release && !st.Newer(running) {
		fmt.Fprintf(s.Stdout, "visor v%s is up to date\n", running)
		return 0
	}
	r := st.Latest
	if release {
		running = "v" + running
	}
	fmt.Fprintf(s.Stdout, "visor %s is out; you have %s.\n", r.Tag, running)
	plan := upgrade.Here(s.Executable, s.Getenv).Plan(r)
	switch plan.Method {
	case upgrade.Brew:
		fmt.Fprintln(s.Stdout, "Running", plan.Command)
		name, args := plan.Argv()
		if err := pp.Foreground.Run(name, args, s.Stdout, s.Stderr); err != nil {
			fmt.Fprintln(s.Stderr, "visor: upgrade failed:", err)
			return 1
		}
		// Running visors close their popups for it.
		_, _ = c.Store.Update(func(st *upgrade.State) { st.Closed = r.Tag })
	case upgrade.Package, upgrade.GoInstall:
		fmt.Fprintf(s.Stdout, "Upgrade with:\n  %s\n", plan.Command)
	default:
		fmt.Fprintln(s.Stdout, "Download it from", plan.Page)
	}
	return 0
}
