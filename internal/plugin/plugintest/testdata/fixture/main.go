// Command fixture is a Plugin that plays scripted protocol behaviours for
// visor's plugin host tests. It speaks raw JSON, the way a Plugin written in
// another language would, and depends on nothing in visor.
//
// Flags pick process-wide behaviour (-protocol answers the handshake with
// another version, -handshake silent never answers it and -handshake empty
// answers without a version, -log records every message); each Widget's
// `mode` setting picks what its refreshes return.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type widget struct {
	ID       string         `json:"widget"`
	Type     string         `json:"type"`
	Settings map[string]any `json:"settings"`
	Size     size           `json:"size"`
	Focused  bool           `json:"focused"`
	Visible  bool           `json:"visible"`
}

type fixture struct {
	mu        sync.Mutex
	out       *json.Encoder
	log       *json.Encoder
	protocol  int
	handshake string
	widgets   map[string]*widget
	refreshes map[string]int
}

func main() {
	protocol := flag.Int("protocol", 1, "the protocol version to answer the handshake with")
	handshake := flag.String("handshake", "", "silent: never answer initialize; empty: answer it without a version")
	logFile := flag.String("log", "", "append every message, in and out, to this file")
	flag.Parse()
	f := &fixture{out: json.NewEncoder(os.Stdout), protocol: *protocol, handshake: *handshake, widgets: map[string]*widget{}, refreshes: map[string]int{}}
	if *logFile != "" {
		lf, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		f.log = json.NewEncoder(lf)
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<24)
	for in.Scan() {
		var msg message
		if err := json.Unmarshal(in.Bytes(), &msg); err != nil {
			fmt.Fprintln(os.Stderr, "fixture: bad message:", err)
			continue
		}
		f.record(json.RawMessage(in.Bytes()))
		f.handle(msg)
	}
}

func (f *fixture) record(v any) {
	if f.log != nil {
		f.log.Encode(v)
	}
}

func (f *fixture) send(msg message) {
	msg.JSONRPC = "2.0"
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(msg)
	f.out.Encode(msg)
}

func (f *fixture) reply(id json.RawMessage, result any) { f.send(message{ID: id, Result: result}) }

func (f *fixture) fail(id json.RawMessage, code int, text string) {
	f.send(message{ID: id, Error: &rpcError{Code: code, Message: text}})
}

func (f *fixture) handle(msg message) {
	var p struct {
		widget
		Action string `json:"action"`
		Item   string `json:"item"`
		Text   string `json:"text"`
		Choice string `json:"choice"`
	}
	json.Unmarshal(msg.Params, &p)
	f.mu.Lock()
	w := f.widgets[p.ID]
	f.mu.Unlock()
	switch msg.Method {
	case "initialize":
		switch f.handshake {
		case "silent":
		case "empty":
			f.reply(msg.ID, map[string]any{})
		default:
			f.reply(msg.ID, map[string]any{"protocol": f.protocol})
		}
	case "widget/open":
		nw := p.widget
		f.mu.Lock()
		f.widgets[nw.ID] = &nw
		f.mu.Unlock()
	case "widget/close":
		f.mu.Lock()
		delete(f.widgets, p.ID)
		f.mu.Unlock()
	case "widget/frame":
		if w == nil {
			return
		}
		f.mu.Lock()
		focused := p.Focused && !w.Focused
		w.Size, w.Focused, w.Visible = p.Size, p.Focused, p.Visible
		f.mu.Unlock()
		if focused {
			f.send(message{Method: "widget/update", Params: raw(map[string]any{
				"widget": w.ID,
				"payload": map[string]any{"lines": []any{
					line("accent", fmt.Sprintf("pushed: focused at %d×%d after %d refreshes", p.Size.Width, p.Size.Height, f.count(w.ID))),
				}, "actions": widgetActions},
			})})
		}
	case "widget/refresh":
		if w == nil {
			f.fail(msg.ID, -32602, "no widget "+p.ID)
			return
		}
		f.refresh(msg.ID, w)
	case "action/invoke":
		f.invoke(msg.ID, w, p.Action, p.Item, p.Text, p.Choice)
	case "action/choices":
		time.Sleep(200 * time.Millisecond)
		f.reply(msg.ID, map[string]any{"choices": []any{
			map[string]string{"id": "todo", "label": "To do"},
			map[string]string{"id": "done", "label": "Done"},
			map[string]string{"id": "web", "label": "Elsewhere", "target": "https://example.com/move"},
		}})
	default:
		if msg.ID != nil {
			f.fail(msg.ID, -32601, "no method "+msg.Method)
		}
	}
}

