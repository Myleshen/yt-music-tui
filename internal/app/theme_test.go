package app

import "testing"

func TestApplyPalette_UpdatesGlobalsAndStyles(t *testing.T) {
	// Save and restore the built-in palette so other tests are unaffected.
	defer ApplyPalette(BuiltinPalette())

	p, ok := PaletteByName("nord")
	if !ok {
		t.Fatal("nord palette not found")
	}
	ApplyPalette(p)

	if colAccentNP != p.AccentNP {
		t.Errorf("colAccentNP not applied: %v want %v", colAccentNP, p.AccentNP)
	}
	if colError != p.Error {
		t.Errorf("colError not applied: %v want %v", colError, p.Error)
	}
	// Styles are rebuilt from the new colours; they must be non-zero values and
	// render with the palette's colour.
	if got := styleTitle.Render("x"); got == "" {
		t.Error("styleTitle did not render after ApplyPalette")
	}
}

func TestBuiltinPalette_Stable(t *testing.T) {
	// BuiltinPalette must not change when another palette is applied.
	before := BuiltinPalette()
	ApplyPalette(Palette{}) // deliberately empty
	defer ApplyPalette(BuiltinPalette())
	if after := BuiltinPalette(); after != before {
		t.Errorf("BuiltinPalette changed after ApplyPalette: %v -> %v", before, after)
	}
}

func TestBuiltinPalettes_Selection(t *testing.T) {
	ps := BuiltinPalettes()
	if len(ps) < 3 {
		t.Fatalf("expected several built-in palettes, got %d", len(ps))
	}
	if ps[0].Name != "default" {
		t.Errorf("first palette = %q, want default", ps[0].Name)
	}
	// Every palette must be usable: at least an accent and a text colour, and
	// a selection pair that differs.
	seen := map[string]bool{}
	for _, np := range ps {
		if np.Name == "" {
			t.Error("palette with empty name")
		}
		if seen[np.Name] {
			t.Errorf("duplicate palette name %q", np.Name)
		}
		seen[np.Name] = true
		if np.Palette.Text.Dark == "" || np.Palette.AccentNP.Dark == "" {
			t.Errorf("%s: missing core colours", np.Name)
		}
		if np.Palette.SelBg.Dark == np.Palette.SelFg.Dark {
			t.Errorf("%s: selection fg and bg are identical", np.Name)
		}
	}

	// PaletteByName is case-insensitive and resolves builtin/default aliases.
	for _, name := range []string{"nord", "NORD", " gruvbox ", "builtin", "default"} {
		if _, ok := PaletteByName(name); !ok {
			t.Errorf("PaletteByName(%q) not found", name)
		}
	}
	if _, ok := PaletteByName("definitely-not-a-palette"); ok {
		t.Error("unknown palette name should not resolve")
	}

	// A named palette must actually differ from the default.
	nord, _ := PaletteByName("nord")
	def, _ := PaletteByName("default")
	if nord.AccentNP == def.AccentNP {
		t.Error("nord should not share the default accent")
	}
}

func TestApplyBuiltinAndCycle(t *testing.T) {
	defer ApplyBuiltin("default")

	if !ApplyBuiltin("dracula") {
		t.Fatal("ApplyBuiltin(dracula) failed")
	}
	if got := ActiveBuiltinPalette(); got != "dracula" {
		t.Errorf("ActiveBuiltinPalette() = %q, want dracula", got)
	}
	p, _ := PaletteByName("dracula")
	if colAccentNP != p.AccentNP {
		t.Errorf("ApplyBuiltin did not install the palette: %v want %v", colAccentNP, p.AccentNP)
	}
	if ApplyBuiltin("nope") {
		t.Error("ApplyBuiltin(unknown) should return false")
	}

	// Cycling advances to a different palette and applies it.
	before := ActiveBuiltinPalette()
	next := CyclePalette()
	if next == "" {
		t.Fatal("CyclePalette returned an empty name")
	}
	if next == before {
		t.Errorf("CyclePalette did not advance from %q", before)
	}
	if got := ActiveBuiltinPalette(); got != next {
		t.Errorf("ActiveBuiltinPalette() = %q, want %q after cycle", got, next)
	}
	// Cycling through every palette returns to the start.
	names := BuiltinPalettes()
	for i := 0; i < len(names); i++ {
		CyclePalette()
	}
	if got := ActiveBuiltinPalette(); got != next {
		t.Errorf("a full cycle should return to %q, got %q", next, got)
	}
}

func TestAccentForTab(t *testing.T) {
	defer ApplyBuiltin("default")
	if !ApplyBuiltin("nord") {
		t.Fatal("ApplyBuiltin(nord) failed")
	}
	p, _ := PaletteByName("nord")
	if accentForTab(TabSearch) != p.AccentSearch {
		t.Error("TabSearch accent mismatch")
	}
	if accentForTab(TabLibrary) != p.AccentLibrary {
		t.Error("TabLibrary accent mismatch")
	}
	if accentForTab(TabPlaylists) != p.AccentPlay {
		t.Error("TabPlaylists accent mismatch")
	}
	if accentForTab(TabNowPlaying) != p.AccentNP {
		t.Error("TabNowPlaying accent mismatch")
	}
}
