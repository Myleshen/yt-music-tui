# macOS first-run checklist

Everything for macOS was written and cross-compiled on Linux — none of it has
run on a real Mac yet. Work through this in order and tick boxes as they pass.
If a step fails, capture what's listed under "When something fails".

## 1. Prerequisites

- [x] `xcode-select -p` returns a path (Go needs the command line tools)
- [x] `go version` (1.26+) — 1.27.1 darwin/arm64
- [x] `brew install mpv yt-dlp` — mpv 0.41.0, yt-dlp 2026.10.26

## 2. Update, test, build

```sh
git pull            # need cfd7f17 or newer
go test ./...       # first time the darwin packages are built and linked
make macos          # -> build/ytm.app
```

- [x] `go test ./...` passes
- [x] `make macos` prints `built build/ytm.app`

## 3. Doctor

```sh
./build/ytm.app/Contents/MacOS/ytm doctor
```

Expect exit code 0 and, in particular:

- [x] `media keys` says `MPNowPlayingInfoCenter (MediaPlayer)`
      (a warning here means the purego/objc load failed — the message is the bug;
      on the first run this was a panic, see "Findings" below)
- [x] `notifications` says `Notification Center (osascript)`
- [x] `mpv` / `yt-dlp` report versions and `/opt/homebrew/bin/...` paths
- [x] `credentials` is OK, or run `./build/ytm.app/Contents/MacOS/ytm auth` first
      (done with `--browser zen`)

## 4. Basic run

```sh
./build/ytm.app/Contents/MacOS/ytm
# in another shell:
tail -f ~/.config/ytmusic-tui/ytm.log
```

- [ ] TUI starts and the library loads
- [ ] a track plays with sound, seek/volume work
- [ ] the log contains `macOS now-playing backend active`
- [ ] no panic/goroutine noise in the log while playing a few tracks

## 5. New macOS behaviour (the point of the port)

Media keys / Now Playing:

- [ ] Control Center's Now Playing tile shows title and artist
- [ ] album art appears in the tile
- [ ] play/pause from the tile controls ytm
- [ ] next/previous from the tile work
- [ ] dragging the position slider seeks
- [ ] hardware play/pause (F8) and AirPods/Bluetooth controls work
      (if dead: press play once on the ytm tile in Control Center, retry —
      macOS arbitrates the keys between apps)
- [ ] quitting ytm clears the Now Playing entry

Notifications:

- [ ] changing tracks shows `▶ <title>` / artist
- [ ] nothing is sent when `notify_track_change = false`

Sockets:

- [ ] a second instance starts and both play (each uses
      `$TMPDIR/ytm-mpv-<pid>.sock` instead of the old fixed `/tmp/ytm-mpv.sock`)
- [ ] `ls $TMPDIR/ytm-mpv-*.sock` shows one per running instance and they
      disappear on a clean quit

## 6. When something fails

Attach these to the report:

1. full `ytm doctor` output and its exit code
2. `~/.config/ytmusic-tui/ytm.log` around the failure (stderr only covers
   early startup; later logs go to the file)
3. `sw_vers`, `uname -m`, `mpv --version | head -1`
4. the step number above, plus what you saw instead of what was expected

Known non-bugs:

- config still lives in `~/.config/ytmusic-tui` on macOS, not
  `~/Library/Application Support` — works, just non-native
- the bundle is unsigned and not notarised; Gatekeeper only complains after
  download/quarantine, a local `make macos` build is fine
- Windows mpv IPC is still unsupported (unrelated to macOS)

## 7. Follow-ups (after the first run works)

- [ ] move config/cache to `~/Library/Application Support` on darwin
- [ ] codesign (and notarise) the bundle if media keys or Gatekeeper misbehave
- [ ] Homebrew formula / tap so `brew install ytm` works

## 8. Findings from the first run (2026-11-01)

Two darwin-only bugs blocked steps 2 and 3. Both are fixed in the working tree;
neither was reachable on Linux.

1. **`ytm doctor` panicked** rather than warning:
   `panic: dlsym(..., dispatch_get_main_queue): symbol not found`.
   `dispatch_get_main_queue()` is a macro for `&_dispatch_main_q` on macOS, not
   an exported function, and `purego.RegisterLibFunc` panics on a missing
   symbol — which breaks the "every failure is non-fatal" contract in
   `internal/mediactl/darwin.go`. The loader now uses the `_dispatch_main_q`
   object's address as the queue and binds each entry point through a
   `resolveFunc` helper that returns an error instead of panicking.

2. **Three socket tests failed with `bind: invalid argument`.** macOS caps
   `sockaddr_un.sun_path` at 104 bytes and `t.TempDir()` embeds the test name,
   pushing paths such as
   `.../TestClientHandshakeAndActivity2612919615/001/discord-ipc-0` to 107
   bytes. Added `internal/testutil.SockPath`, which allocates a short
   `MkdirTemp` directory, and moved the mpv and Discord IPC fakes onto it.
   The mpv case was still hidden at first because mpv was not yet installed and
   the test skips without it.

Still open: everything from step 4 on, which needs someone in front of the
running TUI.

3. **Zen (and Firefox) profiles were invisible on macOS.** `firefoxProfileDBs`
   only globbed Linux roots (`~/.mozilla`, `~/.config/...`, `~/.librewolf`), so
   on macOS `ytm profiles` found nothing — Firefox-family browsers keep their
   profiles under `~/Library/Application Support/<App>/Profiles`, and kooky has
   no finder for Zen at all. Added the macOS roots plus Zen:
   `~/Library/Application Support/zen/Profiles`, reported as
   `zen · h6jee2w6.Default (release)`. Covered by
   `TestFirefoxProfileDBs_MacLayout`.

Caveat worth knowing: `browserutils/sqlite3` reads only the main `.sqlite`
file, ignoring `-wal`. A cookie written since the browser's last checkpoint can
be missed until it is flushed; retry after the browser checkpoints or quits.
