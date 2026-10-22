// Package script is the script Widget type: a shell command run on an
// interval whose output is the Widget, for one-liners that don't deserve a Plugin.
package script

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/shell"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

var Type = widget.Type{
	Name:  "script",
	Title: "Script",
	Description: "A command run on an interval. It prints a JSON array of objects (one Item each, " +
		"one column per key, which `actions` templates can name) or plain text lines; {\"lines\": [...]} gives styled lines.",
	Settings: []widget.Setting{{
		Name: "command", Kind: widget.String, Default: "", Required: true,
		Description: "The command, run by sh (cmd on Windows).",
		Why:         "Show anything a one-liner can print, such as `brew outdated --json=v2 | jq '[.formulae[] | {name, current: .current_version}]'`.",
	}, {
		Name: "interval", Kind: widget.Duration, Default: time.Minute,
		Description: "How often the command runs; `refresh` does the same and wins.",
		Why:         "Slow or rate-limited commands want a longer one.",
	}},
	Refresh: time.Minute,
	Floor:   time.Second,
	Fetches: true,
	OptIn:   true,
	New:     newScript,
}

func newScript(c widget.Config) widget.Widget {
	return &script{command: c.Settings.String("command"), interval: c.Settings.Duration("interval")}
}

type script struct {
	command  string
	interval time.Duration
}

func (s *script) Interval() time.Duration { return s.interval }

func (s *script) Refresh(ctx context.Context, _ time.Time) (widget.Payload, error) {
	if strings.TrimSpace(s.command) == "" {
		return widget.Payload{}, &widget.SetupError{Hint: "Set `command` in this Widget's [[widget]] block"}
	}
	out, err := shell.Run(ctx, s.command)
	if err != nil {
		var f shell.Failure
		if errors.As(err, &f) {
			return widget.Payload{}, failure{reason: f.Reason, msg: f.Msg}
		}
		return widget.Payload{}, err
	}
	return parse(out)
}

// failure is a command that failed, or printed what can't be shown; reason
// is the short form for the stale marker.
type failure struct{ reason, msg string }

func (f failure) Error() string  { return f.msg }
func (f failure) Reason() string { return f.reason }
func (f failure) Hint() string   { return "Check `command` by running it in a shell" }

// parse reads a JSON array of objects as Items, each valued by its keys,
// {"lines": [...]} as styled lines, and anything else as plain text lines.
func parse(out []byte) (p widget.Payload, err error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return widget.Payload{Lines: []widget.Line{widget.Text(theme.Faint, "no output")}}, nil
	}
	switch trimmed[0] {
	case '[':
		return items(trimmed)
	case '{':
		var doc struct {
			Lines []line `json:"lines"`
		}
		if err := json.Unmarshal(trimmed, &doc); err != nil {
			return widget.Payload{}, badJSON(err)
		}
		for _, l := range doc.Lines {
			p.Lines = append(p.Lines, widget.Line(l))
		}
		return p, nil
	}
	for _, l := range strings.Split(strings.TrimRight(ansi.Strip(string(out)), "\n"), "\n") {
		p.Lines = append(p.Lines, widget.Text(theme.Fg, strings.ReplaceAll(l, "\t", "    ")))
	}
	return p, nil
}

func badJSON(err error) error {
	return failure{reason: "bad JSON", msg: "the command printed bad JSON: " + err.Error()}
}

// line is a styled line, or a plain string for a line of fg text.
type line widget.Line

func (l *line) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*l = line(widget.Text(theme.Fg, s))
		return nil
	}
	return json.Unmarshal(b, (*widget.Line)(l))
}

// object is one JSON object with its keys in the order printed.
type object struct {
	keys   []string
	values map[string]string
}

func items(b []byte) (widget.Payload, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if _, err := dec.Token(); err != nil {
		return widget.Payload{}, badJSON(err)
	}
	var objs []object
	var keys []string
	for dec.More() {
		o, err := readObject(dec)
		if err != nil {
			return widget.Payload{}, badJSON(err)
		}
		for _, k := range o.keys {
			if k != "id" && !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
		objs = append(objs, o)
	}
	if _, err := dec.Token(); err != nil {
		return widget.Payload{}, badJSON(err)
	}
	p := widget.Payload{Count: len(objs), Empty: "Nothing to show."}
	for i := range keys {
		p.Columns = append(p.Columns, widget.Column{Priority: i, Flex: i == 0})
	}
	if len(p.Columns) == 0 {
		p.Columns = []widget.Column{{Flex: true}}
	}
	seen := map[string]int{}
	for _, o := range objs {
		it := widget.Item{ID: o.values["id"], Values: o.values}
		for i, k := range keys {
			tok := theme.Dim
			if i == 0 {
				tok = theme.Fg
			}
			it.Fields = append(it.Fields, widget.F(tok, o.values[k]))
		}
		if it.ID == "" && len(keys) > 0 {
			it.ID = o.values[keys[0]]
		}
		seen[it.ID]++
		if n := seen[it.ID]; n > 1 {
			it.ID = fmt.Sprintf("%s#%d", it.ID, n)
		}
		p.Items = append(p.Items, it)
	}
	return p, nil
}

func readObject(dec *json.Decoder) (object, error) {
	o := object{values: map[string]string{}}
	t, err := dec.Token()
	if err != nil {
		return o, err
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return o, errors.New("the array must hold objects")
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return o, err
		}
		k, _ := t.(string)
		var v any
		if err := dec.Decode(&v); err != nil {
			return o, err
		}
		if _, dup := o.values[k]; !dup {
			o.keys = append(o.keys, k)
		}
		o.values[k] = text(v)
	}
	_, err = dec.Token()
	return o, err
}

// text is how a JSON value shows in a column and fills a template.
func text(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return fmt.Sprint(v)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
