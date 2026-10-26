// Package theme maps visor's 13 colour tokens to colours. Widgets and payloads
// only ever name tokens, so a theme recolours everything, plugins included.
package theme

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

type Token int

const (
	Bg Token = iota
	Sel
	Line
	Faint
	Dim
	Fg
	Bright
	Accent
	Warn
	Ok
	Bad
	Info
	Alt
	tokenCount
)

var names = [tokenCount]string{"bg", "sel", "line", "faint", "dim", "fg", "bright", "accent", "warn", "ok", "bad", "info", "alt"}

var uses = [tokenCount]string{
	Bg:     "background",
	Sel:    "selected row background",
	Line:   "borders",
	Faint:  "least important text, weekends, separators",
	Dim:    "secondary text",
	Fg:     "body text and titles",
	Bright: "emphasis, focused titles, big digits",
	Accent: "focused border, today, links",
	Warn:   "warnings, stale data, toasts",
	Ok:     "success",
	Bad:    "errors and failures",
	Info:   "informational highlights",
	Alt:    "a second accent",
}

// Use says what the token colours, for the config reference.
func (t Token) Use() string {
	if t < 0 || t >= tokenCount {
		return ""
	}
	return uses[t]
}

func (t Token) String() string {
	if t < 0 || t >= tokenCount {
		return "unknown"
	}
	return names[t]
}

func Lookup(name string) (Token, bool) {
	for i, n := range names {
		if n == name {
			return Token(i), true
		}
	}
	return 0, false
}

func (t Token) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// UnmarshalText takes a token name only, so payloads can't carry raw colours
// that would ignore the user's theme.
func (t *Token) UnmarshalText(b []byte) error {
	tok, ok := Lookup(string(b))
	if !ok {
		return fmt.Errorf("%q is not a theme token (use one of %s)", b, strings.Join(names[:], " "))
	}
	*t = tok
	return nil
}

// ParseColor reads a "#rrggbb" or "#rgb" colour.
func ParseColor(s string) (color.Color, bool) {
	h, ok := strings.CutPrefix(s, "#")
	if !ok || (len(h) != 3 && len(h) != 6) {
		return nil, false
	}
	if _, err := strconv.ParseUint(h, 16, 32); err != nil {
		return nil, false
	}
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	return lipgloss.Color("#" + h), true
}

func Hex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// Tokens lists every token in canonical order.
func Tokens() []Token {
	ts := make([]Token, tokenCount)
	for i := range ts {
		ts[i] = Token(i)
	}
	return ts
}

type Theme struct {
	colors       [tokenCount]color.Color
	fromTerminal bool
}

// New builds a theme from a colour per token; tokens left out fall back to the visor dark palette.
func New(colors map[Token]color.Color) Theme {
	t := Dark()
	for tok, c := range colors {
		t.colors[tok] = c
	}
	return t
}

// Dark is the prototype's truecolor palette.
func Dark() Theme {
	return palette([tokenCount]string{
		Bg: "#13171d", Sel: "#222a36", Line: "#3d4757", Faint: "#4e5766", Dim: "#7e8797",
		Fg: "#d3d9e2", Bright: "#f5f7fa", Accent: "#8aa9ff", Warn: "#e4ae5c", Ok: "#86c497",
		Bad: "#ef7d7d", Info: "#b59dff", Alt: "#6cc3d8",
	})
}

// Light is the prototype palette's hues for a light background.
func Light() Theme {
	return palette([tokenCount]string{
		Bg: "#f7f8fa", Sel: "#e4e8ef", Line: "#c5ccd8", Faint: "#a9b1bf", Dim: "#6b7383",
		Fg: "#2b313b", Bright: "#0e1116", Accent: "#3f63d8", Warn: "#a86a12", Ok: "#2f8a4a",
		Bad: "#c9433f", Info: "#7a55d6", Alt: "#1f8aa3",
	})
}

func palette(hex [tokenCount]string) Theme {
	var t Theme
	for i, h := range hex {
		t.colors[i] = lipgloss.Color(h)
	}
	return t
}

