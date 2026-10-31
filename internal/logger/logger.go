// Package logger configures structured logging for ytm.
//
// Because ytm occupies the terminal, all log output goes to a file
// (~/.config/ytmusic-tui/ytm.log) rather than stdout/stderr.
// Call Init() once in main; everywhere else use the standard slog package.
package logger

import (
	"log/slog"
	"os"
	"path/filepath"
)

// Init opens (or creates) the log file and sets it as the default slog handler.
// It returns a close function that must be called on shutdown (defer it in main).
// If the log file cannot be opened, logging falls back to stderr.
func Init() (close func(), err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return func() {}, err
	}

	logDir := filepath.Join(home, ".config", "ytmusic-tui")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return func() {}, err
	}

	logPath := filepath.Join(logDir, "ytm.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		// Fall back to stderr so the app still runs
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
		return func() {}, err
	}

	handler := slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		// Redact any attribute key that looks like it carries auth material.
		ReplaceAttr: redactSensitive,
	})
	slog.SetDefault(slog.New(handler))
	slog.Info("ytm starting", "log", logPath)

	return func() { f.Close() }, nil
}

// sensitiveKeys are log attribute keys whose values are always redacted.
var sensitiveKeys = map[string]bool{
	"cookie":        true,
	"authorization": true,
	"token":         true,
	"auth":          true,
	"password":      true,
	"secret":        true,
}

// redactSensitive replaces the value of any attribute whose key matches a
// known-sensitive name with "[REDACTED]", preventing accidental credential
// leakage into the log file.
func redactSensitive(groups []string, a slog.Attr) slog.Attr {
	if sensitiveKeys[a.Key] {
		return slog.String(a.Key, "[REDACTED]")
	}
	return a
}
