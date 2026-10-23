// Package version holds the visor version, stamped at release build time.
package version

import (
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
)

// Version is set by GoReleaser with
// -ldflags "-X github.com/oronbz/visor/internal/version.Version=<tag without v>".
var Version = "dev"

// release is a plain release tag, which `go install …@vX.Y.Z` stamps; a
// checkout's build gets a pseudo-version, which isn't one.
var release = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// Current is the version this binary runs as: the stamped one, else the
// release `go install` built, else "dev".
func Current() string {
	if Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && release.MatchString(bi.Main.Version) {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return Version
}

// Parse reads "1.2.3" or "v1.2.3", ignoring a "-pre" suffix.
func Parse(v string) ([]int, bool) {
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	var out []int
	for f := range strings.SplitSeq(v, ".") {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// Behind reports whether version a is older than b; false when either isn't a release.
func Behind(a, b string) bool {
	x, ok := Parse(a)
	y, ok2 := Parse(b)
	return ok && ok2 && slices.Compare(x, y) < 0
}
