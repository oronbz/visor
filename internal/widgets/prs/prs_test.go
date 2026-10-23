package prs_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/ports"
)

const onePRsWidget = `
[[widget]]
type = "prs"
`

var now = hosttest.DefaultNow

// team is a GitHub with three PRs waiting on me: directly, via my team and
// as a code owner.
func team(t *testing.T) *github {
	g := newGitHub(t)
	g.requested = []pr{
		{repo: "acme/api", number: 412, title: "Retry webhook deliveries", author: "dana", branch: "dana/webhook-retry",
			adds: 120, dels: 14, ci: "SUCCESS", created: now.Add(-50 * time.Hour), requests: []string{"oronbz", "team:platform"}},
		{repo: "acme/web", number: 88, title: "Drop the legacy router", author: "eli", branch: "eli/router",
			adds: 3, dels: 940, ci: "FAILURE", created: now.Add(-3 * time.Hour), requests: []string{"team:platform"}},
		{repo: "acme/infra", number: 7, title: "Pin the runner image", author: "noa", branch: "pin-runner",
			adds: 2, dels: 2, ci: "PENDING", created: now.Add(-45 * time.Minute), requests: []string{"team:sre!"}},
	}
	return g
}

func withPRs(t *testing.T, g *github, o hosttest.Options) *hosttest.Host {
	t.Helper()
	if o.Config == "" {
		o.Config = onePRsWidget
	}
	if o.Env == nil {
		o.Env = map[string]string{"GH_TOKEN": "ghp_fromenv"}
	}
	o.Endpoints = ports.Endpoints{GitHubGraphQL: g.srv.URL + "/graphql", GitHubREST: g.srv.URL}
	return hosttest.New(t, o)
}

// tile is the screen above the footer.
func tile(h *hosttest.Host) string {
	lines := strings.Split(h.Screen(), "\n")
	return strings.Join(lines[:len(lines)-1], "\n")
}

func TestPRsShowsRequestedPRs(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 90, Height: 9})

	want := strings.Join([]string{
		"╭─ Review requests 3 ────────────────────────────────────────────────────────────────────╮",
		"│ ✓ api#412 Retry webhook deliveries                          +120 −14 dana @you      2d │",
		"│ ✗ web#88  Drop the legacy router                             +3 −940 eli  platform  3h │",
		"│ ● infra#7 Pin the runner image                                 +2 −2 noa  owner    45m │",
		"│                                                                                        │",
		"│                                                                                        │",
		"│                                                                                        │",
		"╰────────────────────────────────────────────────────────────────────────────────────────╯",
	}, "\n")
	if got := tile(h); got != want {
		t.Errorf("PRs tile:\n%s\nwant:\n%s", got, want)
	}
	reqs := g.Requests()
	if len(reqs) != 1 || reqs[0].Path != "/graphql" {
		t.Fatalf("requests %v, want one GraphQL search", reqs)
	}
	if got, want := reqs[0].Auth, "bearer ghp_fromenv"; got != want {
		t.Errorf("Authorization %q, want %q", got, want)
	}
}

func TestTheTokenComesFromGHTokenThenGitHubToken(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"GH_TOKEN wins", map[string]string{"GH_TOKEN": "ghp_gh", "GITHUB_TOKEN": "ghp_github"}, "bearer ghp_gh"},
		{"then GITHUB_TOKEN", map[string]string{"GITHUB_TOKEN": "ghp_github"}, "bearer ghp_github"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := team(t)
			h := withPRs(t, g, hosttest.Options{Width: 90, Height: 9, Env: tc.env})
			if reqs := g.Requests(); len(reqs) != 1 || reqs[0].Auth != tc.want {
				t.Fatalf("requests %v, want one search authorised with %q", reqs, tc.want)
			}
			if len(h.Commands.Calls) != 0 {
				t.Errorf("ran %v with a token in the environment", h.Commands.Calls)
			}
		})
	}
}

func TestWithNoTokenPRsNeedsSetupUntilGhIsSignedIn(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 60, Height: 9, Env: map[string]string{}})

	screen := h.Screen()
	for _, want := range []string{"needs setup", "Run `gh auth login`"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	if reqs := g.Requests(); len(reqs) != 0 {
		t.Errorf("GitHub got %v with no token", reqs)
	}
	if calls := h.Commands.Calls; len(calls) != 1 || strings.Join(calls[0], " ") != "gh auth token" {
		t.Errorf("commands run %v, want gh auth token tried", calls)
	}

	h.Commands.Script("gho_fromgh\n", nil, "gh", "auth", "token")
	h.Press("ctrl+r")
	if reqs := g.Requests(); len(reqs) != 1 || reqs[0].Auth != "bearer gho_fromgh" {
		t.Fatalf("after gh auth login, requests %v, want one search authorised with gh's token", reqs)
	}
	if !strings.Contains(h.Line(0), "Review requests 3") {
		t.Errorf("after gh auth login the title reads %q", h.Line(0))
	}
}

