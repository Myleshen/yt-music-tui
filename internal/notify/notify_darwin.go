//go:build darwin

package notify

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// backendPlatform names the macOS delivery mechanism.
func backendPlatform() string { return "Notification Center (osascript)" }

// availablePlatform reports whether a notifier is present: terminal-notifier
// when installed, otherwise the system osascript that ships with macOS.
func availablePlatform() error {
	if _, err := exec.LookPath("terminal-notifier"); err == nil {
		return nil
	}
	if _, err := os.Stat("/usr/bin/osascript"); err != nil {
		return fmt.Errorf("notify: osascript unavailable: %w", err)
	}
	return nil
}

// sendPlatform delivers a notification through AppleScript, which needs no
// helper and is the only route that exists on macOS (both notify-send and the
// freedesktop D-Bus API are Linux-only). terminal-notifier is used when it is
// installed because it shows the notification under its own name instead of
// being attributed to the current terminal.
func sendPlatform(summary, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if path, err := exec.LookPath("terminal-notifier"); err == nil {
		cmd := exec.CommandContext(ctx, path,
			"-title", appName, "-message", body, "-group", appName)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// `display notification <body> with title <title>` — the body is the
	// message and the title is the bold first line in Notification Center.
	script := fmt.Sprintf("display notification %s with title %s",
		quoteAppleScript(body), quoteAppleScript(summary))
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("notify: osascript: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
