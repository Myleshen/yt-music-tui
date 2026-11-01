# ytm — YouTube Music in the terminal

A keyboard-driven terminal client for YouTube Music with **your own account**
(liked songs, playlists, search). Written in Go — no Python, no API keys.
Playback uses `mpv` (with `yt-dlp` for stream extraction).

> Fork of [kunalpjain/tui-yt](https://github.com/kunalpjain/tui-yt) (MIT).
> The Python `ytmusicapi` bridge has been replaced with a native Go InnerTube
> client. See "What changed".

## Features

- **Search** YouTube Music by song
- **Library** — your liked songs
- **Playlists** — browse and play any playlist in your library
- **Now Playing** — title, artist, album, progress bar, next track, like/loop
- **Auto-advance** with background URL prefetch so playback starts fast
- **Resume** where you left off, with a persistent stream-URL cache
- **Vim-style keybindings**, mouse support, command palette, searchable help
- **Themes** — built-in palettes (`default`, `nord`, `gruvbox`, `dracula`,
  `tokyo-night`) cycled with `T`
- **Cached** — persistent stream-URL, metadata, album-art, and lyrics caches
- **Pomodoro** — a built-in focus/break timer (`P`)
- **Listening stats** — local-only play history (`5`)
- **Export / import** — the queue or playlist as M3U + CSV (`E` / `I`)

## Requirements

| | Needed for | Notes |
|---|---|---|
| **mpv** | playback | the actual audio player |
| **yt-dlp** | stream resolution | `brew install yt-dlp` · `sudo pacman -S yt-dlp` |
| **Go ≥ 1.26** | building from source | |

No Python, no browser extension, no API keys.

## Build

```sh
git clone https://github.com/Myleshen/yt-music-tui.git
cd yt-music-tui
make build            # -> ./build/ytm
# or cross-compile linux/darwin/windows standalone binaries:
make cross
```

## Sign in

```sh
ytm auth                 # auto-detect a signed-in Firefox/Chrome/Brave/Edge
ytm auth --browser firefox
ytm auth --paste         # fallback: paste the Cookie header manually
```

Cookies are read from your browser's cookie store (via
[kooky](https://github.com/browserutils/kooky)) and saved to
`~/.config/ytmusic-tui/auth.json` (chmod 600). The client derives a
`SAPISIDHASH` header from `SAPISID`, the same mechanism `ytmusicapi` uses.

**How often do you re-auth?** Rarely — the Google session cookie lasts months
(generally until you sign out or change your password). If you use
`ytm auth` auto-detection you normally never touch it again.

## Run

```sh
ytm
```

| Key | Action |
|---|---|
| `0` | Home / discover |
| `1`–`5` | switch tab (Now Playing / Search / Library / Playlists / Stats) |
| `/` | focus search |
| `enter` | play selected track |
| `space` | play/pause |
| `n` / `p` | next / previous |
| `←` / `→` | seek −5s / +5s |
| `+` / `-` | volume |
| `r` | toggle loop (single track) |
| `s` | toggle shuffle |
| `y` | toggle lyrics (Now Playing) |
| `P` | start / stop the Pomodoro timer |
| `T` | cycle the colour palette |
| `E` / `I` | export queue/playlist / import M3U |
| `?` | keyboard shortcuts |
| `q` | quit |

## Configuration

Configuration is optional: with no file, ytm runs on built-in defaults. On
first launch a fully commented template is written to
`~/.config/ytmusic-tui/config.toml`, which you can edit and restart to apply.

```toml
[ui]
# Album-art width: "auto" (scale to the terminal) or a max in columns.
art_size = "auto"
# Show the lyrics panel on first launch (`y` toggles it either way).
show_lyrics = true
# Built-in palette: "default", "nord", "gruvbox", "dracula", or "tokyo-night".
theme = "default"

[playback]
# Percentage changed by the volume keys.
volume_step = 5
# Upcoming tracks to warm the stream-URL cache for while playing.
prefetch_ahead = 2
# Seconds the seek keys jump.
seek_step = 5

[keys]
# Rebind any action by name. Values may be one key or an array of keys.
# Whitespace " " means the space bar. Unrecognised names are warned and
# ignored. Run `ytm` and check the log for the recognised set.
next       = "n"
prev       = "p"
play_pause = " "
shuffle    = "s"
pomodoro   = "P"

[pomodoro]
# Focus interval length, in minutes.
work_minutes = 25
# Rest interval length, in minutes.
break_minutes = 5
```

| Option | Type | Default | Effect |
|---|---|---|---|
| `ui.art_size` | string | `"auto"` | `"auto"` scales art to ~⅓ of the terminal; an integer caps the width in columns (clamped to 12–80). |
| `ui.show_lyrics` | bool | `true` | Whether the lyrics panel is visible on launch. |
| `ui.theme` | string | `"default"` | A built-in palette name. |
| `playback.volume_step` | int | `5` | Percent per volume key press. |
| `playback.prefetch_ahead` | int | `2` | Upcoming tracks warmed in the background. |
| `playback.seek_step` | int | `5` | Seconds per seek key press. |
| `pomodoro.work_minutes` | int | `25` | Focus interval length. |
| `pomodoro.break_minutes` | int | `5` | Rest interval length. |
| `keys.<action>` | string \| []string | built-in | Rebind an action (see `internal/app/keymap.go` for names). |

Recognised `[keys]` action names: `up`, `down`, `tab0`–`tab4`, `home`,
`search`, `select`, `play_pause`, `next`, `prev`, `volume_up`, `volume_down`,
`seek_forward`, `seek_back`, `loop`, `shuffle`, `pomodoro`, `sleep`, `theme`,
`queue_move_up`, `queue_move_down`, `queue_remove`, `clear_upcoming`, `like`,
`dislike`, `select_mode`, `bulk_add`, `export`, `import`, `lyrics`, `top`,
`bottom`, `lyrics_scroll_up`, `lyrics_scroll_down`, `lyrics_follow`, `help`,
`back`, `quit`, `palette`.

### Pomodoro

Press `P` to start/stop. The countdown shows in the player bar
(`🍅 24:59` / `☕ 04:30`) and cycles work → break → work. When a work phase
ends, playback pauses and a desktop notification nudges you to stand up and
move; when the break ends it notifies you to get back to work.

### Cold start

Resolving an uncached stream with `yt-dlp` spends a couple of seconds in
YouTube's JavaScript challenge. ytm tries the measured fastest reliable yt-dlp
player client first (`android`) and falls back to the default, so a change on
YouTube's side degrades to the previous behaviour rather than breaking
playback.

### Themes

Set `ui.theme` to a built-in palette name, or press `T` to cycle through them
at runtime. Colours adapt to light and dark terminals, and the selection row
stays legible in every palette.

### Caches

Under `~/.config/ytmusic-tui/` ytm keeps a private lyrics cache in `lyrics/`
(`0700/0600`, keyed by a hash of artist+title+album). Lyrics are served from
disk first, hitting [LRCLIB](https://lrclib.net) only on a miss; the
not-found case is cached too, so an instrumental track is not re-queried on
every start. Stream URLs and metadata live in the bbolt state DB.

### MPRIS & radio

Media keys, `playerctl`, and desktop media widgets are supported via MPRIS
(`org.mpris.MediaPlayer2.ytm`), including `mpris:artUrl` cover art for status
bars such as Noctalia/QuickShell. When the queue ends, playback continues
automatically with YouTube Music's radio/mix for the last track.

## What changed vs. upstream

- **Removed Python entirely.** The `ytmusicapi` bridge (`scripts/*.py`) is
  gone; `internal/ytmusic` now speaks the InnerTube API directly in Go.
- **Auth in Go.** `internal/auth` uses `kooky` to read browser cookies, with a
  manual-paste fallback. New `ytm auth` command.
- **Playback via `yt-dlp` + `mpv`**, resolved on demand. An early attempt to
  resolve streams in-process (`kkdai/youtube`) tripped YouTube's abuse
  detection, so `yt-dlp` is used for robustness. The bulk "prefetch the whole
  playlist" behavior was also removed for the same reason.
- Renamed to `ytmusic-tui` / binary `ytm`; config lives in
  `~/.config/ytmusic-tui/`.

## Architecture

```
cmd/ytm/main.go        entry point, `auth` subcommand, theme/palette wiring
internal/app          bubbletea model (tabs, queue, resume, lyrics, art)
internal/config       config.toml loading + key-binding overrides
internal/ytmusic      native Go InnerTube client (search, library, playlists)
internal/auth         browser-cookie extraction (kooky) + manual paste
internal/player       mpv JSON-IPC control + stream-URL cache
internal/store        bbolt persistence (exact queue, URL cache, metadata)
internal/playlistio   M3U/CSV export and import
internal/stats        local listening-history aggregation
internal/mpris        MPRIS D-Bus service (media keys / OS control)
internal/pomodoro     work/break focus timer state machine
internal/logger       file logging with credential redaction
```

## Roadmap

- [ ] Search filters (albums / artists / playlists)
- [ ] Add-to-playlist / create-playlist
- [ ] Optional fully self-contained audio engine (drop the `mpv` dependency)

## License

MIT. Original work © 2025 Kunal Jain
([tui-yt](https://github.com/kunalpjain/tui-yt)); fork modifications © 2026
Myleshen Praburam.
