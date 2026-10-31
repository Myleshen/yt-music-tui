package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette is the full set of colours the UI draws with. It is applied to the
// package-level colour variables through ApplyPalette, which also rebuilds the
// reusable styles.
type Palette struct {
	AccentNP      lipgloss.AdaptiveColor
	AccentSearch  lipgloss.AdaptiveColor
	AccentLibrary lipgloss.AdaptiveColor
	AccentPlay    lipgloss.AdaptiveColor

	Text  lipgloss.AdaptiveColor
	Muted lipgloss.AdaptiveColor

	Bar     lipgloss.AdaptiveColor
	Playing lipgloss.AdaptiveColor
	Error   lipgloss.AdaptiveColor

	SelBg lipgloss.AdaptiveColor
	SelFg lipgloss.AdaptiveColor
}

// Adaptive palette. Every colour is an AdaptiveColor so the UI stays readable
// on both light and dark terminals, and lipgloss automatically degrades the
// truecolor values to the nearest 256-colour equivalent when needed. The goal
// is lively but comfortable: one accent per tab, muted secondary text, and a
// green "playing" indicator that stands out without shouting.
var (
	colAccentNP      = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C4A7FF"} // violet
	colAccentSearch  = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#5EEAD4"} // teal
	colAccentLibrary = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#86EFAC"} // green
	colAccentPlay    = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FDBA74"} // amber

	colText  = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#E5E7EB"}
	colMuted = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}

	colBar     = lipgloss.AdaptiveColor{Light: "#2563EB", Dark: "#60A5FA"}
	colPlaying = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}

	colSelBg = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#6D28D9"}
	colSelFg = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}

	colError = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#FCA5A5"}
)

// BuiltinPalette returns the default palette. It always returns the original
// hard-coded colours, even after ApplyPalette has swapped in a theme, so it is
// safe to use as a fallback.
func BuiltinPalette() Palette {
	return Palette{
		AccentNP:      lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C4A7FF"},
		AccentSearch:  lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#5EEAD4"},
		AccentLibrary: lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#86EFAC"},
		AccentPlay:    lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FDBA74"},

		Text:  lipgloss.AdaptiveColor{Light: "#111827", Dark: "#E5E7EB"},
		Muted: lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"},

		Bar:     lipgloss.AdaptiveColor{Light: "#2563EB", Dark: "#60A5FA"},
		Playing: lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"},
		Error:   lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#FCA5A5"},

		SelBg: lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#6D28D9"},
		SelFg: lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"},
	}
}

// NamedPalette pairs a built-in palette with a stable, user-facing name. The
// name is what ui.theme accepts and what the T key cycles through.
type NamedPalette struct {
	Name    string
	Palette Palette
}

// solidPalette builds a Palette whose colours are identical in light and dark
// terminals. Built-in themes are tuned for dark terminals; the single value
// keeps them legible and predictable everywhere.
func solidPalette(accentNP, accentSearch, accentLibrary, accentPlay, text, muted, bar, playing, errColor, selBg, selFg string) Palette {
	s := func(hex string) lipgloss.AdaptiveColor {
		return lipgloss.AdaptiveColor{Light: hex, Dark: hex}
	}
	return Palette{
		AccentNP:      s(accentNP),
		AccentSearch:  s(accentSearch),
		AccentLibrary: s(accentLibrary),
		AccentPlay:    s(accentPlay),

		Text:  s(text),
		Muted: s(muted),

		Bar:     s(bar),
		Playing: s(playing),
		Error:   s(errColor),

		SelBg: s(selBg),
		SelFg: s(selFg),
	}
}

