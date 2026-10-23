package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/version"
)

const (
	// Interval is how long a check's answer serves every instance.
	Interval = 12 * time.Hour
	timeout  = 10 * time.Second
)

// State is the last check's answer and what the user did about it, shared
// by every instance through one file.
type State struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    Release   `json:"latest"`
	// Closed is the release whose Upgrade popup was closed, by esc or by starting its Upgrade.
	Closed string `json:"closed,omitempty"`
}

// Newer reports whether the latest known release is ahead of running.
func (s State) Newer(running string) bool {
	return s.Latest.Tag != "" && version.Behind(running, s.Latest.Tag)
}

// Store is the State's file.
type Store struct{ File string }

// StoreIn keeps the State in dir, visor's cache directory.
func StoreIn(dir string) Store { return Store{File: filepath.Join(dir, "upgrade.json")} }

// Load reads the State; a missing or unreadable file is the zero State.
func (s Store) Load() State {
	var st State
	if b, err := os.ReadFile(s.File); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

// Update applies f to the State as it is on disk now, so another
// instance's change since this one last read it survives.
func (s Store) Update(f func(*State)) (State, error) {
	st := s.Load()
	f(&st)
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return st, err
	}
	if err := os.MkdirAll(filepath.Dir(s.File), 0o755); err != nil {
		return st, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.File), ".upgrade-*")
	if err != nil {
		return st, err
	}
	_, werr := tmp.Write(b)
	if err := errors.Join(werr, tmp.Close()); err != nil {
		_ = os.Remove(tmp.Name())
		return st, err
	}
	return st, os.Rename(tmp.Name(), s.File)
}

// Checker asks GitHub for visor's latest release.
type Checker struct {
	Store Store
	HTTP  *http.Client
	// API is GitHub's REST base URL.
	API string
}

// For checks through pp, keeping the State in cache, visor's cache directory.
func For(cache string, pp ports.Ports) Checker {
	return Checker{Store: StoreIn(cache), HTTP: pp.HTTP, API: pp.Endpoints.GitHubREST}
}

// Check is the State with the latest release: as stored when an instance
// checked within Interval of now, unless force, else fresh from GitHub and
// saved. A failed check is recorded too, so instances don't retry it each.
func (c Checker) Check(now time.Time, force bool) (State, error) {
	if st := c.Store.Load(); !force && now.Sub(st.CheckedAt) < Interval {
		return st, nil
	}
	r, err := c.latest()
	st, serr := c.Store.Update(func(s *State) {
		s.CheckedAt = now
		if err == nil {
			s.Latest = r
		}
	})
	return st, errors.Join(err, serr)
}

// latest is GitHub's latest release of Repo, which skips prereleases.
func (c Checker) latest() (Release, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.API, "/")+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("checking for a new release: GitHub answered %s", resp.Status)
	}
	var body struct {
		Tag   string `json:"tag_name"`
		Notes string `json:"body"`
		URL   string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("checking for a new release: %w", err)
	}
	if body.Tag == "" {
		return Release{}, errors.New("checking for a new release: GitHub sent no tag")
	}
	return Release{Tag: body.Tag, Notes: body.Notes, URL: body.URL}, nil
}

// commit is the hash GoReleaser's changelog starts each entry with.
var commit = regexp.MustCompile(`^[0-9a-f]{7,40} `)

// Highlights are the first n lines of the release notes, headings and blank
// lines skipped, each list entry a "• " without its commit hash.
func (r Release) Highlights(n int) []string {
	var out []string
	for line := range strings.SplitSeq(r.Notes, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, bullet := range []string{"* ", "- "} {
			if rest, ok := strings.CutPrefix(line, bullet); ok {
				line = "• " + commit.ReplaceAllString(rest, "")
			}
		}
		if out = append(out, line); len(out) == n {
			break
		}
	}
	return out
}
