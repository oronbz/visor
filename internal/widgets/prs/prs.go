// Package prs is the GitHub PRs Widget type: open pull requests waiting for
// my review.
package prs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

var Type = widget.Type{
	Name:        "prs",
	Title:       "Review requests",
	Description: "GitHub pull requests waiting for your review.",
	Settings: []widget.Setting{{
		Name: "drafts", Kind: widget.Bool, Default: true,
		Description: "Show draft PRs.",
		Why:         "Set it to false to see only PRs that are ready for review.",
	}},
	Refresh: 2 * time.Minute,
	Floor:   30 * time.Second,
	Fetches: true,
	New:     newPRs,
}

func newPRs(c widget.Config) widget.Widget {
	return prs{ports: c.Ports, drafts: c.Settings.Bool("drafts"), acted: &acted{}}
}

type prs struct {
	ports  ports.Ports
	drafts bool
	acted  *acted
}

// acted remembers the PRs I approved or removed myself from while GitHub's
// search still lists them, which it does for a few seconds after, so the
// refresh after the Action doesn't bring them back.
type acted struct {
	mu  sync.Mutex
	ids map[string]stays
}

// stays is whether a PR I acted on still waits on me by its fresh review
// requests, which search reports correctly even while its index lags.
type stays func(n node, me string) bool

func (a *acted) add(id string, s stays) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ids == nil {
		a.ids = map[string]stays{}
	}
	a.ids[id] = s
}

// settle drops the PRs I acted on that search still lists but that no
// longer wait on me, and forgets the rest.
func (a *acted) settle(d *searchData) {
	a.mu.Lock()
	defer a.mu.Unlock()
	listed := map[string]stays{}
	kept := d.Requested.Nodes[:0]
	for _, n := range d.Requested.Nodes {
		if id := n.id(); a.ids[id] != nil && !a.ids[id](n, d.Viewer.Login) {
			listed[id] = a.ids[id]
			continue
		}
		kept = append(kept, n)
	}
	a.ids, d.Requested.Nodes = listed, kept
}

func (prs) Credential() string { return credential }

func (w prs) client(ctx context.Context, now time.Time) (client, error) {
	tok, from, err := token(ctx, w.ports)
	if err != nil {
		return client{}, err
	}
	if fineGrained(tok) {
		return client{}, refused(w.ports, from)
	}
	return client{http: w.ports.HTTP, endpoints: w.ports.Endpoints, token: tok, from: from, now: now}, nil
}

// query fetches the viewer and the search in one request.
const query = `query($requested: String!) {
  viewer { login }
  requested: search(query: $requested, type: ISSUE, first: 50) { nodes { ...pr } }
}
fragment pr on PullRequest {
  number title url createdAt additions deletions headRefName
  repository { nameWithOwner name }
  author { login }
  commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }
  reviewRequests(first: 20) {
    nodes { asCodeOwner requestedReviewer { __typename ... on User { login } ... on Team { slug } } }
  }
}`

type searchData struct {
	Viewer    struct{ Login string }
	Requested struct{ Nodes []node }
}

type node struct {
	Number      int
	Title       string
	URL         string
	CreatedAt   time.Time
	Additions   int
	Deletions   int
	HeadRefName string
	Repository  struct{ NameWithOwner, Name string }
	Author      struct{ Login string }
	Commits     struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct{ State string }
			}
		}
	}
	ReviewRequests struct {
		Nodes []struct {
			AsCodeOwner       bool
			RequestedReviewer struct {
				Typename string `json:"__typename"`
				Login    string
				Slug     string
			}
		}
	}
}

func (w prs) Refresh(ctx context.Context, now time.Time) (widget.Payload, error) {
	c, err := w.client(ctx, now)
	if errors.Is(err, errNoToken) || errors.Is(err, errNoKeyring) {
		return widget.Payload{}, needsToken(err)
	}
	if err != nil {
		return widget.Payload{}, err
	}
	filter := "is:pr is:open archived:false"
	if !w.drafts {
		filter += " draft:false"
	}
	var d searchData
	err = c.graphQL(ctx, query, map[string]any{
		"requested": filter + " review-requested:@me",
	}, &d)
	if auth := (authError{}); errors.As(err, &auth) {
		return widget.Payload{}, rejected(w.ports, c.from, auth)
	}
	if scope := (scopeError{}); errors.As(err, &scope) {
		return widget.Payload{}, lacking(w.ports, c.from, scope)
	}
	if err != nil {
		return widget.Payload{}, err
	}
	w.acted.settle(&d)
	return payload(d, now), nil
}

func payload(d searchData, now time.Time) widget.Payload {
	p := widget.Payload{
		Columns: []widget.Column{
			{Priority: 0}, {Priority: 1}, {Priority: 2, Flex: true}, {Priority: 3, Right: true},
			{Priority: 4}, {Priority: 5}, {Priority: 6, Right: true},
		},
		Empty: "No reviews waiting on you.",
	}
	me := d.Viewer.Login
	for _, n := range d.Requested.Nodes {
		if n.Number == 0 {
			continue
		}
		p.Items = append(p.Items, n.item("", reason(n, me), now, me))
	}
	p.Count = len(p.Items)
	return p
}

// id is the PR's Item id, owner/repo#number.
func (n node) id() string { return n.Repository.NameWithOwner + "#" + strconv.Itoa(n.Number) }

