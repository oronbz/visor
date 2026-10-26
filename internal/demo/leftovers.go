package demo

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// pidFile, in a root, names the process whose demo it is.
const pidFile = "visor-demo.pid"

// prune removes the roots in dir of demos whose process is gone, such as
// one killed outright. A root with no readable pid, or whose process may
// still run, is left alone.
func prune(dir string) {
	if dir == "" {
		dir = os.TempDir()
	}
	roots, _ := filepath.Glob(filepath.Join(dir, "visor-demo-*"))
	for _, root := range roots {
		b, err := os.ReadFile(filepath.Join(root, pidFile))
		if err != nil {
			continue
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && gone(pid) {
			_ = os.RemoveAll(root)
		}
	}
}
