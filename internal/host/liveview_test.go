package host_test

import (
	"strings"
	"testing"
	"time"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/ports"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
	"github.com/oronbz/visor/internal/widgets/calendar"
	"github.com/oronbz/visor/internal/widgets/clock"
	"github.com/oronbz/visor/internal/widgets/cpu"
)

func TestLargeClockDrawsBlockDigits(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Types: []widget.Type{clock.Type}, Width: 80, Height: 13, Now: sundayAfternoon})

	// "14" in 3×5 block glyphs, each dot two cells wide: rows 0, 2 and 4.
	x, y, ok := h.Find("  ████  ██  ██")
	if !ok {
		t.Fatalf("no block digits:\n%s", h.Screen())
	}
	if got := cells(h.Line(y+2), x, 14); got != "    ██  ██████" {
		t.Errorf("middle row %q, want %q", got, "    ██  ██████")
	}
	if got := cells(h.Line(y+4), x, 14); got != "    ██      ██" {
		t.Errorf("bottom row %q, want %q", got, "    ██      ██")
	}
	if strings.ContainsAny(h.Screen(), "▀▄") {
		t.Errorf("half blocks in a large clock:\n%s", h.Screen())
	}
	if _, dy, ok := h.Find("Sunday 18 October"); !ok || dy <= y+4 {
		t.Errorf("date not below the digits:\n%s", h.Screen())
	}
}

func TestTwelveHourClockShowsPM(t *testing.T) {
	cfg := `time_format = "12h"`
	small := hosttest.New(t, hosttest.Options{Types: []widget.Type{clock.Type}, Config: cfg, Width: 30, Height: 6, Now: sundayAfternoon})
	if _, _, ok := small.Find("2:05:09 PM"); !ok {
		t.Errorf("small 12h clock:\n%s", small.Screen())
	}

	big := hosttest.New(t, hosttest.Options{Types: []widget.Type{clock.Type}, Config: cfg, Width: 50, Height: 9, Now: sundayAfternoon})
	// P then M in half-block glyphs, top row.
	if _, _, ok := big.Find("█▀▄ █▄ ▄█"); !ok {
		t.Errorf("half-block 12h clock has no PM:\n%s", big.Screen())
	}
}

func TestClockWithoutSecondsChangesOnTheMinute(t *testing.T) {
	cfg := "[[widget]]\ntype = \"clock\"\nseconds = false\n"
	h := hosttest.New(t, hosttest.Options{Types: []widget.Type{clock.Type}, Config: cfg, Width: 30, Height: 6, Now: sundayAfternoon})
	h.Advance(50 * time.Second)
	if _, _, ok := h.Find("14:05"); !ok {
		t.Fatalf("before the minute:\n%s", h.Screen())
	}
	h.Advance(time.Second)
	if _, _, ok := h.Find("14:06"); !ok {
		t.Errorf("on the minute:\n%s", h.Screen())
	}
}

func calendarHost(t *testing.T, cfg string, now time.Time) *hosttest.Host {
	t.Helper()
	return hosttest.New(t, hosttest.Options{Types: []widget.Type{calendar.Type}, Config: cfg, Width: 40, Height: 12, Now: now})
}

// highlighted is the day drawn on the accent background, "" if none.
func highlighted(t *testing.T, h *hosttest.Host) string {
	t.Helper()
	for y, line := range strings.Split(h.Screen(), "\n") {
		for x, r := range []rune(line) {
			if r >= '0' && r <= '9' {
				if bg, ok := h.Bg(x, y); ok && bg == theme.Accent {
					return strings.TrimSpace(cells(line, x-1, 3))
				}
			}
		}
	}
	return ""
}

func TestCalendarHighlightsToday(t *testing.T) {
	h := calendarHost(t, "", sundayAfternoon)
	if got := highlighted(t, h); got != "18" {
		t.Errorf("highlighted %q, want 18:\n%s", got, h.Screen())
	}
	x, y, _ := h.Find("18")
	if fg, _ := h.Fg(x, y); fg != theme.Bg {
		t.Errorf("today drawn in %v, want bg on accent", fg)
	}
}

func TestCalendarWeekStartOrdersTheColumns(t *testing.T) {
	sunday := calendarHost(t, `week_start = "sunday"`, sundayAfternoon)
	for _, want := range []string{"Su Mo Tu We Th Fr Sa", " 4  5  6  7  8  9 10"} {
		if _, _, ok := sunday.Find(want); !ok {
			t.Errorf("sunday-first: no %q:\n%s", want, sunday.Screen())
		}
	}

	monday := calendarHost(t, `week_start = "monday"`, sundayAfternoon)
	for _, want := range []string{"Mo Tu We Th Fr Sa Su", " 5  6  7  8  9 10 11"} {
		if _, _, ok := monday.Find(want); !ok {
			t.Errorf("monday-first: no %q:\n%s", want, monday.Screen())
		}
	}
	for day, tok := range map[string]theme.Token{"Fr": theme.Faint, "Sa": theme.Faint, "Su": theme.Dim, "Th": theme.Dim} {
		x, y, _ := monday.Find(day)
		if got, _ := monday.Fg(x, y); got != tok {
			t.Errorf("%s drawn in %v, want %v", day, got, tok)
		}
	}
}

