package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Myleshen/yt-music-tui/internal/app"
	"github.com/Myleshen/yt-music-tui/internal/auth"
	"github.com/Myleshen/yt-music-tui/internal/config"
	"github.com/Myleshen/yt-music-tui/internal/discordrpc"
	"github.com/Myleshen/yt-music-tui/internal/doctor"
	"github.com/Myleshen/yt-music-tui/internal/logger"
	"github.com/Myleshen/yt-music-tui/internal/mediactl"
	"github.com/Myleshen/yt-music-tui/internal/player"
	"github.com/Myleshen/yt-music-tui/internal/store"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

var version = "0.1.0"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "auth":
			runAuth(os.Args[2:])
			return
		case "profiles":
			runProfiles()
			return
		case "doctor":
			os.Exit(runDoctor())
		case "version", "--version", "-v":
			fmt.Printf("ytm %s\n", version)
			return
		case "help", "--help", "-h":
			usage()
			return
		}
	}
	runTUI()
}

func usage() {
	fmt.Print(`ytm — YouTube Music in the terminal

Usage:
  ytm                 Launch the player
  ytm doctor          Check dependencies and configuration
  ytm auth [flags]    Sign in using browser cookies
  ytm profiles        List browser profiles with YouTube cookies
  ytm version         Print version

Auth flags:
  --browser <name>    Read cookies from a specific browser
                      (firefox, chrome, brave, edge, chromium, opera, ...)
  --profile <name>    Restrict to a specific profile within that browser
  --paste             Paste the Cookie header manually instead

The chosen browser/profile is saved to [auth] in config.toml.

Examples:
  ytm auth
  ytm auth --browser firefox
  ytm auth --browser chrome --profile "Default"
  ytm auth --paste
`)
}

// applyTheme installs the UI palette according to the ui.theme setting.
//
//	"default" (or the legacy "auto") uses the built-in palette.
//	<name>    uses the named built-in palette (nord, gruvbox, dracula,
//	          tokyo-night).
//
// An unknown name falls back to the built-in palette, so a misconfiguration
// never prevents startup.
func applyTheme(theme string) {
	name := strings.ToLower(strings.TrimSpace(theme))
	switch name {
	case "", "auto", "default", "builtin", "none":
		app.ApplyBuiltin("default")
		slog.Info("theme: using built-in palette")
	default:
		if app.ApplyBuiltin(name) {
			slog.Info("theme: applied built-in palette", "palette", name)
			return
		}
		slog.Warn("theme: unknown theme, using built-in palette", "theme", theme)
		app.ApplyBuiltin("default")
	}
}

func runAuth(args []string) {
	fs := flag.NewFlagSet("auth", flag.ExitOnError)
	browser := fs.String("browser", "", "browser to read cookies from (accepts \"browser/profile\")")
	profile := fs.String("profile", "", "profile within the browser to read cookies from")
	paste := fs.Bool("paste", false, "paste the Cookie header manually")
	_ = fs.Parse(args)

	// Allow the compact "browser/profile" form on --browser.
	b, p := auth.ParseProfileRef(*browser)
	if p != "" {
		*profile = p
	}
	*browser = b

	if err := auth.RunSetup(*browser, *profile, *paste); err != nil {
		fmt.Fprintf(os.Stderr, "auth failed: %v\n", err)
		os.Exit(1)
	}
	// Persist an explicit selection so the next launch re-resolves from the
	// same place. Auto-detected (empty) choices leave the config untouched.
	if *browser != "" || *profile != "" {
		if err := config.SaveAuth(config.DefaultPath(), *browser, *profile); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not save auth choice to config: %v\n", err)
		} else {
			fmt.Printf("Saved auth choice to %s\n", config.DefaultPath())
		}
	}
}

// runProfiles lists the browser profiles that hold YouTube login cookies.
func runProfiles() {
	profiles := auth.ListProfiles()
	if len(profiles) == 0 {
		fmt.Println("No signed-in browser profiles with YouTube cookies found.")
		fmt.Println("Sign in to music.youtube.com in a browser, then re-run.")
		return
	}
	fmt.Println("Browser profiles with YouTube cookies:")
	for _, p := range profiles {
		fmt.Printf("  %s\n", p.Label())
	}
	fmt.Println("\nPick one with:  ytm auth --browser <browser> --profile <profile>")
}

// runDoctor prints the environment report and returns the process exit code:
// non-zero when a required dependency or credential is missing.
func runDoctor() int {
	report := doctor.Run(doctor.Options{Version: version})
	fmt.Print(report)
	return report.ExitCode()
}

