// Command visor is the terminal dashboard; everything it does runs through
// cli.Run.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/oronbz/visor/internal/cli"
)

func main() {
	os.Exit(cli.Run(cli.System{
		Executable: launched(),
		Args:       os.Args[1:],
		Getenv:     os.Getenv,
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}))
}

// launched is the path visor was started through, such as brew's link in
// its bin, which an Upgrade runs the new visor through; os.Executable
// resolves that link on Linux.
func launched() string {
	arg := os.Args[0]
	if !strings.ContainsRune(arg, os.PathSeparator) {
		if p, err := exec.LookPath(arg); err == nil {
			arg = p
		}
	}
	if p, err := filepath.Abs(arg); err == nil {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	exe, _ := os.Executable()
	return exe
}