func TestCalendarNAndPChangeTheMonth(t *testing.T) {
	h := calendarHost(t, "", sundayAfternoon)
	h.Press("1", "n")
	if _, _, ok := h.Find("November 2026"); !ok {
		t.Fatalf("n did not move to November:\n%s", h.Screen())
	}
	if got := highlighted(t, h); got != "" {
		t.Errorf("today %q highlighted in another month", got)
	}
	h.Press("p", "p")
	if _, _, ok := h.Find("September 2026"); !ok {
		t.Errorf("p p did not move to September:\n%s", h.Screen())
	}
}

func TestCalendarMovesTodayAtMidnight(t *testing.T) {
	h := calendarHost(t, "", time.Date(2026, 10, 18, 23, 59, 30, 0, time.UTC))
	h.Advance(30 * time.Second)
	if got := highlighted(t, h); got != "19" {
		t.Errorf("after midnight highlighted %q, want 19:\n%s", got, h.Screen())
	}
}

func TestSystemDrawsCPUHistoryAndMemoryBarWithFreeOfTotal(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Types: []widget.Type{cpu.Type}, Width: 40, Height: 10, Now: sundayAfternoon})
	stats := ports.Stats{CPU: 0.5, MemUsed: 35 << 30, MemTotal: 100 << 30}
	h.Machine.Set(stats)
	h.Advance(2 * time.Second)
	h.Advance(2 * time.Second)
	stats.CPU = 1
	h.Machine.Set(stats)
	h.Advance(2 * time.Second)

	// Samples 0 (read at start), 0.5, 0.5, 1.0 over 4 chart rows, newest at the right edge, two
	// samples per cell: the full right column of the last cell is the 1.0.
	chart := []string{" ⢸", " ⢸", "⢸⣿", "⢸⣿"}
	for i, want := range chart {
		if got := cells(h.Line(2+i), 37, 2); got != want {
			t.Errorf("chart row %d ends %q, want %q:\n%s", i, got, want, h.Screen())
		}
		if got := strings.TrimSpace(cells(h.Line(2+i), 1, 36)); got != "" {
			t.Errorf("chart row %d has %q left of the samples", i, got)
		}
	}
	if fg, _ := h.Fg(38, 4); fg != theme.Accent {
		t.Errorf("chart drawn in %v, want accent", fg)
	}

	// Memory in binary units, the way RAM is sold: 65 GiB of 100 left.
	want := map[int]string{
		0: "╭─ System ─",
		1: "│cpu 100%",
		6: "│mem ███████─────────────  35% 65G/100G│",
	}
	for y, w := range want {
		if got := h.Line(y); !strings.HasPrefix(got, w) {
			t.Errorf("line %d %q, want %q", y, got, w)
		}
	}
	for x, tok := range map[int]theme.Token{5: theme.Info, 11: theme.Info, 12: theme.Line} {
		if got, _ := h.Fg(x, 6); got != tok {
			t.Errorf("memory bar cell %d drawn in %v, want %v", x, got, tok)
		}
	}
	if strings.Contains(h.Screen(), "load") {
		t.Errorf("load averages still drawn:\n%s", h.Screen())
	}
}

func TestSystemDrawsTheStartupDiskAsDiskWithFreeOfTotalInDecimalUnits(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Types: []widget.Type{cpu.Type}, Width: 40, Height: 11, Now: sundayAfternoon})
	h.Machine.Set(ports.Stats{MemUsed: 50 << 30, MemTotal: 100 << 30, Volumes: []ports.Volume{
		{Startup: true, Total: 1e12, Free: 250e9},
	}})
	h.Advance(2 * time.Second)

	// Labels and sizes line up, so the bars do too.
	want := map[int]string{
		6: "│mem  █████████─────────  50%  50G/100G│",
		7: "│disk █████████████▌────  75% 250G/1.0T│",
	}
	for y, w := range want {
		if got := h.Line(y); got != w {
			t.Errorf("line %d %q, want %q\n%s", y, got, w, h.Screen())
		}
	}
	if got, _ := h.Fg(6, 7); got != theme.Alt {
		t.Errorf("disk bar drawn in %v, want alt", got)
	}
}

