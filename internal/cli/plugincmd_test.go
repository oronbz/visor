package cli_test

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/oronbz/visor/internal/cli"
	"github.com/oronbz/visor/internal/plugin/plugintest"
)

// compile compiles the JSON Schema document src and returns the schema at ref within it.
func compile(t *testing.T, src []byte, ref string) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("not JSON: %v\n%s", err, src)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("https://example.com/printed.json", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("https://example.com/printed.json" + ref)
	if err != nil {
		t.Fatalf("not a JSON Schema: %v", err)
	}
	return sch
}

func instance(t *testing.T, src string) any {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPluginSchemaPrintsTheManifestAndProtocolSchemas(t *testing.T) {
	s, _, stdout, stderr := sandbox(t, nil, "", "plugin", "schema")
	if code := cli.Run(s); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	manifest := compile(t, stdout.Bytes(), "#/$defs/manifest")
	protocol := compile(t, stdout.Bytes(), "#/$defs/protocol")

	if err := manifest.Validate(instance(t, `{"name": "demo", "protocol": 1, "command": "./demo", "widget": [{"type": "demo", "title": "Demo", "description": "A demo"}]}`)); err != nil {
		t.Errorf("a good manifest: %v", err)
	}
	if err := manifest.Validate(instance(t, `{"name": "demo", "protocol": 1}`)); err == nil {
		t.Error("a manifest without a command passed")
	}
	for _, msg := range []string{
		`{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocol": 1}}`,
		`{"jsonrpc": "2.0", "method": "widget/open", "params": {"widget": "w", "type": "demo", "settings": {}, "size": {"width": 10, "height": 3}, "focused": false, "visible": true}}`,
		`{"jsonrpc": "2.0", "method": "widget/update", "params": {"widget": "w", "payload": {"lines": [{"spans": [{"text": "hi", "token": "accent"}]}]}}}`,
		`{"jsonrpc": "2.0", "id": 2, "method": "action/invoke", "params": {"widget": "w", "action": "done"}}`,
	} {
		if err := protocol.Validate(instance(t, msg)); err != nil {
			t.Errorf("%s: %v", msg, err)
		}
	}
	if err := protocol.Validate(instance(t, `{"jsonrpc": "2.0", "method": "widget/update", "params": {"widget": "w", "payload": {"lines": [{"spans": [{"text": "hi", "token": "#ff0000"}]}]}}}`)); err == nil {
		t.Error("a raw colour passed")
	}
}

func TestPluginCheckPassesAConformingPlugin(t *testing.T) {
	s, home, stdout, stderr := sandbox(t, nil, "")
	dir := plugintest.Write(t, filepath.Join(home, "fixture"), plugintest.Options{})
	s.Args = []string{"plugin", "check", dir}

	if code := cli.Run(s); code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, step := range []string{"manifest", "start", "handshake", "open fixture", "refresh fixture", "action fixture", "close fixture", "quit"} {
		if !regexp.MustCompile(`(?m)^ok +` + step + `\b`).MatchString(stdout.String()) {
			t.Errorf("no passing %q step:\n%s", step, stdout)
		}
	}
	if !strings.HasSuffix(stdout.String(), "PASS\n") {
		t.Errorf("no PASS at the end:\n%s", stdout)
	}
}

func TestPluginCheckRunsEveryWidgetWithNoSecretsSet(t *testing.T) {
	for name, c := range map[string]struct {
		o    plugintest.Options
		want []string
	}{
		"required":       {plugintest.Options{TokenRequired: true}, []string{"`fixture`, Widget types: fixture; secret settings: token (required)", "refresh fixture  needs setup: No token set (⏎ Set token)"}},
		"plugin's setup": {plugintest.Options{Mode: "rejected"}, []string{"`fixture`, Widget types: fixture; secret settings: token\n", "refresh fixture  needs setup: Fixture rejected the token (⏎ Set token)"}},
	} {
		t.Run(name, func(t *testing.T) {
			s, home, stdout, stderr := sandbox(t, nil, "")
			dir := plugintest.Write(t, filepath.Join(home, "fixture"), c.o)
			s.Args = []string{"plugin", "check", dir}

			if code := cli.Run(s); code != 0 {
				t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			for _, want := range c.want {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("no %q:\n%s", want, stdout)
				}
			}
		})
	}
}

func TestPluginCheckFailsTheStepThatBreaksTheProtocol(t *testing.T) {
	for name, c := range map[string]struct {
		o        plugintest.Options
		manifest string
		step     string
		want     []string
		// timeout is how long each step waits; only a Plugin that never
		// answers needs it short, so a slow machine can't fail earlier steps.
		timeout string
	}{
		"invalid manifest":      {manifest: "name = \"broken\"\nprotocol = 1\n", step: "manifest", want: []string{"visor-plugin.toml: missing properties 'command', 'widget'"}},
		"newer protocol":        {o: plugintest.Options{Protocol: 2}, step: "manifest", want: []string{"declares protocol 2, but this visor speaks 1"}},
		"no handshake":          {o: plugintest.Options{Args: []string{"-handshake", "silent"}}, step: "handshake", want: []string{"failed its handshake: didn't answer initialize"}, timeout: "300ms"},
		"empty handshake":       {o: plugintest.Options{Args: []string{"-handshake", "empty"}}, step: "handshake", want: []string{"speaks protocol 0", "answer to initialize breaks the schema: result: missing property 'protocol'"}},
		"malformed":             {o: plugintest.Options{Mode: "malformed"}, step: "refresh fixture", want: []string{"sent a malformed message"}},
		"raw colours":           {o: plugintest.Options{Mode: "raw-colour"}, step: "refresh fixture", want: []string{`"#ff0000" is not a theme token`, "answer to widget/refresh breaks the schema: result: payload.lines.0.spans.0.token"}},
		"unknown secret":        {o: plugintest.Options{Mode: "unknown-secret"}, step: "refresh fixture", want: []string{"sent setup naming Action `secret:password`, but Widget type `fixture` has no secret setting `password`"}},
		"own secret Action":     {o: plugintest.Options{Mode: "own-secret"}, step: "refresh fixture", want: []string{"sent Action `secret:token`, but ids starting `secret:` are visor's own", "answer to widget/refresh breaks the schema: result: payload.actions.0.id"}},
		"secret with a default": {manifest: "name = \"broken\"\nprotocol = 1\ncommand = \"x\"\n\n[[widget]]\ntype = \"x\"\ntitle = \"X\"\ndescription = \"X\"\n\n[[widget.setting]]\nname = \"token\"\nkind = \"secret\"\ndefault = \"abc\"\ndescription = \"A token\"\n", step: "manifest", want: []string{"`default` isn't allowed with this `kind`"}},
	} {
		t.Run(name, func(t *testing.T) {
			s, home, stdout, stderr := sandbox(t, nil, "")
			dir := filepath.Join(home, "fixture")
			if c.manifest != "" {
				writeFile(t, filepath.Join(dir, "visor-plugin.toml"), c.manifest)
			} else {
				plugintest.Write(t, dir, c.o)
			}
			if c.timeout == "" {
				c.timeout = "1m"
			}
			s.Args = []string{"plugin", "check", "--timeout", c.timeout, dir}

			if code := cli.Run(s); code != 1 {
				t.Fatalf("exit %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			out := stdout.String()
			failed := regexp.MustCompile(`(?m)^FAIL +` + c.step + `\b.*(\n {6,}.*)*`).FindString(out)
			if failed == "" {
				t.Fatalf("step %q didn't fail:\n%s", c.step, out)
			}
			for _, want := range c.want {
				if !strings.Contains(failed, want) {
					t.Errorf("the failure doesn't say %q:\n%s", want, out)
				}
			}
			if strings.Contains(out[strings.Index(out, failed):], "\nok") || !strings.HasSuffix(out, "FAIL\n") {
				t.Errorf("steps after the failure ran, or no FAIL at the end:\n%s", out)
			}
		})
	}
}
