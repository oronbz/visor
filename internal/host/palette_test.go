package host_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oronbz/visor/internal/hosttest"
	"github.com/oronbz/visor/internal/layout"
	"github.com/oronbz/visor/internal/widget"
)

// mailAndTodo stages two list Widgets side by side: Mail with an Item and
// Widget Actions, Todo with one Widget Action. Each records what it ran.
func mailAndTodo(t *testing.T, mail, todo widget.Payload) (h *hosttest.Host, mailRan, todoRan *[]widget.Invocation) {
	t.Helper()
	mailRan, todoRan = &[]widget.Invocation{}, &[]widget.Invocation{}
	typ := func(name, title string, p widget.Payload, ran *[]widget.Invocation) widget.Type {
		return widget.Type{Name: name, Title: title, New: func(widget.Config) widget.Widget {
			return inbox{payload: p, ran: ran}
		}}
	}
	h = hosttest.New(t, hosttest.Options{Width: 100, Height: 24, Types: []widget.Type{
		typ("mail", "Mail", mail, mailRan), typ("todo", "Todo", todo, todoRan),
	}})
	return h, mailRan, todoRan
}

func mailbox() widget.Payload {
	its := items("", "hello", "invoice")
	for i := range its {
		its[i].Actions = []widget.Action{{ID: "archive", Key: "a", Label: "archive"}}
	}
	return widget.Payload{
		Columns: []widget.Column{{Flex: true}},
		Items:   its,
		Actions: []widget.Action{{ID: "compose", Key: "c", Label: "compose"}},
	}
}

func todoList() widget.Payload {
	return widget.Payload{
		Columns: []widget.Column{{Flex: true}},
		Actions: []widget.Action{{ID: "sync", Key: "s", Label: "sync"}},
	}
}

// paletteEntries are the palette's rows as their words, such as "Mail archive a".
func paletteEntries(h *hosttest.Host) []string {
	x, top, ok := h.Find("─ Actions ")
	if !ok {
		return nil
	}
	var out []string
	for _, l := range strings.Split(h.Screen(), "\n")[top+1:] {
		box := []rune(l)[x-1:]
		end := slices.Index(box[1:], '│') + 1
		if box[0] != '│' || end < 1 {
			break
		}
		words := strings.Fields(string(box[1:end]))
		if len(words) > 0 && words[0] != "›" {
			out = append(out, strings.Join(words, " "))
		}
	}
	return out
}

func TestCtrlKOpensAPaletteOfEveryWidgetsActionsAndEnterRunsTheChosenOne(t *testing.T) {
	h, mailRan, todoRan := mailAndTodo(t, mailbox(), todoList())

	h.Press("ctrl+k")
	got := strings.Join(paletteEntries(h), " / ")
	for _, want := range []string{"Mail archive a", "Mail compose c", "Mail hide", "Todo sync s", "Todo hide"} {
		if !strings.Contains(got, want) {
			t.Errorf("palette lacks %q while nothing is focused; it lists %q", want, got)
		}
	}

	h.Press("s", "y", "n")
	if got := paletteEntries(h); len(got) != 1 || got[0] != "Todo sync s" {
		t.Errorf("typing syn leaves %q, want only Todo sync", got)
	}
	h.Press("enter")
	if len(*todoRan) != 1 || (*todoRan)[0].Action != "sync" {
		t.Errorf("Todo ran %+v, want sync", *todoRan)
	}
	if len(*mailRan) != 0 {
		t.Errorf("Mail ran %+v, want nothing", *mailRan)
	}
	if _, _, ok := h.Find("─ Actions "); ok {
		t.Errorf("running an Action left the palette open:\n%s", h.Screen())
	}
}

func TestColonOpensThePaletteToRunTheSelectedItemsActionAndEscClosesIt(t *testing.T) {
	h, mailRan, _ := mailAndTodo(t, mailbox(), todoList())
	h.Press("tab", "j", "esc")

	h.Press(":")
	if _, _, ok := h.Find("─ Actions "); !ok {
		t.Fatalf(": didn't open the palette:\n%s", h.Screen())
	}
	if footer := h.Line(23); !strings.Contains(footer, "⏎ run · esc close") {
		t.Errorf("footer %q, want the palette's keys", footer)
	}
	h.Press("esc")
	if _, _, ok := h.Find("─ Actions "); ok {
		t.Fatalf("esc left the palette open:\n%s", h.Screen())
	}

	h.Press(":", "a", "r", "c")
	if got := paletteEntries(h); len(got) != 1 || got[0] != "Mail archive a" {
		t.Errorf("typing arc leaves %q, want only Mail archive", got)
	}
	h.Press("enter")
	if len(*mailRan) != 1 || (*mailRan)[0].Action != "archive" || (*mailRan)[0].Item != "invoice" {
		t.Errorf("Mail ran %+v, want archive on the selected invoice", *mailRan)
	}
}

