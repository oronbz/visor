package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/widget"
)

type CheckOptions struct {
	Clock ports.Clock
	// Timeout bounds each step's wait on the Plugin; zero is 10s.
	Timeout time.Duration
}

// checkAction is the Action the check invokes: no Plugin has it, so the
// round-trip runs none of a Plugin's real Actions.
const checkAction = "visor-plugin-check"

// Check is `visor plugin check`: it runs the Plugin in dir through the same
// host code visor uses, checks every message against the shipped schema and
// prints a line per step to out.
func Check(dir string, o CheckOptions, out io.Writer) bool {
	if o.Timeout <= 0 {
		o.Timeout = handshakeTimeout
	}
	c := &checker{out: out, timeout: o.Timeout, asked: map[string]string{}}
	var m Manifest
	c.step("manifest", func(context.Context) (string, error) {
		var err error
		if m, err = ReadManifest(dir); err != nil {
			return "", err
		}
		if m.Protocol != Protocol {
			return "", fmt.Errorf("declares protocol %d, but this visor speaks %d", m.Protocol, Protocol)
		}
		var types, secrets []string
		for _, w := range m.Widgets {
			types = append(types, w.Type)
		}
		for s := range m.secrets() {
			if s.Required {
				s.Name += " (required)"
			}
			if !slices.Contains(secrets, s.Name) {
				secrets = append(secrets, s.Name)
			}
		}
		detail := fmt.Sprintf("`%s`, Widget types: %s", m.Name, strings.Join(types, ", "))
		if len(secrets) > 0 {
			detail += "; secret settings: " + strings.Join(secrets, ", ")
		}
		return detail, nil
	})
	p := &Plugin{name: m.Name, m: m, clock: o.Clock, widgets: map[string]*pluginWidget{}, tap: c.tap}
	var proc *process
	defer func() {
		if proc != nil {
			proc.stop()
			<-proc.exited
		}
	}()
	c.step("start", func(context.Context) (string, error) {
		var err error
		proc, err = p.spawn()
		return strings.Join(m.Command, " "), err
	})
	c.step("handshake", func(ctx context.Context) (string, error) {
		if err := p.handshake(ctx, proc); err != nil {
			proc = nil
			return "", err
		}
		p.proc = proc
		return fmt.Sprintf("protocol %d", Protocol), nil
	})
	for _, wt := range m.Widgets {
		c.lifecycle(p, wt)
	}
	c.step("quit", func(context.Context) (string, error) {
		proc.stopping.Store(true)
		_ = proc.stdin.Close()
		select {
		case <-proc.exited:
		case <-time.After(exitWait):
			return "", fmt.Errorf("didn't exit within %s of its stdin closing", exitWait)
		}
		defer func() { proc = nil }()
		if st := proc.cmd.ProcessState; !st.Success() {
			return "", proc.stderr.withLastLine(fmt.Sprintf("exited with %v once its stdin closed", st))
		}
		return "exited once its stdin closed", nil
	})
	if c.failed {
		fmt.Fprintln(out, "FAIL")
		return false
	}
	fmt.Fprintln(out, "PASS")
	return true
}

func (c *checker) lifecycle(p *Plugin, wt WidgetType) {
	t, _ := wt.widgetType()
	w := p.widget(t.Name, t.Settings, widget.Config{ID: t.Name, Settings: widget.Defaults(t.Settings), Ports: ports.Ports{Clock: p.clock}})
	c.step("open "+t.Name, func(ctx context.Context) (string, error) {
		w.Observe(widget.Shown{Size: widget.Size{W: 40, H: 10}, Visible: true})
		if _, err := p.connect(ctx, w); err != nil {
			return "", err
		}
		w.Listen(c.pushed)
		w.Observe(widget.Shown{Size: widget.Size{W: 40, H: 10}, Focused: true, Visible: true})
		return "widget/open, then widget/frame focused at 40×10", nil
	})
	var payload widget.Payload
	c.step("refresh "+t.Name, func(ctx context.Context) (string, error) {
		var err error
		payload, err = w.Refresh(ctx, p.clock.Now())
		var setup *widget.SetupError
		var limited *widget.RateLimitError
		var f failure
		switch {
		case errors.As(err, &setup) && setup.Action != nil:
			return fmt.Sprintf("needs setup: %s (⏎ %s)", setup.Hint, setup.Action.Label), nil
		case errors.As(err, &setup):
			return "needs setup: " + setup.Hint, nil
		case errors.As(err, &limited):
			return "rate limited until " + limited.Until.Format(time.RFC3339), nil
		case errors.As(err, &f):
			return "error: " + f.Error(), nil
		case err != nil:
			return "", err
		case len(payload.Columns) > 0:
			return fmt.Sprintf("%d items", len(payload.Items)), nil
		}
		return fmt.Sprintf("%d lines", len(payload.Lines)), nil
	})
	c.step("action "+t.Name, func(ctx context.Context) (string, error) {
		var done []string
		if inv, ok := choiceAction(payload); ok {
			choices, err := w.Choices(ctx, inv)
			var rpc *RPCError
			if err != nil && !errors.As(err, &rpc) {
				return "", err
			}
			done = append(done, fmt.Sprintf("action/choices `%s`: %d choices", inv.Action, len(choices)))
		}
		_, err := w.Invoke(ctx, widget.Invocation{Action: checkAction})
		var rpc *RPCError
		switch {
		case errors.As(err, &rpc):
			done = append(done, fmt.Sprintf("action/invoke `%s`: error %q", checkAction, rpc.Message))
		case err != nil:
			return "", err
		default:
			done = append(done, fmt.Sprintf("action/invoke `%s`: ok", checkAction))
		}
		return strings.Join(done, "; "), nil
	})
	c.step("close "+t.Name, func(context.Context) (string, error) {
		p.wmu.Lock()
		delete(p.widgets, w.id)
		p.wmu.Unlock()
		w.mu.Lock()
		on := w.on
		w.closed, w.push = true, nil
		w.mu.Unlock()
		if on == nil {
			return "", errors.New("the Widget isn't open")
		}
		return "widget/close", on.conn.Notify("widget/close", widgetParams{Widget: w.id})
	})
}

