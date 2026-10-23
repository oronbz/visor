package host

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/oronbz/visor/internal/skills"
	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/version"
)

// skillsMsg is the outcome of installing the agent skills, or of bringing
// them up to date at start (refresh).
type skillsMsg struct {
	refresh bool
	report  skills.Report
	hint    string
	err     error
}

// version is the visor version this Model runs as.
func (m Model) version() string {
	if m.opts.Version != "" {
		return m.opts.Version
	}
	return version.Current()
}

func (m Model) skillsInstaller() (skills.Installer, error) {
	v := m.version()
	getenv := m.opts.Ports.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return skills.For(getenv, v)
}

// refreshSkills updates the skills an older visor installed, off the UI thread.
func (m Model) refreshSkills() tea.Cmd {
	in, err := m.skillsInstaller()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		hint, err := in.Refresh()
		return skillsMsg{refresh: true, hint: hint, err: err}
	}
}

// installSkills is the host Action that does what `visor skills install` does.
func (m Model) installSkills() tea.Cmd {
	in, err := m.skillsInstaller()
	return func() tea.Msg {
		if err != nil {
			return skillsMsg{err: err}
		}
		r, err := in.Install()
		return skillsMsg{report: r, err: err}
	}
}

func (m Model) skillsDone(msg skillsMsg) (Model, tea.Cmd) {
	switch {
	case msg.refresh && msg.err != nil:
		return m.say("Updating agent skills failed: "+msg.err.Error()+"; run `visor skills install`", theme.Warn)
	case msg.refresh && msg.hint != "":
		return m.say(msg.hint, theme.Warn)
	case msg.refresh:
		return m, nil
	case msg.err != nil:
		return m.say("Install agent skills failed: "+msg.err.Error(), theme.Bad)
	}
	text := fmt.Sprintf("Installed %d agent skills", len(msg.report.Installed))
	if len(msg.report.Linked) > 0 {
		text += ", linked for Claude Code"
	}
	if n := len(msg.report.Kept); n > 0 {
		text += fmt.Sprintf("; left %d Claude Code skill folders visor didn't make", n)
	}
	return m.say(text, theme.Fg)
}
