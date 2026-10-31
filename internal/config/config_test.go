package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.UI.ArtSize != "auto" || !c.UI.ShowLyrics || c.UI.Theme != "default" {
		t.Errorf("unexpected UI defaults: %+v", c.UI)
	}
	if c.Playback.VolumeStep != 5 || c.Playback.PrefetchAhead != 2 || c.Playback.SeekStep != 5 {
		t.Errorf("unexpected Playback defaults: %+v", c.Playback)
	}
	if c.Keys == nil {
		t.Error("Keys map should be non-nil")
	}
	if c.Pomodoro.WorkMinutes != DefaultWorkMinutes || c.Pomodoro.BreakMinutes != DefaultBreakMinutes {
		t.Errorf("unexpected Pomodoro defaults: %+v", c.Pomodoro)
	}
}

func TestLoad_MissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.toml")
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load(missing) error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got.UI, Default().UI) {
		t.Errorf("missing file should yield defaults, got %+v", got.UI)
	}
}

func TestLoad_ParsesAllSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const body = `
[ui]
art_size = "30"
show_lyrics = false
theme = "tokyo-night"

[playback]
volume_step = 2
prefetch_ahead = 4
seek_step = 10

[keys]
next = "n"
prev = ["p", "b"]
play_pause = " "

[pomodoro]
work_minutes = 50
break_minutes = 10
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if c.UI.ArtSize != "30" || c.UI.ShowLyrics || c.UI.Theme != "tokyo-night" {
		t.Errorf("UI not parsed: %+v", c.UI)
	}
	if c.Playback.VolumeStep != 2 || c.Playback.PrefetchAhead != 4 || c.Playback.SeekStep != 10 {
		t.Errorf("Playback not parsed: %+v", c.Playback)
	}
	if c.Pomodoro.WorkMinutes != 50 || c.Pomodoro.BreakMinutes != 10 {
		t.Errorf("Pomodoro not parsed: %+v", c.Pomodoro)
	}

	ov := c.KeyOverrides()
	want := map[string][]string{
		"next":       {"n"},
		"prev":       {"p", "b"},
		"play_pause": {" "}, // a lone space means the space bar
	}
	if !reflect.DeepEqual(ov, want) {
		t.Errorf("KeyOverrides() = %v, want %v", ov, want)
	}
}

func TestLoad_IgnoresUnknownKeysAndTypes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// Unknown tables/keys are ignored; a non-string key value is dropped.
	const body = `
[ui]
art_size = "auto"
totally_unknown = 7

[keys]
next = 42
bogus_action = "x"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if c.UI.ArtSize != "auto" {
		t.Errorf("expected defaults preserved, got %+v", c.UI)
	}
	// next=42 is a non-string; it must not appear as an override. Unknown
	// action names are stored here and rejected later by buildKeyMap.
	ov := c.KeyOverrides()
	if _, ok := ov["next"]; ok {
		t.Errorf("non-string key value should be ignored, got %v", ov)
	}
	if got := ov["bogus_action"]; !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("string override should be kept for later validation, got %v", ov)
	}
}

func TestNormalize_RepairsBadValues(t *testing.T) {
	c := Config{
		UI:       UI{ArtSize: "  ", Theme: ""},
		Playback: Playback{VolumeStep: 0, SeekStep: -3, PrefetchAhead: 999},
	}
	c.normalize()
	if c.UI.ArtSize != "auto" || c.UI.Theme != "default" {
		t.Errorf("UI not normalised: %+v", c.UI)
	}
	if c.Playback.VolumeStep != 5 || c.Playback.SeekStep != 5 || c.Playback.PrefetchAhead != 2 {
		t.Errorf("Playback not normalised: %+v", c.Playback)
	}
	if c.Keys == nil {
		t.Error("Keys should be initialised by normalize")
	}
}

