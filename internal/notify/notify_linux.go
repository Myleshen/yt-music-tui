//go:build !darwin && !windows

package notify

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/godbus/dbus/v5"
)

// icon is the freedesktop icon name used for notifications.
const icon = "audio-x-generic"

// backendPlatform names the Linux delivery mechanisms.
func backendPlatform() string { return "notify-send / org.freedesktop.Notifications" }

// availablePlatform reports whether notify-send exists or the session bus is
// reachable, which is all sendPlatform needs to attempt delivery.
func availablePlatform() error {
	if _, err := exec.LookPath("notify-send"); err == nil {
		return nil
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("notify: no notify-send and %w", err)
	}
	return conn.Close()
}

// sendPlatform prefers the notify-send(1) helper when it is on PATH and
// otherwise falls back to the freedesktop.org D-Bus notification API, so it
// works on minimal systems without libnotify.
func sendPlatform(summary, body string) error {
	var cliErr error
	if path, err := exec.LookPath("notify-send"); err == nil {
		cliErr = sendCLI(path, summary, body)
		if cliErr == nil {
			return nil
		}
	}
	if err := sendDBus(summary, body); err != nil {
		if cliErr != nil {
			return fmt.Errorf("notify: notify-send failed (%v) and %w", cliErr, err)
		}
		return err
	}
	return nil
}

// sendCLI runs notify-send with a context deadline so a stalled helper cannot
// block indefinitely.
func sendCLI(path, summary, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--app-name="+appName, "--icon="+icon, summary, body)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("notify: notify-send: %w", err)
	}
	return nil
}

// sendDBus delivers the notification through org.freedesktop.Notifications.
func sendDBus(summary, body string) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("notify: connect session bus: %w", err)
	}
	defer conn.Close()

	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	call := obj.CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0,
		appName,    // app_name
		uint32(0),  // replaces_id
		icon,       // app_icon
		summary,    // summary
		body,       // body
		[]string{}, // actions
		map[string]dbus.Variant{
			"desktop-entry": dbus.MakeVariant(appName),
		}, // hints
		int32(timeout/time.Millisecond), // expire_timeout (ms)
	)
	if call.Err != nil {
		return fmt.Errorf("notify: dbus Notify: %w", call.Err)
	}
	return nil
}
