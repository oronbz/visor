// Package jira is the Jira Widget type: the issues a JQL search finds, by
// default my unresolved assigned ones, with transitions from a choice menu.
package jira

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

// DefaultJQL is my unresolved assigned issues, most recently updated first.
const DefaultJQL = "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC"

var Type = widget.Type{
	Name:        "jira",
	Title:       "Jira",
	Description: "Jira issues from a JQL search, by default your unresolved assigned ones.",
	Settings: []widget.Setting{
		{
			Name: "site", Kind: widget.String, Default: "",
			Description: "Your Jira site, such as \"acme.atlassian.net\".",
			Why:         "visor needs it to know which Jira to sign in to.",
		},
		{
			Name: "email", Kind: widget.String, Default: "",
			Description: "The email you sign in to Jira with; leave it empty for a Data Center personal access token.",
			Why:         "With an API token it signs visor in as you.",
		},
		{
			Name: "jql", Kind: widget.String, Default: DefaultJQL,
			Description: "The JQL search whose issues the Widget lists.",
			Why:         "Change it to watch another queue, such as your team's board.",
		},
	},
	Refresh: 2 * time.Minute,
	Floor:   30 * time.Second,
	Fetches: true,
	New:     newJira,
}

func newJira(c widget.Config) widget.Widget {
	return jira{
		ports: c.Ports,
		site:  host(c.Settings.String("site")),
		email: strings.TrimSpace(c.Settings.String("email")),
		jql:   c.Settings.String("jql"),
		route: &route{},
	}
}

type jira struct {
	ports ports.Ports
	// site is the site's host name, such as acme.atlassian.net.
	site, email, jql string
	route            *route
}

// host is a site setting's host name, whether or not it was given as a URL.
func host(site string) string {
	site = strings.TrimSpace(site)
	if u, err := url.Parse(site); err == nil && u.Host != "" {
		return u.Host
	}
	return strings.TrimSuffix(site, "/")
}

// Credential is the account of visor's keyring item, jira:<site>:<email>,
// which also names the token every Widget on that site and account shares.
func (w jira) Credential() string { return account(w.site, w.email) }

func account(site, email string) string { return "jira:" + site + ":" + email }

func (w jira) browse(key string) string { return "https://" + w.site + "/browse/" + key }

func (w jira) client(ctx context.Context, now time.Time) (client, error) {
	if w.site == "" {
		return client{}, w.needsSetup("Set `site` for this Jira Widget")
	}
	tok, ok := token(w.ports, w.site, w.email, w.Credential())
	if !ok {
		return client{}, w.needsSetup("No Jira API token: set JIRA_API_TOKEN, or sign in with jira-cli")
	}
	site := "https://" + w.site
	if e := w.ports.Endpoints.Jira; e != "" {
		site = strings.TrimSuffix(e, "/")
	}
	api := strings.TrimSuffix(w.ports.Endpoints.AtlassianAPI, "/")
	return client{http: w.ports.HTTP, site: site, api: api, route: w.route, email: w.email, token: tok, now: now}, nil
}

// needsSetup is the Status of a Widget visor can't sign in with, fixed by
// the Set up Jira flow.
func (w jira) needsSetup(hint string) *widget.SetupError {
	a := Setup{}.Start(w.site, w.email)
	return &widget.SetupError{Hint: hint, Action: &a}
}

// maxPages caps a search, so a runaway cursor can't page forever.
const maxPages = 20

type searchPage struct {
	Issues        []issueJSON
	NextPageToken string
	IsLast        bool
}

type issueJSON struct {
	Key    string
	Fields struct {
		Summary string
		Status  struct {
			Name           string
			StatusCategory struct{ Key string }
		}
		Issuetype *issueTypeJSON
	}
}

func (w jira) Refresh(ctx context.Context, now time.Time) (widget.Payload, error) {
	c, err := w.client(ctx, now)
	if err != nil {
		return widget.Payload{}, err
	}
	var issues []issueJSON
	cursor := ""
	for range maxPages {
		q := url.Values{"jql": {w.jql}, "fields": {"summary,status,issuetype"}, "maxResults": {"100"}}
		if cursor != "" {
			q.Set("nextPageToken", cursor)
		}
		var page searchPage
		if err := c.get(ctx, "/rest/api/3/search/jql?"+q.Encode(), &page); err != nil {
			return widget.Payload{}, w.status(err)
		}
		issues = append(issues, page.Issues...)
		if page.IsLast || page.NextPageToken == "" || page.NextPageToken == cursor {
			break
		}
		cursor = page.NextPageToken
	}
	return w.payload(issues), nil
}

