// Package cpu is the System Widget type: a braille history of CPU use, bars
// for memory and each volume, and the network rates. The type keeps its `cpu`
// name so existing configs still place it.
package cpu

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

var Type = widget.Type{
	Name:        "cpu",
	Title:       "System",
	Description: "CPU history, memory and disk use for each volume, and network rates.",
	Refresh:     2 * time.Second,
	Floor:       time.Second,
	New: func(c widget.Config) widget.Widget {
		return &meter{machine: c.Ports.Machine}
	},
}

// history is enough samples for a chart across a wide terminal.
const history = 512

type meter struct {
	machine ports.Machine
	mu      sync.Mutex
	samples []float64
	// last is the previous read, which the network rates are since.
	last   ports.Stats
	lastAt time.Time
}

func (m *meter) Refresh(_ context.Context, now time.Time) (widget.Payload, error) {
	s, err := m.machine.Stats()
	if err != nil {
		return widget.Payload{}, err
	}
	m.mu.Lock()
	m.samples = append(m.samples, s.CPU)
	if len(m.samples) > history {
		m.samples = m.samples[len(m.samples)-history:]
	}
	samples := append([]float64(nil), m.samples...)
	last, lastAt := m.last, m.lastAt
	m.last, m.lastAt = s, now
	m.mu.Unlock()

	rs := rows(s)
	width := 0
	for _, r := range rs {
		width = max(width, ansi.StringWidth(r.label))
	}
	lines := []widget.Line{
		{Spans: []widget.Span{
			{Text: "cpu ", Token: theme.Dim},
			{Text: fmt.Sprintf("%.0f%%", s.CPU*100), Token: level(s.CPU), Bold: true},
		}},
		{Chart: &widget.Chart{Values: samples, Token: theme.Accent}},
	}
	lines = append(lines, bars(rs, width)...)
	down, up := rates(last.Interfaces, s.Interfaces, now.Sub(lastAt).Seconds())
	return widget.Payload{Lines: append(lines, widget.Line{Spans: []widget.Span{
		{Text: pad("net", width), Token: theme.Dim},
		{Text: "↓ ", Token: theme.Ok},
		{Text: down, Token: theme.Fg},
		{Text: "  ↑ ", Token: theme.Info},
		{Text: up, Token: theme.Fg},
	}})}, nil
}

type row struct {
	label, size string
	used        float64
	tok         theme.Token
}

// labelMax keeps a long volume name from squeezing every bar.
const labelMax = 10

func rows(s ports.Stats) []row {
	var mem float64
	if s.MemTotal > 0 {
		mem = float64(s.MemUsed) / float64(s.MemTotal)
	}
	// Memory in binary units, the way RAM is sold.
	gib := func(b uint64) string { return fmt.Sprintf("%.0fG", float64(b)/(1<<30)) }
	rs := []row{{"mem", gib(s.MemTotal-s.MemUsed) + "/" + gib(s.MemTotal), mem, theme.Info}}
	units := []string{"", "K", "M", "G", "T"}
	for _, v := range s.Volumes {
		label := ansi.Truncate(v.Name, labelMax, "…")
		if v.Startup {
			label = "disk"
		}
		used := float64(v.Total-v.Free) / float64(v.Total)
		rs = append(rs, row{label, decimal(float64(v.Free), units) + "/" + decimal(float64(v.Total), units), used, fullness(used)})
	}
	return rs
}

// bars pads labels to width and sizes to the widest so the bars line up.
func bars(rs []row, width int) []widget.Line {
	sizes := 0
	for _, r := range rs {
		sizes = max(sizes, len(r.size))
	}
	lines := make([]widget.Line, 0, len(rs))
	for _, r := range rs {
		lines = append(lines, widget.Line{Spans: []widget.Span{
			{Text: pad(r.label, width), Token: theme.Dim},
			{Token: r.tok, Bar: &widget.Bar{Value: r.used}},
			{Text: fmt.Sprintf(" %3.0f%%", r.used*100), Token: theme.Fg},
			{Text: fmt.Sprintf(" %*s", sizes, r.size), Token: theme.Dim},
		}})
	}
	return lines
}

func pad(label string, width int) string {
	return label + strings.Repeat(" ", width-ansi.StringWidth(label)+1)
}

// rates are the download and upload rates over the interfaces in both reads.
// One that just came up brings everything it moved before, and one whose
// counters went back restarted, so neither has a rate yet.
func rates(last, now []ports.Interface, secs float64) (down, up string) {
	before := map[string]ports.Interface{}
	for _, i := range last {
		before[i.Name] = i
	}
	var recv, sent uint64
	var seen bool
	for _, i := range now {
		b, ok := before[i.Name]
		if !ok || i.Recv < b.Recv || i.Sent < b.Sent {
			continue
		}
		recv, sent, seen = recv+i.Recv-b.Recv, sent+i.Sent-b.Sent, true
	}
	if !seen || secs <= 0 {
		return "–", "–"
	}
	units := []string{" B/s", " KB/s", " MB/s", " GB/s"}
	return decimal(float64(recv)/secs, units), decimal(float64(sent)/secs, units)
}

// decimal is n in the largest of units, steps of 1000, that keeps it under
// 1000 once rounded, with a decimal place under 10, as Finder and Activity
// Monitor show sizes. The first unit is whole bytes, never fractional.
func decimal(n float64, units []string) string {
	i := 0
	for ; i < len(units)-1 && n >= 999.5; i++ {
		n /= 1000
	}
	if i > 0 && n < 9.95 {
		return fmt.Sprintf("%.1f%s", n, units[i])
	}
	return fmt.Sprintf("%.0f%s", n, units[i])
}

func level(cpu float64) theme.Token {
	switch {
	case cpu > 0.85:
		return theme.Bad
	case cpu > 0.65:
		return theme.Warn
	}
	return theme.Fg
}

func fullness(used float64) theme.Token {
	switch {
	case used > 0.9:
		return theme.Bad
	case used > 0.8:
		return theme.Warn
	}
	return theme.Alt
}
