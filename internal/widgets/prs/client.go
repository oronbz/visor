package prs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/widget"
)

// client talks to GitHub's GraphQL and REST APIs with one token.
type client struct {
	http      *http.Client
	endpoints ports.Endpoints
	token     string
	// from is where token came from, for setup hints.
	from string
	// now is when the call is made, for rate limit pauses given in seconds.
	now time.Time
}

// rateLimitWait is the pause when GitHub limits a request without saying for how long.
const rateLimitWait = time.Minute

func (c client) graphQL(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	res, err := c.do(ctx, http.MethodPost, c.endpoints.GitHubGraphQL, body)
	if err != nil {
		return err
	}
	var reply struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(res.body, &reply); err != nil {
		return fmt.Errorf("GitHub sent a reply visor can't read: %w", err)
	}
	if len(reply.Errors) > 0 {
		var msgs []string
		for _, e := range reply.Errors {
			if e.Type == "RATE_LIMITED" {
				return &widget.RateLimitError{Until: c.resetAt(res.header), Err: errors.New("GitHub: " + e.Message)}
			}
			if e.Type == "INSUFFICIENT_SCOPES" {
				return scopeError{e.Message}
			}
			msgs = append(msgs, e.Message)
		}
		return errors.New("GitHub: " + strings.Join(msgs, "; "))
	}
	return json.Unmarshal(reply.Data, out)
}

// rest calls a REST endpoint, path being relative to the API root.
func (c client) rest(ctx context.Context, method, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, method, strings.TrimSuffix(c.endpoints.GitHubREST, "/")+path, body)
	return err
}

type response struct {
	header http.Header
	body   []byte
}

func (c client) do(ctx context.Context, method, url string, body []byte) (response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Authorization", "bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := c.http.Do(req)
	if err != nil {
		return response{}, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return response{}, err
	}
	r := response{header: res.Header, body: b}
	if res.StatusCode < 300 {
		return r, nil
	}
	msg := message(b, res.Status)
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return r, authError{msg}
	case limited(res):
		return r, &widget.RateLimitError{Until: c.resetAt(res.Header), Err: errors.New("GitHub: " + msg)}
	}
	return r, errors.New("GitHub: " + msg)
}

// limited reports a primary or secondary rate limit: a 429, or a 403 that
// says when to retry or that no requests are left.
func limited(res *http.Response) bool {
	if res.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return res.StatusCode == http.StatusForbidden &&
		(res.Header.Get("Retry-After") != "" || res.Header.Get("X-RateLimit-Remaining") == "0")
}

func (c client) resetAt(h http.Header) time.Time {
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil {
		return c.now.Add(time.Duration(s) * time.Second)
	}
	if s, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		if t := time.Unix(s, 0); t.After(c.now) {
			return t
		}
	}
	return c.now.Add(rateLimitWait)
}

func message(body []byte, status string) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return status
}

type authError struct{ msg string }

func (e authError) Error() string  { return "GitHub rejected the token: " + e.msg }
func (e authError) Reason() string { return "token rejected" }
func (e authError) Hint() string   { return "Run `gh auth login`, or check GH_TOKEN and GITHUB_TOKEN" }

// scopeError is a token without the scopes the search needs.
type scopeError struct{ msg string }

func (e scopeError) Error() string  { return "GitHub: " + e.msg }
func (e scopeError) Reason() string { return "missing scope" }
func (e scopeError) Hint() string   { return "Use a classic token with " + scopes }
