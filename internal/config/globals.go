package config

import (
	"cmp"
	"slices"
	"strings"

	"github.com/oronbz/visor/internal/widget"
)

// Locale holds the defaults for options that follow the system locale.
type Locale struct {
	TimeFormat string
	WeekStart  string
}

var weekdays = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// Globals is the schema of config.toml's top-level options.
func Globals(loc Locale) []widget.Setting {
	if loc.TimeFormat == "" {
		loc.TimeFormat = "24h"
	}
	if loc.WeekStart == "" {
		loc.WeekStart = "monday"
	}
	return []widget.Setting{
		{
			Name: "theme", Kind: widget.String, Default: "terminal", Choices: []string{"terminal", "visor"},
			Description: `Colour theme: "terminal" derives colours from your terminal's own, keeping visor's for any too faint to read on your background, and falls back to "visor" when the terminal doesn't answer; "visor" is the built-in palette, light or dark to match your terminal's background.`,
			Why:         `Pick "visor" for visor's own palette whatever your terminal's colour scheme.`,
		},
		{
			Name: "time_format", Kind: widget.String, Default: loc.TimeFormat, Choices: []string{"12h", "24h"},
			Description: "Clock style. Defaults from your locale.",
			Why:         "Set it when your locale's default isn't how you read the time.",
		},
		{
			Name: "week_start", Kind: widget.String, Default: loc.WeekStart, Choices: weekdays,
			Description: "First day of the week in the Calendar. Defaults from your locale.",
			Why:         "Set it when your locale starts the week on a different day than you do.",
		},
		{
			Name: "mouse", Kind: widget.Bool, Default: true,
			Description: "Capture the mouse for clicks, drags and the wheel.",
			Why:         "Turn it off to get the terminal's own text selection back.",
		},
		{
			Name: "upgrade_check", Kind: widget.Bool, Default: true,
			Description: "Check for a new visor release every 12 hours. When one is out, visor shows a popup once and an ↑ vX.Y.Z pill in the footer; ⏎ in the popup, U, a click on the pill or the palette's \"upgrade visor\" starts the Upgrade, which runs brew for a Homebrew install and otherwise copies the command or opens the release page. `visor upgrade` checks right away. VISOR_NO_UPGRADE_CHECK=1 turns it off too.",
			Why:         "Turn it off to keep visor from asking GitHub for releases; `visor upgrade` still works.",
		},
	}
}

// DetectLocale picks the clock style and first weekday. LC_ALL or LC_TIME
// win, being set on purpose; then system, what the OS says ("" where it
// doesn't), since terminals fill LANG in as en_US whatever the region; then
// LANG. A region ("en_US.UTF-8") maps to its defaults following CLDR, and
// unknown regions get 24h and Monday.
func DetectLocale(getenv func(string) string, system Locale) Locale {
	explicit := cmp.Or(getenv("LC_ALL"), getenv("LC_TIME"))
	tag := cmp.Or(explicit, getenv("LANG"))
	tag, _, _ = strings.Cut(tag, ".")
	tag, _, _ = strings.Cut(tag, "@")
	_, region, _ := strings.Cut(strings.ReplaceAll(tag, "-", "_"), "_")
	region = strings.ToUpper(region)

	loc := Locale{TimeFormat: "24h", WeekStart: "monday"}
	if slices.Contains(twelveHour, region) {
		loc.TimeFormat = "12h"
	}
	switch {
	case slices.Contains(sundayFirst, region):
		loc.WeekStart = "sunday"
	case slices.Contains(saturdayFirst, region):
		loc.WeekStart = "saturday"
	}
	if explicit == "" {
		loc.TimeFormat = cmp.Or(system.TimeFormat, loc.TimeFormat)
		loc.WeekStart = cmp.Or(system.WeekStart, loc.WeekStart)
	}
	return loc
}

var twelveHour = []string{"AU", "BD", "CA", "CO", "EG", "IN", "JO", "KR", "MY", "NZ", "PH", "PK", "SA", "TW", "US"}

var sundayFirst = []string{
	"AG", "AS", "BR", "BS", "BT", "BW", "BZ", "CA", "CO", "DM", "DO", "ET", "GT", "GU", "HK", "HN", "ID", "IL",
	"IN", "JM", "JP", "KE", "KH", "KR", "LA", "MH", "MM", "MO", "MT", "MX", "MZ", "NI", "NP", "PA", "PE", "PH",
	"PK", "PR", "PT", "PY", "SA", "SG", "SV", "TH", "TT", "TW", "UM", "US", "VE", "VI", "WS", "YE", "ZA", "ZW",
}

var saturdayFirst = []string{"AE", "AF", "BH", "DJ", "DZ", "EG", "IQ", "IR", "JO", "KW", "LY", "OM", "QA", "SD", "SY"}
