//go:build !darwin

package player

// platformArgs returns mpv flags that only apply on other platforms. There are
// none today: the media-key opt-out is macOS-only.
func platformArgs() []string { return nil }