func runTUI() {
	closeLog, err := logger.Init()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: logger init: %v\n", err)
	}
	defer closeLog()

	// Load configuration (defaults if absent) and publish a commented template
	// on first run. Credentials in [integrations] are stored but never logged.
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		slog.Warn("could not load config; using defaults", "err", err)
		cfg = config.Default()
	}
	if err := config.WriteDefault(config.DefaultPath()); err != nil {
		slog.Debug("could not write default config", "err", err)
	}
	slog.Info("config loaded", "path", config.DefaultPath(),
		"pomodoro_work", cfg.Pomodoro.WorkMinutes,
		"pomodoro_break", cfg.Pomodoro.BreakMinutes)

	// Apply the built-in UI palette selected by ui.theme.
	applyTheme(cfg.UI.Theme)

	// Resolve credentials, preferring a live browser session so the session
	// never goes stale; fall back to the saved auth file. The [auth] config
	// section selects which browser/profile to read from.
	cookie, source, err := auth.ResolveWith(cfg.Auth.Browser, cfg.Auth.Profile)
	if err != nil {
		auth.PrintSetupInstructions()
		if serr := auth.RunSetup(cfg.Auth.Browser, cfg.Auth.Profile, false); serr != nil {
			fmt.Fprintf(os.Stderr, "auth setup failed: %v\n", serr)
			os.Exit(1)
		}
		cookie, source, err = auth.ResolveWith(cfg.Auth.Browser, cfg.Auth.Profile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "auth error: %v\n", err)
			os.Exit(1)
		}
	}
	if strings.HasPrefix(source, "browser:") {
		if serr := auth.SaveCookie(cookie, source, cfg.Auth.Profile); serr != nil {
			slog.Warn("could not persist refreshed cookie", "err", serr)
		}
	}
	slog.Info("auth resolved", "source", source, "browser", cfg.Auth.Browser, "profile", cfg.Auth.Profile)

	// Materialise a Netscape cookies file so yt-dlp requests are authenticated
	// too (reduces throttling and enables age-restricted tracks).
	cookiesFile := filepath.Join(auth.Dir(), "cookies.txt")
	if err := auth.WriteNetscapeCookies(cookiesFile); err != nil {
		slog.Warn("could not write yt-dlp cookies file; continuing without", "err", err)
		cookiesFile = ""
	}

	// Open persistent store (bbolt).
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		slog.Warn("could not open state DB, continuing without persistence", "err", err)
		fmt.Fprintf(os.Stderr, "warning: could not open state DB: %v (continuing without persistence)\n", err)
		st = nil
	} else {
		slog.Info("state DB opened", "path", store.DefaultPath())
		defer st.Close()
	}

	// Build URL cache — warm it from the DB. When the DB could not be opened
	// (st == nil) the cache must stay memory-only: passing a nil *store.Store
	// through the URLPersister interface would be a non-nil interface holding a
	// nil pointer and panic on the first write-through.
	var persister player.URLPersister
	if st != nil {
		persister = st
	}
	cache := player.NewURLCache(persister)
	warmed := 0
	if st != nil {
		for videoID, entry := range st.LoadAllURLs() {
			if entry.CachedAt.IsZero() {
				continue
			}
			cache.Load(videoID, entry.URL, entry.CachedAt)
			warmed++
		}
	}
	slog.Info("URL cache warmed", "entries", warmed)

	// Load last playing state for resume.
	var resume *store.LastPlayingState
	if st != nil {
		if state, ok := st.LoadLastPlaying(); ok && len(state.Tracks) > 0 {
			resume = &state
			slog.Info("resume state loaded", "tracks", len(state.Tracks), "index", state.Index, "position", state.Position)
		}
	}

	client, err := ytmusic.NewClient(cookie)
	if err != nil {
		slog.Error("failed to create ytmusic client", "err", err)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	slog.Info("ytmusic client ready", "authenticated", client.Authenticated())

	// System media integration (MPRIS on Linux, MPNowPlayingInfoCenter on macOS)
	// is best-effort: when the platform has no support, or the bus/name is taken,
	// the player runs unchanged, just without media-key/OS control.
	mediaSvc, mediaErr := mediactl.New()
	if mediaErr != nil {
		slog.Warn("system media integration unavailable; continuing without media-key support", "err", mediaErr)
		mediaSvc = nil
	}
	if mediaSvc != nil {
		defer mediaSvc.Close()
	}
	// When ytm drives the OS media keys itself, mpv must not also grab them.
	player.DisableMediaKeys = mediaSvc != nil

	// Discord Rich Presence is optional and off by default. It is entirely
	// best-effort: without an app id (or without Discord running) the client
	// simply does nothing.
	var discord *discordrpc.Client
	if cfg.Integrations.DiscordRPC {
		if cfg.Integrations.DiscordAppID == "" {
			slog.Warn("discord_rpc enabled but discord_app_id is empty; skipping RPC")
		} else {
			discord = discordrpc.New(cfg.Integrations.DiscordAppID)
			discord.Start()
			defer discord.Close()
			slog.Info("discord rich presence enabled")
		}
	}

	model := app.New(client, st, cache, resume, cookiesFile, app.Options{
		VolumeStep:    cfg.Playback.VolumeStep,
		SeekStep:      cfg.Playback.SeekStep,
		PrefetchAhead: cfg.Playback.PrefetchAhead,
		ArtSize:       cfg.UI.ArtSize,
		ShowLyrics:    cfg.UI.ShowLyrics,
		Keys:          cfg.KeyOverrides(),
		PomodoroWork:  time.Duration(cfg.Pomodoro.WorkMinutes) * time.Minute,
		PomodoroBreak: time.Duration(cfg.Pomodoro.BreakMinutes) * time.Minute,

		SleepDefaultMinutes: cfg.Playback.SleepDefaultMinutes,
		Gapless:             cfg.Playback.Gapless,
		CrossfadeSeconds:    cfg.Playback.CrossfadeSeconds,
		NormalizeAudio:      cfg.Playback.NormalizeAudio,
		NotifyTrackChange:   cfg.UI.NotifyTrackChange,
		Discord:             discord,
	}, mediaSvc)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Route remote control commands into the running TUI. Set after the program
	// is created so the send callback is available.
	if mediaSvc != nil {
		mediaSvc.SetSend(func(cmd mediactl.Command) { p.Send(cmd) })
	}

	runProgram := func() error {
		_, err := p.Run()
		return err
	}

	// Some backends need the process main thread for their event loop (macOS
	// delivers media keys on the AppKit run loop). In that case the TUI runs on
	// a separate goroutine and the main one is handed over.
	var runErr error
	if runner, ok := mediaSvc.(mediactl.Runner); ok {
		done := make(chan struct{})
		go func() {
			runErr = runProgram()
			runner.StopLoop()
			close(done)
		}()
		runner.RunLoop()
		<-done
	} else {
		runErr = runProgram()
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", runErr)
		os.Exit(1)
	}
}