func TestSystemListsEachMountedVolumeByNameColouredByHowFullItIs(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Types: []widget.Type{cpu.Type}, Width: 40, Height: 14, Now: sundayAfternoon})
	stats := ports.Stats{MemUsed: 50 << 30, MemTotal: 100 << 30, Volumes: []ports.Volume{
		{Startup: true, Total: 1e12, Free: 500e9},
		{Name: "Backup", Total: 2e12, Free: 300e9},
		{Name: "Música", Total: 100e9, Free: 500e6},
		{Name: "A Very Long Volume", Total: 64e9, Free: 9.4e9},
	}}
	h.Machine.Set(stats)
	h.Advance(2 * time.Second)

	// Labels pad by cells, not bytes, and a long name is cut to ten.
	for i, r := range []struct {
		label, size string
		tok         theme.Token
	}{
		{"disk       ", "500G/1.0T", theme.Alt},  // 50% used
		{"Backup     ", "300G/2.0T", theme.Warn}, // 85%
		{"Música     ", "500M/100G", theme.Bad},  // 99.5%
		{"A Very Lo… ", " 9.4G/64G", theme.Warn}, // 85%
	} {
		y := 7 + i
		line := h.Line(y)
		if got := cells(line, 1, 11); got != r.label {
			t.Errorf("row %d label %q, want %q\n%s", i, got, r.label, h.Screen())
		}
		if !strings.HasSuffix(line, " "+r.size+"│") {
			t.Errorf("row %d %q doesn't end with size %q", i, line, r.size)
		}
		if got, _ := h.Fg(12, y); got != r.tok {
			t.Errorf("%s bar drawn in %v, want %v", r.label, got, r.tok)
		}
	}

	// Without the startup disk, say when / can't be read, no volume poses as it.
	stats.Volumes = stats.Volumes[1:2]
	h.Machine.Set(stats)
	h.Advance(2 * time.Second)
	if got := cells(h.Line(10), 1, 7); got != "Backup " {
		t.Errorf("row %q, want Backup by its name\n%s", h.Line(10), h.Screen())
	}
	if strings.Contains(h.Screen(), "disk") || strings.Contains(h.Screen(), "Música") {
		t.Errorf("a volume that's gone is still drawn:\n%s", h.Screen())
	}
}

func TestSystemDrawsDownloadAndUploadRatesSinceThePreviousRead(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Types: []widget.Type{cpu.Type}, Width: 40, Height: 11, Now: sundayAfternoon})
	stats := ports.Stats{MemUsed: 50 << 30, MemTotal: 100 << 30, Volumes: []ports.Volume{{Startup: true, Total: 1e12, Free: 500e9}}}
	net := func() string { return strings.TrimRight(strings.Trim(h.Line(8), "│"), " ") }

	// The read at start, before the fake has volumes, has nothing to compare with.
	if got, want := net(), "net ↓ –  ↑ –"; got != want {
		t.Errorf("first read %q, want %q\n%s", got, want, h.Screen())
	}
	en0 := func(recv, sent uint64) ports.Interface { return ports.Interface{Name: "en0", Recv: recv, Sent: sent} }
	for _, step := range []struct {
		name       string
		interfaces []ports.Interface
		want       string
	}{
		{"en0 first seen", []ports.Interface{en0(0, 0)}, "net  ↓ –  ↑ –"},
		{"megabytes and kilobytes", []ports.Interface{en0(4.2e6, 280e3)}, "net  ↓ 2.1 MB/s  ↑ 140 KB/s"},
		{"bytes", []ports.Interface{en0(4.2e6, 281e3)}, "net  ↓ 0 B/s  ↑ 500 B/s"},
		// A USB Ethernet plugged in brings everything it moved since it came up.
		{"gigabytes, en5 just plugged in", []ports.Interface{en0(3.0042e9, 281e3), {Name: "en5", Recv: 50e9, Sent: 50e9}},
			"net  ↓ 1.5 GB/s  ↑ 0 B/s"},
		// 999.5 KB/s rounds to 1000, which is shown as the next unit up.
		{"both interfaces, rounding up a unit", []ports.Interface{en0(3.0061990e9, 281e3), {Name: "en5", Recv: 50e9, Sent: 50e9}},
			"net  ↓ 1.0 MB/s  ↑ 0 B/s"},
		{"read failed", nil, "net  ↓ –  ↑ –"},
		{"en0 back", []ports.Interface{en0(3.0062e9, 281e3)}, "net  ↓ –  ↑ –"},
		// Counters that went back, such as after the interface restarted, give no rate.
		{"en0 counters went back", []ports.Interface{en0(1e6, 281e3)}, "net  ↓ –  ↑ –"},
	} {
		stats.Interfaces = step.interfaces
		h.Machine.Set(stats)
		h.Advance(2 * time.Second)
		if got := net(); got != step.want {
			t.Errorf("%s: %q, want %q", step.name, got, step.want)
		}
	}
	for x, tok := range map[int]theme.Token{6: theme.Ok, 11: theme.Info} {
		if got, _ := h.Fg(x, 8); got != tok {
			t.Errorf("arrow at %d drawn in %v, want %v", x, got, tok)
		}
	}
}

// cells is n cells of line from x, padded with spaces.
func cells(line string, x, n int) string {
	r := []rune(line + strings.Repeat(" ", x+n))
	return string(r[x : x+n])
}
