package jira_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oronbz/visor/internal/hosttest"
)

var spinner = regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]`)

// workflow is one issue with three transitions, the last needing a resolution.
func workflow(t *testing.T) *fakeJira {
	j := newJira(t)
	j.issues = []issue{{key: "OPS-12", summary: "Rotate the TLS certificates", status: "To Do", category: "new", kind: "Task",
		transitions: []transition{{"11", "Start progress", "In Progress"}, {"21", "Block", "Blocked"}, {"31", "Resolve!", "Done"}}}}
	return j
}

// menu is the open menu's entries, read between its side borders.
func menu(h *hosttest.Host, title string) []string {
	x, top, ok := h.Find("─ " + title + " ")
	if !ok {
		return nil
	}
	var out []string
	for _, l := range strings.Split(h.Screen(), "\n")[top+1:] {
		box := []rune(l)[x-1:]
		end := slices.Index(box[1:], '│') + 1
		if box[0] != '│' || end < 1 {
			break
		}
		out = append(out, strings.Join(strings.Fields(string(box[1:end])), " "))
	}
	return out
}

func TestTOpensASpinnerMenuThenTheTransitionsAndPostsTheChosenOne(t *testing.T) {
	j := workflow(t)
	h := withJira(t, j, hosttest.Options{Width: 80, Height: 12})
	var during string
	j.reply = func(r request) (int, http.Header, string) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.Path, "/transitions") {
			during = h.Screen()
		}
		return 0, nil, ""
	}

	h.Press("1", "t")
	if !strings.Contains(during, "Transition OPS-12") || !spinner.MatchString(during) {
		t.Errorf("while the transitions load, want a Transition OPS-12 menu with a spinner:\n%s", during)
	}
	if got, want := menu(h, "Transition OPS-12"), []string{"Start progress → In Progress", "Block → Blocked", "Resolve → Done"}; !slices.Equal(got, want) {
		t.Fatalf("menu %q, want %q", got, want)
	}

	searches := len(j.searches())
	h.Press("j", "enter")
	var posted []request
	for _, r := range j.Requests() {
		if r.Method == http.MethodPost {
			posted = append(posted, r)
		}
	}
	if len(posted) != 1 || posted[0].Path != "/rest/api/3/issue/OPS-12/transitions" {
		t.Fatalf("posted %v, want one transition of OPS-12", posted)
	}
	var body struct {
		Transition struct{ ID string } `json:"transition"`
	}
	if err := json.Unmarshal([]byte(posted[0].Body), &body); err != nil || body.Transition.ID != "21" {
		t.Errorf("posted %s, want transition 21", posted[0].Body)
	}
	if posted[0].Auth != basic("oron@acme.io", "env-token") {
		t.Errorf("posted with Authorization %q", posted[0].Auth)
	}
	if footer := h.Line(11); !strings.Contains(footer, "Transitioned OPS-12") {
		t.Errorf("footer %q, want a toast", footer)
	}
	if len(j.searches()) != searches+1 {
		t.Errorf("searched %d times after the transition, want a refresh", len(j.searches())-searches)
	}
	if got := rows(h); len(got) == 0 || !strings.HasSuffix(got[0], "Blocked") {
		t.Errorf("rows %q, want OPS-12 Blocked after the refresh", got)
	}
}

func TestATransitionThatRequiresFieldsOpensTheIssueInstead(t *testing.T) {
	j := workflow(t)
	h := withJira(t, j, hosttest.Options{Width: 80, Height: 12})

	h.Press("1", "t", "j", "j", "enter")
	if got := h.Opener.Opened; len(got) != 1 || got[0] != "https://acme.atlassian.net/browse/OPS-12" {
		t.Errorf("opened %v, want the issue's page", got)
	}
	for _, r := range j.Requests() {
		if r.Method == http.MethodPost {
			t.Errorf("posted %v for a transition with a required field", r)
		}
	}
}

func TestEnterOpensTheIssue(t *testing.T) {
	h := withJira(t, workflow(t), hosttest.Options{})
	h.Press("1", "enter")
	if got := h.Opener.Opened; len(got) != 1 || got[0] != "https://acme.atlassian.net/browse/OPS-12" {
		t.Errorf("opened %v, want the issue's page", got)
	}
}

func TestARejectedTransitionToastsJirasReason(t *testing.T) {
	j := workflow(t)
	h := withJira(t, j, hosttest.Options{Width: 100, Height: 12})
	j.reply = func(r request) (int, http.Header, string) {
		if r.Method == http.MethodPost {
			return http.StatusBadRequest, nil, `{"errorMessages":[],"errors":{"assignee":"Assignee is required."}}`
		}
		return 0, nil, ""
	}

	h.Press("1", "t", "enter")
	if footer := h.Line(11); !strings.Contains(footer, "Transition failed: Jira: assignee: Assignee is required.") {
		t.Errorf("footer %q, want Jira's reason", footer)
	}
}

func TestARateLimitPausesTheWidgetUntilRetryAfter(t *testing.T) {
	j := queue(t)
	limited := true
	j.reply = func(r request) (int, http.Header, string) {
		if limited {
			return http.StatusTooManyRequests, http.Header{"Retry-After": {"600"}}, `{"errorMessages":["Rate limit exceeded."]}`
		}
		return 0, nil, ""
	}
	h := withJira(t, j, hosttest.Options{})
	if !strings.Contains(h.Screen(), "Rate limit exceeded") {
		t.Errorf("want the rate limit shown:\n%s", h.Screen())
	}
	limited = false
	n := len(j.searches())
	h.Advance(5 * time.Minute)
	if len(j.searches()) != n {
		t.Errorf("searched during the 10m pause")
	}
	h.Advance(6 * time.Minute)
	if len(j.searches()) == n {
		t.Errorf("didn't search again after the pause")
	}
}

func TestARejectedTokenPutsTheWidgetInNeedsSetup(t *testing.T) {
	j := queue(t)
	j.reply = func(request) (int, http.Header, string) {
		return http.StatusUnauthorized, nil, `{"errorMessages":["Client must be authenticated to access this resource."]}`
	}
	h := withJira(t, j, hosttest.Options{Width: 100})
	for _, want := range []string{"needs setup", "Jira rejected the API token for oron@acme.io"} {
		if !strings.Contains(h.Screen(), want) {
			t.Errorf("screen lacks %q:\n%s", want, h.Screen())
		}
	}
}
