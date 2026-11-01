//go:build windows

// The Windows build has no "now playing" integration: Windows exposes media
// controls through SMTC (System Media Transport Controls), which needs WinRT
// bindings the project does not carry. Report it as unavailable so the caller
// logs it once and the player runs unchanged.

package mediactl

import "fmt"

// Backend names the platform integration this build uses.
func Backend() string { return "none" }

// Probe always reports that media integration is unavailable on Windows.
func Probe() error {
	return fmt.Errorf("mediactl: no system media integration on %s", "windows")
}

// New always reports that media integration is unavailable on Windows.
func New() (Service, error) {
	return nil, Probe()
}
