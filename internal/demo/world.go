package demo

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// cloudID is the demo Jira site's id, from its tenant info.
const cloudID = "6d1f9a3c-0b4e-4c2a-9f7d-acme00000001"

// world is GitHub and Jira as the demo's user sees them, served on one
// loopback address: GitHub's GraphQL search at /graphql and its REST API at
// the root, and Jira's REST API at the root and under api.atlassian.com's
// /ex/jira/{cloudId}. Actions change it, so a refresh shows what they did.
type world struct {
	mu     sync.Mutex
	pulls  []pr
	issues []issue
}

func newWorld(now time.Time) *world { return &world{pulls: pulls(now), issues: issues()} }

func (w *world) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.mu.Lock()
	defer w.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/ex/jira/"+cloudID)
	key, isIssue := strings.CutPrefix(path, "/rest/api/3/issue/")
	key, transitions := strings.CutSuffix(key, "/transitions")
	switch {
	case r.Method == http.MethodPost && path == "/graphql":
		w.search(rw, body)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/reviews"),
		r.Method == http.MethodDelete && strings.HasSuffix(path, "/requested_reviewers"):
		w.reviewed(rw, path)
	case r.Method == http.MethodGet && path == "/_edge/tenant_info":
		reply(rw, http.StatusOK, map[string]string{"cloudId": cloudID})
	case r.Method == http.MethodGet && path == "/rest/api/3/search/jql":
		w.jql(rw, r.URL.Query().Get("jql"))
	case isIssue && transitions && r.Method == http.MethodGet:
		w.transitions(rw, key)
	case isIssue && transitions && r.Method == http.MethodPost:
		w.transition(rw, key, body)
	default:
		reply(rw, http.StatusNotFound, map[string]any{"message": "Not Found", "errorMessages": []string{"Not Found"}})
	}
}

func reply(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(v)
}

// search answers the PRs Widget's GraphQL request: the viewer, plus each
// search for review requests.
func (w *world) search(rw http.ResponseWriter, body []byte) {
	var q struct{ Variables map[string]any }
	if err := json.Unmarshal(body, &q); err != nil {
		reply(rw, http.StatusBadRequest, map[string]string{"message": "Problems parsing JSON"})
		return
	}
	data := map[string]any{"viewer": map[string]any{"login": viewer}}
	for name, v := range q.Variables {
		s, _ := v.(string)
		if !strings.Contains(s, "review-requested:@me") {
			continue
		}
		nodes := []any{}
		for _, p := range w.pulls {
			if !p.draft || !strings.Contains(s, "draft:false") {
				nodes = append(nodes, p.node())
			}
		}
		data[name] = map[string]any{"issueCount": len(nodes), "nodes": nodes}
	}
	reply(rw, http.StatusOK, map[string]any{"data": data})
}

