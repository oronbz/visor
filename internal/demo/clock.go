package demo

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/ports"
)

// morning is the time of day the demo clock starts at, whatever the hour
// the demo is run, so a recording late at night still shows a workday.
const morning = 11*time.Hour + 20*time.Minute

// shift is how far the demo clock runs ahead of (or behind) real time for
// now to read morning on the same date.
func shift(now time.Time) time.Duration {
	return midnight(now).Add(morning).Sub(now)
}

// clock is a real clock shifted by a fixed offset: it ticks in real time.
type clock struct {
	base   ports.Clock
	offset time.Duration
}

func (c clock) Now() time.Time { return c.base.Now().Add(c.offset) }

func (c clock) After(d time.Duration, msg func(time.Time) tea.Msg) tea.Cmd {
	return c.base.After(d, func(t time.Time) tea.Msg { return msg(t.Add(c.offset)) })
}
