// Package config loads ytm's optional TOML configuration file
// (~/.config/ytmusic-tui/config.toml). Every option has a sane default, so the
// application works with no config file at all; on first run a commented
// default file is written for discoverability.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// UI holds presentation options.
type UI struct {
	// ArtSize is either "auto" (scale to the terminal, the default) or an
	// integer giving the maximum album-art width in terminal columns.
	ArtSize string `toml:"art_size"`
	// ShowLyrics controls whether the lyrics panel is visible on first launch.
	ShowLyrics bool `toml:"show_lyrics"`
	// Theme is "default" for the built-in palette or the name of a built-in
	// palette (e.g. "nord", "gruvbox", "dracula", "tokyo-night").
	Theme string `toml:"theme"`
	// NotifyTrackChange sends a desktop notification when the track changes.
	NotifyTrackChange bool `toml:"notify_track_change"`
}

// Playback holds playback-tuning options.
type Playback struct {
	// VolumeStep is the percentage change applied by the volume up/down keys.
	VolumeStep int `toml:"volume_step"`
	// PrefetchAhead is how many upcoming tracks to warm the stream-URL cache
	// for while a track plays.
	PrefetchAhead int `toml:"prefetch_ahead"`
	// SeekStep is the number of seconds the seek keys jump.
	SeekStep int `toml:"seek_step"`
	// SleepDefaultMinutes is the duration the sleep timer uses for its
	// "default" menu choice.
	SleepDefaultMinutes int `toml:"sleep_default_minutes"`
	// Gapless enables mpv's gapless-audio mode (no silence between tracks).
	Gapless bool `toml:"gapless"`
	// CrossfadeSeconds sets a per-track fade-in (mpv has no native crossfade);
	// values > 0 also enable gapless playback.
	CrossfadeSeconds float64 `toml:"crossfade_seconds"`
	// NormalizeAudio applies volume normalisation (mpv dynaudnorm filter).
	NormalizeAudio bool `toml:"normalize_audio"`
}

// Auth selects which browser profile's cookies to use. Empty values mean
// "auto-detect", preserving the original behaviour.
type Auth struct {
	// Browser names the browser to read cookies from (e.g. "firefox",
	// "chrome"). Empty means try every installed browser.
	Browser string `toml:"browser"`
	// Profile names the browser profile within Browser. Empty means any
	// profile.
	Profile string `toml:"profile"`
}

// Pomodoro configures the built-in work/break focus timer.
type Pomodoro struct {
	// WorkMinutes is the focus interval length.
	WorkMinutes int `toml:"work_minutes"`
	// BreakMinutes is the rest interval length.
	BreakMinutes int `toml:"break_minutes"`
}

// Integrations holds toggles for external services. Scrobbling was removed
// (see the history); only Discord Rich Presence remains here.
type Integrations struct {
	// DiscordRPC enables Discord Rich Presence (off by default; best-effort).
	DiscordRPC bool `toml:"discord_rpc"`
	// DiscordAppID is the Discord application id used for Rich Presence. RPC
	// is skipped when it is empty.
	DiscordAppID string `toml:"discord_app_id"`
}

// Config is the whole configuration file.
type Config struct {
	UI           UI             `toml:"ui"`
	Playback     Playback       `toml:"playback"`
	Auth         Auth           `toml:"auth"`
	Keys         map[string]any `toml:"keys"`
	Pomodoro     Pomodoro       `toml:"pomodoro"`
	Integrations Integrations   `toml:"integrations"`
}

// Default Pomodoro interval lengths, in minutes.
const (
	DefaultWorkMinutes  = 25
	DefaultBreakMinutes = 5
)

// Default returns a Config populated with the built-in defaults.
func Default() Config {
	return Config{
		UI: UI{
			ArtSize:           "auto",
			ShowLyrics:        true,
			Theme:             "default",
			NotifyTrackChange: true,
		},
		Playback: Playback{
			VolumeStep:          5,
			PrefetchAhead:       2,
			SeekStep:            5,
			SleepDefaultMinutes: 30,
			Gapless:             true,
			CrossfadeSeconds:    0,
			NormalizeAudio:      false,
		},
		Keys: map[string]any{},
		Auth: Auth{},
		Pomodoro: Pomodoro{
			WorkMinutes:  DefaultWorkMinutes,
			BreakMinutes: DefaultBreakMinutes,
		},
		Integrations: Integrations{},
	}
}

// DefaultPath returns ~/.config/ytmusic-tui/config.toml.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ytmusic-tui", "config.toml")
}

// Load reads and parses the config at path. A missing file is not an error:
// the defaults are returned instead. Uncommentable/unknown keys are ignored by
// the TOML decoder.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.normalize()
	return cfg, nil
}

