package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"

	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/widget"
)

const keyringService = "visor"

// Identity is who a Plugin is to its secrets: its install source, or else
// the absolute folder it's loaded from. Never its manifest name, so a local
// Plugin calling itself after an installed one doesn't get that one's secrets.
func Identity(dir string, rec Record) string {
	if rec.Source != "" {
		return rec.Source
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// Secrets are the values of one Plugin's secret settings, kept in the
// keyring under its Identity and shared by all its Widgets.
type Secrets struct {
	Keyring  ports.Keyring
	Identity string
}

func (s Secrets) account(name string) string { return "plugin:" + s.Identity + ":" + name }

// Get is the stored value of secret setting name; ok is false when there's
// none or no keyring to read.
func (s Secrets) Get(name string) (value string, ok bool) {
	if s.Keyring == nil {
		return "", false
	}
	v, err := s.Keyring.Get(keyringService, s.account(name))
	return v, err == nil
}

func (s Secrets) Set(name, value string) error {
	if s.Keyring == nil {
		return errors.New("no keyring")
	}
	return s.Keyring.Set(keyringService, s.account(name), value)
}

// Forget deletes the stored value of every secret setting m declares; one
// it can't read, never stored or with no keyring to hold it, is already gone.
func (s Secrets) Forget(m Manifest) error {
	var errs []error
	for _, wt := range m.Widgets {
		for _, st := range wt.Settings {
			if kinds[st.Kind] != widget.Secret {
				continue
			}
			if _, ok := s.Get(st.Name); !ok {
				continue
			}
			if err := s.Keyring.Delete(keyringService, s.account(st.Name)); err != nil && !errors.Is(err, ports.ErrSecretNotFound) {
				errs = append(errs, fmt.Errorf("couldn't delete its %s from the keyring: %w", st.Name, err))
			}
		}
	}
	return errors.Join(errs...)
}

// withSecrets is settings plus the stored value of each secret in schema.
func (s Secrets) withSecrets(settings map[string]any, schema []widget.Setting) map[string]any {
	out := make(map[string]any, len(settings))
	maps.Copy(out, settings)
	for _, st := range schema {
		if st.Kind != widget.Secret {
			continue
		}
		if v, ok := s.Get(st.Name); ok {
			out[st.Name] = v
		}
	}
	return out
}

// stored are the values the keyring holds for m's secret settings.
func (s Secrets) stored(m Manifest) []string {
	var values []string
	for _, wt := range m.Widgets {
		for _, st := range wt.Settings {
			if kinds[st.Kind] != widget.Secret {
				continue
			}
			if v, ok := s.Get(st.Name); ok {
				values = append(values, v)
			}
		}
	}
	return values
}

// store keeps value as secret setting st, saying why it couldn't.
func (s Secrets) store(st widget.Setting, value string) error {
	if value == "" {
		return fmt.Errorf("no %s entered", st.Label())
	}
	if err := s.Set(st.Name, value); err != nil {
		return fmt.Errorf("couldn't keep the %s in the keyring: %w", st.Label(), err)
	}
	return nil
}

// redactor masks a Plugin's stored secrets in all it sends visor, so one it
// echoes is never drawn or logged.
type redactor struct {
	mu     sync.Mutex
	values []string
}

func (r *redactor) add(values ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range values {
		if v != "" && !slices.Contains(r.values, v) {
			r.values = append(r.values, v)
		}
	}
}

func (r *redactor) secrets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.values)
}

func (r *redactor) string(s string) string { return widget.Redact(s, r.secrets()...) }

// message is raw with every secret masked in its strings, still valid JSON.
func (r *redactor) message(raw []byte) []byte {
	secrets := r.secrets()
	if !slices.ContainsFunc(secrets, func(s string) bool { return bytes.Contains(raw, []byte(s)) || bytes.Contains(raw, quoted(s)) }) {
		return raw
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return raw
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(redacted(v, secrets)); err != nil {
		return raw
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// quoted is s as a JSON string holds it, quotes aside.
func quoted(s string) []byte {
	b, _ := json.Marshal(s)
	return b[1 : len(b)-1]
}

func redacted(v any, secrets []string) any {
	switch v := v.(type) {
	case string:
		return widget.Redact(v, secrets...)
	case []any:
		for i, x := range v {
			v[i] = redacted(x, secrets)
		}
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[widget.Redact(k, secrets...)] = redacted(x, secrets)
		}
		return out
	}
	return v
}