func TestThePaletteMovesItsSelectionAndAsksBeforeAConfirmedAction(t *testing.T) {
	todo := todoList()
	todo.Actions = append(todo.Actions, widget.Action{ID: "purge", Key: "p", Label: "purge", Confirm: true})
	h, _, todoRan := mailAndTodo(t, mailbox(), todo)

	h.Press("ctrl+k")
	// The palette shows its first 12 entries; visor's own Add Widget ones follow.
	want := "Mail archive a / Mail compose c / Mail refresh / Mail hide / Mail edit settings / Mail remove / " +
		"Todo sync s / Todo purge p / Todo refresh / Todo hide / Todo edit settings / Todo remove"
	if got := strings.Join(paletteEntries(h), " / "); got != want {
		t.Fatalf("palette lists %q, want Layout order %q", got, want)
	}
	if x, y, _ := h.Find("archive"); !isSel(h, x, y) {
		t.Errorf("first entry isn't selected:\n%s", h.Screen())
	}
	h.Press("down", "down", "down", "down", "down", "down", "up", "ctrl+n")
	if x, y, _ := h.Find("sync"); !isSel(h, x, y) {
		t.Errorf("down ×6, up, ctrl+n didn't select Todo sync:\n%s", h.Screen())
	}
	h.Press("down")
	if x, y, _ := h.Find("purge"); !isSel(h, x, y) {
		t.Errorf("down didn't select Todo purge:\n%s", h.Screen())
	}
	h.Press("enter")
	if _, _, ok := h.Find("Purge?"); !ok {
		t.Fatalf("a confirmed Action from the palette didn't ask first:\n%s", h.Screen())
	}
	if len(*todoRan) != 0 {
		t.Fatalf("purge ran before it was confirmed: %+v", *todoRan)
	}
	h.Press("y")
	if len(*todoRan) != 1 || (*todoRan)[0].Action != "purge" {
		t.Errorf("Todo ran %+v, want purge after confirming", *todoRan)
	}
}

func TestTheHostOpensAndCopiesForOpenAndCopyVerbs(t *testing.T) {
	mail := mailbox()
	mail.Items[0].Actions = []widget.Action{
		{ID: "browse", Label: "open in browser", Primary: true, Verb: widget.Open, Target: "https://mail.example/hello"},
		{ID: "link", Key: "y", Label: "copy link", Verb: widget.Copy, Target: "https://mail.example/hello"},
	}
	h, mailRan, _ := mailAndTodo(t, mail, todoList())

	h.Press("tab", "enter", "y")
	if want := []string{"https://mail.example/hello"}; !slices.Equal(h.Opener.Opened, want) {
		t.Errorf("opened %q, want %q", h.Opener.Opened, want)
	}
	if want := []string{"https://mail.example/hello"}; !slices.Equal(h.Clipboard.Copied, want) {
		t.Errorf("copied %q over OSC 52, want %q", h.Clipboard.Copied, want)
	}
	if _, _, ok := h.Find("Copied"); !ok {
		t.Errorf("no toast after copying:\n%s", h.Screen())
	}

	h.Press("esc", "ctrl+k", "c", "o", "p", "y", " ", "l", "enter")
	if len(h.Clipboard.Copied) != 2 {
		t.Errorf("copy from the palette copied %q, want a second copy", h.Clipboard.Copied)
	}
	if len(*mailRan) != 0 {
		t.Errorf("the host's verbs reached the Widget: %+v", *mailRan)
	}

	h.Opener.Err = errors.New("no browser")
	h.Press("tab", "enter")
	if _, _, ok := h.Find("Open in browser failed: no browser"); !ok {
		t.Errorf("no failure toast when the opener fails:\n%s", h.Screen())
	}
}

func TestAWidgetActionOnAHostKeyRunsOnlyFromThePalette(t *testing.T) {
	todo := todoList()
	todo.Actions = append(todo.Actions, widget.Action{ID: "quarantine", Key: "q", Label: "quarantine"})
	h, _, todoRan := mailAndTodo(t, mailbox(), todo)
	h.Press("2")

	if got := h.Line(22); strings.Contains(got, "quarantine") {
		t.Errorf("bottom border %q offers the shadowed q", got)
	}
	h.Press("ctrl+k", "q", "u", "a", "r")
	if got := paletteEntries(h); len(got) != 1 || got[0] != "Todo quarantine" {
		t.Errorf("palette lists %q, want Todo quarantine without its shadowed key", got)
	}
	h.Press("enter")
	if len(*todoRan) != 1 || (*todoRan)[0].Action != "quarantine" {
		t.Fatalf("Todo ran %+v, want quarantine from the palette", *todoRan)
	}
	h.Press("q")
	if !h.Quit() {
		t.Error("q didn't quit while Todo has an Action on it")
	}
	if len(*todoRan) != 1 {
		t.Errorf("q also ran the Widget's Action: %+v", *todoRan)
	}
}

func TestHideFromThePaletteHidesTheWidgetAndRecordsIt(t *testing.T) {
	h, _, todoRan := mailAndTodo(t, mailbox(), todoList())
	h.Press("2", "ctrl+k", "t", "o", "h", "i", "d", "e", "enter")

	if _, _, ok := h.Find("─ Todo "); ok {
		t.Errorf("Todo still drawn after Hide:\n%s", h.Screen())
	}
	if footer := h.Line(23); !strings.Contains(footer, "Tab/←→ focus") {
		t.Errorf("footer %q after hiding the focused Todo, want visor unfocused", footer)
	}
	if len(*todoRan) != 0 {
		t.Errorf("Hide reached the Widget: %+v", *todoRan)
	}
	saved, err := layout.Load(h.Paths.Config)
	if err != nil {
		t.Fatalf("layout.json not written: %v", err)
	}
	if !slices.Equal(saved.Hidden, []string{"todo"}) {
		t.Errorf("saved hidden %v, want [todo]", saved.Hidden)
	}
}

func TestVisorsOwnActionsAreTitledVisorInThePalette(t *testing.T) {
	h := hosttest.New(t, hosttest.Options{Width: 120, Height: 30})

	h.Press("ctrl+k")
	h.Type("agent skills")
	if got := paletteEntries(h); !slices.Contains(got, "visor install agent skills") {
		t.Errorf("palette entries %q, want install agent skills under visor", got)
	}
}