func (p pr) node() map[string]any {
	var rollup any
	if p.ci != "" {
		rollup = map[string]any{"state": p.ci}
	}
	requests := []any{}
	for _, r := range p.requests {
		owner := strings.HasSuffix(r, "!")
		r = strings.TrimSuffix(r, "!")
		reviewer := map[string]any{"__typename": "User", "login": r}
		if slug, ok := strings.CutPrefix(r, "team:"); ok {
			reviewer = map[string]any{"__typename": "Team", "slug": slug}
		}
		requests = append(requests, map[string]any{"asCodeOwner": owner, "requestedReviewer": reviewer})
	}
	repo := p.repo[strings.Index(p.repo, "/")+1:]
	return map[string]any{
		"number": p.number, "title": p.title, "isDraft": p.draft,
		"url":       "https://github.com/" + p.repo + "/pull/" + itoa(p.number),
		"createdAt": p.created.UTC().Format(time.RFC3339),
		"additions": p.adds, "deletions": p.dels, "headRefName": p.branch,
		"repository":     map[string]any{"nameWithOwner": p.repo, "name": repo},
		"author":         map[string]any{"login": p.author},
		"commits":        map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{"statusCheckRollup": rollup}}}},
		"reviewRequests": map[string]any{"nodes": requests},
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// reviewed drops the PR at path, /repos/{owner}/{repo}/pulls/{n}/..., once
// the viewer approved it or left its reviewers: it no longer waits on them.
func (w *world) reviewed(rw http.ResponseWriter, path string) {
	i := slices.IndexFunc(w.pulls, func(p pr) bool {
		return strings.HasPrefix(path, "/repos/"+p.repo+"/pulls/"+itoa(p.number)+"/")
	})
	if i < 0 {
		reply(rw, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	w.pulls = slices.Delete(w.pulls, i, i+1)
	reply(rw, http.StatusOK, map[string]any{})
}

// step is a status of the demo's Jira workflow and the transition into it.
type step struct{ status, category, id, move string }

var workflow = []step{
	{"To Do", "new", "11", "Back to To Do"},
	{"In Progress", "indeterminate", "21", "Start progress"},
	{"In Review", "indeterminate", "31", "Send to review"},
	{"Done", "done", "41", "Done"},
}

func category(status string) string {
	for _, s := range workflow {
		if s.status == status {
			return s.category
		}
	}
	return "new"
}

// jql answers a search in one page. Any JQL lists every issue assigned to
// the user, most recently updated first; one leaving out Done, as the
// default does, leaves those out.
func (w *world) jql(rw http.ResponseWriter, jql string) {
	open := strings.Contains(strings.ToLower(jql), "statuscategory != done")
	list := slices.Clone(w.issues)
	slices.SortStableFunc(list, func(a, b issue) int { return b.updated - a.updated })
	found := []any{}
	for _, is := range list {
		if open && category(is.status) == "done" {
			continue
		}
		found = append(found, map[string]any{"id": "1" + strings.TrimPrefix(is.key, "ACME-"), "key": is.key, "fields": map[string]any{
			"summary":   is.summary,
			"status":    map[string]any{"name": is.status, "statusCategory": map[string]any{"key": category(is.status)}},
			"issuetype": map[string]any{"name": is.issueType, "subtask": is.subtask},
		}})
	}
	reply(rw, http.StatusOK, map[string]any{"issues": found, "isLast": true})
}

func (w *world) find(key string) *issue {
	for i := range w.issues {
		if w.issues[i].key == key {
			return &w.issues[i]
		}
	}
	return nil
}

func (w *world) transitions(rw http.ResponseWriter, key string) {
	is := w.find(key)
	if is == nil {
		reply(rw, http.StatusNotFound, map[string]any{"errorMessages": []string{"Issue does not exist or you do not have permission to see it."}})
		return
	}
	ts := []any{}
	for _, s := range workflow {
		if s.status != is.status {
			ts = append(ts, map[string]any{"id": s.id, "name": s.move, "to": map[string]string{"name": s.status}, "fields": map[string]any{}})
		}
	}
	reply(rw, http.StatusOK, map[string]any{"transitions": ts})
}

// transition moves the issue into the transition's status, making it the
// most recently updated.
func (w *world) transition(rw http.ResponseWriter, key string, body []byte) {
	var b struct {
		Transition struct{ ID string }
	}
	is := w.find(key)
	if is == nil || json.Unmarshal(body, &b) != nil {
		reply(rw, http.StatusBadRequest, map[string]any{"errorMessages": []string{"Bad request"}})
		return
	}
	i := slices.IndexFunc(workflow, func(s step) bool { return s.id == b.Transition.ID })
	if i < 0 {
		reply(rw, http.StatusBadRequest, map[string]any{"errorMessages": []string{"Transition id '" + b.Transition.ID + "' is not valid for this issue."}})
		return
	}
	latest := 0
	for _, other := range w.issues {
		latest = max(latest, other.updated)
	}
	is.status, is.updated = workflow[i].status, latest+1
	rw.WriteHeader(http.StatusNoContent)
}
