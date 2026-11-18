package prs_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// github is a fake GitHub: GraphQL search over fixture PRs, and the REST
// endpoints the PRs Widget's Actions call. It records every request.
type github struct {
	srv *httptest.Server

	mu        sync.Mutex
	viewer    string
	requested []pr
	requests  []request
	// reply, when set, answers a request instead of the fixtures; status 0 falls through.
	reply func(r request) (status int, header http.Header, body string)
}

type request struct {
	Method, Path, Auth string
	Body               string
}

func (r request) String() string { return r.Method + " " + r.Path }

type pr struct {
	repo    string
	number  int
	title   string
	author  string
	branch  string
	adds    int
	dels    int
	ci      string // SUCCESS, FAILURE, PENDING or "" for no checks
	created time.Time
	// requests are the PR's pending review requests: a login, or "team:<slug>";
	// a trailing "!" marks a code owner request.
	requests []string
}

func newGitHub(t *testing.T) *github {
	g := &github{viewer: "oronbz"}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *github) serve(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	req := request{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: string(b)}
	g.mu.Lock()
	g.requests = append(g.requests, req)
	reply := g.reply
	g.mu.Unlock()
	if reply != nil {
		if status, header, body := reply(req); status != 0 {
			for k, v := range header {
				w.Header()[k] = v
			}
			w.WriteHeader(status)
			io.WriteString(w, body)
			return
		}
	}
	if r.Method == http.MethodPost && r.URL.Path == "/graphql" {
		g.search(w, b)
		return
	}
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reviews"):
		g.reviewed(r.URL.Path)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"id": 1, "state": "APPROVED"}`)
	case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/requested_reviewers"):
		g.withdrawn(r.URL.Path, b)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{}`)
	default:
		http.NotFound(w, r)
	}
}

// reviewed fulfils my review request on the PR at path, as GitHub does when I
// review; search keeps listing the PR, as GitHub's index does for a while.
func (g *github) reviewed(path string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, p := range g.requested {
		if path == "/repos/"+p.repo+"/pulls/"+itoa(p.number)+"/reviews" {
			g.requested[i].requests = slices.DeleteFunc(slices.Clone(p.requests), func(r string) bool { return r == g.viewer })
		}
	}
}

// withdrawn drops the review requests body names from the PR at path; search
// keeps listing the PR, as GitHub's index does for a while.
func (g *github) withdrawn(path string, body []byte) {
	var raw struct {
		Reviewers     []string `json:"reviewers"`
		TeamReviewers []string `json:"team_reviewers"`
	}
	json.Unmarshal(body, &raw)
	drop := raw.Reviewers
	for _, t := range raw.TeamReviewers {
		drop = append(drop, "team:"+t)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, p := range g.requested {
		if path == "/repos/"+p.repo+"/pulls/"+itoa(p.number)+"/requested_reviewers" {
			g.requested[i].requests = slices.DeleteFunc(slices.Clone(p.requests), func(r string) bool {
				return slices.Contains(drop, strings.TrimSuffix(r, "!"))
			})
		}
	}
}

// search answers the Widget's one GraphQL request: the viewer, plus each
// search whose query string asks for review requests.
func (g *github) search(w http.ResponseWriter, body []byte) {
	var q struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(body, &q); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	data := map[string]any{"viewer": map[string]any{"login": g.viewer}}
	for name, v := range q.Variables {
		s, _ := v.(string)
		if !strings.Contains(s, "review-requested:@me") {
			continue
		}
		prs := g.requested
		if strings.Contains(s, "user-review-requested:@me") {
			prs = nil
			for _, p := range g.requested {
				if slices.ContainsFunc(p.requests, func(r string) bool { return strings.TrimSuffix(r, "!") == g.viewer }) {
					prs = append(prs, p)
				}
			}
		}
		data[name] = results(prs)
	}
	json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func results(prs []pr) map[string]any {
	nodes := []any{}
	for _, p := range prs {
		nodes = append(nodes, p.node())
	}
	return map[string]any{"issueCount": len(nodes), "nodes": nodes}
}

func (p pr) node() map[string]any {
	var rollup any
	if p.ci != "" {
		rollup = map[string]any{"state": p.ci}
	}
	var requests []any
	for _, r := range p.requests {
		owner := strings.HasSuffix(r, "!")
		r = strings.TrimSuffix(r, "!")
		reviewer := map[string]any{"__typename": "User", "login": r}
		if slug, ok := strings.CutPrefix(r, "team:"); ok {
			reviewer = map[string]any{"__typename": "Team", "slug": slug, "name": slug}
		}
		requests = append(requests, map[string]any{"asCodeOwner": owner, "requestedReviewer": reviewer})
	}
	return map[string]any{
		"number": p.number, "title": p.title, "isDraft": false,
		"url":       "https://github.com/" + p.repo + "/pull/" + itoa(p.number),
		"createdAt": p.created.Format(time.RFC3339),
		"additions": p.adds, "deletions": p.dels, "headRefName": p.branch,
		"repository":     map[string]any{"nameWithOwner": p.repo, "name": p.repo[strings.Index(p.repo, "/")+1:]},
		"author":         map[string]any{"login": p.author},
		"commits":        map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{"statusCheckRollup": rollup}}}},
		"reviewRequests": map[string]any{"nodes": requests},
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// Requests are the requests GitHub has received so far.
func (g *github) Requests() []request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]request(nil), g.requests...)
}

// searches counts the GraphQL requests so far.
func (g *github) searches() int {
	n := 0
	for _, r := range g.Requests() {
		if r.Path == "/graphql" {
			n++
		}
	}
	return n
}
