package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// The palette is deliberately tiny. Colour carries *status* and nothing else —
// identity is conveyed by position and name — so a screen of idle lanes is a
// screen with almost no colour in it. Two hues total: one for "alive", one for
// "wants you". Everything else is greyscale weight.
var (
	fgLight, fgDark       = lipgloss.Color("#1F2328"), lipgloss.Color("#E6EDF3")
	mutedLight, mutedDark = lipgloss.Color("#6E7781"), lipgloss.Color("#8B949E")
	faintLight, faintDark = lipgloss.Color("#C3CAD1"), lipgloss.Color("#3D444D")
	aliveLight, aliveDark = lipgloss.Color("#1A7F37"), lipgloss.Color("#3FB950")
	accentL, accentD      = lipgloss.Color("#BC4C00"), lipgloss.Color("#F0883E")
	selBgLight, selBgDark = lipgloss.Color("#0B5FBF"), lipgloss.Color("#1F6FEB")
	selFgLight, selFgDark = lipgloss.Color("#FFFFFF"), lipgloss.Color("#F6F8FA")
)

// Glyphs are separated out because the obvious choices (● ○ ←) are East-Asian
// *ambiguous* width: some terminals draw them double-wide and shear every
// column. The ASCII set is narrow everywhere.
type Glyphs struct {
	Lane     string
	Archived string
	Branch   string
	Last     string
	Pipe     string
	Blank    string
	Fork     string
	Run      string
	Agent    string
	Seam     string
	Failed   string
}

func glyphsFor(ascii bool) Glyphs {
	if ascii {
		return Glyphs{Lane: "*", Archived: "o", Branch: "|-", Last: "`-", Pipe: "|  ", Blank: "   ", Fork: "<", Run: "~", Agent: "@", Seam: "=", Failed: "!"}
	}
	return Glyphs{Lane: "●", Archived: "○", Branch: "├─", Last: "└─", Pipe: "│  ", Blank: "   ", Fork: "←", Run: "⋯", Agent: "⊕", Seam: "═", Failed: "⚠"}
}

// Theme holds every style the map draws with.
type Theme struct {
	Glyphs Glyphs

	Brand    lipgloss.Style
	Header   lipgloss.Style
	Rail     lipgloss.Style
	Title    lipgloss.Style
	Dim      lipgloss.Style
	Faint    lipgloss.Style
	Alive    lipgloss.Style
	Accent   lipgloss.Style
	Column   lipgloss.Style
	Selected lipgloss.Style
	Border   lipgloss.Style
	Label    lipgloss.Style
	Value    lipgloss.Style
	Panel    lipgloss.Style
	Footer   lipgloss.Style
	Key      lipgloss.Style
	Empty    lipgloss.Style
}

// NewTheme builds the theme for the terminal's actual background, so the same
// binary reads correctly in a light and a dark terminal.
func NewTheme(isDark, ascii bool) Theme {
	c := lipgloss.LightDark(isDark)
	pick := func(l, d color.Color) color.Color { return c(l, d) }

	fg := pick(fgLight, fgDark)
	muted := pick(mutedLight, mutedDark)
	faint := pick(faintLight, faintDark)
	alive := pick(aliveLight, aliveDark)
	accent := pick(accentL, accentD)
	selBg := pick(selBgLight, selBgDark)
	selFg := pick(selFgLight, selFgDark)

	return Theme{
		Glyphs: glyphsFor(ascii),
		Brand:  lipgloss.NewStyle().Foreground(fg).Bold(true),
		Header: lipgloss.NewStyle().Foreground(muted),
		Rail:   lipgloss.NewStyle().Foreground(faint),
		Title:  lipgloss.NewStyle().Foreground(fg),
		Dim:    lipgloss.NewStyle().Foreground(muted),
		Faint:  lipgloss.NewStyle().Foreground(faint),
		Alive:  lipgloss.NewStyle().Foreground(alive),
		Accent: lipgloss.NewStyle().Foreground(accent),
		Column: lipgloss.NewStyle().Foreground(accent).Bold(true),
		Border: lipgloss.NewStyle().Foreground(faint),
		Label:  lipgloss.NewStyle().Foreground(muted),
		Value:  lipgloss.NewStyle().Foreground(fg),
		Panel:  lipgloss.NewStyle().Foreground(fg).Bold(true),
		// Selection is a solid band across the whole row, k9s style: one flat
		// style rather than nested per-segment colours, so it never tears.
		Selected: lipgloss.NewStyle().Background(selBg).Foreground(selFg),
		Footer:   lipgloss.NewStyle().Foreground(faint),
		Key:      lipgloss.NewStyle().Foreground(muted).Bold(true),
		Empty:    lipgloss.NewStyle().Foreground(muted).Italic(true),
	}
}