// choiceAction is the first choice Action in payload, on the first Item
// that has one or on the Widget.
func choiceAction(payload widget.Payload) (widget.Invocation, bool) {
	isChoice := func(a widget.Action) bool { return a.Input == widget.ChoiceInput }
	for _, it := range payload.Items {
		if i := slices.IndexFunc(it.Actions, isChoice); i >= 0 {
			return widget.Invocation{Action: it.Actions[i].ID, Item: it.ID}, true
		}
	}
	if i := slices.IndexFunc(payload.Actions, isChoice); i >= 0 {
		return widget.Invocation{Action: payload.Actions[i].ID}, true
	}
	return widget.Invocation{}, false
}

// checker runs steps, skipping the rest once one fails, and collects what
// the schema finds wrong with the messages in between.
type checker struct {
	out     io.Writer
	timeout time.Duration
	failed  bool

	mu sync.Mutex
	// asked is each request's method, by its id as JSON, to check its result.
	asked map[string]string
	bad   []string
}

func (c *checker) step(name string, run func(context.Context) (string, error)) {
	if c.failed {
		fmt.Fprintf(c.out, "skip  %s\n", name)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	detail, err := run(ctx)
	cancel()
	c.mu.Lock()
	why := c.bad
	c.bad = nil
	c.mu.Unlock()
	if err != nil {
		why = append([]string{err.Error()}, why...)
	}
	if len(why) == 0 {
		fmt.Fprintf(c.out, "ok    %-16s %s\n", name, detail)
		return
	}
	c.failed = true
	fmt.Fprintf(c.out, "FAIL  %-16s %s\n", name, why[0])
	for _, w := range why[1:] {
		fmt.Fprintf(c.out, "      %-16s %s\n", "", w)
	}
}

// tap checks one message against the protocol schema and a result against
// the schema of the request it answers.
func (c *checker) tap(out bool, raw []byte) {
	var env struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
	}
	_ = json.Unmarshal(raw, &env)
	what := "the Plugin's message"
	if out {
		what = "visor's message"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	asked := c.asked[string(env.ID)]
	switch {
	case env.Method != "":
		what = strings.TrimSuffix(what, "message") + env.Method
		if out && env.ID != nil {
			c.asked[string(env.ID)] = env.Method
		}
	case asked != "":
		what = "the Plugin's answer to " + asked
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err == nil {
		err = Validate("protocol.json", doc)
	}
	if def := resultDefs[asked]; err == nil && env.Method == "" && env.Result != nil && def != "" {
		if err = Validate("protocol.json#/$defs/"+def, doc.(map[string]any)["result"]); err != nil {
			err = fmt.Errorf("result: %w", err)
		}
	}
	if err != nil {
		c.bad = append(c.bad, fmt.Sprintf("%s breaks the schema: %v", what, err))
	}
}

var resultDefs = map[string]string{
	"initialize":     "initializeResult",
	"widget/refresh": "refreshResult",
	"action/choices": "choicesResult",
	"action/invoke":  "invokeResult",
}

// pushed records a pushed update visor couldn't use.
func (c *checker) pushed(push widget.Push) {
	if push.Err == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bad = append(c.bad, "widget/update: "+push.Err.Error())
}
