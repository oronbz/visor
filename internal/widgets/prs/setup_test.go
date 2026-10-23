package prs_test

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/hosttest"
)

const (
	tokenPage = "https://github.com/settings/tokens/new?scopes=repo,read:org&description=visor"
	pasteHint = "Run `gh auth login`, or press Enter to paste a classic token"
	classic   = "ghp_pAsTeDcLaSsIc0123456789abcdefABCDEF"
)

// stored is the token visor keeps in the keyring, or "".
func stored(t *testing.T, h *hosttest.Host) string {
	t.Helper()
	s, err := h.Keyring.Get("visor", "github:github.com")
	if err != nil {
		return ""
	}
	return s
}

// leaks lists every place outside the keyring where secret shows up: the
// screen, every file visor wrote, and whatever it handed other programs.
func leaks(t *testing.T, h *hosttest.Host, secret string) []string {
	t.Helper()
	var out []string
	if strings.Contains(h.Screen(), secret) {
		out = append(out, "screen")
	}
	_ = filepath.WalkDir(h.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), secret) {
			out = append(out, path)
		}
		return nil
	})
	for _, c := range h.Commands.Calls {
		if strings.Contains(strings.Join(c, " "), secret) {
			out = append(out, "command line "+strings.Join(c, " "))
		}
	}
	for _, s := range append(append(append([]string{}, h.Opener.Opened...), h.Clipboard.Copied...), h.Editor.Edited...) {
		if strings.Contains(s, secret) {
			out = append(out, "opener/clipboard/editor")
		}
	}
	return out
}

func TestWithNoTokenEnterOpensTheTokenPageAndStoresThePastedToken(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 100, Height: 12, Env: map[string]string{}})
	if !strings.Contains(h.Screen(), pasteHint) {
		t.Fatalf("needs setup lacks %q:\n%s", pasteHint, h.Screen())
	}

	h.Press("1", "enter")
	if got := h.Opener.Opened; len(got) != 1 || got[0] != tokenPage {
		t.Fatalf("opened %v, want %s", got, tokenPage)
	}
	if !strings.Contains(h.Screen(), "INPUT") {
		t.Fatalf("no prompt after enter:\n%s", h.Screen())
	}
	for i, r := range classic {
		h.Type(string(r))
		if typed := classic[:i+1]; len(typed) >= 4 {
			if l := leaks(t, h, typed); len(l) > 0 {
				t.Fatalf("the typed %q leaks to %v:\n%s", typed, l, h.Screen())
			}
		}
	}
	h.Press("enter")

	if got := stored(t, h); got != classic {
		t.Fatalf("keyring holds %q, want the pasted token", got)
	}
	reqs := g.Requests()
	if len(reqs) == 0 || reqs[len(reqs)-1].Auth != "bearer "+classic {
		t.Fatalf("requests %v, want a search with the pasted token", reqs)
	}
	if !strings.Contains(h.Line(0), "Review requests 3") {
		t.Errorf("after pasting the title reads %q", h.Line(0))
	}
	if l := leaks(t, h, classic); len(l) > 0 {
		t.Errorf("the token leaks to %v:\n%s", l, h.Screen())
	}
}

func TestAPastedTokenMayCarryTheNewlineOfTheCopy(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 100, Height: 12, Env: map[string]string{}})
	h.Press("1", "enter")
	h.Send(tea.PasteMsg{Content: classic + "\n"})
	if l := leaks(t, h, classic); len(l) > 0 {
		t.Fatalf("the pasted token leaks to %v:\n%s", l, h.Screen())
	}
	h.Press("enter")
	if got := stored(t, h); got != classic {
		t.Fatalf("keyring holds %q, want the pasted token", got)
	}
}

func TestAFineGrainedTokenIsRefusedAndNotStored(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 100, Height: 12, Env: map[string]string{}})
	pat := "github_pat_11ABCDEFG0123456789_abcdefghijklmnop"
	h.Press("1", "enter")
	h.Type(pat)
	h.Press("enter")

	if got := stored(t, h); got != "" {
		t.Fatalf("keyring holds %q, want nothing stored", got)
	}
	if footer := h.Line(11); !strings.Contains(footer, "Use a classic token with repo and read:org scopes") {
		t.Errorf("footer %q, want the classic-token message", footer)
	}
	if !strings.Contains(h.Screen(), pasteHint) {
		t.Errorf("not back in needs setup:\n%s", h.Screen())
	}
	if l := leaks(t, h, pat); len(l) > 0 {
		t.Errorf("the refused token leaks to %v", l)
	}
	if reqs := g.Requests(); len(reqs) != 0 {
		t.Errorf("GitHub got %v", reqs)
	}
}

func TestTheStoredTokenIsReadAfterGhAuthToken(t *testing.T) {
	kr := &hosttest.Keyring{}
	if err := kr.Set("visor", "github:github.com", classic); err != nil {
		t.Fatal(err)
	}
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 90, Height: 9, Env: map[string]string{}, Keyring: kr})
	if reqs := g.Requests(); len(reqs) != 1 || reqs[0].Auth != "bearer "+classic {
		t.Fatalf("requests %v, want one search with the stored token", reqs)
	}

	h.Commands.Script("gho_fromgh\n", nil, "gh", "auth", "token")
	h.Press("ctrl+r")
	if reqs := g.Requests(); len(reqs) != 2 || reqs[1].Auth != "bearer gho_fromgh" {
		t.Fatalf("requests %v, want gh's token ahead of the stored one", reqs)
	}
}

func TestARejectedStoredTokenReturnsTheWidgetToNeedsSetup(t *testing.T) {
	kr := &hosttest.Keyring{}
	if err := kr.Set("visor", "github:github.com", "ghp_expired"); err != nil {
		t.Fatal(err)
	}
	g := team(t)
	g.reply = func(r request) (int, http.Header, string) {
		if r.Auth == "bearer ghp_expired" {
			return http.StatusUnauthorized, nil, `{"message": "Bad credentials"}`
		}
		return 0, nil, ""
	}
	h := withPRs(t, g, hosttest.Options{Width: 140, Height: 12, Env: map[string]string{}, Keyring: kr})
	screen := h.Screen()
	for _, want := range []string{"needs setup", "rejected", "press Enter to paste a classic token"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}

	h.Press("1", "enter")
	h.Type(classic)
	h.Press("enter")
	if got := stored(t, h); got != classic {
		t.Fatalf("keyring holds %q, want the new token", got)
	}
	if !strings.Contains(h.Line(0), "Review requests 3") {
		t.Errorf("after a new token the title reads %q:\n%s", h.Line(0), h.Screen())
	}
}

func TestWithNoKeyringNeedsSetupSaysToSetGHToken(t *testing.T) {
	g := team(t)
	h := withPRs(t, g, hosttest.Options{Width: 100, Height: 12, Env: map[string]string{},
		Keyring: &hosttest.Keyring{Err: errors.New("no secret service on the session bus")}})
	screen := h.Screen()
	for _, want := range []string{"needs setup", "GH_TOKEN"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "paste") {
		t.Errorf("offers to paste a token it can't store:\n%s", screen)
	}
	h.Press("1", "enter")
	if len(h.Opener.Opened) != 0 || strings.Contains(h.Screen(), "INPUT") {
		t.Errorf("enter started a paste with no keyring: opened %v\n%s", h.Opener.Opened, h.Screen())
	}
}
