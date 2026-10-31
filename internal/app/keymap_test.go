package app

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// keyMsg builds a KeyMsg from a key string for binding assertions. Printable
// strings (including a lone space) become runes; a few named keys are mapped to
// their special KeyType so aliases like "enter" still match.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestBuildKeyMap_NoOverrides(t *testing.T) {
	km, unknown := buildKeyMap(nil)
	if len(unknown) != 0 {
		t.Errorf("no overrides should yield no unknown actions, got %v", unknown)
	}
	if !key.Matches(keyMsg("n"), km.Next) {
		t.Error("default Next binding should still match n")
	}
}

func TestBuildKeyMap_Overrides(t *testing.T) {
	km, unknown := buildKeyMap(map[string][]string{
		"next":       {"x"},
		"prev":       {"p", "b"},
		"play_pause": {" "},
	})
	if len(unknown) != 0 {
		t.Errorf("unexpected unknown actions: %v", unknown)
	}
	if !key.Matches(keyMsg("x"), km.Next) {
		t.Error("Next should be rebound to x")
	}
	if key.Matches(keyMsg("n"), km.Next) {
		t.Error("old Next key n should no longer match")
	}
	if !key.Matches(keyMsg("b"), km.Prev) {
		t.Error("Prev should accept the second configured key b")
	}
	if !key.Matches(keyMsg(" "), km.PlayPause) {
		t.Error("PlayPause should accept the space bar")
	}
	// The help description is preserved from the default binding.
	if km.Next.Help().Desc != DefaultKeyMap.Next.Help().Desc {
		t.Errorf("help desc changed: %q", km.Next.Help().Desc)
	}
}

func TestBuildKeyMap_NormalisesActionNames(t *testing.T) {
	km, unknown := buildKeyMap(map[string][]string{
		"Play-Pause": {"z"}, // dash + mixed case
		"  next  ":   {"y"}, // surrounding whitespace
	})
	if len(unknown) != 0 {
		t.Errorf("normalised names should be recognised, got unknown %v", unknown)
	}
	if !key.Matches(keyMsg("z"), km.PlayPause) {
		t.Error("Play-Pause should map to PlayPause")
	}
	if !key.Matches(keyMsg("y"), km.Next) {
		t.Error("trimmed 'next' should map to Next")
	}
}

func TestBuildKeyMap_UnknownActionsReported(t *testing.T) {
	_, unknown := buildKeyMap(map[string][]string{
		"zeta":  {"z"},
		"alpha": {"a"},
		"next":  {"n"},
	})
	want := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(unknown, want) {
		t.Errorf("unknown = %v, want sorted %v", unknown, want)
	}
}

func TestBuildKeyMap_EmptyOverrideIgnored(t *testing.T) {
	km, _ := buildKeyMap(map[string][]string{
		"next": {"", "  "}, // both coerce away for a non-empty-only list...
	})
	// "  " becomes the space bar, so next is rebound to space, not left empty.
	if !key.Matches(keyMsg(" "), km.Next) {
		t.Error("whitespace-only override should mean the space bar")
	}
}

func TestBuildKeyMap_SleepAndTheme(t *testing.T) {
	// Defaults: uppercase S and T (lowercase s stays shuffle).
	km, unknown := buildKeyMap(nil)
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknown actions: %v", unknown)
	}
	if !key.Matches(keyMsg("S"), km.Sleep) {
		t.Error("default Sleep binding should match S")
	}
	if key.Matches(keyMsg("s"), km.Sleep) {
		t.Error("lowercase s must not trigger the sleep timer (it is shuffle)")
	}
	if !key.Matches(keyMsg("T"), km.Theme) {
		t.Error("default Theme binding should match T")
	}

	// Rebinding via config is recognised (not reported as unknown).
	km, unknown = buildKeyMap(map[string][]string{
		"sleep": {"Z"},
		"theme": {"y"},
	})
	if len(unknown) != 0 {
		t.Fatalf("sleep/theme should be recognised actions, got unknown %v", unknown)
	}
	if !key.Matches(keyMsg("Z"), km.Sleep) {
		t.Error("Sleep should be rebindable")
	}
	if !key.Matches(keyMsg("y"), km.Theme) {
		t.Error("Theme should be rebindable")
	}
}

func TestSanitizeKeys(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"dedupes", []string{"a", "a", "b"}, []string{"a", "b"}},
		{"drops empty", []string{"", "a"}, []string{"a"}},
		{"space alias", []string{"space"}, []string{" "}},
		{"return alias", []string{"return"}, []string{"enter"}},
		{"mixed case alias", []string{"Space"}, []string{" "}},
		{"whitespace is space bar", []string{"   "}, []string{" "}},
		{"nil", nil, []string{}},
	}
	for _, c := range cases {
		if got := sanitizeKeys(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: sanitizeKeys(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}