// builtinPalettes is the ordered list of built-in palettes. The first entry is
// the original built-in palette and is what "default"/"builtin" resolve to.
var builtinPalettes = []NamedPalette{
	{Name: "default", Palette: BuiltinPalette()},
	{Name: "nord", Palette: solidPalette(
		"#88C0D0", "#81A1C1", "#A3BE8C", "#EBCB8B",
		"#D8DEE9", "#81A1C1", "#5E81AC", "#A3BE8C", "#BF616A",
		"#434C5E", "#ECEFF4")},
	{Name: "gruvbox", Palette: solidPalette(
		"#FABD2F", "#83A598", "#B8BB26", "#FE8019",
		"#EBDBB2", "#A89984", "#83A598", "#B8BB26", "#FB4934",
		"#504945", "#FBF1C7")},
	{Name: "dracula", Palette: solidPalette(
		"#BD93F9", "#8BE9FD", "#50FA7B", "#FFB86C",
		"#F8F8F2", "#6272A4", "#8BE9FD", "#50FA7B", "#FF5555",
		"#44475A", "#F8F8F2")},
	{Name: "tokyo-night", Palette: solidPalette(
		"#7AA2F7", "#7DCFFF", "#9ECE6A", "#E0AF68",
		"#C0CAF5", "#565F89", "#7AA2F7", "#9ECE6A", "#F7768E",
		"#3B4261", "#C0CAF5")},
}

// BuiltinPalettes returns the ordered built-in palettes. The returned slice is
// a copy, so callers cannot mutate the registry.
func BuiltinPalettes() []NamedPalette {
	out := make([]NamedPalette, len(builtinPalettes))
	copy(out, builtinPalettes)
	return out
}

// PaletteByName returns the built-in palette with the given name
// (case-insensitive). "builtin"/"default" both resolve to the default palette.
func PaletteByName(name string) (Palette, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "builtin" {
		n = "default"
	}
	for _, np := range builtinPalettes {
		if np.Name == n {
			return np.Palette, true
		}
	}
	return Palette{}, false
}

// activePaletteIdx tracks the current built-in palette so CyclePalette can
// advance predictably.
var activePaletteIdx int

// ApplyBuiltin installs the named built-in palette and records it as active. It
// reports false (and changes nothing) when name is unknown.
func ApplyBuiltin(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "builtin" {
		n = "default"
	}
	for i, np := range builtinPalettes {
		if np.Name == n {
			activePaletteIdx = i
			ApplyPalette(np.Palette)
			return true
		}
	}
	return false
}

// ActiveBuiltinPalette returns the name of the currently selected built-in
// palette.
func ActiveBuiltinPalette() string {
	if activePaletteIdx < 0 || activePaletteIdx >= len(builtinPalettes) {
		return ""
	}
	return builtinPalettes[activePaletteIdx].Name
}

// CyclePalette installs the next built-in palette (wrapping around), records it
// as active, and returns its name. It is the runtime equivalent of changing
// ui.theme, used by the T key.
func CyclePalette() string {
	if len(builtinPalettes) == 0 {
		return ""
	}
	activePaletteIdx = (activePaletteIdx + 1) % len(builtinPalettes)
	np := builtinPalettes[activePaletteIdx]
	ApplyPalette(np.Palette)
	return np.Name
}

// ApplyPalette installs p as the active palette and rebuilds the shared
// styles. It must run before the model (and its list delegates) is constructed.
func ApplyPalette(p Palette) {
	colAccentNP = p.AccentNP
	colAccentSearch = p.AccentSearch
	colAccentLibrary = p.AccentLibrary
	colAccentPlay = p.AccentPlay

	colText = p.Text
	colMuted = p.Muted

	colBar = p.Bar
	colPlaying = p.Playing
	colError = p.Error

	colSelBg = p.SelBg
	colSelFg = p.SelFg

	rebuildStyles()
}

// rebuildStyles recreates the shared styles from the current colour variables.
// It runs at init and again whenever a new palette is applied.
func rebuildStyles() {
	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(colText)
	styleMuted = lipgloss.NewStyle().Foreground(colMuted)
	styleErr = lipgloss.NewStyle().Foreground(colError)
}

// accentForTab returns the accent colour associated with a tab.
func accentForTab(tab Tab) lipgloss.AdaptiveColor {
	switch tab {
	case TabSearch:
		return colAccentSearch
	case TabLibrary:
		return colAccentLibrary
	case TabPlaylists:
		return colAccentPlay
	case TabHome, TabStats:
		return colAccentSearch
	default:
		return colAccentNP
	}
}

// Reusable styles built from the palette. They are values, so they can be
// shared freely between the (value-copied) model updates, and they are rebuilt
// by ApplyPalette.
var (
	styleTitle lipgloss.Style
	styleMuted lipgloss.Style
	styleErr   lipgloss.Style
)

func init() {
	rebuildStyles()
}