func TestColumnsDropByPriorityAsTheWidgetNarrows(t *testing.T) {
	h := withPRs(t, team(t), hosttest.Options{Width: 90, Height: 9})
	// Row 1's columns in priority order: CI, repo#n, title, +add −del, author, reason, age.
	columns := []string{"✓ ", "api#412", "Retry", "+120 −14", "dana", "@you", "2d"}
	dropped := len(columns)
	for w := 90; w >= 26; w-- {
		h.Resize(w, 9)
		line := h.Line(1)
		kept := 0
		for kept < len(columns) && strings.Contains(line, columns[kept]) {
			kept++
		}
		for _, c := range columns[kept:] {
			if strings.Contains(line, c) {
				t.Fatalf("at width %d %q shows %q after dropping %q", w, line, c, columns[kept])
			}
		}
		if kept > dropped {
			t.Fatalf("at width %d %q brought back a column", w, line)
		}
		dropped = kept
		if !strings.Contains(h.Line(0), "Review requests 3") {
			t.Fatalf("at width %d the title reads %q, want the count", w, h.Line(0))
		}
	}
	if dropped != 3 {
		t.Errorf("at width 26 kept %d columns (%q), want CI, repo#n and title", dropped, h.Line(1))
	}
}

// writes are the requests other than searches.
func writes(g *github) []request {
	var out []request
	for _, r := range g.Requests() {
		if r.Path != "/graphql" {
			out = append(out, r)
		}
	}
	return out
}

func footer(h *hosttest.Host) string {
	lines := strings.Split(h.Screen(), "\n")
	return lines[len(lines)-1]
}

func TestApproveAsksFirstThenApprovesTheSelectedPRAndRefreshes(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 90, Height: 12})
	h.Press("1", "a")
	if !strings.Contains(h.Screen(), "Approve?") || !strings.Contains(h.Screen(), "Retry webhook deliveries") {
		t.Fatalf("a didn't ask to approve the PR:\n%s", h.Screen())
	}
	h.Press("esc")
	if w := writes(g); len(w) != 0 {
		t.Fatalf("cancelling sent %v", w)
	}

	h.Press("a", "enter")
	w := writes(g)
	if len(w) != 1 || w[0].String() != "POST /repos/acme/api/pulls/412/reviews" {
		t.Fatalf("writes %v, want one review posted on acme/api#412", w)
	}
	if !strings.Contains(w[0].Body, `"event":"APPROVE"`) || w[0].Auth != "bearer ghp_fromenv" {
		t.Errorf("review %q with %q, want an APPROVE event with the token", w[0].Body, w[0].Auth)
	}
	if got := footer(h); !strings.Contains(got, "Approved api#412") {
		t.Errorf("footer %q, want the approval toasted", got)
	}
	if n := g.searches(); n != 2 {
		t.Errorf("searched %d times, want a refresh after approving", n)
	}
}

func TestAnApprovedPRStaysGoneWhileSearchStillListsItUnlessIAmAskedAgain(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 90, Height: 12})
	h.Press("1", "a", "enter")
	if strings.Contains(tile(h), "Retry webhook deliveries") || !strings.Contains(tile(h), "Review requests 2") {
		t.Fatalf("the approved PR came back with the refresh:\n%s", h.Screen())
	}

	g.mu.Lock()
	g.requested[0].requests = append(g.requested[0].requests, "oronbz")
	g.mu.Unlock()
	h.Advance(3 * time.Minute)
	if !strings.Contains(tile(h), "Retry webhook deliveries") {
		t.Errorf("asked by name again, the PR stayed hidden:\n%s", h.Screen())
	}
}

