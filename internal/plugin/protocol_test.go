package plugin_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oronbz/visor/internal/plugin/plugintest"
	"github.com/oronbz/visor/internal/widget"
)

func invoke(t *testing.T, w widget.Widget, inv widget.Invocation) (widget.Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return w.(widget.Actor).Invoke(ctx, inv)
}

func TestActionsRoundTripWithTheirInput(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	w := open(t, typ, "fx", &clock{now: t0}, nil)
	for _, c := range []struct {
		inv  widget.Invocation
		want string
	}{
		{widget.Invocation{Action: "done", Item: "bravo"}, "done bravo"},
		{widget.Invocation{Action: "note", Text: "buy milk"}, "noted buy milk"},
		{widget.Invocation{Action: "token", Text: "hunter2"}, "stored hunter2"},
	} {
		res, err := invoke(t, w, c.inv)
		if err != nil || res.Toast != c.want {
			t.Errorf("%s: toast %q, %v; want %q", c.inv.Action, res.Toast, err, c.want)
		}
	}
	if _, err := invoke(t, w, widget.Invocation{Action: "nope"}); err == nil || err.Error() != "no action nope" {
		t.Errorf("unknown action: %v, want the Plugin's error", err)
	}
}

func TestAnActionLeadingIntoAnActionOfVisorsOwnIsRejected(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	w := open(t, typ, "fx", &clock{now: t0}, nil)
	_, err := invoke(t, w, widget.Invocation{Action: "chain-secret"})
	if err == nil || err.Error() != "Plugin `fixture` sent Action `secret:token`, but ids starting `secret:` are visor's own" {
		t.Errorf("err = %v", err)
	}
}

