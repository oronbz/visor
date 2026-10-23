package host

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/render"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/upgrade"
	"github.com/oronbz/visor/internal/version"
	"github.com/oronbz/visor/internal/widget"
)

// upgrades is what the host knows about newer visor releases (GLOSSARY.md, Upgrade).
type upgrades struct {
	// on is upgrade_check.
	on      bool
	install upgrade.Install
	known   upgrade.State
	popup   bool
	// restart is the path to run visor again through once brew has upgraded it.
	restart string
	// chain tells the check timers from those of a chain that ended when
	// upgrade_check was turned off.
	chain int
}

type upgradeDueMsg struct{ chain int }

type upgradeCheckedMsg struct {
	state upgrade.State
	chain int
}

// upgradedMsg is brew's outcome; tag is the release it upgraded to.
type upgradedMsg struct {
	tag string
	err error
}

const highlights = 5

func (m Model) upgradeStore() upgrade.Store { return upgrade.StoreIn(m.opts.Paths.Cache) }

// Restart is the path visor asked to be run again through as it quit, after
// brew upgraded it, or "".
func (m Model) Restart() string { return m.upgrades.restart }

func (m Model) getenv(k string) string {
	if m.opts.Ports.Getenv == nil {
		return ""
	}
	return m.opts.Ports.Getenv(k)
}

// checksUpgrades is false for a build that isn't a release and with
// VISOR_NO_UPGRADE_CHECK set; upgrade_check is read on its own, since a
// config reload can change it.
func (m Model) checksUpgrades() bool {
	_, release := version.Parse(m.version())
	return release && m.getenv("VISOR_NO_UPGRADE_CHECK") == ""
}

// newer is the release an Upgrade would move to, while one is known.
func (m Model) newer() (upgrade.Release, bool) {
	u := m.upgrades
	if !u.on || !m.checksUpgrades() || !u.known.Newer(m.version()) {
		return upgrade.Release{}, false
	}
	return u.known.Latest, true
}

// upgradeDue waits until the last check, whichever instance made it, is
// Interval old. Checks run as one timer chain, from Init or from turning
// upgrade_check on.
func (m Model) upgradeDue() tea.Cmd {
	if !m.checksUpgrades() || !m.upgrades.on {
		return nil
	}
	due := upgradeDueMsg{m.upgrades.chain}
	wait := m.upgrades.known.CheckedAt.Add(upgrade.Interval).Sub(m.opts.Ports.Clock.Now())
	if wait <= 0 {
		return func() tea.Msg { return due }
	}
	return m.opts.Ports.Clock.After(wait, func(time.Time) tea.Msg { return due })
}

func (m Model) checkUpgrade(msg upgradeDueMsg) tea.Cmd {
	if msg.chain != m.upgrades.chain || !m.upgrades.on {
		return nil
	}
	c := upgrade.For(m.opts.Paths.Cache, m.opts.Ports)
	now := m.opts.Ports.Clock.Now()
	return func() tea.Msg {
		// A failed check waits for the next one, as quietly as visor started.
		st, _ := c.Check(now, false)
		return upgradeCheckedMsg{st, msg.chain}
	}
}

func (m Model) upgradeChecked(msg upgradeCheckedMsg) (Model, tea.Cmd) {
	m = m.adoptUpgrade(msg.state)
	if msg.chain != m.upgrades.chain {
		return m, nil
	}
	return m, m.upgradeDue()
}

// adoptUpgrade takes st, read from disk, as what's known. It keeps a
// release closed here that st, written before, doesn't name yet.
func (m Model) adoptUpgrade(st upgrade.State) Model {
	if st.Closed == "" || version.Behind(st.Closed, m.upgrades.known.Closed) {
		st.Closed = m.upgrades.known.Closed
	}
	m.upgrades.known = st
	return m
}

// offerUpgrade opens the popup of a newer release once, as soon as visor is
// in plain NORMAL mode with nothing else open, and closes it once that
// release is closed elsewhere or checks are off.
func (m Model) offerUpgrade() Model {
	r, ok := m.newer()
	switch {
	case !ok || r.Tag == m.upgrades.known.Closed:
		m.upgrades.popup = false
	case !m.modal() && m.drag == nil:
		m.upgrades.popup = true
	}
	return m
}

// showsPopup is false while the terminal is too small to draw the popup,
// which then leaves the keys to visor.
func (m Model) showsPopup() bool { return m.upgrades.popup && !m.frame().TooSmall }

// closePopup records that release tag's popup was answered, here and for every instance.
func (m Model) closePopup(tag string) (Model, tea.Cmd) {
	m.upgrades.popup = false
	m.upgrades.known.Closed = tag
	store := m.upgradeStore()
	return m, func() tea.Msg {
		// Unsaved, the popup only opens again in the next visor.
		_, _ = store.Update(func(s *upgrade.State) { s.Closed = tag })
		return nil
	}
}

func (m Model) popupKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	r, _ := m.newer()
	switch msg.String() {
	case "esc":
		return m.closePopup(r.Tag)
	case "enter":
		return m.startUpgrade()
	case "o":
		return m, m.openPage(r)
	}
	return m, nil
}

