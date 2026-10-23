// Command visor is the terminal dashboard; everything it does runs through
// cli.Run.
package main

import (
	"os"

	"github.com/oronbz/visor/internal/cli"
)

func main() {
	exe, _ := os.Executable()
	os.Exit(cli.Run(cli.System{
		Executable: exe,
		Args:       os.Args[1:],
		Getenv:     os.Getenv,
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}))
}
