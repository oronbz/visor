package jira_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeJira is a fake Jira Cloud site: JQL search with cursor paging over
// fixture issues, and each issue's transitions. It also stands in for
// api.atlassian.com's /ex/jira/{cloudId} route to the same site. It records
// every request.
type fakeJira struct {
	srv *httptest.Server

	mu     sync.Mutex
	issues []issue
	// queries answers a JQL other than the default with its own issues.
	queries map[string][]issue
	// page is how many issues one search page holds.
	page     int
	requests []request
	// reply, when set, answers a request instead of the fixtures; status 0 falls through.
	reply func(r request) (status int, header http.Header, body string)
	// scoped is a scoped token: the site turns it down, only the
	// api.atlassian.com route takes it.
	scoped string
}

// cloudID is the site's id, from its tenant info.
const cloudID = "0f3c1e2d-7a4b-4c5d-9e8f-123456789abc"

// apiRoute is the api.atlassian.com path prefix to the site.
const apiRoute = "/ex/jira/" + cloudID

type issue struct {
	key, summary, status string
	// category is the status category key: new, indeterminate or done.
	category  string
	issueType string
	subtask   bool
	// transitions are offered by the issue; a trailing "!" on a name means
	// the transition has a required field.
	transitions []transition
}

type transition struct{ id, name, to string }

type request struct {
	Method, Path, Auth string
	Query              url.Values
	Body               string
}

func (r request) String() string { return r.Method + " " + r.Path }

func newJira(t *testing.T) *fakeJira {
	j := &fakeJira{page: 50, queries: map[string][]issue{}}
	j.srv = httptest.NewServer(http.HandlerFunc(j.serve))
	t.Cleanup(j.srv.Close)
	return j
}

func (j *fakeJira) serve(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	req := request{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Query: r.URL.Query(), Body: string(b)}
	j.mu.Lock()
	j.requests = append(j.requests, req)
	reply := j.reply
	j.mu.Unlock()
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
	j.mu.Lock()
	defer j.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	path, viaAPI := strings.CutPrefix(r.URL.Path, apiRoute)
	if r.URL.Path == "/_edge/tenant_info" {
		if req.Auth != "" {
			http.Error(w, "tenant info takes no credentials", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"cloudId": cloudID})
		return
	}
	if _, pass, _ := r.BasicAuth(); j.scoped != "" && pass == j.scoped && !viaAPI {
		http.Error(w, `{"errorMessages":["Client must be authenticated to access this resource."]}`, http.StatusUnauthorized)
		return
	}
	switch key, ok := strings.CutPrefix(path, "/rest/api/3/issue/"); {
	case r.Method == http.MethodGet && path == "/rest/api/3/search/jql":
		j.search(w, req.Query)
	case ok && r.Method == http.MethodGet && strings.HasSuffix(key, "/transitions"):
		j.transitions(w, strings.TrimSuffix(key, "/transitions"))
	case ok && r.Method == http.MethodPost && strings.HasSuffix(key, "/transitions"):
		j.transition(w, strings.TrimSuffix(key, "/transitions"), b)
	default:
		http.NotFound(w, r)
	}
}

// search answers one page of a JQL search; nextPageToken is the index of
// the page's first issue.
func (j *fakeJira) search(w http.ResponseWriter, q url.Values) {
	all, ok := j.queries[q.Get("jql")]
	if !ok {
		all = j.issues
	}
	from, _ := strconv.Atoi(q.Get("nextPageToken"))
	to := min(from+j.page, len(all))
	var issues []any
	for _, is := range all[from:to] {
		var issueType any
		if is.issueType != "" {
			issueType = map[string]any{"name": is.issueType, "subtask": is.subtask}
		}
		issues = append(issues, map[string]any{"id": "1" + is.key, "key": is.key, "fields": map[string]any{
			"summary":   is.summary,
			"status":    map[string]any{"name": is.status, "statusCategory": map[string]any{"key": is.category}},
			"issuetype": issueType,
		}})
	}
	page := map[string]any{"issues": issues, "isLast": to == len(all)}
	if to < len(all) {
		page["nextPageToken"] = strconv.Itoa(to)
	}
	json.NewEncoder(w).Encode(page)
}

func (j *fakeJira) find(key string) *issue {
	for _, list := range append([][]issue{j.issues}, mapValues(j.queries)...) {
		for i := range list {
			if list[i].key == key {
				return &list[i]
			}
		}
	}
	return nil
}

func (j *fakeJira) transitions(w http.ResponseWriter, key string) {
	is := j.find(key)
	if is == nil {
		http.Error(w, `{"errorMessages":["Issue does not exist"]}`, http.StatusNotFound)
		return
	}
	var ts []any
	for _, t := range is.transitions {
		fields := map[string]any{}
		name, required := strings.CutSuffix(t.name, "!")
		if required {
			fields["resolution"] = map[string]any{"required": true, "hasDefaultValue": false, "name": "Resolution"}
		}
		fields["comment"] = map[string]any{"required": false, "name": "Comment"}
		ts = append(ts, map[string]any{"id": t.id, "name": name, "to": map[string]any{"name": t.to}, "hasScreen": required, "fields": fields})
	}
	json.NewEncoder(w).Encode(map[string]any{"transitions": ts})
}

// transition moves the issue to the transition's status, as Jira would.
func (j *fakeJira) transition(w http.ResponseWriter, key string, body []byte) {
	var b struct {
		Transition struct{ ID string } `json:"transition"`
	}
	is := j.find(key)
	if is == nil || json.Unmarshal(body, &b) != nil {
		http.Error(w, `{"errorMessages":["bad request"]}`, http.StatusBadRequest)
		return
	}
	for _, t := range is.transitions {
		if t.id == b.Transition.ID {
			for _, list := range append([][]issue{j.issues}, mapValues(j.queries)...) {
				for i := range list {
					if list[i].key == key {
						list[i].status = t.to
					}
				}
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	http.Error(w, `{"errorMessages":["Transition id '`+b.Transition.ID+`' is not valid for this issue."]}`, http.StatusBadRequest)
}

func mapValues(m map[string][]issue) [][]issue {
	var out [][]issue
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// Requests are the requests Jira has received so far.
func (j *fakeJira) Requests() []request {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]request(nil), j.requests...)
}

// searches are the JQL search requests so far.
func (j *fakeJira) searches() []request {
	var out []request
	for _, r := range j.Requests() {
		if strings.HasSuffix(r.Path, "/rest/api/3/search/jql") {
			out = append(out, r)
		}
	}
	return out
}