func (m Model) openPage(r upgrade.Release) tea.Cmd {
	return m.open("", widget.Action{Label: "open the release page"}, r.Page())
}

// upgradeAction is the Action that asks before starting the Upgrade, naming
// exactly what it runs, copies or opens.
func (m Model) upgradeAction() (bound, bool) {
	r, ok := m.newer()
	if !ok {
		return bound{}, false
	}
	p := m.upgrades.install.Plan(r)
	subject := p.Command
	if subject == "" {
		subject = p.Page
	}
	return bound{Action: widget.Action{ID: "upgrade", Label: "upgrade visor (v" + m.version() + " → " + r.Tag + ")", Confirm: true}, by: upgradeVisor, subject: subject}, true
}

// askUpgrade is U and a click on the pill: the upgrade Action's confirm, while a newer release is known.
func (m Model) askUpgrade() (Model, tea.Cmd) {
	if a, ok := m.upgradeAction(); ok {
		return m.run("", a)
	}
	return m, nil
}

// startUpgrade hands the Upgrade to whatever installed visor (ADR 0004).
func (m Model) startUpgrade() (Model, tea.Cmd) {
	r, ok := m.newer()
	if !ok {
		return m, nil
	}
	m, closed := m.closePopup(r.Tag)
	p := m.upgrades.install.Plan(r)
	switch p.Method {
	case upgrade.Brew:
		name, args := p.Argv()
		return m, tea.Batch(closed, m.opts.Ports.Foreground.Suspend(name, args, func(err error) tea.Msg {
			return upgradedMsg{tag: r.Tag, err: err}
		}))
	case upgrade.Package, upgrade.GoInstall:
		m, toast := m.say("Copied `"+p.Command+"` to the clipboard; run it to upgrade", theme.Fg)
		return m, tea.Batch(closed, m.opts.Ports.Clipboard.Copy(p.Command), toast)
	}
	return m, tea.Batch(closed, m.openPage(r))
}

// upgraded runs the new visor through the path this one was launched by,
// which brew has repointed at it, unlike the Caskroom path it deleted.
func (m Model) upgraded(msg upgradedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.say("Upgrade failed: "+msg.err.Error(), theme.Bad)
	}
	if m.opts.Executable == "" {
		return m.say("Upgraded to "+msg.tag+"; restart visor", theme.Ok)
	}
	m.upgrades.restart = m.opts.Executable
	return m, m.quit()
}

// pill is the footer's ↑ vX.Y.Z while a newer release is known, stepping
// aside while a toast or config problem needs the width.
func (m Model) pill() string {
	r, ok := m.newer()
	if !ok || m.toast.text != "" || m.configProblem != "" {
		return ""
	}
	th := m.opts.Theme
	return th.Fg(theme.Bg).Background(th.Color(theme.Accent)).Bold(true).Render(" ↑ " + r.Tag + " ")
}

// onPill reports whether cell x, y is the footer's pill.
func (m Model) onPill(x, y int) bool {
	p := m.pill()
	return p != "" && y == m.h-footerHeight && x >= m.w-ansi.StringWidth(p)
}

func (m Model) popupLayer() *lipgloss.Layer {
	r, _ := m.newer()
	p := m.upgrades.install.Plan(r)
	w := min(64, m.w-4)
	cut := func(s string) string { return ansi.Truncate(s, w-3, "…") }
	lines := []widget.Line{
		widget.Text(theme.Fg, cut(" A new release of visor is out. You have v"+m.version()+".")),
		{},
		{Spans: []widget.Span{{Text: " What's new", Token: theme.Bright, Bold: true}}},
	}
	for _, l := range r.Highlights(highlights) {
		lines = append(lines, widget.Text(theme.Fg, cut(" "+l)))
	}
	enter, does := "open release page", widget.Line{Spans: []widget.Span{{Text: " ⏎ opens the release page", Token: theme.Dim}}}
	switch p.Method {
	case upgrade.Brew:
		enter, does = "upgrade", commandLine(" ⏎ runs ", p.Command, w-3)
	case upgrade.Package, upgrade.GoInstall:
		enter, does = "copy command", commandLine(" ⏎ copies ", p.Command, w-3)
	}
	lines = append(lines, widget.Line{}, does)
	h := min(m.h-footerHeight, len(lines)+2)
	box := render.Draw(m.opts.Theme, render.Tile{
		Title: "visor " + r.Tag, W: w, H: h, Focused: true,
		Payload: widget.Payload{Lines: lines},
		Hints:   []render.Hint{{Key: "enter", Label: enter}, {Key: "o", Label: "release notes"}, {Key: "esc", Label: "later"}},
	})
	return lipgloss.NewLayer(box).X((m.w - w) / 2).Y(max(0, (m.h-h)/3)).Z(2)
}

func commandLine(lead, command string, w int) widget.Line {
	return widget.Line{Spans: []widget.Span{
		{Text: lead, Token: theme.Dim},
		{Text: ansi.Truncate("`"+command+"`", max(0, w-ansi.StringWidth(lead)), "…"), Token: theme.Accent},
	}}
}