// Names of the themes config.toml's `theme` picks from.
const (
	Visor         = "visor"
	TerminalTheme = "terminal"
)

// Terminal holds the terminal's answers to the colour queries; a nil colour went unanswered.
type Terminal struct {
	Bg, Fg color.Color
	// ANSI holds palette colours by number; the terminal theme uses 1–6.
	ANSI [8]color.Color
}

// ansiTokens maps palette colour numbers (red, green, yellow, blue, magenta, cyan) to tokens.
var ansiTokens = map[int]Token{1: Bad, 2: Ok, 3: Warn, 4: Accent, 5: Info, 6: Alt}

// Overrides are per-token colours from config.toml's [colors.dark] and
// [colors.light], for dark and light backgrounds.
type Overrides struct {
	Dark, Light map[Token]color.Color
}

// Resolve builds the named theme for what the terminal answered, with the
// overrides for its background on top. visor picks its light or dark palette
// from the background, dark when there's none. terminal derives tokens from
// the terminal's colours, and is visor until both foreground and background
// are known.
func Resolve(name string, term Terminal, o Overrides) Theme {
	dark := term.Bg == nil || IsDark(term.Bg)
	t := base(name, term, dark)
	over := o.Dark
	if !dark {
		over = o.Light
	}
	for tok, c := range over {
		t.colors[tok] = c
	}
	return t
}

func base(name string, term Terminal, dark bool) Theme {
	t := Dark()
	if !dark {
		t = Light()
	}
	if name != TerminalTheme || term.Bg == nil || term.Fg == nil {
		return t
	}
	t.fromTerminal = true
	bg, fg := term.Bg, term.Fg
	t.colors[Bg], t.colors[Fg] = bg, fg
	t.colors[Sel] = mix(fg, bg, 0.08)
	t.colors[Line] = mix(fg, bg, 0.22)
	t.colors[Faint] = mix(fg, bg, 0.32)
	t.colors[Dim] = mix(fg, bg, 0.55)
	extreme := color.Color(color.White)
	if !dark {
		extreme = color.Black
	}
	t.colors[Bright] = mix(extreme, fg, 0.5)
	for n, tok := range ansiTokens {
		if c := term.ANSI[n]; c != nil && contrast(c, bg) >= minContrast {
			t.colors[tok] = c
		}
	}
	return t
}

// minContrast is the WCAG ratio a palette colour needs against the
// background to stand in for a token; below it the token keeps visor's own,
// since many schemes ship a blue or yellow too faint to read on their background.
const minContrast = 3.0

// contrast is the WCAG contrast ratio of a and b, from 1 to 21.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// luminance is the WCAG relative luminance of c.
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// mix is p of a over 1-p of b, per sRGB channel.
func mix(a, b color.Color, p float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	ch := func(x, y uint32) uint8 {
		return uint8(math.Round((float64(x>>8)*p + float64(y>>8)*(1-p))))
	}
	return color.RGBA{ch(ar, br), ch(ag, bg), ch(ab, bb), 0xff}
}

// FromTerminal reports whether t was derived from the terminal's colours,
// so visor leaves the terminal's background alone.
func (t Theme) FromTerminal() bool { return t.fromTerminal }

// IsDark reports whether c has an HSL lightness under one half.
func IsDark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return max(r, g, b)+min(r, g, b) < 0xffff
}

// IsZero reports whether t was never built, as in an unset option.
func (t Theme) IsZero() bool { return t.colors[Bg] == nil }

func (t Theme) Color(tok Token) color.Color { return t.colors[tok] }

// Blend is the colour p of the way along toks, mixed between neighbours.
func (t Theme) Blend(toks []Token, p float64) color.Color {
	if len(toks) == 1 {
		return t.colors[toks[0]]
	}
	at := min(max(p, 0), 1) * float64(len(toks)-1)
	i := min(int(at), len(toks)-2)
	return mix(t.colors[toks[i+1]], t.colors[toks[i]], at-float64(i))
}

func (t Theme) Fg(tok Token) lipgloss.Style { return lipgloss.NewStyle().Foreground(t.colors[tok]) }
