package host_test

import (
	"strings"
	"testing"

	"github.com/oronbz/visor/internal/host"
	"github.com/oronbz/visor/internal/hosttest"
)

func TestQuestionMarkShowsAndClosesTheKeyMap(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 100, Height: 30})
	if _, _, ok := h.Find("Keys"); ok {
		t.Fatalf("key map shown before ?:\n%s", h.Screen())
	}

	h.Press("?")
	for _, b := range host.Bindings() {
		line := b.Help().Key + "  " + b.Help().Desc
		if !strings.Contains(squeeze(h.Screen()), line) {
			t.Errorf("key map lacks %q:\n%s", line, h.Screen())
		}
	}

	h.Press("?")
	if _, _, ok := h.Find("Keys"); ok {
		t.Errorf("? left the key map open:\n%s", h.Screen())
	}
	for _, closer := range []string{"esc", "q"} {
		h.Press("?", closer)
		if _, _, ok := h.Find("Keys"); ok {
			t.Errorf("%s left the key map open:\n%s", closer, h.Screen())
		}
		if h.Quit() {
			t.Fatalf("%s on the key map quit visor", closer)
		}
	}
}

func TestTheKeyMapSwallowsOtherKeys(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 100, Height: 30})
	h.Press("?", "tab", "?")
	if got := focused(t, h, []string{"Clock", "Calendar"}); got != "" {
		t.Errorf("tab behind the key map focused %q", got)
	}
}

func TestNoHostKeyUsesTheHerdrPrefix(t *testing.T) {
	for _, b := range host.Bindings() {
		for _, k := range b.Keys() {
			if k == "ctrl+b" {
				t.Errorf("%q is bound to ctrl+b, the first key of herdr's close sequence", b.Help().Desc)
			}
		}
	}

	h := hosttest.New(t, hosttest.Options{Width: 100, Height: 30})
	before := h.Screen()
	h.Press("ctrl+b")
	if h.Quit() || h.Screen() != before {
		t.Errorf("ctrl+b did something:\n%s", h.Screen())
	}
}

// squeeze collapses runs of spaces to two, so a key and its description read
// as "key  desc" whatever the column alignment.
func squeeze(s string) string {
	for strings.Contains(s, "   ") {
		s = strings.ReplaceAll(s, "   ", "  ")
	}
	return s
}

func TestTheKeyMapNamesThePaletteAsEveryAction(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 100, Height: 40})

	h.Press("?")
	if want := "^K :  every Action, the Widgets' and visor's own"; !strings.Contains(squeeze(h.Screen()), want) {
		t.Errorf("key map lacks %q:\n%s", want, h.Screen())
	}
}
