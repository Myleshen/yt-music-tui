# macOS first-run checklist

Everything for macOS was written and cross-compiled on Linux — none of it has
run on a real Mac yet. Work through this in order and tick boxes as they pass.
If a step fails, capture what's listed under "When something fails".

## 1. Prerequisites

- [ ] `xcode-select -p` returns a path (Go needs the command line tools)
- [ ] `go version` (1.26+)
- [ ] `brew install mpv yt-dlp`

## 2. Update, test, build

```sh
git pull            # need cfd7f17 or newer
go test ./...       # first time the darwin packages are built and linked
make macos          # -> build/ytm.app
```

- [ ] `go test ./...` passes
- [ ] `make macos` prints `built build/ytm.app`

## 3. Doctor

```sh
./build/ytm.app/Contents/MacOS/ytm doctor
```

Expect exit code 0 and, in particular:

- [ ] `media keys` says `MPNowPlayingInfoCenter (MediaPlayer)`
      (a warning here means the purego/objc load failed — the message is the bug)
- [ ] `notifications` says `Notification Center (osascript)`
- [ ] `mpv` / `yt-dlp` report versions and `/opt/homebrew/bin/...` paths
- [ ] `credentials` is OK, or run `./build/ytm.app/Contents/MacOS/ytm auth` first

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