// normalize repairs empty or nonsensical values so downstream code can trust
// the result.
func (c *Config) normalize() {
	if strings.TrimSpace(c.UI.ArtSize) == "" {
		c.UI.ArtSize = "auto"
	}
	if strings.TrimSpace(c.UI.Theme) == "" {
		c.UI.Theme = "default"
	}
	if c.Playback.VolumeStep <= 0 || c.Playback.VolumeStep > 100 {
		c.Playback.VolumeStep = 5
	}
	if c.Playback.SeekStep <= 0 || c.Playback.SeekStep > 600 {
		c.Playback.SeekStep = 5
	}
	if c.Playback.PrefetchAhead <= 0 || c.Playback.PrefetchAhead > 10 {
		c.Playback.PrefetchAhead = 2
	}
	if c.Playback.SleepDefaultMinutes <= 0 || c.Playback.SleepDefaultMinutes > 600 {
		c.Playback.SleepDefaultMinutes = 30
	}
	if c.Playback.CrossfadeSeconds < 0 {
		c.Playback.CrossfadeSeconds = 0
	}
	if c.Playback.CrossfadeSeconds > 30 {
		c.Playback.CrossfadeSeconds = 30
	}
	c.Auth.Browser = strings.TrimSpace(c.Auth.Browser)
	c.Auth.Profile = strings.TrimSpace(c.Auth.Profile)
	if c.Keys == nil {
		c.Keys = map[string]any{}
	}
	if c.Pomodoro.WorkMinutes <= 0 {
		c.Pomodoro.WorkMinutes = DefaultWorkMinutes
	}
	if c.Pomodoro.BreakMinutes <= 0 {
		c.Pomodoro.BreakMinutes = DefaultBreakMinutes
	}
}

// KeyOverrides returns the [keys] section normalised to action -> key strings.
// Values may be a single string or an array of strings; anything else is
// ignored.
func (c Config) KeyOverrides() map[string][]string {
	out := make(map[string][]string)
	for action, raw := range c.Keys {
		keys := toStrings(raw)
		if len(keys) == 0 {
			continue
		}
		out[action] = keys
	}
	return out
}

// toStrings coerces a decoded TOML value into a list of non-empty key strings.
// Whitespace-only strings are preserved (a single space means the space bar)
// rather than trimmed away.
func toStrings(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			s, ok := e.(string)
			if !ok || s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	case []string:
		return t
	default:
		return nil
	}
}

// WriteDefault writes a commented template to path, unless a file already
// exists there. The directory is created if needed. It is safe to call on
// every startup.
func WriteDefault(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultFile), 0o644)
}

// SaveAuth updates the [auth] section of the config at path, creating the file
// from the default template when absent. Other sections and comments are
// preserved, so a user's hand-written config is never clobbered.
func SaveAuth(path, browser, profile string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		data = []byte(defaultFile)
	}
	section := fmt.Sprintf("[auth]\n# Which browser profile's cookies to read. Empty means auto-detect.\nbrowser = %s\nprofile = %s\n",
		strconv.Quote(browser), strconv.Quote(profile))
	updated := replaceSection(string(data), "auth", section)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

// replaceSection replaces the named TOML section (header "[name]" through the
// line before the next section header, or EOF) with replacement. When the
// section is absent, replacement is appended.
func replaceSection(doc, name, replacement string) string {
	lines := strings.Split(doc, "\n")
	header := "[" + name + "]"
	start, end := -1, len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start < 0 {
			if trimmed == header {
				start = i
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			end = i
			break
		}
	}
	if start < 0 {
		out := strings.TrimRight(doc, "\n")
		if out != "" {
			out += "\n\n"
		}
		return out + replacement
	}
	repl := strings.Split(strings.TrimRight(replacement, "\n"), "\n")
	out := append([]string{}, lines[:start]...)
	out = append(out, repl...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

const defaultFile = `# ytm — configuration
# All options are optional; delete this file to fall back to the defaults.
# Location: ~/.config/ytmusic-tui/config.toml

[ui]
# Album art width: "auto" (scale to the terminal) or a maximum in columns.
art_size = "auto"
# Show the lyrics panel on first launch.
show_lyrics = true
# "default" for the built-in palette, or a built-in palette name (nord,
# gruvbox, dracula, tokyo-night). Cycle palettes live with the T key.
theme = "default"
# Send a desktop notification when the track changes.
notify_track_change = true

[playback]
# Percentage changed by the volume up/down keys.
volume_step = 5
# Upcoming tracks to warm the stream-URL cache for while playing (1–10).
prefetch_ahead = 2
# Seconds the seek keys jump.
seek_step = 5
# Default duration (minutes) offered by the sleep timer menu (S key).
sleep_default_minutes = 30
# Gapless playback (no silence between tracks).
gapless = true
# Crossfade seconds between tracks (0 disables). mpv has no native crossfade,
# so this is applied as a per-track fade-in combined with gapless playback.
crossfade_seconds = 0
# Volume normalisation via mpv's dynaudnorm filter.
normalize_audio = false

[auth]
# Which browser profile's cookies to read. Empty means auto-detect.
#   browser = "firefox"
#   profile = "Default (release)"
# Run 'ytm profiles' to list available choices.

# [keys]
# Rebind actions by name. Values may be a single key or an array of keys.
# next          = "n"
# prev          = "p"
# play_pause    = " "
# shuffle       = "s"
# pomodoro      = "P"
# sleep         = "S"    # sleep timer menu
# theme         = "T"    # cycle built-in palettes
# lyrics        = "y"
# lyrics_follow = "f"    # auto-scroll to the active synced lyric line
# palette       = "ctrl+k"
# Unrecognised action names are ignored.

[pomodoro]
# Focus interval length, in minutes.
work_minutes = 25
# Rest interval length, in minutes.
break_minutes = 5

[integrations]
# Discord Rich Presence (best-effort; off by default).
discord_rpc = false
# Your Discord application id (required for RPC to start).
discord_app_id = ""
`