func (f *fixture) count(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes[id]
}

func (f *fixture) refresh(id json.RawMessage, w *widget) {
	f.mu.Lock()
	f.refreshes[w.ID]++
	n := f.refreshes[w.ID]
	f.mu.Unlock()
	greeting, _ := w.Settings["greeting"].(string)
	signedIn := ""
	if _, ok := w.Settings["token"].(string); ok {
		signedIn = " (signed in)"
	}
	switch mode, _ := w.Settings["mode"].(string); mode {
	case "lines":
		f.reply(id, map[string]any{"payload": map[string]any{"lines": []any{
			line("accent", greeting+" from lines"),
			map[string]any{"digits": "half-block", "spans": []any{map[string]any{"text": "12:34", "token": "fg"}}},
			line("dim", fmt.Sprintf("refresh %d", n)),
		}, "image": map[string]any{"data": pixel, "side": "right"}}})
	case "image-file":
		f.reply(id, map[string]any{"payload": map[string]any{"lines": []any{line("fg", "hi")}, "image": map[string]any{"path": "pixel.png"}}})
	case "two-images":
		f.reply(id, map[string]any{"payload": map[string]any{"lines": []any{line("fg", "hi")}, "image": map[string]any{"data": pixel, "path": "/tmp/pixel.png"}}})
	case "bad-side":
		f.reply(id, map[string]any{"payload": map[string]any{"lines": []any{line("fg", "hi")}, "image": map[string]any{"data": pixel, "side": "under"}}})
	case "raw-colour":
		f.reply(id, map[string]any{"payload": map[string]any{"lines": []any{line("#ff0000", "red")}}})
	case "unknown-field":
		f.reply(id, map[string]any{"payload": map[string]any{"lines": []any{line("fg", "hi")}, "colour": "red"}})
	case "malformed":
		f.mu.Lock()
		os.Stdout.WriteString("{this is not json\n")
		f.mu.Unlock()
	case "setup":
		f.reply(id, map[string]any{"status": map[string]any{
			"state": "setup", "message": "Sign in to Fixture",
			"action": map[string]any{"id": "sign-in", "label": "sign in", "input": "secret", "prompt": "Fixture token"},
		}})
	case "rejected":
		f.reply(id, map[string]any{"status": map[string]any{"state": "setup", "message": "Fixture rejected the token", "action": "secret:token"}})
	case "unknown-secret":
		f.reply(id, map[string]any{"status": map[string]any{"state": "setup", "message": "Fixture needs a password", "action": "secret:password"}})
	case "own-secret":
		f.reply(id, map[string]any{"payload": map[string]any{"lines": []any{line("fg", "hi")}, "actions": []any{
			map[string]any{"id": "secret:token", "label": "Set token my way", "input": "secret"},
		}}})
	case "leak":
		// Echoes the stored token on purpose: the host must mask it everywhere.
		f.reply(id, map[string]any{"payload": map[string]any{"lines": []any{line("fg", "token "+token(w))}, "actions": []any{
			map[string]any{"id": "leak-toast", "key": "t", "label": "leak toast"},
			map[string]any{"id": "leak-error", "key": "e", "label": "leak error"},
			map[string]any{"id": "leak-crash", "key": "x", "label": "leak crash"},
		}}})
	case "leak-status":
		f.reply(id, map[string]any{"status": map[string]any{"state": "error", "message": "upstream refused " + token(w), "reason": "refused"}})
	case "error":
		f.reply(id, map[string]any{"status": map[string]any{"state": "error", "message": "upstream is down", "reason": "down", "hint": "try later"}})
	default:
		item := func(id, name string) map[string]any {
			return map[string]any{
				"id": id,
				"fields": []any{
					[]any{map[string]any{"text": greeting + " " + name + signedIn, "token": "fg"}},
					[]any{map[string]any{"text": fmt.Sprintf("refresh %d", n), "token": "dim"}},
				},
				"actions": []any{
					map[string]any{"id": "done", "key": "d", "label": "done"},
					map[string]any{"id": "open", "key": "o", "label": "open", "primary": true, "verb": "open", "target": "https://example.com/" + id},
					map[string]any{"id": "move", "key": "c", "label": "move", "input": "choice", "prompt": "Move " + id},
				},
			}
		}
		f.reply(id, map[string]any{"payload": map[string]any{
			"columns": []any{map[string]any{"flex": true}, map[string]any{"priority": 1, "right": true}},
			"items":   []any{item("alpha", "alpha"), item("bravo", "bravo")},
			"count":   2,
			"actions": widgetActions,
		}})
	}
}

