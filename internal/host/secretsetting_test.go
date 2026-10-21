package host_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/paths"
	"github.com/oronbz/visor/internal/plugin"
	"github.com/oronbz/visor/internal/plugin/plugintest"
	"github.com/oronbz/visor/internal/widget"
)

const twoFixtures = "[[widget]]\ntype = \"fixture\"\nid = \"a\"\n\n[[widget]]\ntype = \"fixture\"\nid = \"b\"\n"

// sessionLog is where the fixture records every message it reads and writes.
type sessionLog string

// installed starts visor with the fixture installed from source, logging its
// session, and keyring standing in for the OS keyring.
func installed(t *testing.T, source string, k *hosttest.Keyring, config string) (*hosttest.Host, sessionLog) {
	t.Helper()
	return installedWith(t, plugintest.Options{}, source, k, config)
}

// installedWith is installed with the fixture's manifest written from o.
func installedWith(t *testing.T, o plugintest.Options, source string, k *hosttest.Keyring, config string) (*hosttest.Host, sessionLog) {
	t.Helper()
	root := t.TempDir()
	log := filepath.Join(t.TempDir(), "session.jsonl")
	o.Args = append(o.Args, "-log", log)
	dir := plugintest.Write(t, filepath.Join(paths.Under(root).Data, "plugins", "fixture"), o)
	if err := (plugin.Record{Source: source, Commit: "0123abc"}).Write(dir); err != nil {
		t.Fatal(err)
	}
	return hosttest.New(t, hosttest.Options{Root: root, Width: 100, Height: 12, Types: []widget.Type{}, Config: config, Keyring: k}), sessionLog(log)
}

// received are the params of every message with method the fixture read.
func (l sessionLog) received(t *testing.T, method string) []map[string]any {
	t.Helper()
	b, _ := os.ReadFile(string(l))
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		var msg struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
			Result any            `json:"result"`
		}
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Method == method && msg.Result == nil {
			out = append(out, msg.Params)
		}
	}
	return out
}

// opened is the settings of the last widget/open each Widget got.
func (l sessionLog) opened(t *testing.T) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, p := range l.received(t, "widget/open") {
		settings, _ := p["settings"].(map[string]any)
		out[p["widget"].(string)] = settings
	}
	return out
}

func setToken(h *hosttest.Host, value string) {
	h.Press("ctrl+k")
	h.Type("set token")
	h.Press("enter")
	h.Type(value)
	h.Press("enter")
}

func TestSettingASecretKeepsItForThePluginAndReopensEveryWidgetOfItWithIt(t *testing.T) {
	h, log := installed(t, "octo/fixture", &hosttest.Keyring{}, twoFixtures)
	shows(t, h, "hello alpha")
	hides(t, h, "signed in")
	for id, s := range log.opened(t) {
		if _, ok := s["token"]; ok {
			t.Errorf("Widget %s opened with a token before one was set: %v", id, s)
		}
	}
	before := configText(t, h)

	setToken(h, secret)
	if got := h.Opener.Opened; len(got) != 1 || got[0] != "https://example.com/tokens/new" {
		t.Errorf("opened %v, want the setting's target", got)
	}
	h.ReceiveUntil(func() bool { return strings.Count(h.Screen(), "hello alpha (signed in)") == 2 })
	shows(t, h, "Saved the token to the keyring")
	noSecret(t, h, "after setting it")
	for _, id := range []string{"a", "b"} {
		if got := log.opened(t)[id]["token"]; got != secret {
			t.Errorf("Widget %s last opened with token %v, want the secret", id, got)
		}
	}
	if inv := log.received(t, "action/invoke"); len(inv) != 0 {
		t.Errorf("the Plugin was asked to invoke %v", inv)
	}
	if after := configText(t, h); after != before {
		t.Errorf("config.toml changed:\n%s", after)
	}
}

