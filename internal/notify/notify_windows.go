//go:build windows

package notify

import "fmt"

// backendPlatform names the Windows delivery mechanism, of which there is none.
func backendPlatform() string { return "none" }

// availablePlatform reports that notifications are not wired up on Windows yet.
func availablePlatform() error {
	return fmt.Errorf("notify: desktop notifications are not supported on windows")
}

// sendPlatform reports that notifications are not wired up on Windows yet. The
// caller logs it and carries on, exactly as with a missing notifier on Linux.
func sendPlatform(summary, body string) error {
	return availablePlatform()
}
