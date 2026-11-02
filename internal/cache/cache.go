// Package cache keeps the last Payload of each fetching Widget on disk, so
// visor's first frame draws without waiting for the network and instances
// running side by side share one fetch.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oronbz/visor/internal/widget"
)

// Entry is one Widget's cached data and the outcome of its last fetch.
type Entry struct {
	Payload widget.Payload `json:"payload"`
	// FetchedAt is when Payload was fetched; zero is never.
	FetchedAt time.Time `json:"fetchedAt"`
	// CheckedAt is when the last fetch started, whatever its outcome.
	CheckedAt time.Time `json:"checkedAt"`
	Fails     int       `json:"fails,omitempty"`
	Status    Status    `json:"status"`
}

// Status is how the last fetch went, as far as it outlives the error itself.
type Status struct {
	// Error is the failure's message; empty is a success.
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
	Hint   string `json:"hint,omitempty"`
	// Setup, with Error as its hint, is a fetch that needs setup, fixed by Action.
	Setup  bool           `json:"setup,omitempty"`
	Action *widget.Action `json:"action,omitempty"`
	Until  time.Time      `json:"until,omitzero"`
}

// Store is the cache directory, ~/.cache/visor/widgets. Each entry has a
// directory of its own, so a write wakes only the instances watching that
// entry, and their watchers rescan a directory of one or two files.
type Store struct{ Dir string }

// Key names a Widget's entry: its id plus a hash of its type, settings and
// the visor version, so changing any starts a new entry. An upgrade never shows
// a payload the previous version built.
func Key(id, typ, version string, settings widget.Settings) string {
	b, _ := json.Marshal(struct {
		ID       string
		Type     string
		Version  string
		Settings widget.Settings
	}{id, typ, version, settings})
	sum := sha256.Sum256(b)
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, id)
	return safe + "-" + hex.EncodeToString(sum[:6])
}

// File is the entry file of key, the file to watch for other instances' fetches.
func (s Store) File(key string) string { return filepath.Join(s.Dir, key, "entry.json") }

func (s Store) lockFile(key string) string { return filepath.Join(s.Dir, key, "lock") }

// Load reads key's entry; ok is false when there is none or it doesn't parse.
func (s Store) Load(key string) (e Entry, ok bool) {
	b, err := os.ReadFile(s.File(key))
	if err != nil || json.Unmarshal(b, &e) != nil {
		return Entry{}, false
	}
	return e, true
}

// Save writes key's entry atomically, readable only by the user.
func (s Store) Save(key string, e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(s.Dir, key), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Join(s.Dir, key), "entry.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.File(key))
}

// Lock takes key's fetch lock at now without waiting; ok is false while
// another instance holds it. A lock taken timeout or more before now, or by
// a process that has since exited, was abandoned, and is taken over.
func (s Store) Lock(key string, now time.Time, timeout time.Duration) (unlock func(), ok bool) {
	if err := os.MkdirAll(filepath.Join(s.Dir, key), 0o700); err != nil {
		return nil, false
	}
	name := s.lockFile(key)
	for range 2 {
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = f.WriteString(now.Format(time.RFC3339Nano) + " " + strconv.Itoa(os.Getpid()))
			f.Close()
			if err != nil {
				os.Remove(name)
				return nil, false
			}
			return func() { os.Remove(name) }, true
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, false
		}
		b, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		if held(string(b), now, timeout) {
			return nil, false
		}
		os.Remove(name)
	}
	return nil, false
}

// held is whether a lock reading "<time> <pid>" is still held at now. A lock
// that doesn't parse is a write cut short, as old as it gets.
func held(lock string, now time.Time, timeout time.Duration) bool {
	stamp, owner, _ := strings.Cut(lock, " ")
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil || now.Sub(at) >= timeout {
		return false
	}
	pid, err := strconv.Atoi(owner)
	return err != nil || running(pid)
}

// Clean deletes what was last written before cutoff in the cache directory:
// entries that belong to none of keep, those of removed or reconfigured
// Widgets, the files of the flat layout older versions wrote, and in the
// entries kept, temporary files a write cut short left.
func (s Store) Clean(keep []string, cutoff time.Time) error {
	des, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, de := range des {
		if de.IsDir() && slices.Contains(keep, de.Name()) {
			leftovers, _ := filepath.Glob(filepath.Join(s.Dir, de.Name(), "entry.*.tmp"))
			for _, name := range leftovers {
				removeOlder(name, cutoff)
			}
			continue
		}
		removeOlder(filepath.Join(s.Dir, de.Name()), cutoff)
	}
	return nil
}

func removeOlder(name string, cutoff time.Time) {
	if info, err := os.Lstat(name); err == nil && info.ModTime().Before(cutoff) {
		os.RemoveAll(name)
	}
}
