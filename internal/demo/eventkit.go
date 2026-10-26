package demo

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/oronbz/visor/internal/ports"
)

// eventKit is a calendar and a Reminders store with access granted.
type eventKit struct {
	mu     sync.Mutex
	events []ports.Event
	open   []reminder
}

// reminder repeats every repeat days, or not at all when it's zero.
type reminder struct {
	ports.Reminder
	repeat int
}

func newEventKit(now time.Time) *eventKit {
	return &eventKit{events: events(now), open: reminders(now)}
}

func (*eventKit) Status(ports.Entity) ports.Access { return ports.AccessGranted }

func (*eventKit) Request(context.Context, ports.Entity) (ports.Access, error) {
	return ports.AccessGranted, nil
}

func (e *eventKit) Events(_ context.Context, from, to time.Time) ([]ports.Event, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []ports.Event
	for _, ev := range e.events {
		if ev.End.After(from) && ev.Start.Before(to) {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (e *eventKit) Reminders(context.Context) ([]ports.Reminder, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]ports.Reminder, len(e.open))
	for i, r := range e.open {
		out[i] = r.Reminder
	}
	return out, nil
}

// Complete removes the reminder, or moves a repeating one to its next due
// date, as Reminders.app does.
func (e *eventKit) Complete(_ context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	i := slices.IndexFunc(e.open, func(r reminder) bool { return r.ID == id })
	switch {
	case i < 0:
		return errors.New("reminder not found")
	case e.open[i].repeat > 0:
		e.open[i].Due = e.open[i].Due.AddDate(0, 0, e.open[i].repeat)
	default:
		e.open = slices.Delete(e.open, i, i+1)
	}
	return nil
}

// keyring is an in-memory keyring.
type keyring struct {
	mu      sync.Mutex
	secrets map[[2]string]string
}

func (k *keyring) Get(service, account string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	s, ok := k.secrets[[2]string{service, account}]
	if !ok {
		return "", ports.ErrSecretNotFound
	}
	return s, nil
}

func (k *keyring) Set(service, account, secret string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.secrets == nil {
		k.secrets = map[[2]string]string{}
	}
	k.secrets[[2]string{service, account}] = secret
	return nil
}

func (k *keyring) Delete(service, account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.secrets[[2]string{service, account}]; !ok {
		return ports.ErrSecretNotFound
	}
	delete(k.secrets, [2]string{service, account})
	return nil
}