func TestToStrings(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []string
	}{
		{"string", "n", []string{"n"}},
		{"empty string", "", nil},
		{"space kept", " ", []string{" "}},
		{"array", []any{"a", "b"}, []string{"a", "b"}},
		{"array drops empties", []any{"a", "", "b"}, []string{"a", "b"}},
		{"array drops non-strings", []any{"a", 1, "b"}, []string{"a", "b"}},
		{"string slice", []string{"x"}, []string{"x"}},
		{"int", 5, nil},
		{"nil", nil, nil},
	}
	for _, c := range cases {
		if got := toStrings(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: toStrings(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestWriteDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("default file not written: %v", err)
	}
	// The written template must itself be valid and yield the defaults.
	c, err := Load(path)
	if err != nil {
		t.Fatalf("written default does not parse: %v", err)
	}
	if !reflect.DeepEqual(c.UI, Default().UI) {
		t.Errorf("template UI = %+v, want defaults", c.UI)
	}
	if len(data) == 0 {
		t.Error("empty default file")
	}

	// A second call must not clobber an existing file.
	if err := os.WriteFile(path, []byte("# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault (existing) error = %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "# mine\n" {
		t.Errorf("existing config was overwritten: %q", got)
	}
}

func TestLoad_ParsesPolishKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const body = `
[ui]
theme = "nord"
notify_track_change = false

[playback]
sleep_default_minutes = 45
gapless = false
crossfade_seconds = 6.5
normalize_audio = true

[integrations]
discord_rpc = true
discord_app_id = "1234567890"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if c.UI.Theme != "nord" {
		t.Errorf("UI.Theme = %q, want nord", c.UI.Theme)
	}
	if c.UI.NotifyTrackChange {
		t.Error("notify_track_change should parse to false")
	}
	if c.Playback.SleepDefaultMinutes != 45 {
		t.Errorf("sleep_default_minutes = %d, want 45", c.Playback.SleepDefaultMinutes)
	}
	if c.Playback.Gapless {
		t.Error("gapless should parse to false")
	}
	if c.Playback.CrossfadeSeconds != 6.5 {
		t.Errorf("crossfade_seconds = %v, want 6.5", c.Playback.CrossfadeSeconds)
	}
	if !c.Playback.NormalizeAudio {
		t.Error("normalize_audio should parse to true")
	}
	if !c.Integrations.DiscordRPC || c.Integrations.DiscordAppID != "1234567890" {
		t.Errorf("integrations not parsed: %+v", c.Integrations)
	}
}

func TestDefault_PolishKeys(t *testing.T) {
	c := Default()
	if !c.UI.NotifyTrackChange {
		t.Error("notify_track_change should default on")
	}
	if c.Playback.SleepDefaultMinutes != 30 {
		t.Errorf("sleep_default_minutes default = %d, want 30", c.Playback.SleepDefaultMinutes)
	}
	if !c.Playback.Gapless {
		t.Error("gapless should default on")
	}
	if c.Playback.CrossfadeSeconds != 0 {
		t.Errorf("crossfade_seconds default = %v, want 0", c.Playback.CrossfadeSeconds)
	}
	if c.Playback.NormalizeAudio {
		t.Error("normalize_audio should default off")
	}
	if c.Integrations.DiscordRPC || c.Integrations.DiscordAppID != "" {
		t.Errorf("discord should default off/empty: %+v", c.Integrations)
	}
}

func TestNormalize_PolishClamps(t *testing.T) {
	c := Config{
		Playback: Playback{SleepDefaultMinutes: -5, CrossfadeSeconds: -1},
	}
	c.normalize()
	if c.Playback.SleepDefaultMinutes != 30 {
		t.Errorf("sleep default not clamped: %d", c.Playback.SleepDefaultMinutes)
	}
	if c.Playback.CrossfadeSeconds != 0 {
		t.Errorf("negative crossfade not clamped: %v", c.Playback.CrossfadeSeconds)
	}
	c = Config{Playback: Playback{SleepDefaultMinutes: 5000, CrossfadeSeconds: 999}}
	c.normalize()
	if c.Playback.SleepDefaultMinutes != 30 {
		t.Errorf("oversized sleep default not clamped: %d", c.Playback.SleepDefaultMinutes)
	}
	if c.Playback.CrossfadeSeconds != 30 {
		t.Errorf("oversized crossfade not clamped: %v", c.Playback.CrossfadeSeconds)
	}
}

func TestDefaultPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "ytmusic-tui", "config.toml")
	if got := DefaultPath(); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestSaveAuth_createsAndUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := SaveAuth(path, "firefox", "Default (release)"); err != nil {
		t.Fatalf("SaveAuth: %v", err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load after SaveAuth: %v", err)
	}
	if c.Auth.Browser != "firefox" || c.Auth.Profile != "Default (release)" {
		t.Fatalf("auth not persisted: %+v", c.Auth)
	}

	// Updating must replace, not duplicate, the section and preserve comments.
	if err := SaveAuth(path, "firefox", ""); err != nil {
		t.Fatalf("SaveAuth update: %v", err)
	}
	data, _ := os.ReadFile(path)
	if got := strings.Count(string(data), "[auth]"); got != 1 {
		t.Errorf("[auth] appears %d times, want 1", got)
	}
	c, _ = Load(path)
	if c.Auth.Browser != "firefox" || c.Auth.Profile != "" {
		t.Errorf("auth not updated: %+v", c.Auth)
	}
}