func TestRemoveMeWithdrawsMyOwnRequestOrMyTeams(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 90, Height: 12})
	h.Press("1", "r")
	if !strings.Contains(h.Screen(), "Remove me?") {
		t.Fatalf("r didn't ask first:\n%s", h.Screen())
	}
	h.Press("enter")
	h.Press("j", "r", "y")

	w := writes(g)
	if len(w) != 2 {
		t.Fatalf("writes %v, want two review requests withdrawn", w)
	}
	for i, want := range []struct{ req, body string }{
		{"DELETE /repos/acme/api/pulls/412/requested_reviewers", `"reviewers":["oronbz"]`},
		{"DELETE /repos/acme/web/pulls/88/requested_reviewers", `"team_reviewers":["platform"]`},
	} {
		if w[i].String() != want.req || !strings.Contains(w[i].Body, want.body) {
			t.Errorf("write %d: %v %s, want %s with %s", i, w[i], w[i].Body, want.req, want.body)
		}
	}
	if got := footer(h); !strings.Contains(got, "web#88") {
		t.Errorf("footer %q, want the removal toasted", got)
	}
	if n := g.searches(); n != 3 {
		t.Errorf("searched %d times, want a refresh after each removal", n)
	}
}

func TestAPRIRemovedMyselfFromStaysOnlyWhileItStillAsksMe(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 90, Height: 12})
	h.Press("1", "r", "enter")
	if !strings.Contains(tile(h), "Retry webhook deliveries") {
		t.Fatalf("removed by name, the PR my team is still asked on went away:\n%s", h.Screen())
	}

	h.Press("j", "r", "enter")
	if strings.Contains(tile(h), "Drop the legacy router") || !strings.Contains(tile(h), "Review requests 2") {
		t.Fatalf("the PR I removed my team from came back with the refresh:\n%s", h.Screen())
	}

	g.mu.Lock()
	g.requested[1].requests = []string{"oronbz"}
	g.mu.Unlock()
	h.Advance(3 * time.Minute)
	if !strings.Contains(tile(h), "Drop the legacy router") {
		t.Errorf("asked by name again, the PR stayed hidden:\n%s", h.Screen())
	}
}

func TestYCopiesTheBranchAndEnterOpensThePR(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 90, Height: 12})
	h.Press("1", "y")
	if got := h.Clipboard.Copied; len(got) != 1 || got[0] != "dana/webhook-retry" {
		t.Errorf("copied %q, want the branch", got)
	}
	h.Press("j", "enter")
	if got := h.Opener.Opened; len(got) != 1 || got[0] != "https://github.com/acme/web/pull/88" {
		t.Errorf("opened %q, want the PR's page", got)
	}
	if w := writes(g); len(w) != 0 {
		t.Errorf("copy and open sent %v to GitHub", w)
	}
}

func TestTheDraftsSettingHidesDraftsInTheSearch(t *testing.T) {
	g := team(t)
	withPRs(t, g, hosttest.Options{Width: 90, Height: 9})
	withPRs(t, g, hosttest.Options{Width: 90, Height: 9, Config: onePRsWidget + "drafts = false\n"})

	reqs := g.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests %v, want one search per host", reqs)
	}
	if strings.Contains(reqs[0].Body, "draft:false") {
		t.Errorf("by default the search hides drafts: %s", reqs[0].Body)
	}
	if !strings.Contains(reqs[1].Body, "draft:false") {
		t.Errorf("with drafts = false the search should say draft:false: %s", reqs[1].Body)
	}
}

func TestA429PausesEveryPRsWidgetOnTheTokenUntilRetryAfter(t *testing.T) {
	g := team(t)
	first := true
	g.reply = func(r request) (int, http.Header, string) {
		if first {
			first = false
			return http.StatusTooManyRequests, http.Header{"Retry-After": {"300"}}, `{"message": "You have exceeded a secondary rate limit."}`
		}
		return 0, nil, ""
	}
	h := withPRs(t, g, hosttest.Options{Width: 90, Height: 20, Config: `
[[widget]]
type = "prs"

[[widget]]
type = "prs"
id = "prs-2"
title = "Team reviews"
`})
	if n := g.searches(); n != 2 {
		t.Fatalf("searched %d times at start, want once per Widget", n)
	}
	if !strings.Contains(h.Screen(), "rate limit") {
		t.Errorf("the limited Widget doesn't say so:\n%s", h.Screen())
	}

	h.Advance(4*time.Minute + 59*time.Second)
	if n := g.searches(); n != 2 {
		t.Fatalf("searched %d times during the 5m pause, want none after the 429", n)
	}
	h.Advance(31 * time.Second)
	if n := g.searches(); n != 4 {
		t.Errorf("searched %d times in all, want both Widgets refreshed once the pause passed", n)
	}
	if strings.Contains(h.Screen(), "rate limit") {
		t.Errorf("still rate limited after the pause:\n%s", h.Screen())
	}
}

