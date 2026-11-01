// Package doctor implements `ytm doctor`, the environment and dependency check.
//
// The command answers the questions people ask when the player misbehaves or
// when setting it up on a new machine: can the config be parsed, is there a
// usable YouTube session, are mpv and yt-dlp installed, will media keys and
// notifications work on this OS, and is the config directory writable. It sends
// no notifications and claims no OS resources, so it is safe to run at any
// time — including while the player is running.
package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Myleshen/yt-music-tui/internal/auth"
	"github.com/Myleshen/yt-music-tui/internal/config"
	"github.com/Myleshen/yt-music-tui/internal/mediactl"
	"github.com/Myleshen/yt-music-tui/internal/notify"
)

// versionTimeout bounds each `<tool> --version` probe so a hung helper cannot
// stall the command.
const versionTimeout = 3 * time.Second

// Status is the outcome of a single check.
type Status int

const (
	// StatusOK means the check passed and the feature will work.
	StatusOK Status = iota
	// StatusWarn means something is missing but the player still starts.
	StatusWarn
	// StatusFail means a required piece is missing or broken.
	StatusFail
	// StatusInfo is a non-check, reported for orientation only.
	StatusInfo
)

// tag returns the short marker shown in the report.
func (s Status) tag() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	default:
		return "info"
	}
}

// Result is one line of the report.
type Result struct {
	Name   string
	Status Status
	Detail string
}

// Report is the full outcome of Run.
type Report struct {
	Version   string
	GOOS      string
	GOARCH    string
	GoVersion string
	Results   []Result
}

// Counts returns how many checks passed, warned, and failed. Info results are
// not counted.
func (r Report) Counts() (ok, warn, fail int) {
	for _, res := range r.Results {
		switch res.Status {
		case StatusOK:
			ok++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		}
	}
	return ok, warn, fail
}

// ExitCode is 1 when any check failed, so scripts and users get a signal.
func (r Report) ExitCode() int {
	if _, _, fail := r.Counts(); fail > 0 {
		return 1
	}
	return 0
}

// String renders the report with aligned status tags and a summary line.
func (r Report) String() string {
	var b strings.Builder
	version := r.Version
	if version == "" {
		version = "dev"
	}
	fmt.Fprintf(&b, "ytm doctor %s\n", version)
	fmt.Fprintf(&b, "platform %s/%s %s\n\n", r.GOOS, r.GOARCH, r.GoVersion)

	width := 0
	for _, res := range r.Results {
		if n := len(res.Name); n > width {
			width = n
		}
	}
	for _, res := range r.Results {
		fmt.Fprintf(&b, "  [%4s] %-*s  %s\n", res.Status.tag(), width, res.Name, res.Detail)
	}

	ok, warn, fail := r.Counts()
	fmt.Fprintf(&b, "\n%d passed, %d warnings, %d failed\n", ok, warn, fail)
	return b.String()
}

// Options overrides the environment Run inspects. Zero values select the real
// implementation; tests fill in fakes so they never touch the network, D-Bus,
// or the user's browser cookie stores.
type Options struct {
	Version     string
	ConfigPath  string
	Dir         string // directory holding the config, log and state DB
	LogPath     string
	LookPath    func(string) (string, error)
	VersionOf   func(path string) (string, error)
	ResolveAuth func(browser, profile string) (source string, err error)
	ProbeMedia  func() (name string, err error)
	ProbeNotify func() (name string, err error)
}

// Run performs every check and returns the report.
func Run(opts Options) Report {
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	dir := opts.Dir
	if dir == "" {
		dir = filepath.Dir(configPath)
	}
	logPath := opts.LogPath
	if logPath == "" {
		logPath = filepath.Join(dir, "ytm.log")
	}
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	versionOf := opts.VersionOf
	if versionOf == nil {
		versionOf = toolVersion
	}
	resolveAuth := opts.ResolveAuth
	if resolveAuth == nil {
		resolveAuth = func(browser, profile string) (string, error) {
			_, source, err := auth.ResolveWith(browser, profile)
			return source, err
		}
	}
	probeMedia := opts.ProbeMedia
	if probeMedia == nil {
		probeMedia = func() (string, error) {
			return mediactl.Backend(), mediactl.Probe()
		}
	}
	probeNotify := opts.ProbeNotify
	if probeNotify == nil {
		probeNotify = func() (string, error) {
			return notify.Backend(), notify.Available()
		}
	}

	var results []Result
	add := func(name string, status Status, detail string) {
		results = append(results, Result{Name: name, Status: status, Detail: detail})
	}

	cfg, cfgErr := config.Load(configPath)
	if cfgErr != nil {
		// The TUI warns and continues with defaults, so a bad file is a
		// warning here too rather than a failure.
		add("config", StatusWarn, fmt.Sprintf("%v — defaults will be used", cfgErr))
		cfg = config.Default()
	} else if _, err := os.Stat(configPath); err == nil {
		add("config", StatusOK, configPath)
	} else {
		add("config", StatusOK, configPath+" (not created yet; defaults will be used)")
	}

	if source, err := resolveAuth(cfg.Auth.Browser, cfg.Auth.Profile); err != nil {
		add("credentials", StatusFail, fmt.Sprintf("%v — run `ytm auth`", err))
	} else if browser, ok := strings.CutPrefix(source, "browser:"); ok {
		add("credentials", StatusOK, "live browser session ("+browser+")")
	} else {
		add("credentials", StatusOK, "saved cookie ("+source+")")
	}

	// mpv and yt-dlp are hard requirements: without them nothing plays.
	checkTool := func(name, hint string, path string, err error) {
		if err != nil {
			add(name, StatusFail, "not found on PATH — "+hint)
			return
		}
		detail := path
		if ver, verr := versionOf(path); verr == nil && ver != "" {
			detail = ver + " (" + path + ")"
		}
		add(name, StatusOK, detail)
	}
	mpvPath, mpvErr := lookPath("mpv")
	checkTool("mpv", "install mpv (brew install mpv)", mpvPath, mpvErr)
	ytdlpPath, ytdlpErr := lookPath("yt-dlp")
	checkTool("yt-dlp", "install yt-dlp (brew install yt-dlp)", ytdlpPath, ytdlpErr)

	if name, err := probeMedia(); err != nil {
		add("media keys", StatusWarn, "unavailable: "+err.Error()+"; the player runs without OS media control")
	} else {
		add("media keys", StatusOK, name)
	}
	if name, err := probeNotify(); err != nil {
		add("notifications", StatusWarn, "unavailable: "+err.Error())
	} else {
		add("notifications", StatusOK, name)
	}

	if err := ensureWritable(dir); err != nil {
		add("config dir", StatusFail, err.Error())
	} else {
		add("config dir", StatusOK, dir)
	}
	if _, err := os.Stat(logPath); err == nil {
		add("log", StatusInfo, logPath)
	} else {
		add("log", StatusInfo, logPath+" (created on launch)")
	}

	return Report{
		Version:   opts.Version,
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
		GoVersion: runtime.Version(),
		Results:   results,
	}
}

// ensureWritable creates dir if needed and proves it is writable with a
// throwaway file, which also catches permission problems on an existing dir.
func ensureWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("not writable: %w", err)
	}
	f, err := os.CreateTemp(dir, ".ytm-doctor-*")
	if err != nil {
		return fmt.Errorf("not writable: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("not writable: %w", err)
	}
	return nil
}

// toolVersion runs `<path> --version` and returns its first line with mpv's
// trailing copyright banner removed.
func toolVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	// mpv prints "mpv 0.39.0 Copyright © …" on its first line.
	if i := strings.Index(line, " Copyright"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	return line, nil
}
