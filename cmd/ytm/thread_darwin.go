//go:build darwin

package main

import "runtime"

// init pins the main goroutine to the process main thread. AppKit — and with it
// MPRemoteCommandCenter, which is where the media keys arrive — only works
// there, so when the macOS now-playing backend is active the TUI moves to a
// separate goroutine and this one is handed to the AppKit run loop.
func init() { runtime.LockOSThread() }
