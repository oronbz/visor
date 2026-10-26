package host_test

import (
	"runtime"
	"slices"
	"testing"

	"github.com/oronbz/visor/internal/hosttest"
)

func TestMeetingsIsBuiltInOnMacOSOnly(t *testing.T) {
	mac := runtime.GOOS == "darwin"
	h := hosttest.New(t, hosttest.Options{})
	if _, _, ok := h.Find("─ Meetings "); ok != mac {
		t.Errorf("Meetings in the default dashboard = %v on %s:\n%s", ok, runtime.GOOS, h.Screen())
	}

	h.Press("ctrl+k")
	h.Type("add meetings")
	if got := slices.Contains(paletteEntries(h), "visor add Meetings Widget"); got != mac {
		t.Errorf("Add Widget offers Meetings = %v on %s: %q", got, runtime.GOOS, paletteEntries(h))
	}
}

func TestRemindersIsBuiltInOnMacOSOnly(t *testing.T) {
	mac := runtime.GOOS == "darwin"
	h := hosttest.New(t, hosttest.Options{})
	if _, _, ok := h.Find("─ Reminders "); ok != mac {
		t.Errorf("Reminders in the default dashboard = %v on %s:\n%s", ok, runtime.GOOS, h.Screen())
	}

	h.Press("ctrl+k")
	h.Type("add reminders")
	if got := slices.Contains(paletteEntries(h), "visor add Reminders Widget"); got != mac {
		t.Errorf("Add Widget offers Reminders = %v on %s: %q", got, runtime.GOOS, paletteEntries(h))
	}
}
