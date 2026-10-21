package plugin

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strings"

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
// never stored is already gone.
func (s Secrets) Forget(m Manifest) error {
	if s.Keyring == nil {
		return nil
	}
	var errs []error
	for _, wt := range m.Widgets {
		for _, st := range wt.Settings {
			if kinds[st.Kind] != widget.Secret {
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

// store keeps value as secret setting st, saying why it couldn't.
func (s Secrets) store(st widget.Setting, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("no %s entered", st.Label())
	}
	if err := s.Set(st.Name, value); err != nil {
		return fmt.Errorf("couldn't keep the %s in the keyring: %w", st.Label(), err)
	}
	return nil
}
