//go:build darwin

package player

// platformArgs returns the mpv flags that only make sense on macOS. mpv grabs
// the system media keys by default (--input-media-keys=yes); when ytm's own
// now-playing backend is driving them, mpv must stand down or both would react
// to every key press.
func platformArgs() []string {
	if DisableMediaKeys {
		return []string{"--input-media-keys=no"}
	}
	return nil
}
