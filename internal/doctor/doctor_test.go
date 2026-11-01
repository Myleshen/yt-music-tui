package doctor

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// healthyOptions wires every check to a passing fake in a temp directory.
func healthyOptions(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()
	return Options{
		Version:    "test",
		ConfigPath: filepath.Join(dir, "config.toml"),
		Dir:        dir,
		LogPath:    filepath.Join(dir, "ytm.log"),
		LookPath: func(name string) (string, error) {
			return "/usr/bin/" + name, nil
		},
		VersionOf: func(path string) (string, error) {
			if strings.HasSuffix(path, "mpv") {
				return "mpv 0.39.0", nil
			}
			return "2025.06.09", nil
		},
		ResolveAuth: func(_, _ string) (string, error) { return "browser:firefox", nil },
		ProbeMedia:  func() (string, error) { return "MPRIS (test)", nil },
		ProbeNotify: func() (string, error) { return "notify-send (test)", nil },
	}
}

func statusOf(t *testing.T, report Report, name string) Result {
	t.Helper()
	for _, res := range report.Results {
		if res.Name == name {
			return res
		}
	}
	t.Fatalf("report has no %q check: %+v", name, report.Results)
	return Result{}
}

func TestRunAllChecksPass(t *testing.T) {
	report := Run(healthyOptions(t))

	if got := report.ExitCode(); got != 0 {
		t.Fatalf("ExitCode = %d, want 0; report:\n%s", got, report)
	}
	if ok, warn, fail := report.Counts(); ok != 7 || warn != 0 || fail != 0 {
		t.Errorf("Counts = (%d, %d, %d), want (7, 0, 0); report:\n%s", ok, warn, fail, report)
	}

	if res := statusOf(t, report, "credentials"); res.Detail != "live browser session (firefox)" {
		t.Errorf("credentials detail = %q", res.Detail)
	}
	if res := statusOf(t, report, "mpv"); res.Detail != "mpv 0.39.0 (/usr/bin/mpv)" {
		t.Errorf("mpv detail = %q", res.Detail)
	}
	if res := statusOf(t, report, "yt-dlp"); res.Detail != "2025.06.09 (/usr/bin/yt-dlp)" {
		t.Errorf("yt-dlp detail = %q", res.Detail)
	}
	if res := statusOf(t, report, "config"); res.Status != StatusOK {
		t.Errorf("config status = %v, want ok", res.Status)
	}
	if res := statusOf(t, report, "log"); res.Status != StatusInfo {
		t.Errorf("log status = %v, want info", res.Status)
	}
}

func TestRunReportsMissingRequirements(t *testing.T) {
	opts := healthyOptions(t)
	opts.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	opts.ResolveAuth = func(_, _ string) (string, error) { return "", errors.New("no credentials found") }
	opts.ProbeMedia = func() (string, error) { return "none", errors.New("no bus") }
	opts.ProbeNotify = func() (string, error) { return "none", errors.New("no notifier") }

	report := Run(opts)

	if got := report.ExitCode(); got != 1 {
		t.Fatalf("ExitCode = %d, want 1; report:\n%s", got, report)
	}
	for _, name := range []string{"credentials", "mpv", "yt-dlp"} {
		if res := statusOf(t, report, name); res.Status != StatusFail {
			t.Errorf("%s status = %v, want fail (detail %q)", name, res.Status, res.Detail)
		}
	}
	// Missing media keys or notifications must never stop the player.
	for _, name := range []string{"media keys", "notifications"} {
		if res := statusOf(t, report, name); res.Status != StatusWarn {
			t.Errorf("%s status = %v, want warn", name, res.Status)
		}
	}
	if res := statusOf(t, report, "credentials"); !strings.Contains(res.Detail, "run `ytm auth`") {
		t.Errorf("credentials detail %q should point at `ytm auth`", res.Detail)
	}
	if res := statusOf(t, report, "mpv"); !strings.Contains(res.Detail, "install mpv") {
		t.Errorf("mpv detail %q should say how to install mpv", res.Detail)
	}
}

func TestRunWarnsOnInvalidConfig(t *testing.T) {
	opts := healthyOptions(t)
	if err := os.WriteFile(opts.ConfigPath, []byte("[[[ not toml"), 0o644); err != nil {
		t.Fatal(err)
	}

	report := Run(opts)

	res := statusOf(t, report, "config")
	if res.Status != StatusWarn {
		t.Fatalf("config status = %v, want warn; detail %q", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, "defaults will be used") {
		t.Errorf("config detail %q should mention the fallback", res.Detail)
	}
	// A malformed file is not fatal: the TUI starts with defaults.
	if got := report.ExitCode(); got != 0 {
		t.Errorf("ExitCode = %d, want 0", got)
	}
}

func TestRunFailsOnUnwritableDir(t *testing.T) {
	opts := healthyOptions(t)
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	opts.Dir = blocker
	opts.ConfigPath = filepath.Join(blocker, "config.toml")

	report := Run(opts)

	if res := statusOf(t, report, "config dir"); res.Status != StatusFail {
		t.Fatalf("config dir status = %v, want fail (detail %q)", res.Status, res.Detail)
	}
	if got := report.ExitCode(); got != 1 {
		t.Errorf("ExitCode = %d, want 1", got)
	}
}

func TestReportStringRendersSummary(t *testing.T) {
	report := Run(healthyOptions(t))
	out := report.String()

	for _, want := range []string{"ytm doctor test", "platform ", "[  ok]", "[info]", "7 passed, 0 warnings, 0 failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered report missing %q:\n%s", want, out)
		}
	}
}