func TestA403WithNoRequestsLeftPausesUntilTheRateLimitResets(t *testing.T) {
	g := team(t)
	reset := now.Add(10 * time.Minute).Unix()
	first := true
	g.reply = func(request) (int, http.Header, string) {
		if first {
			first = false
			return http.StatusForbidden, http.Header{
				"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {strconv.FormatInt(reset, 10)},
			}, `{"message": "API rate limit exceeded"}`
		}
		return 0, nil, ""
	}
	h := withPRs(t, g, hosttest.Options{Width: 90, Height: 9})
	h.Advance(9*time.Minute + 59*time.Second)
	if n := g.searches(); n != 1 {
		t.Fatalf("searched %d times before the limit reset, want once", n)
	}
	h.Advance(2 * time.Second)
	if n := g.searches(); n != 2 || !strings.Contains(h.Line(0), "Review requests 3") {
		t.Errorf("after the reset: %d searches, title %q; want a fresh search", n, h.Line(0))
	}
}

// tokenSource stages a GitHub token in one place PRs reads one from. fix is
// what needs setup says to do about an unusable token there, and paste
// whether it offers to paste one: only a pasted token can replace it.
type tokenSource struct {
	name, fix string
	paste     bool
	o         func(tok string) hosttest.Options
}

var tokenSources = []tokenSource{
	{"GH_TOKEN", "Unset it or set a classic token with repo and read:org scopes", false, func(tok string) hosttest.Options {
		return hosttest.Options{Env: map[string]string{"GH_TOKEN": tok}}
	}},
	{"GITHUB_TOKEN", "Unset it or set a classic token with repo and read:org scopes", false, func(tok string) hosttest.Options {
		return hosttest.Options{Env: map[string]string{"GITHUB_TOKEN": tok}}
	}},
	{"`gh auth token`", "Run `gh auth login` again", false, func(tok string) hosttest.Options {
		c := &hosttest.Commands{}
		c.Script(tok+"\n", nil, "gh", "auth", "token")
		return hosttest.Options{Env: map[string]string{}, Commands: c}
	}},
	{"The saved token", "Run `gh auth login`, or press Enter to paste a classic token", true, func(tok string) hosttest.Options {
		kr := &hosttest.Keyring{}
		_ = kr.Set("visor", "github:github.com", tok)
		return hosttest.Options{Env: map[string]string{}, Keyring: kr}
	}},
}

// offersPaste reports whether enter on the focused PRs Widget opens the
// paste prompt.
func offersPaste(h *hosttest.Host) bool {
	h.Press("1", "enter")
	opened := strings.Contains(h.Screen(), "INPUT")
	h.Press("esc")
	return opened
}

// unusableFrom runs PRs with tok staged in src, and checks it needs setup
// for problem, which is prefixed by src's name, with src's fix, and offers
// to paste a token only where src does.
func unusableFrom(t *testing.T, g *github, src tokenSource, tok, problem string) {
	t.Helper()
	o := src.o(tok)
	o.Width, o.Height = 220, 12
	h := withPRs(t, g, o)
	screen := h.Screen()
	for _, want := range []string{"needs setup", src.name + problem, src.fix} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	if got := offersPaste(h); got != src.paste {
		t.Errorf("enter opens the paste prompt: %v, want %v", got, src.paste)
	}
}

func TestARejectedTokenNeedsSetupNamingItsSourceAndGitHubsMessage(t *testing.T) {
	for _, src := range tokenSources {
		t.Run(src.name, func(t *testing.T) {
			g := team(t)
			g.reply = func(request) (int, http.Header, string) {
				return http.StatusUnauthorized, nil, `{"message": "Bad credentials"}`
			}
			unusableFrom(t, g, src, "ghp_expired", " was rejected by GitHub: Bad credentials")
		})
	}
}

func TestAFineGrainedTokenNeedsSetupNamingItsSourceAndIsNeverSent(t *testing.T) {
	for _, src := range tokenSources {
		t.Run(src.name, func(t *testing.T) {
			g := team(t)
			unusableFrom(t, g, src, "github_pat_fake", " is a fine-grained token")
			if reqs := g.Requests(); len(reqs) != 0 {
				t.Errorf("GitHub got %v", reqs)
			}
		})
	}
}

func TestATokenMissingAScopeNeedsSetupNamingTheScopesAndGitHubsMessage(t *testing.T) {
	for _, src := range tokenSources {
		t.Run(src.name, func(t *testing.T) {
			g := team(t)
			g.reply = func(request) (int, http.Header, string) {
				return http.StatusOK, nil, `{"errors": [{"type": "INSUFFICIENT_SCOPES", "message": "Your token has not been granted the required scopes to execute this query."}]}`
			}
			unusableFrom(t, g, src, "ghp_narrow", " needs repo and read:org scopes. GitHub: Your token has not been granted the required scopes")
		})
	}
}
