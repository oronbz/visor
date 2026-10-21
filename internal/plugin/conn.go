package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// message is any JSON-RPC 2.0 message.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error a Plugin answered a request with.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return e.Message }

// conn is one JSON-RPC 2.0 session over a Plugin's stdin and stdout,
// newline-delimited. Writes are queued, so senders never wait on the Plugin.
type conn struct {
	w      io.Writer
	redact func(raw []byte) []byte
	tap    func(out bool, raw []byte)
	notify func(method string, params json.RawMessage)

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan message
	queue   [][]byte
	err     error

	wake chan struct{}
	done chan struct{}
}

// newConn starts reading r and writing w. redact rewrites each message read
// first, so nothing sees what it masks. tap, when not nil, sees each
// message as it's sent or read, before anything acts on it. notify receives
// the Plugin's notifications on the reader's goroutine; closed is called
// once with why the session ended, after every pending call has failed with it.
func newConn(r io.Reader, w io.Writer, redact func([]byte) []byte, tap func(bool, []byte), notify func(string, json.RawMessage), closed func(error)) *conn {
	c := &conn{w: w, redact: redact, tap: tap, notify: notify, pending: map[int64]chan message{}, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go c.write()
	go func() { closed(c.read(r)) }()
	return c
}

func (c *conn) read(r io.Reader) error {
	dec := json.NewDecoder(r)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				err = io.EOF
			} else {
				err = fmt.Errorf("sent a malformed message: %w", err)
			}
			return c.fail(err)
		}
		raw = c.redact(raw)
		if c.tap != nil {
			c.tap(false, raw)
		}
		var msg message
		if err := json.Unmarshal(raw, &msg); err != nil || msg.JSONRPC != "2.0" {
			if err == nil {
				err = errors.New(`"jsonrpc" must be "2.0"`)
			}
			return c.fail(fmt.Errorf("sent a malformed message: %w", err))
		}
		switch {
		case msg.Method != "" && msg.ID != nil:
			c.send(message{ID: msg.ID, Error: &RPCError{Code: -32601, Message: "visor answers no requests"}})
		case msg.Method != "":
			c.notify(msg.Method, msg.Params)
		case msg.ID != nil:
			c.mu.Lock()
			ch := c.pending[*msg.ID]
			delete(c.pending, *msg.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
		}
	}
}

// fail ends the session with err: pending and later calls fail with it.
func (c *conn) fail(err error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.err = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	close(c.done)
	return err
}

func (c *conn) write() {
	for {
		select {
		case <-c.wake:
		case <-c.done:
			return
		}
		c.mu.Lock()
		out := c.queue
		c.queue = nil
		c.mu.Unlock()
		for _, b := range out {
			if _, err := c.w.Write(b); err != nil {
				c.fail(fmt.Errorf("stopped reading: %w", err))
				return
			}
		}
	}
}

func (c *conn) send(msg message) error {
	msg.JSONRPC = "2.0"
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	if c.tap != nil {
		c.tap(true, buf.Bytes())
	}
	c.queue = append(c.queue, buf.Bytes())
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}

func (c *conn) Notify(method string, params any) error {
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(message{Method: method, Params: b})
}

func (c *conn) Call(ctx context.Context, method string, params, result any) error {
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan message, 1)
	c.mu.Lock()
	if c.err != nil {
		defer c.mu.Unlock()
		return c.err
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.send(message{ID: &id, Method: method, Params: b}); err != nil {
		return err
	}
	select {
	case msg, ok := <-ch:
		if !ok {
			return c.closedErr()
		}
		if msg.Error != nil {
			return msg.Error
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(msg.Result, result); err != nil {
			return fmt.Errorf("answered %s badly: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("didn't answer %s: %w", method, ctx.Err())
	}
}

func (c *conn) closedErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *conn) Close() { c.fail(errClosed) }

var errClosed = errors.New("closed by visor")
