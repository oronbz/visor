//go:build !unix

package demo

// gone can't tell here, so every root is taken to be in use.
func gone(int) bool { return false }
