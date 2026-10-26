package demo

import (
	"fmt"
	"strings"
	"time"

	"github.com/oronbz/visor/internal/ports"
)

// The workday is laid out for the demo clock, which starts at morning: the
// standup is over, a design review is under way, and lunch and the
// afternoon's meetings are ahead.

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func events(now time.Time) []ports.Event {
	today := midnight(now)
	at := func(day, hour, minute int) time.Time {
		return time.Date(today.Year(), today.Month(), today.Day()+day, hour, minute, 0, 0, now.Location())
	}
	ev := func(id, title, place string, start time.Time, d time.Duration) ports.Event {
		return ports.Event{ID: id, Title: title, Location: place, Calendar: "Work", Start: start, End: start.Add(d)}
	}
	return []ports.Event{
		{ID: "demo-freeze", Title: "Release freeze", Calendar: "Work", AllDay: true, Start: today, End: today.AddDate(0, 0, 1)},
		ev("demo-standup", "Daily standup", "Zoom", at(0, 9, 30), 15*time.Minute),
		ev("demo-design", "Design review: checkout flow", "Room 4B", at(0, 11, 0), 45*time.Minute),
		ev("demo-1on1", "1:1 with Priya", "Zoom", at(0, 11, 45), 30*time.Minute),
		ev("demo-lunch", "Lunch with the API team", "Cantina", at(0, 12, 30), time.Hour),
		ev("demo-incident", "Incident review: search latency", "Room 2A", at(0, 14, 0), 45*time.Minute),
		ev("demo-retro", "Sprint retro", "Room 4B", at(0, 16, 0), 45*time.Minute),
		ev("demo-standup", "Daily standup", "Zoom", at(1, 9, 30), 15*time.Minute),
		ev("demo-planning", "Sprint planning", "Room 4B", at(1, 11, 0), time.Hour),
		ev("demo-standup", "Daily standup", "Zoom", at(2, 9, 30), 15*time.Minute),
		ev("demo-arch", "Architecture sync", "Room 2A", at(2, 14, 0), time.Hour),
	}
}

func reminders(now time.Time) []reminder {
	today := midnight(now)
	r := func(id, title, list string, due time.Time, dateOnly bool, repeat int) reminder {
		return reminder{Reminder: ports.Reminder{ID: id, Title: title, List: list, Due: due, DateOnly: dateOnly}, repeat: repeat}
	}
	return []reminder{
		r("demo-expenses", "Submit the expense report", "Work", today.AddDate(0, 0, -1), true, 0),
		r("demo-offsite", "Book the team offsite venue", "Work", today, true, 0),
		r("demo-plants", "Water the plants", "Home", today, true, 3),
		r("demo-cleaning", "Pick up the dry cleaning", "Home", time.Date(today.Year(), today.Month(), today.Day(), 18, 0, 0, 0, now.Location()), false, 0),
		r("demo-passport", "Renew passport", "Home", today.AddDate(0, 0, 9), true, 0),
	}
}

func tasks(now time.Time) string {
	day := func(n int) string { return midnight(now).AddDate(0, 0, n).Format(time.DateOnly) }
	return strings.Join([]string{
		"- [ ] Review the rate-limit rollout plan due:" + day(0),
		"- [ ] Write up the checkout A/B results due:" + day(1),
		"- [ ] Update the onboarding doc for new hires",
		"- [ ] Renew the staging TLS certificate due:" + day(3),
		"- [x] Reply to the design feedback thread",
		"",
	}, "\n")
}

const notes = `# Today
- Saved cards ship behind the checkout_saved_cards flag
- Ask Priya how the settings form migration is going
- Rate limits: start at 60 req/min, watch the error dashboards

# Ideas
- Cache the cart total on the server
- Dark mode for the admin console
`

// pr is a pull request waiting on viewer's review.
type pr struct {
	repo    string
	number  int
	title   string
	author  string
	branch  string
	adds    int
	dels    int
	draft   bool
	created time.Time
	// ci is SUCCESS, FAILURE, PENDING or "" for no checks.
	ci string
	// requests are a login, or "team:<slug>"; a trailing "!" marks a code owner request.
	requests []string
}

func (p pr) id() string { return fmt.Sprintf("%s#%d", p.repo, p.number) }

func pulls(now time.Time) []pr {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	return []pr{
		{"acme/web", 482, "Checkout: show saved cards first", "maya-chen", "maya/saved-cards", 214, 37, false, ago(3 * time.Hour), "SUCCESS", []string{viewer}},
		{"acme/api", 1290, "Rate-limit the public search endpoint", "leo-park", "leo/search-rate-limit", 88, 12, false, ago(45 * time.Minute), "PENDING", []string{"team:backend"}},
		{"acme/web", 479, "Move the settings page to the new form components", "priya-nair", "priya/settings-forms", 512, 430, false, ago(50 * time.Hour), "FAILURE", []string{viewer, "team:web-platform"}},
		{"acme/mobile", 215, "Fix a crash when the cart is empty", "noor-ali", "noor/empty-cart-crash", 23, 4, false, ago(5 * time.Hour), "SUCCESS", []string{viewer}},
		{"acme/infra", 77, "Bump Postgres to 16.4 in staging", "tomas-kral", "tomas/pg-16-4", 6, 6, false, ago(26 * time.Hour), "SUCCESS", []string{"team:platform!"}},
		{"acme/api", 1284, "Add idempotency keys to payment requests", "maya-chen", "maya/idempotency-keys", 301, 58, false, ago(6 * time.Hour), "PENDING", []string{"team:payments"}},
		{"acme/design-system", 58, "Button: add a loading state", "leo-park", "leo/button-loading", 97, 15, true, ago(20 * time.Minute), "", []string{viewer}},
	}
}

// issue is a Jira issue assigned to the demo's user.
type issue struct {
	key, summary, issueType, status string
	subtask                         bool
	// updated orders the search, latest first.
	updated int
}

func issues() []issue {
	return []issue{
		{"ACME-157", "Cart total ignores the discount code after sign-in", "Bug", "In Progress", false, 6},
		{"ACME-142", "Show saved cards first at checkout", "Story", "In Progress", false, 5},
		{"ACME-139", "Write the rollout plan for search rate limits", "Task", "In Review", false, 4},
		{"ACME-150", "Add analytics events to the checkout steps", "Sub-task", "To Do", true, 3},
		{"ACME-161", "Settings page drops unsaved changes on back", "Bug", "To Do", false, 2},
		{"ACME-128", "Export orders as CSV", "Story", "To Do", false, 1},
	}
}