func (f *fixture) invoke(id json.RawMessage, w *widget, action, item, text, choice string) {
	switch action {
	case "done":
		f.reply(id, map[string]any{"toast": "done " + item})
	case "move":
		f.reply(id, map[string]any{"toast": "moved " + item + " to " + choice})
	case "greet":
		f.reply(id, map[string]any{"toast": "greeting set", "settings": map[string]any{"greeting": text}})
	case "bad-settings":
		f.reply(id, map[string]any{"settings": map[string]any{"volume": 11}})
	case "bad-mode":
		f.reply(id, map[string]any{"settings": map[string]any{"mode": "loud"}})
	case "token-setting":
		f.reply(id, map[string]any{"settings": map[string]any{"token": "from the Plugin"}})
	case "chain":
		f.reply(id, map[string]any{"then": map[string]any{"id": "note", "label": "note", "input": "text", "prompt": "Note"}})
	case "chain-secret":
		f.reply(id, map[string]any{"then": map[string]any{"id": "secret:token", "label": "Set token my way", "input": "secret"}})
	case "note":
		f.reply(id, map[string]any{"toast": "noted " + text})
	case "token", "sign-in":
		// Quotes the secret back on purpose: the host must never show it.
		f.reply(id, map[string]any{"toast": "stored " + text})
	case "crash":
		fmt.Fprintln(os.Stderr, "fixture: boom")
		os.Exit(3)
	case "leak-toast":
		f.reply(id, map[string]any{"toast": "signed in with " + token(w)})
	case "leak-error":
		f.fail(id, 1, "refused "+token(w))
	case "leak-crash":
		fmt.Fprintln(os.Stderr, "Traceback (most recent call last):")
		fmt.Fprintln(os.Stderr, "fixture.AuthError: bad token "+token(w))
		os.Exit(3)
	default:
		f.fail(id, 1, "no action "+action)
	}
}

// pixel is a 1×1 PNG, base64.
const pixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="

var widgetActions = []any{
	map[string]any{"id": "note", "key": "t", "label": "note", "input": "text", "prompt": "Note"},
	map[string]any{"id": "token", "key": "s", "label": "token", "input": "secret", "prompt": "Token"},
	map[string]any{"id": "crash", "key": "x", "label": "crash"},
	map[string]any{"id": "greet", "key": "e", "label": "greet", "input": "text", "prompt": "Greeting"},
	map[string]any{"id": "chain", "key": "n", "label": "chain"},
}

func token(w *widget) string {
	if w == nil {
		return ""
	}
	t, _ := w.Settings["token"].(string)
	return t
}

func line(token, text string) map[string]any {
	return map[string]any{"spans": []any{map[string]any{"text": text, "token": token}}}
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
