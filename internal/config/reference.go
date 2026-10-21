package config

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/oronbz/visor/internal/theme"
	"github.com/oronbz/visor/internal/widget"
)

// Reference is the full commented config.toml: every global option and
// every Widget type with all its settings at their defaults. Prose lines
// start with "# "; option lines start with a bare "#", so deleting that one
// character turns them on.
func Reference(types []widget.Type, loc Locale) string {
	var b strings.Builder
	prose := func(format string, args ...any) { fmt.Fprintf(&b, "# "+format+"\n", args...) }
	option := func(format string, args ...any) { fmt.Fprintf(&b, "#"+format+"\n", args...) }
	wrapped := func(text string) {
		for _, line := range strings.Split(ansi.Wordwrap(text, 78, ""), "\n") {
			prose("%s", line)
		}
	}
	setting := func(s widget.Setting) {
		prose("%s", s.Description)
		if s.Kind == widget.Secret {
			wrapped("A secret, kept in the keyring, never in this file: " + s.SetHint() + ".")
			prose("Why: %s", s.Why)
			return
		}
		if len(s.Choices) > 0 {
			prose("One of: %s.", strings.Join(s.Choices, ", "))
		}
		for _, f := range s.Fields {
			required := ""
			if f.Required {
				required = " Required."
			}
			prose("  %-8s %s%s", f.Name, f.Description, required)
		}
		prose("Why: %s", s.Why)
		option("%s = %s", s.Name, widget.TOMLValue(s.Default))
	}

	prose("visor config.toml: the full reference, everything commented out.")
	prose("Uncomment a line (delete its leading #) to change an option. visor reads this")
	prose("file but never rewrites it; new options appear in `visor config default`.")
	b.WriteString("\n")
	for _, s := range Globals(loc) {
		setting(s)
		b.WriteString("\n")
	}

	prose("Colour overrides. Everything is drawn with 13 colour tokens; set one to")
	prose("change it everywhere, plugins included. [colors.dark] applies when the terminal")
	prose("background is dark (or unknown), [colors.light] when it's light, on top of")
	prose("`theme`. Values are \"#rrggbb\"; an invalid one is reported and ignored. The")
	prose("values below are visor's own palette. Tokens:")
	for _, tok := range theme.Tokens() {
		prose("  %-7s %s", tok, tok.Use())
	}
	for _, mode := range []struct {
		name    string
		palette theme.Theme
	}{{"dark", theme.Dark()}, {"light", theme.Light()}} {
		option("[colors.%s]", mode.name)
		for _, tok := range theme.Tokens() {
			option("%s = %q", tok, theme.Hex(mode.palette.Color(tok)))
		}
		b.WriteString("\n")
	}

	prose("Plugins under development, loaded from a local directory.")
	option("[[plugin]]")
	option(`path = "~/code/my-visor-plugin"`)
	b.WriteString("\n")

	var defaults []string
	for _, w := range Default(types, loc).Widgets {
		defaults = append(defaults, w.Title)
	}
	prose("Widgets. Each [[widget]] block places one Widget; the dashboard shows the")
	if len(defaults) == 0 {
		prose("blocks in this file.")
	} else {
		prose("blocks in this file, or the default Widgets when there are none:")
		wrapped(strings.Join(defaults, ", ") + ".")
	}
	prose("Every block takes:")
	prose(`  type     the Widget type (required)`)
	prose(`  id       defaults to the type name; give each Widget of the same type its own`)
	prose(`  title    drawn in the top border; defaults to the type's name`)
	prose(`  refresh  how often it fetches, such as "5m"; defaults to the type's interval`)
	for _, t := range types {
		b.WriteString("\n")
		prose("%s Widget. %s", t.Title, t.Description)
		if t.Refresh > 0 {
			floor := ""
			if t.Floor > 0 {
				floor = fmt.Sprintf(", %s at the shortest", widget.ShortDuration(t.Floor))
			}
			prose("Refreshes every %s unless `refresh` says otherwise%s.", widget.ShortDuration(t.Refresh), floor)
		}
		option("[[widget]]")
		option("type = %q", t.Name)
		option("id = %q", t.Name)
		option("title = %q", t.Title)
		for _, s := range t.Settings {
			setting(s)
		}
	}
	return b.String()
}
