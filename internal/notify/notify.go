// Package notify sends best-effort desktop notifications.
//
// Each platform has its own delivery mechanism — notify-send(1) or the
// freedesktop.org D-Bus API on Linux, AppleScript (or terminal-notifier) on
// macOS — and every failure is returned as an error. Notifications are a
// convenience, never a correctness requirement, so callers only log the error.
package notify

import "time"

// timeout bounds a single notification attempt so a hung notifier can never
// stall the caller.
const timeout = 3 * time.Second

// appName is the application name shown by the notification service.
const appName = "ytm"

// Send delivers a desktop notification with the given summary and body. It
// returns nil when the notification was handed off successfully (or, more
// often, best-effort accepted); errors describe why nothing was shown.
func Send(summary, body string) error {
	return sendPlatform(summary, body)
}

// Backend names the delivery mechanism this build uses.
func Backend() string {
	return backendPlatform()
}

// Available reports whether notifications can be delivered on this platform,
// without actually sending one. It is used by `ytm doctor`.
func Available() error {
	return availablePlatform()
}