func TestASecretTheKeyringCantKeepIsAnErrorAndGoesNowhereElse(t *testing.T) {
	h, log := installed(t, "octo/fixture", &hosttest.Keyring{Err: errors.New("no Secret Service")}, fixtureWidget)
	shows(t, h, "hello alpha")
	before, opens := configText(t, h), len(log.received(t, "widget/open"))

	setToken(h, secret)
	shows(t, h, "Set token failed: couldn't keep the token in the keyring: no Secret Service")
	hides(t, h, "signed in")
	noSecret(t, h, "after the keyring failed")
	if got := log.received(t, "widget/open"); len(got) != opens {
		t.Errorf("the Plugin's Widgets were reopened: %v", got)
	}
	if inv := log.received(t, "action/invoke"); len(inv) != 0 {
		t.Errorf("the Plugin was asked to invoke %v", inv)
	}
	if after := configText(t, h); after != before {
		t.Errorf("config.toml changed:\n%s", after)
	}
	if b, _ := os.ReadFile(string(log)); bytes.Contains(b, []byte(secret)) {
		t.Errorf("the secret reached the Plugin:\n%s", b)
	}
	err := filepath.WalkDir(h.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if b, _ := os.ReadFile(path); bytes.Contains(b, []byte(secret)) {
			t.Errorf("the secret was written to %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestABlankSecretIsntKept(t *testing.T) {
	h, _ := installed(t, "octo/fixture", &hosttest.Keyring{}, fixtureWidget)
	setToken(h, "   ")
	shows(t, h, "Set token failed: no token entered")
	hides(t, h, "signed in")
}

func TestASecretInConfigIsAConfigErrorNamingIt(t *testing.T) {
	h, log := installed(t, "octo/fixture", &hosttest.Keyring{}, fixtureWidget+"token = \""+secret+"\"\n")
	h.Resize(140, 12)
	shows(t, h, "needs setup", "config.toml:3: `token` in Widget `fixture` is a secret, kept in the keyring: set it with the Widget's \"Set token\" Action")
	noSecret(t, h, "with it in config.toml")
	if opened := log.received(t, "widget/open"); len(opened) != 0 {
		t.Errorf("the Plugin opened %v", opened)
	}
}

func TestASecretGoesOnlyToThePluginFromTheSourceItWasSetFor(t *testing.T) {
	k := &hosttest.Keyring{}
	h, _ := installed(t, "octo/fixture", k, fixtureWidget)
	setToken(h, secret)
	shows(t, h, "hello alpha (signed in)")

	again, _ := installed(t, "octo/fixture", k, fixtureWidget)
	shows(t, again, "hello alpha (signed in)")

	other, log := installed(t, "someone/fixture", k, fixtureWidget)
	hides(t, other, "signed in")
	opened, ok := log.opened(t)["fixture"]
	if !ok {
		t.Fatal("the Plugin from elsewhere never opened its Widget")
	}
	if got, ok := opened["token"]; ok {
		t.Errorf("a Plugin of the same name from elsewhere opened with token %v", got)
	}

	root := t.TempDir()
	plugintest.Write(t, filepath.Join(paths.Under(root).Config, "dev", "fixture"), plugintest.Options{})
	local := hosttest.New(t, hosttest.Options{Root: root, Width: 100, Height: 12, Types: []widget.Type{}, Keyring: k,
		Config: "[[plugin]]\npath = \"dev/fixture\"\n\n" + fixtureWidget})
	shows(t, local, "hello alpha")
	hides(t, local, "signed in")
	setToken(local, "local-token")
	shows(t, local, "hello alpha (signed in)")
	_, log = installed(t, "octo/fixture", k, fixtureWidget)
	if got := log.opened(t)["fixture"]["token"]; got != secret {
		t.Errorf("after the local Plugin set its own, the installed one opened with token %v", got)
	}
}

func TestAMissingRequiredSecretNeedsSetupUntilItsSetWithoutRefreshingThePlugin(t *testing.T) {
	h, log := installedWith(t, plugintest.Options{TokenRequired: true}, "octo/fixture", &hosttest.Keyring{}, fixtureWidget)
	shows(t, h, "needs setup", "No token set", "Set token")
	hides(t, h, "hello alpha")
	if got := log.received(t, "widget/refresh"); len(got) != 0 {
		t.Errorf("the Plugin was refreshed with no token: %v", got)
	}

	setToken(h, secret)
	h.ReceiveUntil(func() bool { return strings.Contains(h.Screen(), "hello alpha (signed in)") })
	hides(t, h, "needs setup")
	noSecret(t, h, "after setting it")
}

func TestAPluginsSetupNamingASecretOffersTheHostsActionForIt(t *testing.T) {
	h, log := installed(t, "octo/fixture", &hosttest.Keyring{}, fixtureWidget+"mode = \"rejected\"\n")
	shows(t, h, "needs setup", "Fixture rejected the token", "⏎ Set token")

	setToken(h, secret)
	h.ReceiveUntil(func() bool { return strings.Contains(h.Screen(), "Saved the token to the keyring") })
	noSecret(t, h, "after setting it")
	if got := log.opened(t)["fixture"]["token"]; got != secret {
		t.Errorf("the Widget last opened with token %v, want the secret", got)
	}
	if inv := log.received(t, "action/invoke"); len(inv) != 0 {
		t.Errorf("the Plugin was asked to invoke %v", inv)
	}
}

func TestAPluginsSetupNamingASecretItDoesntDeclareIsAnError(t *testing.T) {
	h, _ := installed(t, "octo/fixture", &hosttest.Keyring{}, fixtureWidget+"mode = \"unknown-secret\"\n")
	h.Resize(140, 12)
	shows(t, h, "Plugin `fixture` sent setup naming Action `secret:password`, but Widget type `fixture` has no secret setting `password`")
	hides(t, h, "needs setup")
}

func TestAPluginsOwnSecretPrefixedActionIsRejected(t *testing.T) {
	h, _ := installed(t, "octo/fixture", &hosttest.Keyring{}, fixtureWidget+"mode = \"own-secret\"\n")
	h.Resize(140, 12)
	shows(t, h, "Plugin `fixture` sent Action `secret:token`, but ids starting `secret:` are visor's own")
	hides(t, h, "my way")
}

const leaking = fixtureWidget + "mode = \"leak\"\n"

func TestASecretSetWhileThePluginRunsIsMaskedInAllItSendsBack(t *testing.T) {
	h, _ := installed(t, "octo/fixture", &hosttest.Keyring{}, leaking)
	shows(t, h, "token")
	setToken(h, secret)
	h.ReceiveUntil(func() bool { return strings.Contains(h.Screen(), "token ••••") })
	noSecret(t, h, "in the payload")

	runFromPalette(t, h, "leak toast")
	shows(t, h, "signed in with ••••")
	noSecret(t, h, "in a toast")

	runFromPalette(t, h, "leak error")
	shows(t, h, "refused ••••")
	noSecret(t, h, "in an error")

	runFromPalette(t, h, "leak crash")
	h.ReceiveUntil(func() bool { return strings.Contains(h.Screen(), "stopped:") })
	shows(t, h, "bad token ••••")
	noSecret(t, h, "in the last stderr line")
}

func TestASecretStoredBeforeThePluginStartsIsMaskedInAllItSendsBack(t *testing.T) {
	k := &hosttest.Keyring{}
	first, _ := installed(t, "octo/fixture", k, fixtureWidget)
	setToken(first, secret)
	shows(t, first, "hello alpha (signed in)")

	h, _ := installed(t, "octo/fixture", k, leaking)
	shows(t, h, "token ••••")
	noSecret(t, h, "in the payload")
	runFromPalette(t, h, "leak crash")
	h.ReceiveUntil(func() bool { return strings.Contains(h.Screen(), "stopped:") })
	shows(t, h, "bad token ••••")
	noSecret(t, h, "in the last stderr line")

	status, _ := installed(t, "octo/fixture", k, fixtureWidget+"mode = \"leak-status\"\n")
	shows(t, status, "upstream refused ••••")
	noSecret(t, status, "in a Status message")
}
