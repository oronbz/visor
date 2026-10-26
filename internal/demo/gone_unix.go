//go:build unix

package demo

import (
	"errors"
	"syscall"
)

// gone reports that no process has pid. A process of another user's is
// still there: signalling it is refused, not unanswered.
func gone(pid int) bool {
	return pid > 0 && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
