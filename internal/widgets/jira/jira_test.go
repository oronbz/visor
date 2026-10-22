package jira_test

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/theme"
)

const (
	defaultJQL = "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC"
	oneJira    = `
[[widget]]
type = "jira"
site = "acme.atlassian.net"
email = "oron@acme.io"
`
)

// queue is five assigned issues, served two to a page.
func queue(t *testing.T) *fakeJira {
	j := newJira(t)
	j.page = 2
	j.issues = []issue{
		{key: "OPS-12", summary: "Rotate the TLS certificates", status: "In Progress", category: "indeterminate", kind: "Story"},
		{key: "OPS-9", summary: "Upgrade Postgres to 17", status: "To Do", category: "new", kind: "Bug"},
		{key: "WEB-140", summary: "Dark mode for settings", status: "In Review", category: "indeterminate", kind: "Epic"},
		{key: "WEB-133", summary: "Fix the flaky checkout test", status: "To Do", category: "new", kind: "Sub-task"},
		{key: "API-7", summary: "Document the rate limits", status: "Backlog", category: "new"},
	}
	return j
}

func withJira(t *testing.T, j *fakeJira, o hosttest.Options) *hosttest.Host {
	t.Helper()
	if o.Config == "" {
		o.Config = oneJira
	}
	if o.Env == nil {
		o.Env = map[string]string{"JIRA_API_TOKEN": "env-token"}
	}
	if o.Width == 0 {
		o.Width, o.Height = 80, 10
	}
	o.Endpoints = ports.Endpoints{Jira: j.srv.URL, AtlassianAPI: j.srv.URL}
	return hosttest.New(t, o)
}

func basic(email, token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token))
}

// rows are the tile's body lines, trimmed, without the borders.
func rows(h *hosttest.Host) []string {
	var out []string
	lines := strings.Split(h.Screen(), "\n")
	for _, l := range lines[1 : len(lines)-2] {
		l = strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "│"))
		if l != "" {
			out = append(out, strings.Join(strings.Fields(l), " "))
		}
	}
	return out
}

func TestJiraListsAssignedIssuesFollowingTheSearchCursor(t *testing.T) {
	j := queue(t)
	h := withJira(t, j, hosttest.Options{})

	if top := h.Line(0); !strings.Contains(top, "Jira 5") {
		t.Errorf("top border %q, want the title with a count of 5", top)
	}
	want := []string{
		"◆ OPS-12 Rotate the TLS certificates In Progress",
		"● OPS-9 Upgrade Postgres to 17 To Do",
		"✦ WEB-140 Dark mode for settings In Review",
		"↳ WEB-133 Fix the flaky checkout test To Do",
		"· API-7 Document the rate limits Backlog",
	}
	if got := rows(h); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	for _, c := range []struct {
		text string
		want theme.Token
	}{{"In Progress", theme.Accent}, {"To Do", theme.Dim}, {"◆", theme.Ok}, {"●", theme.Bad}, {"✦", theme.Info}, {"↳", theme.Dim}} {
		x, y, ok := h.Find(c.text)
		if !ok {
			t.Errorf("%q not on screen", c.text)
			continue
		}
		if got, _ := h.Fg(x, y); got != c.want {
			t.Errorf("%q drawn in %v, want %v", c.text, got, c.want)
		}
	}

	s := j.searches()
	if len(s) != 3 {
		t.Fatalf("searched %d times for 5 issues two to a page, want 3", len(s))
	}
	for i, tok := range []string{"", "2", "4"} {
		if got := s[i].Query.Get("nextPageToken"); got != tok {
			t.Errorf("search %d sent nextPageToken %q, want %q", i+1, got, tok)
		}
		if got := s[i].Query.Get("jql"); got != defaultJQL {
			t.Errorf("search %d sent jql %q, want the default", i+1, got)
		}
		if got, want := s[i].Auth, basic("oron@acme.io", "env-token"); got != want {
			t.Errorf("search %d Authorization %q, want %q", i+1, got, want)
		}
	}
}

func TestTwoJiraWidgetsSearchTheirOwnJQLSideBySide(t *testing.T) {
	j := newJira(t)
	j.queries["project = OPS AND sprint in openSprints()"] = []issue{
		{key: "OPS-1", summary: "Sprint work", status: "To Do", category: "new"},
	}
	j.queries["reporter = currentUser()"] = []issue{
		{key: "WEB-2", summary: "Reported bug", status: "Done", category: "done"},
		{key: "WEB-3", summary: "Another report", status: "To Do", category: "new"},
	}
	h := withJira(t, j, hosttest.Options{Width: 100, Height: 8, Config: `
[[widget]]
type = "jira"
id = "sprint"
title = "Sprint"
site = "acme.atlassian.net"
email = "oron@acme.io"
jql = "project = OPS AND sprint in openSprints()"

[[widget]]
type = "jira"
id = "reported"
title = "Reported"
site = "acme.atlassian.net"
email = "oron@acme.io"
jql = "reporter = currentUser()"
`})

	var sent []string
	for _, s := range j.searches() {
		sent = append(sent, s.Query.Get("jql"))
	}
	slices.Sort(sent)
	if want := []string{"project = OPS AND sprint in openSprints()", "reporter = currentUser()"}; !slices.Equal(sent, want) {
		t.Errorf("searched %q, want each Widget's own jql once", sent)
	}
	top := h.Line(0)
	for _, want := range []string{"Sprint 1", "Reported 2"} {
		if !strings.Contains(top, want) {
			t.Errorf("top row %q lacks %q", top, want)
		}
	}
	screen := h.Screen()
	for _, want := range []string{"Sprint work", "Reported bug", "Another report"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
}
