// Package ports holds visor's outside-world adapters. The host reaches the world
// only through Ports, so the headless test host can swap every one for a fake.
package ports

import (
	"context"
	"io"
	"net/http"
	"time"

	tea "charm.land/bubbletea/v2"
)

type Ports struct {
	Clock     Clock
	Opener    Opener
	Clipboard Clipboard
	Editor    Editor
	Keyring   Keyring
	Commands  Commands
	// Foreground runs commands on the terminal, such as brew for an Upgrade.
	Foreground Foreground
	EventKit   EventKit
	Machine    Machine
	Watcher    Watcher
	Inbox      Inbox
	HTTP       *http.Client
	Endpoints  Endpoints
	Getenv     func(key string) string
	Locale     func() SystemLocale
}

// SystemLocale is the clock style ("12h" or "24h") and first weekday
// ("sunday") the OS is set to, each "" where the OS doesn't say. On macOS it
// follows the region and the 24-hour time toggle, which a terminal's LANG
// doesn't carry.
type SystemLocale struct {
	TimeFormat string
	WeekStart  string
}

// Clock is the only source of time. Timers go through After so that a fake
// clock can fire them deterministically instead of sleeping.
type Clock interface {
	Now() time.Time
	// After returns a Cmd that delivers msg(t) once d has passed, t being the time it fired.
	After(d time.Duration, msg func(time.Time) tea.Msg) tea.Cmd
}

// Opener hands a URL or path to the system opener (open, xdg-open, rundll32).
type Opener interface {
	Open(target string) error
}

type Clipboard interface {
	Copy(text string) tea.Cmd
}

// Editor hands a file to $EDITOR while the TUI is suspended, then calls
// done. A line above zero opens the file at that line.
type Editor interface {
	Edit(path string, line int, done func(error) tea.Msg) tea.Cmd
}

type Keyring interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

// Watcher reports changes to files on disk, whoever made them: another
// visor instance, an editor, or this instance itself.
type Watcher interface {
	// Watch replaces the set of watched files. A file that doesn't exist
	// yet is reported when it's created. A directory is also reported when
	// an entry directly in it is created, removed or renamed.
	Watch(files []string) error
	// Next returns a Cmd that waits for the next change and delivers
	// msg(files changed), each spelt as it was given to Watch.
	Next(msg func(files []string) tea.Msg) tea.Cmd
}

// Inbox turns messages that arrive on their own, from goroutines outside the
// program such as a Plugin's reader, into Cmds.
type Inbox interface {
	// Next returns a Cmd that waits for the next message on ch.
	Next(ch <-chan tea.Msg) tea.Cmd
}

type Machine interface {
	Stats() (Stats, error)
}

type Stats struct {
	// CPU is the share of CPU time in use since the previous read, 0 to 1.
	CPU float64
	// MemUsed never exceeds MemTotal.
	MemUsed, MemTotal uint64
	// Volumes are the startup disk, unless it can't be read, then each other
	// volume mounted.
	Volumes []Volume
	// Interfaces are those traffic leaves by, Wi-Fi and Ethernet: not loopback,
	// nor a VPN's or a container's, which would count the same bytes twice.
	Interfaces []Interface
}

type Volume struct {
	// Name is what Finder calls it; the startup disk's is empty.
	Name    string
	Startup bool
	// Free never exceeds Total, which is never zero.
	Total, Free uint64
}

// Interface is a network interface and the bytes it has moved since it came up.
type Interface struct {
	Name       string
	Recv, Sent uint64
}

// Commands runs other CLIs visor borrows from, such as `gh auth token` and `herdr`.
type Commands interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Foreground runs a command with the terminal as its stdio, so its output shows.
type Foreground interface {
	// Run runs it to the end on stdin and the given writers, for subcommands.
	Run(name string, args []string, stdout, stderr io.Writer) error
	// Suspend runs it while the TUI is suspended, then calls done.
	Suspend(name string, args []string, done func(error) tea.Msg) tea.Cmd
}

type Access int

const (
	AccessUnsupported Access = iota
	AccessUndetermined
	AccessDenied
	AccessGranted
	// AccessUnaskable is undetermined access the last Request in this
	// process got no answer for, as when the terminal app lacks the
	// entitlement and macOS refuses without a prompt.
	AccessUnaskable
)

type Entity int

const (
	Events Entity = iota
	Reminders
)

// EventKit is the macOS calendar and reminders store. Off darwin it reports
// AccessUnsupported.
type EventKit interface {
	// Status reads e's access without ever asking the user.
	Status(e Entity) Access
	// Request asks the user for full access to e when they haven't answered
	// yet, blocking until they do; otherwise it reports the access as it is.
	Request(ctx context.Context, e Entity) (Access, error)
	// Events are the events from every calendar that overlap [from, to).
	// Without granted access it fails rather than asking.
	Events(ctx context.Context, from, to time.Time) ([]Event, error)
	// Reminders are the reminders not yet completed, from every list.
	// Without granted access it fails rather than asking.
	Reminders(ctx context.Context) ([]Reminder, error)
	Complete(ctx context.Context, id string) error
}

type Reminder struct {
	ID    string
	Title string
	List  string
	// Due is zero when the reminder has no due date.
	Due time.Time
	// DateOnly is set when Due is a day without a time of day.
	DateOnly bool
}

// Event is one occurrence of a calendar event.
type Event struct {
	// ID is the event's EventKit identifier; occurrences of a recurring
	// event share it.
	ID         string
	Title      string
	Start, End time.Time
	AllDay     bool
	Location   string
	Calendar   string
	Declined   bool
	Canceled   bool
}

// Endpoints holds the base URLs of every HTTP service visor talks to, so tests
// can point them at httptest servers.
type Endpoints struct {
	GitHubGraphQL string
	GitHubREST    string
	AtlassianAPI  string
	// Jira, when set, stands in for every Jira site's URL, so tests reach a
	// fake Jira whatever `site` says. It's empty for real use.
	Jira      string
	OpenMeteo string
	Geocoding string
	IPAPI     string
}

func DefaultEndpoints() Endpoints {
	return Endpoints{
		GitHubGraphQL: "https://api.github.com/graphql",
		GitHubREST:    "https://api.github.com",
		AtlassianAPI:  "https://api.atlassian.com",
		OpenMeteo:     "https://api.open-meteo.com",
		Geocoding:     "https://geocoding-api.open-meteo.com",
		IPAPI:         "https://api.ipapi.is",
	}
}