func (n node) item(group string, why widget.Field, now time.Time, me string) widget.Item {
	glyph, tok := ci(n)
	it := widget.Item{
		ID:    n.id(),
		Group: group,
		Fields: []widget.Field{
			widget.F(tok, glyph),
			widget.F(theme.Dim, n.Repository.Name+"#"+strconv.Itoa(n.Number)),
			widget.F(theme.Fg, n.Title),
			{{Text: "+" + strconv.Itoa(n.Additions), Token: theme.Ok}, {Text: " −" + strconv.Itoa(n.Deletions), Token: theme.Bad}},
			widget.F(theme.Dim, n.Author.Login),
			why,
			widget.F(theme.Faint, age(now.Sub(n.CreatedAt))),
		},
		Actions: []widget.Action{
			{ID: "open", Key: "enter", Label: "open", Primary: true, Verb: widget.Open, Target: n.URL},
			{ID: "approve", Key: "a", Label: "approve", Confirm: true},
		},
	}
	if r := n.removal(me); r != "" {
		it.Actions = append(it.Actions, widget.Action{ID: "remove", Key: "r", Label: "remove me", Confirm: true, Value: r})
	}
	it.Actions = append(it.Actions, widget.Action{ID: "copy", Key: "y", Label: "copy branch", Verb: widget.Copy, Target: n.HeadRefName})
	return it
}

// ci is the glyph for the head commit's combined checks.
func ci(n node) (string, theme.Token) {
	state := ""
	if c := n.Commits.Nodes; len(c) > 0 && c[0].Commit.StatusCheckRollup != nil {
		state = c[0].Commit.StatusCheckRollup.State
	}
	switch state {
	case "SUCCESS":
		return "✓", theme.Ok
	case "FAILURE", "ERROR":
		return "✗", theme.Bad
	case "PENDING", "EXPECTED":
		return "●", theme.Warn
	}
	return "·", theme.Faint
}

// reason is why I was asked to review: @you, a team of mine, or as a code owner.
func reason(n node, me string) widget.Field {
	var team string
	owner := false
	for _, r := range n.ReviewRequests.Nodes {
		rv := r.RequestedReviewer
		switch {
		case rv.Typename == "User" && rv.Login == me:
			if r.AsCodeOwner {
				return widget.F(theme.Alt, "owner")
			}
			return widget.F(theme.Accent, "@you")
		case rv.Typename == "Team" && team == "" && !owner:
			team, owner = rv.Slug, r.AsCodeOwner
		}
	}
	if owner {
		return widget.F(theme.Alt, "owner")
	}
	return widget.F(theme.Info, team)
}

// asks is whether login is asked by name to review the PR.
func (n node) asks(login string) bool {
	for _, r := range n.ReviewRequests.Nodes {
		if rv := r.RequestedReviewer; rv.Typename == "User" && rv.Login == login {
			return true
		}
	}
	return false
}

// awaits is whether the PR still asks me by name or through any team, as
// one I removed myself from stays listed through another of my requests.
func (n node) awaits(me string) bool {
	return n.asks(me) || n.removal(me) != ""
}

// removal is the review request "remove me" withdraws: "user:<login>" when
// I was asked directly, else "team:<slug>" for the team I was asked through.
// It's carried in the Action's Value so a cached payload can still run it.
func (n node) removal(me string) string {
	if n.asks(me) {
		return "user:" + me
	}
	for _, r := range n.ReviewRequests.Nodes {
		if rv := r.RequestedReviewer; rv.Typename == "Team" {
			return "team:" + rv.Slug
		}
	}
	return ""
}

// age is how long ago something happened in its largest whole unit: 45m, 3h, 2d.
func age(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(0, int(d.Minutes())))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func (w prs) Invoke(ctx context.Context, inv widget.Invocation) (widget.Result, error) {
	if inv.Action == pasteToken {
		return store(w.ports, inv.Text)
	}
	repo, num, ok := strings.Cut(inv.Item, "#")
	if !ok {
		return widget.Result{}, fmt.Errorf("PRs has no Item %q", inv.Item)
	}
	c, err := w.client(ctx, inv.Now)
	if errors.Is(err, errNoToken) || errors.Is(err, errNoKeyring) {
		return widget.Result{}, errors.New("no GitHub token; run `gh auth login`")
	}
	if err != nil {
		return widget.Result{}, err
	}
	pull := "/repos/" + repo + "/pulls/" + num
	short := repo[strings.Index(repo, "/")+1:] + "#" + num
	switch inv.Action {
	case "approve":
		if err := c.rest(ctx, http.MethodPost, pull+"/reviews", map[string]any{"event": "APPROVE"}); err != nil {
			return widget.Result{}, err
		}
		w.acted.add(inv.Item, node.asks)
		return widget.Result{Toast: "Approved " + short}, nil
	case "remove":
		body := map[string]any{"reviewers": []string{}, "team_reviewers": []string{}}
		kind, who, _ := strings.Cut(inv.Base, ":")
		switch kind {
		case "user":
			body["reviewers"] = []string{who}
		case "team":
			body["team_reviewers"] = []string{who}
		default:
			return widget.Result{}, errors.New("you aren't a requested reviewer of " + short)
		}
		if err := c.rest(ctx, http.MethodDelete, pull+"/requested_reviewers", body); err != nil {
			return widget.Result{}, err
		}
		w.acted.add(inv.Item, node.awaits)
		return widget.Result{Toast: "Removed you from the reviewers of " + short}, nil
	}
	return widget.Result{}, fmt.Errorf("PRs has no Action %q", inv.Action)
}
