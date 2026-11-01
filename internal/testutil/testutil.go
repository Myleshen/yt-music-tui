// Package testutil holds helpers shared by tests in several packages.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// SockPath returns a path for a unix socket inside a fresh temporary directory.
//
// t.TempDir() derives its directory from the test name, so on macOS the result
// can exceed sockaddr_un.sun_path (104 bytes) and bind fails with "invalid
// argument" before any code under test runs. A short MkdirTemp name keeps the
// absolute path inside the limit on both macOS and Linux.
func SockPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ytm")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}