// status turns a rejected token into needs setup.
func (w jira) status(err error) error {
	if errors.As(err, new(authError)) {
		if w.email == "" {
			return w.needsSetup("Jira rejected the personal access token; check JIRA_API_TOKEN or set up Jira again")
		}
		return w.needsSetup(fmt.Sprintf("Jira rejected the API token for %s; check JIRA_API_TOKEN or set up Jira again", w.email))
	}
	return err
}

func (w jira) payload(issues []issueJSON) widget.Payload {
	p := widget.Payload{
		Columns: []widget.Column{{Priority: 3}, {Priority: 1}, {Priority: 0, Flex: true}, {Priority: 2, Right: true}},
		Empty:   "No issues match the JQL.",
		Count:   len(issues),
	}
	if w.jql == DefaultJQL {
		p.Empty = "Nothing assigned to you."
	}
	for _, is := range issues {
		glyph, tok := issueType(is.Fields.Issuetype)
		p.Items = append(p.Items, widget.Item{
			ID: is.Key,
			Fields: []widget.Field{
				widget.F(tok, glyph),
				widget.F(theme.Dim, is.Key),
				widget.F(theme.Fg, is.Fields.Summary),
				widget.F(category(is.Fields.Status.StatusCategory.Key), is.Fields.Status.Name),
			},
			Actions: []widget.Action{
				{ID: "open", Key: "enter", Label: "open", Primary: true, Verb: widget.Open, Target: w.browse(is.Key)},
				{ID: transitionAction, Key: "t", Label: "transition", Input: widget.ChoiceInput, Prompt: "Transition " + is.Key},
			},
		})
	}
	return p
}

type issueTypeJSON struct {
	Name    string
	Subtask bool
}

// issueType is the glyph for an issue's type, coloured as Jira colours it; a
// site's own types, such as "Infra Task", go by the stock type they name.
func issueType(t *issueTypeJSON) (string, theme.Token) {
	if t == nil {
		return "·", theme.Faint
	}
	name := strings.ToLower(t.Name)
	switch {
	case t.Subtask || strings.Contains(name, "sub-task") || strings.Contains(name, "subtask"):
		return "↳", theme.Dim
	case strings.Contains(name, "bug"):
		return "●", theme.Bad
	case strings.Contains(name, "epic"):
		return "✦", theme.Info
	case strings.Contains(name, "story"):
		return "◆", theme.Ok
	case strings.Contains(name, "task"):
		return "✓", theme.Accent
	}
	return "·", theme.Faint
}

func category(key string) theme.Token {
	switch key {
	case "indeterminate":
		return theme.Accent
	case "done":
		return theme.Ok
	}
	return theme.Dim
}

const transitionAction = "transition"

type transitionsJSON struct {
	Transitions []struct {
		ID, Name string
		To       struct{ Name string }
		Fields   map[string]struct {
			Required, HasDefaultValue bool
		}
	}
}

// Choices are the issue's transitions; one with a required field can't be
// filled in here, so it opens the issue instead.
func (w jira) Choices(ctx context.Context, inv widget.Invocation) ([]widget.Choice, error) {
	c, err := w.client(ctx, inv.Now)
	if err != nil {
		return nil, err
	}
	var ts transitionsJSON
	if err := c.get(ctx, "/rest/api/3/issue/"+url.PathEscape(inv.Item)+"/transitions?expand=transitions.fields", &ts); err != nil {
		return nil, err
	}
	var out []widget.Choice
	for _, t := range ts.Transitions {
		ch := widget.Choice{ID: t.ID, Label: t.Name}
		if t.To.Name != "" && !strings.EqualFold(t.To.Name, t.Name) {
			ch.Label += " → " + t.To.Name
		}
		for _, f := range t.Fields {
			if f.Required && !f.HasDefaultValue {
				ch.Target = w.browse(inv.Item)
				break
			}
		}
		out = append(out, ch)
	}
	return out, nil
}

func (w jira) Invoke(ctx context.Context, inv widget.Invocation) (widget.Result, error) {
	if IsSetup(inv.Action) {
		return Setup{Keyring: w.ports.Keyring}.Invoke(ctx, inv)
	}
	if inv.Action != transitionAction {
		return widget.Result{}, fmt.Errorf("Jira has no Action %q", inv.Action)
	}
	c, err := w.client(ctx, inv.Now)
	if err != nil {
		return widget.Result{}, err
	}
	body := map[string]any{"transition": map[string]string{"id": inv.Choice}}
	if err := c.send(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(inv.Item)+"/transitions", body); err != nil {
		return widget.Result{}, err
	}
	return widget.Result{Toast: "Transitioned " + inv.Item}, nil
}