func TestChoicesAreFetchedWhenAsked(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	w := open(t, typ, "fx", &clock{now: t0}, nil)
	chooser := w.(widget.Chooser)
	got, err := chooser.Choices(context.Background(), widget.Invocation{Action: "move", Item: "alpha"})
	if err != nil || len(got) != 3 || got[1] != (widget.Choice{ID: "done", Label: "Done"}) || got[2].Target != "https://example.com/move" {
		t.Fatalf("choices = %v, %v", got, err)
	}
	if res, err := invoke(t, w, widget.Invocation{Action: "move", Item: "alpha", Choice: "done"}); err != nil || res.Toast != "moved alpha to done" {
		t.Errorf("picking a choice: %q, %v", res.Toast, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := chooser.Choices(ctx, widget.Invocation{Action: "move"}); err == nil || !strings.Contains(err.Error(), "didn't answer action/choices") {
		t.Errorf("slow choices past the deadline: %v", err)
	}
}

func TestResultsCanChainAnActionAndSaveSettings(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	w := open(t, typ, "fx", &clock{now: t0}, nil)
	res, err := invoke(t, w, widget.Invocation{Action: "chain"})
	if err != nil || res.Then == nil || res.Then.ID != "note" || res.Then.Input != widget.TextInput {
		t.Errorf("chain: %+v, %v", res, err)
	}
	res, err = invoke(t, w, widget.Invocation{Action: "greet", Text: "howdy"})
	if err != nil || res.Settings.String("greeting") != "howdy" || res.Toast != "greeting set" {
		t.Errorf("greet: %+v, %v", res, err)
	}
	if _, err := invoke(t, w, widget.Invocation{Action: "greet", Text: "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsAPluginSendsAreCheckedAgainstItsSchema(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	w := open(t, typ, "fx", &clock{now: t0}, nil)
	if _, err := invoke(t, w, widget.Invocation{Action: "bad-settings"}); err == nil || err.Error() != "Plugin `fixture` sent an unknown setting `volume`" {
		t.Errorf("unknown setting: %v", err)
	}
	if _, err := invoke(t, w, widget.Invocation{Action: "bad-mode"}); err == nil || !strings.HasPrefix(err.Error(), "Plugin `fixture` sent setting `mode` that must be one of") {
		t.Errorf("bad value: %v", err)
	}
	if _, err := invoke(t, w, widget.Invocation{Action: "token-setting"}); err == nil || !strings.HasPrefix(err.Error(), "Plugin `fixture` sent setting `token` that is a secret") {
		t.Errorf("secret: %v", err)
	}
}

func TestARawColourIsRejectedNamingIt(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	w := open(t, typ, "fx", &clock{now: t0}, widget.Settings{"mode": "raw-colour"})
	_, err := refresh(t, w)
	if err == nil || !strings.Contains(err.Error(), "Plugin `fixture` sent a payload visor can't show") || !strings.Contains(err.Error(), `"#ff0000" is not a theme token`) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnItemValueThatIsntTextIsRejected(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	w := open(t, typ, "fx", &clock{now: t0}, widget.Settings{"mode": "number-value"})
	_, err := refresh(t, w)
	if err == nil || !strings.Contains(err.Error(), "Plugin `fixture` sent a payload visor can't show") {
		t.Fatalf("err = %v", err)
	}
}

func TestStatusesBecomeTheWidgetContractsErrors(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	c := &clock{now: t0}
	_, err := refresh(t, open(t, typ, "a", c, widget.Settings{"mode": "setup"}))
	var setup *widget.SetupError
	if !errors.As(err, &setup) || setup.Hint != "Sign in to Fixture" || setup.Action == nil || setup.Action.Input != widget.SecretInput {
		t.Errorf("setup: %#v", err)
	}
	_, err = refresh(t, open(t, typ, "b", c, widget.Settings{"mode": "error"}))
	var r interface {
		Reason() string
		Hint() string
	}
	if !errors.As(err, &r) || err.Error() != "upstream is down" || r.Reason() != "down" || r.Hint() != "try later" {
		t.Errorf("error: %v", err)
	}
}

func TestAWrongProtocolVersionNeedsSetupNamingBoth(t *testing.T) {
	for name, c := range map[string]struct {
		o    plugintest.Options
		want string
	}{
		"handshake": {plugintest.Options{Args: []string{"-protocol", "2"}}, "speaks protocol 2"},
		"manifest":  {plugintest.Options{Protocol: 3}, "declares protocol 3"},
	} {
		t.Run(name, func(t *testing.T) {
			_, typ := fixture(t, c.o)
			_, err := refresh(t, open(t, typ, "fx", &clock{now: t0}, nil))
			var setup *widget.SetupError
			if !errors.As(err, &setup) {
				t.Fatalf("err = %v, want needs setup", err)
			}
			if want := fmt.Sprintf("Plugin `fixture` %s, but this visor speaks 1.", c.want); !strings.HasPrefix(setup.Hint, want) {
				t.Errorf("hint = %q, want %q…", setup.Hint, want)
			}
		})
	}
}

// pushes collects a Widget's pushes.
type pushes struct{ ch chan widget.Push }

func listen(w widget.Widget) *pushes {
	p := &pushes{ch: make(chan widget.Push, 16)}
	w.(widget.Pusher).Listen(func(push widget.Push) { p.ch <- push })
	return p
}

func (p *pushes) next(t *testing.T) widget.Push {
	t.Helper()
	select {
	case push := <-p.ch:
		return push
	case <-time.After(10 * time.Second):
		t.Fatal("no push")
	}
	return widget.Push{}
}

func TestTheWidgetLearnsHowItsShownAndThePluginPushes(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	w := open(t, typ, "fx", &clock{now: t0}, nil)
	got := listen(w)
	w.(widget.Observer).Observe(widget.Shown{Size: widget.Size{W: 40, H: 6}, Focused: true, Visible: true})
	push := got.next(t)
	if push.Err != nil || push.Payload == nil || len(push.Payload.Lines) != 1 || push.Payload.Lines[0].Spans[0].Text != "pushed: focused at 40×6 after 0 refreshes" {
		t.Fatalf("push = %+v", push)
	}
}

func TestAMalformedMessageStopsThePluginNamingWhy(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	w := open(t, typ, "fx", &clock{now: t0}, widget.Settings{"mode": "malformed"})
	got := listen(w)
	_, err := refresh(t, w)
	if err == nil || !strings.Contains(err.Error(), "Plugin `fixture` sent a malformed message") {
		t.Fatalf("err = %v", err)
	}
	if push := got.next(t); !push.Lost || !strings.Contains(push.Err.Error(), "malformed message") {
		t.Errorf("push = %+v", push)
	}
}

func TestACrashFailsTheWidgetsAndRestartsWithGrowingDelays(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	c := &clock{now: t0}
	w := open(t, typ, "fx", c, nil)
	got := listen(w)
	crash := func() widget.Push {
		t.Helper()
		if _, err := invoke(t, w, widget.Invocation{Action: "crash"}); err == nil || err.Error() != "Plugin `fixture` stopped" {
			t.Errorf("crash action: %v", err)
		}
		return got.next(t)
	}
	for i, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		push := crash()
		if !push.Lost || !push.Retry.Equal(c.Now().Add(delay)) {
			t.Fatalf("crash %d: lost %t, retry in %v; want %v", i+1, push.Lost, push.Retry.Sub(c.Now()), delay)
		}
		if msg := push.Err.Error(); msg != "Plugin `fixture` stopped: exit status 3 (fixture: boom)" {
			t.Errorf("crash %d: %q", i+1, msg)
		}
		c.add(delay - time.Millisecond)
		if _, err := refresh(t, w); err == nil || !strings.Contains(err.Error(), "stopped") {
			t.Fatalf("crash %d: refresh before the delay: %v, want it still stopped", i+1, err)
		}
		c.add(time.Millisecond)
		if p, err := refresh(t, w); err != nil || len(p.Items) != 2 {
			t.Fatalf("crash %d: refresh after the delay: %v", i+1, err)
		}
	}
	c.add(time.Minute)
	if push := crash(); !push.Retry.Equal(c.Now().Add(time.Second)) {
		t.Errorf("a crash after a minute up restarts in %v, want 1s", push.Retry.Sub(c.Now()))
	}
}

func TestRestartDelaysStopAtAMinute(t *testing.T) {
	_, typ := fixture(t, plugintest.Options{})
	c := &clock{now: t0}
	w := open(t, typ, "fx", c, nil)
	got := listen(w)
	var last time.Duration
	for range 8 {
		invoke(t, w, widget.Invocation{Action: "crash"})
		last = got.next(t).Retry.Sub(c.Now())
		c.add(last)
		if _, err := refresh(t, w); err != nil {
			t.Fatal(err)
		}
	}
	if last != time.Minute {
		t.Errorf("eighth delay = %v, want 1m", last)
	}
}
