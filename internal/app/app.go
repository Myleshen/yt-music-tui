package app

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Myleshen/yt-music-tui/internal/config"
	"github.com/Myleshen/yt-music-tui/internal/discordrpc"
	"github.com/Myleshen/yt-music-tui/internal/mpris"
	"github.com/Myleshen/yt-music-tui/internal/notify"
	"github.com/Myleshen/yt-music-tui/internal/player"
	"github.com/Myleshen/yt-music-tui/internal/pomodoro"
	"github.com/Myleshen/yt-music-tui/internal/stats"
	"github.com/Myleshen/yt-music-tui/internal/store"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// streamResolver resolves a playable stream URL for a watch URL. It is a var so
// tests can inject a deterministic resolver (e.g. to exercise URL refresh).
var streamResolver = extractStreamURL

// sendNotification delivers a desktop notification. It is a var so tests can
// substitute a recorder.
var sendNotification = notify.Send

// Radio/autoplay tuning: keep the queue growing without hammering the API.
const (
	// radioAppendMax caps how many fresh tracks one radio fetch may append.
	radioAppendMax = 25
	// radioTotalMax caps how many tracks autoplay may add over a session so the
	// queue cannot grow without bound.
	radioTotalMax = 200
	// radioCooldown throttles repeated fetches seeded by the same track.
	radioCooldown = 20 * time.Second
)

// upNextMinWidth is the narrowest terminal that lays out all of Now Playing:
// album art, the track header, lyrics, and the Up Next queue panel. Below it
// the layout prioritises Up Next (lyrics stay toggleable with `y`).
const upNextMinWidth = 110

// maxUpNextRows caps how many upcoming tracks the Up Next panel renders.
const maxUpNextRows = 15

// suggestionDebounce is how long typing pauses before a live suggestions fetch
// is issued, so a fast typist triggers one request rather than one per key.
const suggestionDebounce = 180 * time.Millisecond

// Tab represents the active tab.
type Tab int

const (
	TabNowPlaying Tab = iota
	TabSearch
	TabLibrary
	TabPlaylists
	TabHome
	TabPage
	TabStats
)

// pageKind identifies what a pushed TabPage is showing.
type pageKind int

const (
	pageNone pageKind = iota
	pageAlbum
	pageArtist
	pagePlaylist
)

// Message types for async operations.

type tickMsg time.Time

type searchResultMsg struct {
	tracks []ytmusic.Track
	items  []ytmusic.BrowseItem
	filter ytmusic.SearchFilter
	err    error
}

type playlistsMsg struct {
	playlists []ytmusic.Playlist
	err       error
}

type libraryMsg struct {
	tracks []ytmusic.Track
	err    error
}

type playlistTracksMsg struct {
	tracks []ytmusic.Track
	title  string
	err    error
}

// homeMsg carries the assembled Discover feed (Home + Moods + Charts + New
// Releases sections).
type homeMsg struct {
	sections []ytmusic.Section
	err      error
}

// homeCollectionMsg carries the result of opening a collection tile for the
// Discover sub-view (a playlist/album's tracks, or a mood's playlist shelves).
type homeCollectionMsg struct {
	title    string
	sections []ytmusic.Section
	err      error
}

// pageAlbumMsg / pageArtistMsg carry a pushed artist/album browse page.
type pageAlbumMsg struct {
	page ytmusic.AlbumPage
	err  error
}

type pageArtistMsg struct {
	page ytmusic.ArtistPage
	err  error
}

// pageTracksMsg carries the tracks of a playlist opened as a pushed page.
type pageTracksMsg struct {
	tracks     []ytmusic.Track
	title      string
	playlistID string
	err        error
}

// createPlaylistMsg reports the result of creating a playlist.
type createPlaylistMsg struct {
	id    string
	title string
	err   error
}

// playlistEditMsg reports the result of adding/removing playlist items.
type playlistEditMsg struct {
	action         string // "add" or "remove"
	status         string
	err            error
	playlistID     string // context playlist (for local list updates)
	removedVideoID string // for action == "remove"
}

// mpvStartedMsg is returned when mpv has been started with a new stream.
type mpvStartedMsg struct {
	mpv          *player.MPV
	queue        *player.Queue
	fromPlaylist string
	err          error
}

// prefetchDoneMsg is a no-op sent when background prefetch finishes.
type prefetchDoneMsg struct{}

// mpvLoadMsg is returned when loading a new URL into existing mpv.
type mpvLoadMsg struct {
	url   string
	title string
	err   error
}

// radioMsg carries the result of fetching a radio/mix for a seed track.
type radioMsg struct {
	tracks []ytmusic.Track
	err    error
}

// suggestionsMsg carries live search suggestions back to the UI. gen is the
// generation of the request so stale responses (from earlier keystrokes) can be
// dropped.
type suggestionsMsg struct {
	query string
	gen   int
	sugg  []string
	err   error
}

// suggestionTickMsg fires after the debounce interval to trigger a suggestions
// fetch for the query captured when the timer was scheduled.
type suggestionTickMsg struct {
	query string
	gen   int
}

// osdHideMsg tells the model to hide the transient OSD, but only if its
// generation still matches (so a newer OSD is not hidden early).
type osdHideMsg struct{ gen int }

// ratingMsg carries the result of a like/dislike API call. prev is the rating
// to roll back to when the call fails.
type ratingMsg struct {
	videoID string
	prev    ytmusic.Rating
	err     error
}

// Model is the root bubbletea model.
type Model struct {
	keys     KeyMap
	width    int
	height   int
	tab      Tab
	showHelp bool
	status   string

	// Search tab
	searchInput   textinput.Model
	searchMode    bool
	searchFilter  ytmusic.SearchFilter
	searchResults []ytmusic.Track
	searchItems   []ytmusic.BrowseItem
	searchList    list.Model
	// Live suggestions and recent-search history shown while the search box is
	// active. searchOptions is whichever list is currently on screen (history
	// when the box is empty, suggestions otherwise) and searchCursor indexes it
	// (-1 = nothing highlighted, so Enter runs the typed query).
	searchHistory  []string
	searchOptions  []string
	searchHistMode bool
	searchCursor   int
	suggestions    []string
	suggGen        int
	suggLoading    bool

	// Library tab
	libraryTracks  []ytmusic.Track
	libraryList    list.Model
	libraryLoaded  bool
	libraryLoading bool // a background refresh is in flight

	// Playlists tab
	playlists        []ytmusic.Playlist
	playlistList     list.Model
	playlistTracks   []ytmusic.Track
	trackList        list.Model
	playlistLoaded   bool
	playlistsLoading bool // a background refresh is in flight
	inPlaylist       bool
	playlistID       string // ID of the playlist currently open (for edits)

	// Pushed browse page (artist/album/playlist opened from a track or search)
	pageKind     pageKind
	pageOrigin   Tab
	pageTitle    string
	pageSubtitle string
	pageList     list.Model
	pagePlaylist string // playlist ID when pageKind == pagePlaylist

	// Type-to-filter/jump overlay (operates on the active list)
	filterMode  bool
	filterInput textinput.Model
	filterBase  []list.Item

	// Add-to-playlist picker overlay
	pickMode       bool
	pickList       list.Model
	pickTrack      ytmusic.Track // track the picker will add
	pendingPick    bool          // load playlists, then open the picker
	pendingPickTrk ytmusic.Track // track the deferred picker will add

	// Create-playlist name prompt
	nameMode  bool
	nameInput textinput.Model

	// Destructive-action confirmation
	confirmMode   bool
	confirmPrompt string
	confirmCmd    tea.Cmd

	// Home / Discover tab. homeViews is a stack: index 0 is the feed, and each
	// further entry is a drilled-in collection; the last is the active view.
	homeViews   []homeView
	homeLoaded  bool
	homeLoading bool

	// Player
	mpv           *player.MPV
	queue         *player.Queue
	playerState   player.State
	autoAdvancing bool   // true while resolving the next track's stream URL
	looping       bool   // single-track loop
	fromPlaylist  string // playlist name if track was queued from a playlist

	// Config-derived tuning.
	volumeStep    int // percent per volume key press
	seekStep      int // seconds per seek key press
	prefetchAhead int // upcoming tracks to warm the stream-URL cache for

	// Audio pipeline (applied when mpv starts).
	gapless          bool
	crossfadeSeconds float64
	normalizeAudio   bool

	// Sleep timer.
	sleep        SleepTimer
	sleepMode    bool          // sleep-timer menu is open
	sleepDefault time.Duration // duration offered for the "default" choice

	// Desktop notifications & Discord Rich Presence.
	notifyTrackChange bool
	discord           *discordrpc.Client
	discordPlaying    bool
	discordInit       bool

	// Expired-URL recovery: videoIDs already retried once after a load failure.
	retried map[string]bool

	// Radio/autoplay: appended to the queue when the queue runs out.
	radioLoading  bool
	radioSource   string // seed videoID of the last radio fetch
	radioAppended int    // tracks autoplay has appended this session
	lastRadioAt   time.Time

	// Album art (Now Playing). artCache is keyed by videoID@width; the render
	// depends on terminal width, so a resize produces a new key.
	artCache   map[string]string
	artLoading string // videoID whose art is currently being fetched

	// Lyrics overlay
	showLyrics    bool
	lyrics        []lyricLine
	lyricsSynced  bool
	lyricsFollow  bool // auto-scroll to the active synced line
	lyricsLoading bool
	lyricsNoMatch bool
	lyricsVideoID string
	lyricsScroll  int
	lyricsCache   map[string]Lyrics

	// Command palette
	paletteOpen    bool
	paletteInput   textinput.Model
	paletteCursor  int
	paletteMatches []paletteAction

	// Searchable help
	helpFilter  string
	helpCursor  int
	helpMatches []paletteAction

	// Up Next panel (Now Playing): upNextCursor is an offset into the upcoming
	// tracks (0 = the first track after the current one). j/k move it and Enter
	// jumps to the selection.
	upNextCursor int

	// Ratings (liked/disliked) keyed by videoID, and the multi-select state for
	// the list tabs (marks holds indices into the active list).
	ratings    map[string]ytmusic.Rating
	selectMode bool
	marks      map[int]bool

	// Transient on-screen display (volume / play-pause / seek). A generation
	// counter makes stale auto-hide timers harmless.
	osdText    string
	osdVisible bool
	osdGen     int
	osdError   bool // render the OSD as a warning (error toast) instead of an info flash
	lastVolume int

	// Services
	ytClient    *ytmusic.Client
	urlCache    *player.URLCache
	store       *store.Store
	resumeState *store.LastPlayingState // last session state, cleared once playback starts
	tickCount   int                     // incremented each tick for periodic saves
	cookiesFile string                  // Netscape cookies file handed to yt-dlp ("" = none)
	prefetcher  *prefetcher             // serial background stream-URL warmer
	mpris       *mpris.Service          // nil when MPRIS is unavailable (best-effort)

	// Pomodoro focus timer (driven by the existing tick; never blocks the UI).
	pomodoro pomodoro.Timer

	// Local listening stats view.
	statsSummary stats.Summary
	statsLoaded  bool

	// Play recording (local stats): the track currently being listened to and
	// how long it has actually played, accumulated from the tick.
	recordingID     string
	recordingTitle  string
	recordingArtist string
	listenSeconds   float64
}

// Options carries the runtime configuration derived from config.toml into the
// model. A zero value is valid and yields the built-in defaults.
type Options struct {
	// VolumeStep is the percentage applied by the volume keys.
	VolumeStep int
	// SeekStep is the seconds applied by the seek keys.
	SeekStep int
	// PrefetchAhead is how many upcoming tracks to warm.
	PrefetchAhead int
	// ArtSize is "auto" or an integer maximum art width in columns.
	ArtSize string
	// ShowLyrics controls whether the lyrics panel starts visible.
	ShowLyrics bool
	// Keys maps an action name to one or more replacement key strings.
	Keys map[string][]string
	// PomodoroWork and PomodoroBreak are the focus/break interval lengths.
	PomodoroWork  time.Duration
	PomodoroBreak time.Duration

	// SleepDefaultMinutes is the default sleep-timer duration.
	SleepDefaultMinutes int
	// Gapless enables mpv gapless-audio.
	Gapless bool
	// CrossfadeSeconds sets mpv crossfade (implies gapless when > 0).
	CrossfadeSeconds float64
	// NormalizeAudio applies mpv's dynaudnorm filter.
	NormalizeAudio bool
	// NotifyTrackChange sends a desktop notification on track change.
	NotifyTrackChange bool
	// Discord is the optional Discord Rich Presence client (nil disables RPC).
	Discord *discordrpc.Client
}

// withDefaults fills in the built-in values for unset option fields.
func (o Options) withDefaults() Options {
	if o.VolumeStep <= 0 {
		o.VolumeStep = 5
	}
	if o.SeekStep <= 0 {
		o.SeekStep = 5
	}
	if o.PrefetchAhead <= 0 {
		o.PrefetchAhead = 2
	}
	if o.ArtSize == "" {
		o.ArtSize = "auto"
	}
	if o.PomodoroWork <= 0 {
		o.PomodoroWork = time.Duration(config.DefaultWorkMinutes) * time.Minute
	}
	if o.PomodoroBreak <= 0 {
		o.PomodoroBreak = time.Duration(config.DefaultBreakMinutes) * time.Minute
	}
	if o.SleepDefaultMinutes <= 0 {
		o.SleepDefaultMinutes = 30
	}
	return o
}

// New creates the initial Model.
func New(ytClient *ytmusic.Client, st *store.Store, cache *player.URLCache, resume *store.LastPlayingState, cookiesFile string, opts Options, mp *mpris.Service) Model {
	opts = opts.withDefaults()
	keys, unknown := buildKeyMap(opts.Keys)
	if len(unknown) > 0 {
		slog.Warn("ignoring unknown key bindings in config", "actions", unknown)
	}
	SetArtMaxCols(parseArtSize(opts.ArtSize))
	si := textinput.New()
	si.Prompt = "" // searchTabView draws the focused/unfocused prompt itself
	si.Placeholder = "Search YouTube Music... (press / to start)"
	si.CharLimit = 100

	fi := textinput.New()
	fi.Prompt = ""
	fi.Placeholder = "type to filter…"
	fi.CharLimit = 100

	ni := textinput.New()
	ni.Prompt = ""
	ni.Placeholder = "playlist name…"
	ni.CharLimit = 80

	m := Model{
		keys:          keys,
		tab:           TabNowPlaying,
		searchInput:   si,
		searchFilter:  ytmusic.FilterSongs,
		searchList:    newList(""),
		libraryList:   newList("Liked Songs"),
		playlistList:  newList("Playlists"),
		trackList:     newList(""),
		pageList:      newList(""),
		filterInput:   fi,
		nameInput:     ni,
		pickList:      newList("Add to playlist"),
		ytClient:      ytClient,
		urlCache:      cache,
		store:         st,
		resumeState:   resume,
		cookiesFile:   cookiesFile,
		prefetcher:    newPrefetcher(cache, cookiesFile),
		mpris:         mp,
		retried:       make(map[string]bool),
		artCache:      make(map[string]string),
		lyricsCache:   make(map[string]Lyrics),
		ratings:       make(map[string]ytmusic.Rating),
		marks:         make(map[int]bool),
		paletteInput:  newPaletteInput(),
		lastVolume:    -1,
		showLyrics:    opts.ShowLyrics,
		lyricsFollow:  true,
		volumeStep:    opts.VolumeStep,
		seekStep:      opts.SeekStep,
		prefetchAhead: opts.PrefetchAhead,

		gapless:           opts.Gapless,
		crossfadeSeconds:  opts.CrossfadeSeconds,
		normalizeAudio:    opts.NormalizeAudio,
		sleepDefault:      time.Duration(opts.SleepDefaultMinutes) * time.Minute,
		notifyTrackChange: opts.NotifyTrackChange,
		discord:           opts.Discord,

		pomodoro: pomodoro.New(opts.PomodoroWork, opts.PomodoroBreak),
	}

	if st != nil {
		m.searchHistory = st.LoadSearchHistory()
	}

	// Serve cached metadata instantly so the Library/Playlists tabs are
	// populated the moment they are opened. A background refresh is kicked off
	// from Init() to keep the data current.
	if st != nil {
		if tracks, savedAt, ok := st.LoadLibrary(); ok && len(tracks) > 0 {
			m.libraryTracks = tracks
			m.libraryList.SetItems(m.trackItems(tracks))
			m.libraryLoaded = true
			slog.Debug("library served from cache", "n", len(tracks), "age", time.Since(savedAt))
		}
		if pls, savedAt, ok := st.LoadPlaylists(); ok && len(pls) > 0 {
			m.playlists = pls
			m.playlistList.SetItems(playlistsToItems(pls))
			m.playlistLoaded = true
			slog.Debug("playlists served from cache", "n", len(pls), "age", time.Since(savedAt))
		}
	}

	// Restore persisted ratings, then treat the cached liked-songs list as
	// liked without overwriting anything already known.
	if st != nil {
		if saved, ok := st.LoadRatings(); ok {
			for id, r := range saved {
				m.ratings[id] = ytmusic.Rating(r)
			}
		}
	}
	m.seedLiked(m.libraryTracks)

	// Mark the startup refresh in flight so a tab-open while it runs does not
	// fire a duplicate request. Cleared when the corresponding msg arrives.
	if ytClient != nil && ytClient.Authenticated() {
		m.libraryLoading = true
		m.playlistsLoading = true
	}

	return m
}

func newList(title string) list.Model {
	l := list.New(nil, newListDelegate(), 0, 0)
	l.Title = title
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(colAccentNP).Padding(0, 1)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	return l
}

// newListDelegate builds a list delegate whose row styles follow the currently
// applied palette. It is a function (rather than a shared value) so the T key
// can rebuild every list's delegate after cycling palettes.
func newListDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()

	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colSelFg).
		Background(colSelBg).
		Padding(0, 0, 0, 1).
		BorderLeft(true).
		BorderForeground(colSelBg)
	d.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(colSelFg).
		Faint(true).
		Background(colSelBg).
		Padding(0, 0, 0, 1).
		BorderLeft(true).
		BorderForeground(colSelBg)
	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(colText).
		Padding(0, 0, 0, 2)
	d.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(colMuted).
		Padding(0, 0, 0, 2)

	return d
}

// restyleLists rebuilds every list delegate from the active palette. The colour
// globals are read at render time, but list delegates bake their styles in when
// constructed, so they must be refreshed after a palette change.
func (m *Model) restyleLists() {
	d := newListDelegate()
	m.searchList.SetDelegate(d)
	m.libraryList.SetDelegate(d)
	m.playlistList.SetDelegate(d)
	m.trackList.SetDelegate(d)
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick()}
	// Refresh library/playlists in the background at startup. Cached data is
	// already displayed; this keeps it current without blocking the UI.
	if m.ytClient != nil && m.ytClient.Authenticated() {
		cmds = append(cmds, m.loadLibraryCmd(), m.loadPlaylistsCmd())
	}
	return tea.Batch(cmds...)
}

// tickInterval is how often the UI tick fires. It doubles as the resolution of
// the local listening-time accumulator.
const tickInterval = 500 * time.Millisecond

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeLists()
		// Art is rasterised at a specific width; on resize re-render it.
		if t, ok := m.currentTrack(); ok {
			if cmd := m.albumArtCmd(t); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tickMsg:
		m.tickCount++
		// Advance the Pomodoro timer from the same tick; it does no I/O and
		// reports phase completions so we can notify without blocking.
		if m.pomodoro.Running() {
			if completed, done := m.pomodoro.Tick(time.Now()); done {
				cmds = append(cmds, m.pomodoroCompletedCmds(completed)...)
			}
		}
		if m.mpv != nil && m.mpv.IsAlive() {
			m.playerState = m.mpv.GetState()
			// When mpv is idle there is no file loaded (e.g. after the sleep
			// timer stopped playback or a manual stop). mpv reports pause=false
			// in that state, so correct the snapshot so the UI shows paused
			// instead of a stale "playing" indicator.
			if m.mpv.IsIdle() {
				m.playerState.Playing = false
			}
			// Local listening stats: accumulate listened time while playing,
			// and roll the recording over when the track changes.
			if t, ok := m.currentTrack(); ok && t.VideoID != "" {
				if m.recordingID != t.VideoID {
					cmds = append(cmds, m.finalizePlay()...)
					m.recordingID = t.VideoID
					m.recordingTitle = t.Title
					m.recordingArtist = t.Artist
					m.listenSeconds = 0
				}
				if m.playerState.Playing {
					m.listenSeconds += tickInterval.Seconds()
				}
			}
			// Surface externally-driven volume changes (or corrections to our
			// optimistic estimate) as an OSD.
			if m.lastVolume >= 0 && m.playerState.Volume != m.lastVolume {
				cmds = append(cmds, m.showVolumeOSD(m.playerState.Volume))
			} else if m.lastVolume < 0 {
				m.lastVolume = m.playerState.Volume
			}
			// Expired stream URL: mpv could not open the file (e.g. a
			// googlevideo URL past its ~6h expiry returning 403). Evict the
			// cached URL, re-resolve it, and retry once for this track.
			if !m.autoAdvancing && m.mpv.TakeFailed() {
				if t, ok := m.currentTrack(); ok && m.retriableTrack(t) {
					m.retried[t.VideoID] = true
					m.autoAdvancing = true
					m.status = "refreshing stream…"
					cmds = append(cmds, m.showOSD("⟳  refreshing stream…"))
					cmds = append(cmds, m.refreshAndLoadCmd(t))
				} else {
					// Already retried once (or no track): report and stop rather
					// than looping on a persistently broken stream.
					cmds = append(cmds, m.showToast("⚠  couldn't play this track"))
				}
			}
			// Auto-advance only when mpv reports the current track actually
			// ended (end-file/eof). TakeEnded consumes the signal once, so a
			// pause (which sets idle-active but not "ended") never advances.
			// When the queue is exhausted, continue with the radio mix.
			if !m.autoAdvancing && m.mpv.TakeEnded() {
				if m.queue != nil && m.queue.HasNext() {
					m.autoAdvancing = true
					cmds = append(cmds, m.advanceQueueCmd(1))
				} else if cmd := m.radioCmd(); cmd != nil {
					m.autoAdvancing = true
					m.status = "tuning the radio…"
					cmds = append(cmds, cmd)
				}
			}
			// Persist state every 5 seconds (10 ticks)
			if m.tickCount%10 == 0 {
				cmds = append(cmds, m.saveStateCmd())
			}
		}
		m.updateMPRIS()
		m.updateDiscord(false)
		cmds = append(cmds, m.checkSleep()...)
		cmds = append(cmds, tick())

	case osdHideMsg:
		if msg.gen == m.osdGen {
			m.osdVisible = false
			m.osdText = ""
		}

	case artResultMsg:
		if m.artLoading == msg.videoID {
			m.artLoading = ""
		}
		if msg.err != nil {
			slog.Debug("art fetch failed", "videoID", msg.videoID, "err", msg.err)
		} else if msg.rendered != "" {
			if m.artCache == nil {
				m.artCache = make(map[string]string)
			}
			m.artCache[artKey(msg.videoID, msg.width)] = msg.rendered
		}

	case lyricsMsg:
		if m.lyricsVideoID != msg.videoID {
			break // a newer track superseded this fetch
		}
		m.lyricsLoading = false
		switch {
		case msg.err != nil:
			slog.Debug("lyrics fetch failed", "videoID", msg.videoID, "err", msg.err)
			m.lyricsNoMatch = true
		case !msg.found || msg.lyrics.empty():
			m.lyricsNoMatch = true
			if m.lyricsCache == nil {
				m.lyricsCache = make(map[string]Lyrics)
			}
			m.lyricsCache[msg.videoID] = Lyrics{}
		default:
			if m.lyricsCache == nil {
				m.lyricsCache = make(map[string]Lyrics)
			}
			m.lyricsCache[msg.videoID] = msg.lyrics
			m.lyrics = msg.lyrics.Lines
			m.lyricsSynced = msg.lyrics.Synced
			m.lyricsNoMatch = false
			// A freshly loaded synced track starts following the music.
			if msg.lyrics.Synced {
				m.lyricsFollow = true
			}
		}

	case suggestionsMsg:
		m.suggLoading = false
		if msg.gen != m.suggGen {
			break // superseded by newer input
		}
		if msg.err != nil {
			slog.Debug("suggestions fetch failed", "query", msg.query, "err", msg.err)
			break
		}
		m.suggestions = msg.sugg
		if !m.searchHistMode {
			m.searchOptions = msg.sugg
			if m.searchCursor >= len(m.searchOptions) {
				m.searchCursor = -1
			}
		}

	case suggestionTickMsg:
		if msg.gen != m.suggGen || strings.TrimSpace(m.searchInput.Value()) != msg.query {
			break // superseded
		}
		if m.ytClient != nil {
			m.suggLoading = true
			cmds = append(cmds, m.suggestCmd(msg.query, msg.gen))
		}

	case prefetchDoneMsg:
		// no-op — cache was filled in background

	case mpris.Command:
		cmds = append(cmds, m.handleMPRISCommand(msg)...)

	case statsMsg:
		m.statsLoaded = true
		if msg.err != nil {
			cmds = append(cmds, m.flashError("couldn't load stats", msg.err))
		} else {
			m.statsSummary = msg.summary
		}

	case exportDoneMsg:
		if msg.err != nil {
			cmds = append(cmds, m.flashError("export failed", msg.err))
		} else {
			cmds = append(cmds, m.showOSD(fmt.Sprintf("↥  exported %d → %s", msg.n, filepath.Base(msg.m3u))))
			slog.Info("export complete", "m3u", msg.m3u, "csv", msg.csv, "n", msg.n)
		}

	case importDoneMsg:
		switch {
		case msg.err != nil:
			m.status = "import: no import.m3u"
			cmds = append(cmds, m.showToast("⚠  no ~/.config/ytmusic-tui/import.m3u"))
		case len(msg.tracks) == 0:
			cmds = append(cmds, m.showToast("⚠  nothing importable in import.m3u"))
		default:
			if msg.skipped > 0 {
				cmds = append(cmds, m.showOSD(fmt.Sprintf("↧  imported %d (skipped %d)", len(msg.tracks), msg.skipped)))
			} else {
				cmds = append(cmds, m.showOSD(fmt.Sprintf("↧  imported %d tracks", len(msg.tracks))))
			}
			tracks := msg.tracks
			cmds = append(cmds, func() tea.Msg {
				return m.startPlayback(tracks, 0, "import.m3u", 0, false, nil)
			})
		}

	case radioMsg:
		m.radioLoading = false
		m.autoAdvancing = false
		m.status = ""
		switch {
		case msg.err != nil:
			cmds = append(cmds, m.showToast("⚠  radio unavailable — end of queue"))
		case m.appendRadio(msg.tracks) == 0:
			cmds = append(cmds, m.showToast("⚠  no more similar tracks"))
		default:
			m.prefetchNeighbors()
			if m.queue != nil && m.queue.HasNext() {
				m.autoAdvancing = true
				cmds = append(cmds, m.advanceQueueCmd(1))
			}
		}

	case searchResultMsg:
		if msg.err != nil {
			cmds = append(cmds, m.flashError("search failed", msg.err))
		} else {
			m.searchFilter = msg.filter
			m.searchResults = msg.tracks
			m.searchItems = msg.items
			if msg.filter.ReturnsTracks() {
				m.searchList.SetItems(tracksToItems(msg.tracks))
				m.status = fmt.Sprintf("%d results — prefetching streams...", len(msg.tracks))
				cmds = append(cmds, m.prefetchTracksCmd(msg.tracks))
			} else {
				m.searchList.SetItems(browseToItems(msg.items))
				m.status = fmt.Sprintf("%d %s — enter to open", len(msg.items), strings.ToLower(msg.filter.Label()))
			}
		}

	case libraryMsg:
		m.libraryLoading = false
		if msg.err != nil {
			cmds = append(cmds, m.flashError("couldn't load your library", msg.err))
		} else {
			m.libraryTracks = msg.tracks
			m.libraryList.SetItems(m.trackItems(msg.tracks))
			m.libraryLoaded = true
			m.seedLiked(msg.tracks)
			if m.tab == TabLibrary {
				m.status = "prefetching streams..."
			}
			cmds = append(cmds, m.prefetchTracksCmd(msg.tracks))
		}

	case playlistsMsg:
		m.playlistsLoading = false
		if msg.err != nil {
			cmds = append(cmds, m.flashError("couldn't load playlists", msg.err))
		} else {
			m.playlists = msg.playlists
			m.playlistList.SetItems(playlistsToItems(msg.playlists))
			m.playlistLoaded = true
			if m.tab == TabPlaylists {
				m.status = ""
			}
			// A pending "add to playlist" resumes once playlists have loaded.
			if m.pendingPick {
				m.pendingPick = false
				m.openPicker(m.pendingPickTrk)
			}
		}
	case playlistTracksMsg:
		if msg.err != nil {
			cmds = append(cmds, m.flashError("couldn't load playlist", msg.err))
		} else {
			m.playlistTracks = msg.tracks
			if msg.title != "" {
				m.trackList.Title = msg.title
			}
			m.trackList.SetItems(m.trackItems(msg.tracks))
			m.inPlaylist = true
			if m.tab == TabPlaylists {
				m.status = "prefetching streams..."
			}
			cmds = append(cmds, m.prefetchTracksCmd(msg.tracks))
		}

	case homeMsg:
		m.homeLoading = false
		if msg.err != nil && len(msg.sections) == 0 {
			m.homeLoaded = false
			cmds = append(cmds, m.flashError("couldn't load home", msg.err))
		} else {
			m.homeViews = []homeView{newHomeView("Home", msg.sections)}
			m.homeLoaded = true
			if m.tab == TabHome {
				m.status = ""
			}
		}

	case homeCollectionMsg:
		if msg.err != nil {
			cmds = append(cmds, m.flashError("couldn't open this", msg.err))
		} else if len(msg.sections) == 0 {
			cmds = append(cmds, m.showToast("⚠  nothing to play here"))
		} else {
			m.homeViews = append(m.homeViews, newHomeView(msg.title, msg.sections))
			m.status = ""
		}

	case ratingMsg:
		if msg.err != nil {
			// Roll the optimistic change back and tell the user.
			m.setRating(msg.videoID, msg.prev)
			cmds = append(cmds, m.showToast("⚠  couldn't update rating"))
		}

	case pageAlbumMsg:
		if msg.err != nil {
			m.status = "album error: " + msg.err.Error()
		} else {
			m.pageTitle = firstNonEmpty(msg.page.Album.Title, m.pageTitle)
			artist, year := msg.page.Album.Artist, msg.page.Album.Year
			m.pageSubtitle = joinNonEmpty(" · ", artist, year)
			m.pageKind = pageAlbum
			m.pageList.SetItems(tracksToItems(msg.page.Tracks))
			if m.tab == TabPage {
				m.status = fmt.Sprintf("%d tracks", len(msg.page.Tracks))
			}
			cmds = append(cmds, m.prefetchTracksCmd(msg.page.Tracks))
		}

	case pageArtistMsg:
		if msg.err != nil {
			m.status = "artist error: " + msg.err.Error()
		} else {
			m.pageTitle = firstNonEmpty(msg.page.Artist.Name, m.pageTitle)
			m.pageSubtitle = msg.page.Artist.Subscribers
			m.pageKind = pageArtist
			items := make([]list.Item, 0, len(msg.page.Albums)+len(msg.page.Tracks))
			for _, al := range msg.page.Albums {
				items = append(items, browseItem{al})
			}
			for _, tr := range msg.page.Tracks {
				items = append(items, trackItem{track: tr})
			}
			m.pageList.SetItems(items)
			if m.tab == TabPage {
				m.status = fmt.Sprintf("%d songs · %d albums", len(msg.page.Tracks), len(msg.page.Albums))
			}
			cmds = append(cmds, m.prefetchTracksCmd(msg.page.Tracks))
		}

	case pageTracksMsg:
		if msg.err != nil {
			m.status = "playlist error: " + msg.err.Error()
		} else {
			m.pageTitle = firstNonEmpty(msg.title, m.pageTitle)
			m.pageSubtitle = "Playlist"
			m.pageKind = pagePlaylist
			m.pagePlaylist = msg.playlistID
			m.pageList.SetItems(tracksToItems(msg.tracks))
			if m.tab == TabPage {
				m.status = fmt.Sprintf("%d tracks", len(msg.tracks))
			}
			cmds = append(cmds, m.prefetchTracksCmd(msg.tracks))
		}

	case createPlaylistMsg:
		if msg.err != nil {
			m.status = "create playlist failed: " + msg.err.Error()
		} else {
			m.status = fmt.Sprintf("created playlist %q", msg.title)
			// Reflect the new playlist immediately.
			m.playlists = append(m.playlists, ytmusic.Playlist{PlaylistID: msg.id, Title: msg.title})
			m.playlistList.SetItems(playlistsToItems(m.playlists))
			m.playlistLoaded = true
		}

	case playlistEditMsg:
		if msg.err != nil {
			m.status = msg.action + " failed: " + msg.err.Error()
		} else {
			m.status = fmt.Sprintf("%s ok (%s)", msg.action, msg.status)
			if msg.action == "remove" && msg.removedVideoID != "" {
				m.removeTrackFromActivePlaylist(msg.playlistID, msg.removedVideoID)
			}
		}

	case mpvStartedMsg:
		if msg.err != nil {
			cmds = append(cmds, m.flashError("couldn't play this track", msg.err))
		} else {
			if m.mpv != nil {
				m.mpv.Quit()
			}
			m.mpv = msg.mpv
			m.queue = msg.queue
			m.looping = false
			m.upNextCursor = 0
			m.selectMode = false
			m.marks = make(map[int]bool)
			m.fromPlaylist = msg.fromPlaylist
			m.resumeState = nil // consumed
			// A brand-new queue: reset per-session recovery/radio bookkeeping.
			m.retried = make(map[string]bool)
			m.radioLoading = false
			m.radioSource = ""
			m.radioAppended = 0
			m.status = ""
			m.tab = TabNowPlaying
			m.prefetchNeighbors()
			cmds = append(cmds, m.onTrackChanged()...)
		}

	case mpvLoadMsg:
		m.autoAdvancing = false
		if msg.err != nil {
			cmds = append(cmds, m.flashError("couldn't play this track", msg.err))
		} else if m.mpv != nil {
			if err := m.mpv.LoadURL(msg.url, msg.title); err != nil {
				m.status = "load error: " + err.Error()
			} else {
				m.status = "" // stream resolved & loaded
				m.prefetchNeighbors()
				cmds = append(cmds, m.onTrackChanged()...)
			}
		}

	case tea.KeyMsg:
		if m.paletteOpen {
			return m.updatePalette(msg, cmds)
		}
		if m.showHelp {
			return m.updateHelp(msg, cmds)
		}
		if m.searchMode {
			return m.updateSearchMode(msg, cmds)
		}
		if m.sleepMode {
			return m.updateSleepMode(msg)
		}
		return m.updateNormalMode(msg, cmds)

	case tea.MouseMsg:
		return m.updateMouse(msg, cmds)
	}

	m, listCmd := m.updateActiveList(msg)
	cmds = append(cmds, listCmd)
	return m, tea.Batch(cmds...)
}

func (m Model) updateNormalMode(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	// Multi-select mode intercepts a few keys (space marks, a adds, esc
	// cancels); j/k and everything else fall through to the active list.
	if m.selectMode {
		switch {
		case key.Matches(msg, m.keys.Back):
			m.exitSelect()
			return m, tea.Batch(cmds...)
		case key.Matches(msg, m.keys.PlayPause), key.Matches(msg, m.keys.Select):
			cmds = append(cmds, m.toggleMark())
			return m, tea.Batch(cmds...)
		case key.Matches(msg, m.keys.BulkAdd):
			cmds = append(cmds, m.commitSelect()...)
			return m, tea.Batch(cmds...)
		}
	}

	// ctrl+c always quits, even inside a modal (where `q` is filter text).
	if msg.String() == "ctrl+c" {
		if m.mpv != nil {
			m.mpv.Quit()
		}
		if m.ytClient != nil {
			m.ytClient.Close()
		}
		return m, tea.Quit
	}
	// Modal overlays take precedence over every global key.
	if m.confirmMode {
		return m.updateConfirmMode(msg, cmds)
	}
	if m.filterMode {
		return m.updateFilterMode(msg, cmds)
	}
	if m.nameMode {
		return m.updateNameMode(msg, cmds)
	}
	if m.pickMode {
		return m.updatePickMode(msg, cmds)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.recordPlayNow() // synchronous: a queued cmd would not run after Quit
		if m.mpv != nil {
			m.mpv.Quit()
		}
		if m.ytClient != nil {
			m.ytClient.Close()
		}
		return m, tea.Batch(append(cmds, tea.Quit)...)

	case key.Matches(msg, m.keys.Help):
		m.openHelp()
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.Palette):
		m.openPalette()
		cmds = append(cmds, textinput.Blink)
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.Tab0):
		m.tab = TabNowPlaying
		m.showHelp = false
		m.exitSelect()

	case key.Matches(msg, m.keys.Tab1):
		m.tab = TabSearch
		m.showHelp = false
		m.exitSelect()

	case key.Matches(msg, m.keys.Tab2):
		m.tab = TabLibrary
		m.showHelp = false
		m.exitSelect()
		if !m.libraryLoaded && !m.libraryLoading {
			m.status = "loading liked songs..."
			m.libraryLoading = true
			cmds = append(cmds, m.loadLibraryCmd())
		}

	case key.Matches(msg, m.keys.Tab3):
		m.tab = TabPlaylists
		m.showHelp = false
		m.exitSelect()
		if !m.playlistLoaded && !m.playlistsLoading {
			m.status = "loading playlists..."
			m.playlistsLoading = true
			cmds = append(cmds, m.loadPlaylistsCmd())
		}

	case key.Matches(msg, m.keys.Home):
		m.tab = TabHome
		m.showHelp = false
		if !m.homeLoaded && !m.homeLoading {
			m.status = "loading home..."
			m.homeLoading = true
			cmds = append(cmds, m.loadHomeCmd())
		}

	case key.Matches(msg, m.keys.Tab4):
		m.tab = TabStats
		m.showHelp = false
		cmds = append(cmds, m.loadStatsCmd())

	case key.Matches(msg, m.keys.Loop):
		if m.mpv != nil {
			m.looping = !m.looping
			m.mpv.SetLoop(m.looping)
			if m.looping {
				cmds = append(cmds, m.showOSD("↺  loop on"))
			} else {
				cmds = append(cmds, m.showOSD("↺  loop off"))
			}
		}

	case key.Matches(msg, m.keys.Shuffle):
		cmds = append(cmds, m.toggleShuffle()...)

	case key.Matches(msg, m.keys.Pomodoro):
		m.pomodoro.Toggle(time.Now())
		if m.pomodoro.Running() {
			m.status = ""
			cmds = append(cmds, m.showOSD(m.pomodoroOSD()))
		} else {
			cmds = append(cmds, m.showOSD("🍅  pomodoro stopped"))
		}

	// Queue editing from the Up Next panel.
	case key.Matches(msg, m.keys.QueueMoveUp), key.Matches(msg, m.keys.QueueMoveDown):
		if m.tab == TabNowPlaying {
			cmds = append(cmds, m.reorderUpNext(key.Matches(msg, m.keys.QueueMoveDown))...)
			return m, tea.Batch(cmds...)
		}

	case key.Matches(msg, m.keys.QueueRemove):
		if m.tab == TabNowPlaying {
			cmds = append(cmds, m.removeUpNext()...)
			return m, tea.Batch(cmds...)
		}
		// `x` is shared with playlist removal; on list tabs defer to that.
		if key.Matches(msg, m.keys.RemoveFromList) {
			m.confirmRemoveSelected()
			return m, tea.Batch(cmds...)
		}

	case key.Matches(msg, m.keys.ClearUpcoming):
		if m.tab == TabNowPlaying {
			cmds = append(cmds, m.clearUpcoming()...)
			return m, tea.Batch(cmds...)
		}

	// Rating (like / dislike) of the current track.
	case key.Matches(msg, m.keys.Like):
		if m.mpv != nil {
			cmds = append(cmds, m.toggleRating(ytmusic.RatingLike)...)
		}

	case key.Matches(msg, m.keys.Dislike):
		if m.mpv != nil {
			cmds = append(cmds, m.toggleRating(ytmusic.RatingDislike)...)
		}

	// Multi-select on list tabs.
	case key.Matches(msg, m.keys.SelectMode):
		if m.tab != TabNowPlaying {
			m.toggleSelect()
		}

	case key.Matches(msg, m.keys.Sleep):
		m.sleepMode = true
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.Theme):
		name := CyclePalette()
		m.restyleLists()
		cmds = append(cmds, m.showOSD("🎨  "+name))

	case key.Matches(msg, m.keys.Export):
		cmds = append(cmds, m.exportSelection()...)

	case key.Matches(msg, m.keys.Import):
		cmds = append(cmds, m.importM3U()...)

	case key.Matches(msg, m.keys.Lyrics):
		if m.tab == TabNowPlaying {
			m.showLyrics = !m.showLyrics
			if m.showLyrics {
				m.lyricsScroll = 0
				m.lyricsFollow = true
				if t, ok := m.currentTrack(); ok {
					cmds = append(cmds, m.ensureLyrics(t))
				}
			}
		}

	// "f" is shared with FilterNext: on Now Playing with the panel open it
	// toggles auto-follow; otherwise the guard fails and the Search-tab filter
	// case below handles it.
	case key.Matches(msg, m.keys.LyricsFollow) && m.tab == TabNowPlaying && m.showLyrics:
		m.lyricsFollow = !m.lyricsFollow
		if m.lyricsFollow {
			cmds = append(cmds, m.showOSD("◉  lyrics follow on"))
		} else {
			cmds = append(cmds, m.showOSD("◎  lyrics follow off"))
		}

	// On Now Playing, j/k (and ↑/↓) move the Up Next cursor. On every other tab
	// they fall through to the active list below.
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.Down):
		if m.tab == TabNowPlaying {
			m.moveUpNext(key.Matches(msg, m.keys.Down))
			return m, tea.Batch(cmds...)
		}
		if m.tab == TabHome {
			m.moveHome(key.Matches(msg, m.keys.Down))
			return m, tea.Batch(cmds...)
		}

	// Lyrics viewport paging uses [ / ] (arrows and j/k now belong to Up Next).
	case key.Matches(msg, m.keys.LyricsScrollUp), key.Matches(msg, m.keys.LyricsScrollDown):
		if m.tab == TabNowPlaying && m.showLyrics {
			if key.Matches(msg, m.keys.LyricsScrollUp) {
				m.scrollLyrics(-1)
			} else {
				m.scrollLyrics(1)
			}
			return m, tea.Batch(cmds...)
		}

	// g / G jump to the top / bottom of the lyrics, or of the active list.
	case key.Matches(msg, m.keys.Top), key.Matches(msg, m.keys.Bottom):
		if m.tab == TabNowPlaying && m.showLyrics {
			if key.Matches(msg, m.keys.Top) {
				m.lyricsScroll = 0
			} else {
				m.scrollLyrics(1 << 30)
			}
			return m, tea.Batch(cmds...)
		}
		if m.hasActiveList() {
			if key.Matches(msg, m.keys.Top) {
				m.listToTop()
			} else {
				m.listToBottom()
			}
			return m, tea.Batch(cmds...)
		}

	// Fast scrolling of long lists.
	case key.Matches(msg, m.keys.PageDown), key.Matches(msg, m.keys.PageUp):
		if m.hasActiveList() {
			if key.Matches(msg, m.keys.PageDown) {
				m.listPage(1)
			} else {
				m.listPage(-1)
			}
			return m, tea.Batch(cmds...)
		}

	// Type-to-filter / jump within the active list.
	case key.Matches(msg, m.keys.TypeToFilter):
		if m.hasActiveList() {
			m.startFilter()
			return m, tea.Batch(cmds...)
		}

	// Cycle the Search filter.
	case key.Matches(msg, m.keys.FilterNext), key.Matches(msg, m.keys.FilterPrev):
		if m.tab == TabSearch {
			if key.Matches(msg, m.keys.FilterNext) {
				m.searchFilter = m.searchFilter.Next()
			} else {
				m.searchFilter = m.searchFilter.Prev()
			}
			m.status = "filter: " + m.searchFilter.Label() + " — press enter to search"
			return m, tea.Batch(cmds...)
		}

	// Browse the selected track's artist / album.
	case key.Matches(msg, m.keys.GoArtist):
		var cmd tea.Cmd
		m, cmd = m.goToArtist()
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.GoAlbum):
		var cmd tea.Cmd
		m, cmd = m.goToAlbum()
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)

	// Playlist editing.
	case key.Matches(msg, m.keys.CreatePlaylist):
		m.nameMode = true
		m.nameInput.SetValue("")
		m.nameInput.Focus()
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.AddToPlaylist):
		var cmd tea.Cmd
		m, cmd = m.addToPlaylist()
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.RemoveFromList):
		m.confirmRemoveSelected()
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.Search):
		m.tab = TabSearch
		m.showHelp = false
		m.exitSelect()
		m.searchMode = true
		m.searchInput.Focus()
		m.searchHistMode = true
		m.searchOptions = m.searchHistory
		m.searchCursor = -1
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.Back):
		if m.tab == TabPage {
			origin := m.pageOrigin
			if origin == TabPage {
				origin = TabSearch
			}
			m.tab = origin
			m.pageKind = pageNone
			m.pagePlaylist = ""
		} else if m.tab == TabPlaylists && m.inPlaylist {
			m.inPlaylist = false
			m.playlistID = ""
		}
		if m.tab == TabHome {
			m.popHome()
		}
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.Select):
		// Resume from last session if on Now Playing with no active mpv
		if m.tab == TabNowPlaying && m.mpv == nil && m.resumeState != nil {
			rs := m.resumeState
			cmds = append(cmds, func() tea.Msg {
				return m.startPlayback(rs.Tracks, rs.Index, rs.FromPlaylist, rs.Position, rs.Shuffle, rs.Order)
			})
		} else if m.tab == TabNowPlaying && m.mpv != nil {
			// Enter jumps to the Up Next selection (a no-op when nothing is
			// upcoming). The load path unpauses, so it starts playing at 0:00.
			if cmd := m.jumpToUpNextCmd(); cmd != nil {
				m.autoAdvancing = true
				m.status = "loading…"
				cmds = append(cmds, cmd)
			}
		} else {
			var cmd tea.Cmd
			m, cmd = m.handleSelect()
			cmds = append(cmds, cmd)
		}

	case key.Matches(msg, m.keys.PlayPause):
		if m.mpv == nil && m.resumeState != nil && m.tab == TabNowPlaying {
			rs := m.resumeState
			cmds = append(cmds, func() tea.Msg {
				return m.startPlayback(rs.Tracks, rs.Index, rs.FromPlaylist, rs.Position, rs.Shuffle, rs.Order)
			})
		} else if m.mpv != nil {
			m.mpv.PlayPause()
			if m.playerState.Playing {
				cmds = append(cmds, m.showOSD("⏸  paused"))
			} else {
				cmds = append(cmds, m.showOSD("▶  playing"))
			}
		}

	case key.Matches(msg, m.keys.Next):
		if cmd := m.advanceQueueCmd(1); cmd != nil {
			m.autoAdvancing = true
			m.status = "loading next…"
			cmds = append(cmds, cmd)
		}

	case key.Matches(msg, m.keys.Prev):
		if cmd := m.advanceQueueCmd(-1); cmd != nil {
			m.autoAdvancing = true
			m.status = "loading previous…"
			cmds = append(cmds, cmd)
		}

	case key.Matches(msg, m.keys.VolumeUp):
		if m.mpv != nil {
			step := m.volumeStepOr()
			m.mpv.VolumeBy(step)
			cmds = append(cmds, m.showVolumeOSD(minInt(100, m.playerState.Volume+step)))
		}

	case key.Matches(msg, m.keys.VolumeDown):
		if m.mpv != nil {
			step := m.volumeStepOr()
			m.mpv.VolumeBy(-step)
			cmds = append(cmds, m.showVolumeOSD(maxInt(0, m.playerState.Volume-step)))
		}

	case key.Matches(msg, m.keys.SeekFwd):
		if m.mpv != nil {
			step := m.seekStepOr()
			m.mpv.Seek(float64(step))
			cmds = append(cmds, m.showOSD(fmt.Sprintf("⏩  +%ds", step)))
		}

	case key.Matches(msg, m.keys.SeekBack):
		if m.mpv != nil {
			step := m.seekStepOr()
			m.mpv.Seek(-float64(step))
			cmds = append(cmds, m.showOSD(fmt.Sprintf("⏪  −%ds", step)))
		}
	}

	m, listCmd := m.updateActiveList(msg)
	cmds = append(cmds, listCmd)
	return m, tea.Batch(cmds...)
}

func (m Model) updateSearchMode(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeSearch()
		return m, tea.Batch(cmds...)
	case "up":
		if m.searchCursor > 0 {
			m.searchCursor--
		} else {
			m.searchCursor = -1
		}
		return m, tea.Batch(cmds...)
	case "down":
		if m.searchCursor < len(m.searchOptions)-1 {
			m.searchCursor++
		}
		return m, tea.Batch(cmds...)
	case "enter":
		query := strings.TrimSpace(m.searchInput.Value())
		if m.searchCursor >= 0 && m.searchCursor < len(m.searchOptions) {
			query = m.searchOptions[m.searchCursor] // run the highlighted suggestion
		}
		if query != "" {
			return m.startSearch(query, cmds)
		}
		return m, tea.Batch(cmds...)
	}
	var inputCmd tea.Cmd
	m.searchInput, inputCmd = m.searchInput.Update(msg)
	cmds = append(cmds, inputCmd)
	m.refreshSearchOptions(&cmds)
	return m, tea.Batch(cmds...)
}

// closeSearch leaves search-entry mode without running a query.
func (m *Model) closeSearch() {
	m.searchMode = false
	m.searchInput.Blur()
	m.searchOptions = nil
	m.searchCursor = -1
}

// refreshSearchOptions recomputes the list shown under the search box: recent
// history while the box is empty, live suggestions otherwise. Typing schedules
// a debounced, generation-tagged suggestions fetch so only the latest request
// is applied.
func (m *Model) refreshSearchOptions(cmds *[]tea.Cmd) {
	q := strings.TrimSpace(m.searchInput.Value())
	m.suggGen++
	if q == "" {
		m.searchHistMode = true
		m.searchOptions = m.searchHistory
		m.searchCursor = -1
		m.suggestions = nil
		return
	}
	m.searchHistMode = false
	m.searchOptions = nil
	m.suggestions = nil
	m.searchCursor = -1
	gen := m.suggGen
	*cmds = append(*cmds, tea.Tick(suggestionDebounce, func(time.Time) tea.Msg {
		return suggestionTickMsg{query: q, gen: gen}
	}))
}

// startSearch leaves search-entry mode, records the query in history, and kicks
// off the search.
func (m Model) startSearch(query string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	query = strings.TrimSpace(query)
	m.searchMode = false
	m.searchInput.Blur()
	m.searchInput.SetValue(query)
	m.searchOptions = nil
	m.searchCursor = -1
	m.status = "searching..."
	m.searchHistory = store.AddToHistory(m.searchHistory, query)
	cmds = append(cmds, m.searchCmd(query))
	return m, tea.Batch(cmds...)
}

func (m Model) updateActiveList(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.tab {
	case TabSearch:
		m.searchList, cmd = m.searchList.Update(msg)
	case TabLibrary:
		m.libraryList, cmd = m.libraryList.Update(msg)
	case TabPlaylists:
		if m.inPlaylist {
			m.trackList, cmd = m.trackList.Update(msg)
		} else {
			m.playlistList, cmd = m.playlistList.Update(msg)
		}
	case TabPage:
		m.pageList, cmd = m.pageList.Update(msg)
	}
	return m, cmd
}

func (m Model) handleSelect() (Model, tea.Cmd) {
	if m.tab == TabHome {
		sec, entry, ok := m.currentHome().selected()
		if !ok {
			return m, nil
		}
		switch entry.kind {
		case entryTrack:
			tracks, idx := sec.tracks, entry.trackIdx
			if idx < 0 || idx >= len(tracks) {
				return m, nil
			}
			return m, func() tea.Msg { return m.startPlayback(tracks, idx, sec.title, 0, false, nil) }
		case entryCollection:
			c := entry.collection
			return m, m.loadHomeCollectionCmd(c.BrowseID, c.Params, c.Title)
		}
		return m, nil
	}
	l := m.activeListPtr()
	if l == nil {
		return m, nil
	}
	switch it := l.SelectedItem().(type) {
	case trackItem:
		items := l.Items()
		tracks := tracksFromItems(items)
		idx := indexOfTrack(items, it.track.VideoID)
		if idx < 0 || len(tracks) == 0 {
			return m, nil
		}
		from := m.playbackSource()
		return m, func() tea.Msg { return m.startPlayback(tracks, idx, from, 0, false, nil) }
	case browseItem:
		return m.openBrowseItem(it.it)
	case playlistItem:
		return m.openPlaylistInTab(it.pl)
	}
	return m, nil
}

// openPlaylistInTab opens a library playlist in the Playlists tab, serving cached
// tracks instantly and refreshing in the background.
func (m Model) openPlaylistInTab(pl ytmusic.Playlist) (Model, tea.Cmd) {
	m.tab = TabPlaylists
	m.inPlaylist = true
	m.playlistID = pl.PlaylistID
	if m.store != nil {
		if tracks, savedAt, ok := m.store.LoadPlaylistTracks(pl.PlaylistID); ok && len(tracks) > 0 {
			m.playlistTracks = tracks
			m.trackList.Title = pl.Title
			m.trackList.SetItems(tracksToItems(tracks))
			m.status = "refreshing " + pl.Title + "…"
			slog.Debug("playlist tracks served from cache",
				"id", pl.PlaylistID, "n", len(tracks), "age", time.Since(savedAt))
			return m, m.loadPlaylistTracksCmd(pl.PlaylistID, pl.Title)
		}
	}
	return m, m.loadPlaylistTracksCmd(pl.PlaylistID, pl.Title)
}

// --- Async commands ---

func (m Model) searchCmd(query string) tea.Cmd {
	client := m.ytClient
	filter := m.searchFilter
	st := m.store
	return func() tea.Msg {
		if st != nil {
			if err := st.AddSearchHistory(query); err != nil {
				slog.Debug("save search history", "err", err)
			}
		}
		tracks, items, err := client.SearchFiltered(query, filter)
		return searchResultMsg{tracks: tracks, items: items, filter: filter, err: err}
	}
}

// suggestCmd fetches live suggestions for query, tagged with the generation of
// the request so a stale response can be discarded.
func (m Model) suggestCmd(query string, gen int) tea.Cmd {
	client := m.ytClient
	return func() tea.Msg {
		sugg, err := client.GetSearchSuggestions(query)
		return suggestionsMsg{query: query, gen: gen, sugg: sugg, err: err}
	}
}

func (m Model) loadLibraryCmd() tea.Cmd {
	client := m.ytClient
	st := m.store
	return func() tea.Msg {
		slog.Debug("loadLibrary: fetching liked songs")
		tracks, err := client.GetLikedSongs()
		slog.Debug("loadLibrary: done", "n", len(tracks), "err", err)
		if err == nil && st != nil {
			if serr := st.SaveLibrary(tracks); serr != nil {
				slog.Warn("save library cache", "err", serr)
			}
		}
		return libraryMsg{tracks: tracks, err: err}
	}
}

func (m Model) loadPlaylistsCmd() tea.Cmd {
	client := m.ytClient
	st := m.store
	return func() tea.Msg {
		slog.Debug("loadPlaylists: fetching")
		playlists, err := client.GetPlaylists()
		slog.Debug("loadPlaylists: done", "n", len(playlists), "err", err)
		if err == nil && st != nil {
			if serr := st.SavePlaylists(playlists); serr != nil {
				slog.Warn("save playlists cache", "err", serr)
			}
		}
		return playlistsMsg{playlists: playlists, err: err}
	}
}

func (m Model) loadPlaylistTracksCmd(playlistID, title string) tea.Cmd {
	client := m.ytClient
	st := m.store
	return func() tea.Msg {
		slog.Debug("loadPlaylistTracks: fetching", "id", playlistID)
		tracks, err := client.GetPlaylistTracks(playlistID)
		slog.Debug("loadPlaylistTracks: done", "n", len(tracks), "err", err)
		if err == nil && st != nil {
			if serr := st.SavePlaylistTracks(playlistID, tracks); serr != nil {
				slog.Warn("save playlist tracks cache", "id", playlistID, "err", serr)
			}
		}
		return playlistTracksMsg{tracks: tracks, title: title, err: err}
	}
}

// loadHomeCmd assembles the Discover feed from the mood/genre landing page, the
// personalised home feed, charts, and new releases. Each page is best-effort:
// a single failing request does not blank the whole tab. All four run in one
// background command so opening Home never blocks the UI.
func (m Model) loadHomeCmd() tea.Cmd {
	client := m.ytClient
	return func() tea.Msg {
		if client == nil {
			return homeMsg{err: fmt.Errorf("no client")}
		}
		var sections []ytmusic.Section
		var firstErr error
		add := func(s []ytmusic.Section, err error) {
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			sections = append(sections, s...)
		}
		moods, mErr := client.GetMoods()
		add(moods, mErr)
		home, hErr := client.GetHome()
		add(home, hErr)
		charts, cErr := client.GetCharts()
		add(charts, cErr)
		releases, rErr := client.GetNewReleases()
		add(releases, rErr)
		slog.Debug("home feed loaded", "sections", len(sections))
		if len(sections) == 0 {
			return homeMsg{err: firstErr}
		}
		return homeMsg{sections: sections}
	}
}

// loadHomeCollectionCmd opens a Discover collection tile (playlist, album, or
// mood/genre category) in the background.
func (m Model) loadHomeCollectionCmd(browseID, params, title string) tea.Cmd {
	client := m.ytClient
	return func() tea.Msg {
		if client == nil {
			return homeCollectionMsg{title: title, err: fmt.Errorf("no client")}
		}
		sections, err := client.GetCollection(browseID, params, title)
		return homeCollectionMsg{title: title, sections: sections, err: err}
	}
}

// startPlayback is called inside a tea.Cmd goroutine. It looks up the stream
// URL (cache first, then resolved in-process) and starts mpv, returning an mpvStartedMsg.
// startPos > 0 seeks to that position on start (for resume). shuffle/order
// restore a persisted shuffled session (order may be nil).
func (m Model) startPlayback(tracks []ytmusic.Track, index int, fromPlaylist string, startPos float64, shuffle bool, order []int) tea.Msg {
	if len(tracks) == 0 || index < 0 || index >= len(tracks) {
		return mpvStartedMsg{err: fmt.Errorf("invalid track index")}
	}
	track := tracks[index]

	url, err := m.resolveMedia(track)
	if err != nil {
		slog.Error("stream resolution failed", "videoID", track.VideoID, "err", err)
		return mpvStartedMsg{err: err}
	}

	slog.Info("starting playback", "videoID", track.VideoID, "title", track.Title, "startPos", startPos)
	mpvInst, err := player.Start(url, mediaTitle(track), startPos, player.StartOptions{
		Gapless:          m.gapless,
		CrossfadeSeconds: m.crossfadeSeconds,
		NormalizeAudio:   m.normalizeAudio,
	})
	if err != nil {
		slog.Error("mpv start failed", "err", err)
		return mpvStartedMsg{err: err}
	}
	q := player.NewQueue(tracks, index)
	if shuffle || len(order) > 0 {
		q.RestoreOrder(order, shuffle)
	}
	return mpvStartedMsg{mpv: mpvInst, queue: q, fromPlaylist: fromPlaylist}
}

// saveStateCmd persists the exact queue (ordered tracks, current index,
// position, shuffle flag/order, and origin) to the DB (fire-and-forget).
func (m Model) saveStateCmd() tea.Cmd {
	if m.store == nil || m.queue == nil {
		return nil
	}
	shuffled := m.queue.Shuffled()
	var order []int
	if shuffled {
		order = m.queue.Order()
	}
	state := store.LastPlayingState{
		Tracks:       m.queue.Tracks(),
		Index:        m.queue.CurrentIndex(),
		Position:     m.playerState.Position,
		FromPlaylist: m.fromPlaylist,
		Shuffle:      shuffled,
		Order:        order,
	}
	st := m.store
	return func() tea.Msg {
		_ = st.SaveLastPlaying(state)
		return nil
	}
}

// prefetchTracksCmd warms the stream-URL cache for the first few tracks
// of a freshly loaded list via the serial background prefetcher. Resolving an
// entire playlist up front floods YouTube with requests and triggers its
// abuse/bot throttle (GOOGLE_ABUSE / ENHANCE_YOUR_CALM), so this stays
// deliberately tiny; everything else resolves on demand or via neighbour
// prefetch once playback starts.
func (m Model) prefetchTracksCmd(tracks []ytmusic.Track) tea.Cmd {
	p := m.prefetcher
	if p == nil {
		return nil
	}
	const warm = 5
	if len(tracks) > warm {
		tracks = tracks[:warm]
	}
	head := tracks
	return func() tea.Msg {
		p.Prewarm(head)
		return prefetchDoneMsg{}
	}
}

// prefetchNeighbors warms the next few (prefetch_ahead, default 2) and the
// previous one track relative to the current queue position, in the background,
// so n/p skips find their stream URL already cached (only a loadfile is then
// needed).
func (m Model) prefetchNeighbors() {
	if m.prefetcher == nil || m.queue == nil {
		return
	}
	ahead := m.prefetchAheadOr()
	if ahead < 1 {
		ahead = 1
	}
	m.prefetcher.Prewarm(m.queue.Neighbors(ahead, 1))
}

// volumeStepOr, seekStepOr and prefetchAheadOr return the configured tuning
// values, falling back to the built-in defaults when a Model was constructed
// without options (e.g. in unit tests).
func (m Model) volumeStepOr() int {
	if m.volumeStep > 0 {
		return m.volumeStep
	}
	return 5
}

func (m Model) seekStepOr() int {
	if m.seekStep > 0 {
		return m.seekStep
	}
	return 5
}

func (m Model) prefetchAheadOr() int {
	if m.prefetchAhead > 0 {
		return m.prefetchAhead
	}
	return 2
}

func (m Model) advanceQueueCmd(delta int) tea.Cmd {
	if m.queue == nil || m.mpv == nil {
		return nil
	}
	var track ytmusic.Track
	var ok bool
	if delta > 0 {
		track, ok = m.queue.Next()
	} else {
		track, ok = m.queue.Prev()
	}
	if !ok {
		return nil
	}
	return m.loadTrackCmd(track)
}

// jumpToQueueCmd moves the queue index to the given absolute index and returns
// a command that loads that track. It is the Up Next panel's Enter action.
func (m Model) jumpToQueueCmd(index int) tea.Cmd {
	if m.queue == nil || m.mpv == nil {
		return nil
	}
	track, ok := m.queue.SetCurrent(index)
	if !ok {
		return nil
	}
	return m.loadTrackCmd(track)
}

// jumpToUpNextCmd loads the track currently selected by the Up Next cursor.
func (m Model) jumpToUpNextCmd() tea.Cmd {
	idx, ok := m.upNextTargetIndex()
	if !ok {
		return nil
	}
	return m.jumpToQueueCmd(idx)
}

// upNextTargetIndex returns the absolute queue index selected by the Up Next
// cursor, or ok=false when no track is upcoming.
func (m Model) upNextTargetIndex() (int, bool) {
	if m.queue == nil {
		return 0, false
	}
	return m.queue.UpcomingIndex(m.upNextCursor)
}

// moveUpNext shifts the Up Next cursor down (down=true) or up, clamped to the
// number of upcoming tracks so it can never point past the end of the queue.
func (m *Model) moveUpNext(down bool) {
	if m.queue == nil {
		return
	}
	n := m.queue.UpcomingCount()
	if n <= 0 {
		m.upNextCursor = 0
		return
	}
	if down {
		if m.upNextCursor < n-1 {
			m.upNextCursor++
		}
	} else if m.upNextCursor > 0 {
		m.upNextCursor--
	}
}

// loadTrackCmd resolves a track's stream URL (cache, then yt-dlp on miss) and
// returns the message that loads it into the existing mpv.
// LoadURL unpauses, so a skip or jump always starts playing at 0:00.
func (m Model) loadTrackCmd(track ytmusic.Track) tea.Cmd {
	return func() tea.Msg {
		url, err := m.resolveMedia(track)
		if err != nil {
			return mpvLoadMsg{err: err}
		}
		return mpvLoadMsg{url: url, title: mediaTitle(track)}
	}
}

// --- Shuffle, URL refresh, radio & MPRIS ---

// toggleShuffle flips queue shuffle mode and returns the OSD command. The
// current track keeps playing; turning shuffle off restores the original order.
func (m *Model) toggleShuffle() []tea.Cmd {
	if m.queue == nil {
		return nil
	}
	on := !m.queue.Shuffled()
	m.queue.SetShuffle(on)
	if on {
		return []tea.Cmd{m.showOSD("⇄  shuffle on")}
	}
	return []tea.Cmd{m.showOSD("⇄  shuffle off")}
}

// retriableTrack reports whether t may be retried after a failed load (it has a
// videoID and has not already been retried once this session).
func (m Model) retriableTrack(t ytmusic.Track) bool {
	return t.VideoID != "" && !m.retried[t.VideoID]
}

// refreshAndLoadCmd evicts the cached (presumably expired) stream URL for t,
// re-resolves it, and returns the message that loads the fresh URL. The retry
// is bounded by retriedTrack, so a persistently broken track is not looped.
func (m Model) refreshAndLoadCmd(t ytmusic.Track) tea.Cmd {
	cache := m.urlCache
	cookies := m.cookiesFile
	return func() tea.Msg {
		cache.Evict(t.VideoID)
		url, err := streamResolver(cookies, t.URL)
		if err != nil {
			slog.Warn("stream URL refresh failed", "videoID", t.VideoID, "err", err)
			return mpvLoadMsg{err: fmt.Errorf("stream URL refresh: %w", err)}
		}
		cache.Set(t.VideoID, url)
		slog.Info("stream URL refreshed", "videoID", t.VideoID)
		return mpvLoadMsg{url: url, title: mediaTitle(t)}
	}
}

// radioCmd fetches YouTube Music's radio mix for the current (last) track so
// playback can continue when the queue is exhausted. It is rate-limited and
// guarded against concurrent fetches; it returns nil when radio should not run.
func (m *Model) radioCmd() tea.Cmd {
	if m.queue == nil || m.ytClient == nil || m.radioLoading {
		return nil
	}
	if m.radioAppended >= radioTotalMax {
		return nil
	}
	t, ok := m.queue.Current()
	if !ok || t.VideoID == "" {
		return nil
	}
	// Don't refetch the same seed repeatedly within the cooldown window (e.g.
	// when the mix yields nothing new and mpv keeps ending).
	if m.radioSource == t.VideoID && time.Since(m.lastRadioAt) < radioCooldown {
		return nil
	}
	m.radioLoading = true
	m.radioSource = t.VideoID
	m.lastRadioAt = time.Now()
	client := m.ytClient
	return func() tea.Msg {
		tracks, err := client.GetRadio(t.VideoID)
		return radioMsg{tracks: tracks, err: err}
	}
}

// appendRadio appends deduped radio tracks to the queue, capped by radioAppendMax
// per fetch and radioQueueCap overall. It returns how many were appended.
func (m *Model) appendRadio(tracks []ytmusic.Track) int {
	if m.queue == nil {
		return 0
	}
	room := radioTotalMax - m.radioAppended
	if room <= 0 {
		return 0
	}
	seen := make(map[string]bool)
	for _, t := range m.queue.Tracks() {
		seen[t.VideoID] = true
	}
	var fresh []ytmusic.Track
	for _, t := range tracks {
		if t.VideoID == "" || seen[t.VideoID] {
			continue
		}
		seen[t.VideoID] = true
		fresh = append(fresh, t)
		if len(fresh) >= radioAppendMax || len(fresh) >= room {
			break
		}
	}
	added := m.queue.Append(fresh)
	m.radioAppended += added
	return added
}

// handleMPRISCommand turns an MPRIS control command into TUI commands.
func (m *Model) handleMPRISCommand(cmd mpris.Command) []tea.Cmd {
	var cmds []tea.Cmd
	switch cmd.Action {
	case mpris.ActionPlay:
		if m.mpv != nil {
			m.mpv.Play()
		} else if m.resumeState != nil {
			cmds = append(cmds, m.resumeCmd())
		}
	case mpris.ActionPause:
		if m.mpv != nil {
			m.mpv.Pause()
		}
	case mpris.ActionPlayPause:
		if m.mpv != nil {
			m.mpv.PlayPause()
		} else if m.resumeState != nil {
			cmds = append(cmds, m.resumeCmd())
		}
	case mpris.ActionStop:
		if m.mpv != nil {
			_ = m.mpv.Stop()
		}
	case mpris.ActionNext:
		if cmd := m.advanceQueueCmd(1); cmd != nil {
			m.autoAdvancing = true
			cmds = append(cmds, cmd)
		}
	case mpris.ActionPrevious:
		if cmd := m.advanceQueueCmd(-1); cmd != nil {
			m.autoAdvancing = true
			cmds = append(cmds, cmd)
		}
	case mpris.ActionSeek:
		if m.mpv != nil {
			_ = m.mpv.Seek(cmd.Offset.Seconds())
		}
	case mpris.ActionSetPosition:
		if m.mpv != nil {
			delta := cmd.Position.Seconds() - m.playerState.Position
			_ = m.mpv.Seek(delta)
		}
	case mpris.ActionSetVolume:
		if m.mpv != nil {
			vol := int(cmd.Volume*100 + 0.5)
			_ = m.mpv.SetVolume(vol)
			cmds = append(cmds, m.showVolumeOSD(vol))
		}
	case mpris.ActionSetShuffle:
		if m.queue != nil {
			m.queue.SetShuffle(cmd.Shuffle)
			if cmd.Shuffle {
				cmds = append(cmds, m.showOSD("⇄  shuffle on"))
			} else {
				cmds = append(cmds, m.showOSD("⇄  shuffle off"))
			}
		}
	case mpris.ActionSetLoop:
		if m.mpv != nil {
			m.looping = cmd.Loop == "Track"
			_ = m.mpv.SetLoop(m.looping)
		}
	case mpris.ActionQuit:
		cmds = append(cmds, tea.Quit)
	}
	return cmds
}

// resumeCmd starts playback from the persisted resume state.
func (m *Model) resumeCmd() tea.Cmd {
	rs := m.resumeState
	return func() tea.Msg {
		return m.startPlayback(rs.Tracks, rs.Index, rs.FromPlaylist, rs.Position, rs.Shuffle, rs.Order)
	}
}

// updateMPRIS publishes the current playback snapshot to MPRIS (best-effort).
func (m *Model) updateMPRIS() {
	if m.mpris == nil {
		return
	}
	snap := mpris.Snapshot{
		Playing:  m.playerState.Playing,
		Position: time.Duration(m.playerState.Position * float64(time.Second)),
		Duration: time.Duration(m.playerState.Duration * float64(time.Second)),
		Volume:   float64(m.playerState.Volume) / 100,
		Loop:     m.looping,
	}
	if t, ok := m.currentTrack(); ok && m.mpv != nil {
		snap.HasTrack = true
		snap.VideoID = t.VideoID
		snap.Title = t.Title
		snap.Artist = t.Artist
		snap.Album = t.Album
	}
	if m.queue != nil {
		snap.CanNext = m.queue.HasNext()
		snap.CanPrev = m.queue.HasPrev()
		snap.Shuffle = m.queue.Shuffled()
	}
	m.mpris.Update(snap)
}

// --- Sleep timer, notifications & Discord RPC ---

// checkSleep fires once when the sleep timer's deadline passes: it stops
// playback, shows an OSD, and sends a desktop notification. It is driven by the
// existing tick, so it never blocks the UI.
func (m *Model) checkSleep() []tea.Cmd {
	if !m.sleep.Active() || !m.sleep.Expired(time.Now()) {
		return nil
	}
	m.sleep.Cancel()
	var cmds []tea.Cmd
	if m.mpv != nil {
		slog.Info("sleep timer fired — stopping playback")
		_ = m.mpv.Stop()
	}
	cmds = append(cmds, m.showOSD("😴  sleep timer — playback stopped"))
	cmds = append(cmds, m.notifyTextCmd("ytm", "Sleep timer — playback stopped"))
	if m.discord != nil {
		m.discord.Clear()
	}
	return cmds
}

// updateSleepMode handles keys while the sleep-timer menu is open.
func (m Model) updateSleepMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "1":
		return m.armSleep(15)
	case "2":
		return m.armSleep(30)
	case "3":
		return m.armSleep(45)
	case "4":
		return m.armSleep(60)
	case "d":
		return m.armSleep(int(m.sleepDefault / time.Minute))
	case "0", "c", "x", "backspace":
		m.sleep.Cancel()
		m.sleepMode = false
		return m, m.showOSD("😴  sleep timer off")
	case "esc", "q", "S":
		m.sleepMode = false
		return m, nil
	}
	return m, nil
}

// armSleep closes the menu and arms the sleep timer for the given minutes.
func (m Model) armSleep(minutes int) (tea.Model, tea.Cmd) {
	if minutes <= 0 {
		minutes = 30
	}
	m.sleep.Set(time.Duration(minutes)*time.Minute, time.Now())
	m.sleepMode = false
	return m, m.showOSD(fmt.Sprintf("😴  sleep in %dm", minutes))
}

// sleepIndicator returns a compact countdown for the header, or "" when no
// timer is armed.
func (m Model) sleepIndicator() string {
	if !m.sleep.Active() {
		return ""
	}
	return "😴 " + formatDuration(m.sleep.Remaining(time.Now()).Seconds())
}

// notifyTrackCmd returns a command that announces a track change. It runs on a
// tea.Cmd goroutine, so a slow notifier never stalls the UI.
func (m Model) notifyTrackCmd(t ytmusic.Track) tea.Cmd {
	title := t.Title
	if title == "" {
		title = mediaTitle(t)
	}
	summary := "▶  " + title
	body := t.Artist
	return m.notifyTextCmd(summary, body)
}

// notifyTextCmd returns a best-effort notification command.
func (m Model) notifyTextCmd(summary, body string) tea.Cmd {
	return func() tea.Msg {
		if err := sendNotification(summary, body); err != nil {
			slog.Debug("desktop notification failed", "err", err)
		}
		return nil
	}
}

// updateDiscord publishes the current track to Discord Rich Presence. It is a
// no-op without a client. force sends unconditionally (on a track change);
// otherwise it only sends when the play/pause state changed, so idle ticks stay
// quiet. Elapsed time is conveyed via the activity timestamp.
func (m *Model) updateDiscord(force bool) {
	if m.discord == nil {
		return
	}
	stateChanged := !m.discordInit || m.playerState.Playing != m.discordPlaying
	m.discordInit = true
	m.discordPlaying = m.playerState.Playing
	if !force && !stateChanged {
		return
	}
	t, ok := m.currentTrack()
	if !ok || m.mpv == nil {
		m.discord.Clear()
		return
	}
	a := discordrpc.Activity{Details: t.Title, State: t.Artist}
	if m.playerState.Playing {
		a.StartTime = time.Now().Add(-time.Duration(m.playerState.Position * float64(time.Second)))
	}
	m.discord.SetActivity(a)
}

// --- Album art, lyrics & OSD helpers ---

// currentTrack returns the currently playing/queued track.
func (m Model) currentTrack() (ytmusic.Track, bool) {
	if m.queue == nil {
		return ytmusic.Track{}, false
	}
	return m.queue.Current()
}

// onTrackChanged kicks off the async work that depends on the now-current
// track: fetching its album art, and refreshing lyrics if the panel is open.
func (m *Model) onTrackChanged() []tea.Cmd {
	var cmds []tea.Cmd
	t, ok := m.currentTrack()
	if !ok {
		return cmds
	}
	// The track changed: point the Up Next cursor at the new first upcoming.
	m.upNextCursor = 0
	m.artLoading = t.VideoID
	if cmd := m.albumArtCmd(t); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Reset the lyrics view for the new track (the memory cache persists, so
	// switching back to a previously seen track is instant).
	m.lyricsScroll = 0
	m.lyricsFollow = true
	if m.lyricsVideoID != t.VideoID {
		m.lyrics = nil
		m.lyricsSynced = false
		m.lyricsNoMatch = false
		m.lyricsLoading = false
		m.lyricsVideoID = ""
	}
	if m.showLyrics {
		if cmd := m.ensureLyrics(t); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if m.notifyTrackChange {
		cmds = append(cmds, m.notifyTrackCmd(t))
	}
	m.updateDiscord(true)
	return cmds
}

// albumArtCmd returns a command that fetches and renders the track's art,
// unless it is already cached at the current width or the terminal is narrow.
func (m Model) albumArtCmd(t ytmusic.Track) tea.Cmd {
	// Art is only shown in the wide Now Playing layout, so don't fetch it on
	// terminals too narrow to render it.
	if t.VideoID == "" || m.width < upNextMinWidth {
		return nil
	}
	if m.artCache != nil {
		if _, ok := m.artCache[artKey(t.VideoID, m.width)]; ok {
			return nil
		}
	}
	width := m.width
	return func() tea.Msg {
		rendered, err := fetchAndRenderArt(t, width)
		return artResultMsg{videoID: t.VideoID, width: width, rendered: rendered, err: err}
	}
}

// currentArt returns the cached rendered art for the current track/width.
func (m Model) currentArt() (string, bool) {
	t, ok := m.currentTrack()
	if !ok {
		return "", false
	}
	art, ok := m.artCache[artKey(t.VideoID, m.width)]
	return art, ok && art != ""
}

// ensureLyrics makes the lyrics for t available in the model, using the memory
// cache when possible and otherwise spawning a fetch. It reports whether a
// fetch is already in flight for this track so callers don't duplicate work.
func (m *Model) ensureLyrics(t ytmusic.Track) tea.Cmd {
	if t.VideoID == "" {
		return nil
	}
	if m.lyricsVideoID == t.VideoID &&
		(m.lyricsLoading || m.lyrics != nil || m.lyricsNoMatch) {
		return nil
	}
	if lines, ok := m.lyricsCache[t.VideoID]; ok {
		m.lyricsVideoID = t.VideoID
		m.lyricsLoading = false
		m.lyrics = lines.Lines
		m.lyricsSynced = lines.Synced
		m.lyricsNoMatch = lines.empty()
		m.lyricsScroll = 0
		return nil
	}
	m.lyricsVideoID = t.VideoID
	m.lyricsLoading = true
	m.lyrics = nil
	m.lyricsSynced = false
	m.lyricsNoMatch = false
	m.lyricsScroll = 0
	return func() tea.Msg {
		lyrics, found, err := fetchLyricsCached(t)
		return lyricsMsg{videoID: t.VideoID, lyrics: lyrics, found: found, err: err}
	}
}

// showOSD displays informational text transiently, bumping the generation so
// any older auto-hide timer is ignored. It returns the hide timer command.
func (m *Model) showOSD(text string) tea.Cmd {
	m.osdError = false
	return m.showTransient(text)
}

// showToast displays a transient error/warning toast (rendered in the error
// style). It shares the OSD slot with showOSD.
func (m *Model) showToast(text string) tea.Cmd {
	m.osdError = true
	return m.showTransient(text)
}

// showTransient bumps the OSD generation, sets the text, and schedules the
// auto-hide. A newer OSD supersedes any pending hide timer via the generation.
func (m *Model) showTransient(text string) tea.Cmd {
	m.osdGen++
	m.osdText = text
	m.osdVisible = true
	gen := m.osdGen
	return tea.Tick(1200*time.Millisecond, func(time.Time) tea.Msg {
		return osdHideMsg{gen: gen}
	})
}

// flashError shows a friendly, transient error toast and records a friendly
// status line. It never surfaces a raw error string to the user.
func (m *Model) flashError(prefix string, err error) tea.Cmd {
	friendly := friendlyError(err)
	if prefix != "" {
		m.status = prefix
	} else {
		m.status = friendly
	}
	return m.showToast("⚠  " + friendly)
}

// friendlyError maps an error to a short, human message. It is deliberately
// heuristic: recognising common transport/HTTP failures is more useful than
// showing the user a stack of Go error text.
func friendlyError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "not authenticated"),
		strings.Contains(s, "unauthorized"),
		strings.Contains(s, "401"):
		return "sign-in expired — run `ytm auth`"
	case strings.Contains(s, "456"), strings.Contains(s, "abuse"),
		strings.Contains(s, "rate limit"), strings.Contains(s, "too many"),
		strings.Contains(s, "429"):
		return "YouTube is rate-limiting — try again shortly"
	case strings.Contains(s, "403"), strings.Contains(s, "forbidden"):
		return "this stream expired — retrying"
	case strings.Contains(s, "no such host"),
		strings.Contains(s, "connection refused"),
		strings.Contains(s, "no route to host"),
		strings.Contains(s, "network is unreachable"),
		strings.Contains(s, "dial tcp"),
		strings.Contains(s, "timeout"), strings.Contains(s, "timed out"),
		strings.Contains(s, "tls"):
		return "network problem — check your connection"
	case strings.Contains(s, "yt-dlp"):
		return "couldn't resolve the audio stream"
	default:
		return "something went wrong"
	}
}

// showVolumeOSD shows the volume indicator and records the level so the next
// tick does not immediately re-show it as an external change.
func (m *Model) showVolumeOSD(vol int) tea.Cmd {
	m.lastVolume = vol
	return m.showOSD(volumeOSD(vol))
}

// volumeOSD formats the volume indicator: an icon, a ten-segment bar, and the
// percentage.
func volumeOSD(vol int) string {
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}
	const segs = 10
	filled := (vol*segs + 50) / 100
	icon := "🔊"
	if vol == 0 {
		icon = "🔇"
	}
	bar := strings.Repeat("▇", filled) + strings.Repeat("▁", segs-filled)
	return fmt.Sprintf("%s %s  %d%%", icon, bar, vol)
}

// artPlaceholder reserves the art area while the real image is loading. It is
// sized to the grid a square source would occupy, which is the common case for
// album art and keeps the layout stable as the real image arrives.
func artPlaceholder(cols, rows int) string {
	if cols < 1 || rows < 1 {
		return ""
	}
	line := styleMuted.Render(strings.Repeat("·", cols))
	out := make([]string, rows)
	for i := range out {
		out[i] = line
	}
	return strings.Join(out, "\n")
}

// clip truncates s to at most width display cells, adding an ellipsis.
func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	if len(r) > width-1 {
		r = r[:width-1]
	}
	return string(r) + "…"
}

// --- View ---

func (m Model) View() string {
	if m.paletteOpen {
		return m.paletteView()
	}
	if m.showHelp {
		return m.helpView()
	}
	if m.sleepMode {
		return m.sleepMenuView()
	}

	header := m.headerView()
	playerBar := m.playerBarView()
	headerH := lipgloss.Height(header)
	playerH := lipgloss.Height(playerBar)
	contentH := m.height - headerH - playerH
	if contentH < 1 {
		contentH = 1
	}

	content := m.contentView(contentH)
	return lipgloss.JoinVertical(lipgloss.Left, header, content, playerBar)
}

func (m Model) headerView() string {
	tabs := []struct {
		key   string
		label string
		tab   Tab
	}{
		{"0", "Home", TabHome},
		{"1", "Now Playing", TabNowPlaying},
		{"2", "Search", TabSearch},
		{"3", "Library", TabLibrary},
		{"4", "Playlists", TabPlaylists},
		{"5", "Stats", TabStats},
	}
	// On narrow terminals the long tab names would overflow; fall back to the
	// numeric keys only.
	compact := m.width < 88
	var parts []string
	for _, t := range tabs {
		label := fmt.Sprintf("[%s:%s]", t.key, t.label)
		if compact {
			label = "[" + t.key + "]"
		}
		if t.tab == m.tab {
			accent := accentForTab(t.tab)
			parts = append(parts, lipgloss.NewStyle().
				Bold(true).
				Foreground(colSelFg).
				Background(accent).
				Padding(0, 1).
				Render(label))
		} else {
			parts = append(parts, lipgloss.NewStyle().
				Foreground(accentForTab(t.tab)).
				Faint(true).
				Padding(0, 1).
				Render(label))
		}
	}
	tabBar := "  " + strings.Join(parts, " ")

	hint := "[?] help   [ctrl+k] commands   [q]uit"
	if compact {
		hint = "[?]"
	}
	right := styleMuted.Render(hint)
	if ind := m.sleepIndicator(); ind != "" {
		right = styleMuted.Render(ind+"   ") + right
	}
	line := fitLine(tabBar, right, m.width)
	divider := lipgloss.NewStyle().Foreground(accentForTab(m.tab)).Render(strings.Repeat("─", m.width))
	return line + "\n" + divider
}

// fitLine lays left and right on one line, padding the middle so the result is
// exactly width columns wide. When both cannot fit, the right-hand hint is
// dropped; when left alone is still too wide, it is clipped. left is expected to
// be a small, styled fragment (tab bar), so dropping right is preferable to
// mangling ANSI sequences.
func fitLine(left, right string, width int) string {
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if lw+rw <= width {
		gap := width - lw - rw
		return left + strings.Repeat(" ", gap) + right
	}
	if lw <= width {
		return left
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(left)
}

func (m Model) contentView(height int) string {
	// Modal overlays take over the content area.
	switch {
	case m.nameMode:
		return m.namePromptView(height)
	case m.pickMode:
		m.pickList.SetHeight(height)
		return m.pickList.View()
	}
	switch m.tab {
	case TabNowPlaying:
		return m.nowPlayingView(height)
	case TabSearch:
		return m.searchTabView(height)
	case TabLibrary:
		m.libraryList.SetHeight(height)
		return m.libraryList.View()
	case TabPlaylists:
		if m.inPlaylist {
			m.trackList.SetHeight(height)
			return m.trackList.View()
		}
		m.playlistList.SetHeight(height)
		return m.playlistList.View()
	case TabHome:
		return m.homeTabView(height)
	case TabPage:
		return m.pageTabView(height)
	case TabStats:
		return m.statsView(height)
	}
	return ""
}

// pageTabView renders a pushed artist/album/playlist page: a header line (title
// + subtitle) above the track/album list.
func (m Model) pageTabView(height int) string {
	accent := accentForTab(TabPage)
	title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(clip(m.pageTitle, m.width-4))
	kindLabel := ""
	switch m.pageKind {
	case pageAlbum:
		kindLabel = "album"
	case pageArtist:
		kindLabel = "artist"
	case pagePlaylist:
		kindLabel = "playlist"
	}
	sub := m.pageSubtitle
	if kindLabel != "" {
		if sub != "" {
			sub = kindLabel + " · " + sub
		} else {
			sub = kindLabel
		}
	}
	header := "  " + title
	if sub != "" {
		header += "   " + styleMuted.Render(clip(sub, m.width-6-lipgloss.Width(title)))
	}
	header += "\n  " + styleMuted.Render("[esc] back   [enter] play   [a]/[A] artist/album   [x] remove")
	divider := lipgloss.NewStyle().Foreground(accent).Render(strings.Repeat("─", m.width))
	head := header + "\n" + divider
	headH := lipgloss.Height(head)
	listH := height - headH
	if listH < 1 {
		listH = 1
	}
	m.pageList.SetHeight(listH)
	return head + "\n" + m.pageList.View()
}

func (m Model) namePromptView(height int) string {
	prompt := lipgloss.NewStyle().Bold(true).Foreground(accentForTab(TabPlaylists)).
		Render("  New playlist name:")
	line := prompt + "  > " + m.nameInput.View()
	hint := styleMuted.Render("  enter to create · esc to cancel")
	divider := strings.Repeat("─", m.width)
	return lipgloss.JoinVertical(lipgloss.Left,
		"", line, divider, hint,
		vertCenter(styleMuted.Render("a new private playlist will be created on your account"), maxInt(1, height-5)),
	)
}

func (m Model) searchTabView(height int) string {
	prompt := "  "
	if m.searchMode {
		prompt = "> "
	}
	inputLine := prompt + m.searchInput.View()
	filter := lipgloss.NewStyle().Bold(true).Foreground(accentForTab(TabSearch)).
		Render(m.searchFilter.Label())
	filterLine := "  filter: " + filter + styleMuted.Render("   [f]/[F] change")
	divider := strings.Repeat("─", m.width)
	header := inputLine + "\n" + filterLine + "\n" + divider
	headerH := lipgloss.Height(header)

	listH := height - headerH
	if listH < 1 {
		listH = 1
	}

	// While typing, show recent searches (empty box) or live suggestions in
	// place of the results list.
	if m.searchMode && len(m.searchOptions) > 0 {
		return header + "\n" + m.searchOptionsView(listH)
	}
	if m.searchMode && m.searchHistMode {
		return header + "\n" + styleMuted.Render("  no recent searches — type to search")
	}

	m.searchList.SetHeight(listH)
	return header + "\n" + m.searchList.View()
}

// searchOptionsView renders the history/suggestions list shown under the search
// box. The highlighted option (searchCursor) is what Enter will run.
func (m Model) searchOptionsView(height int) string {
	label := "Suggestions"
	if m.searchHistMode {
		label = "Recent searches"
	}
	lines := []string{styleMuted.Render("  " + label)}
	avail := height - 1
	if avail < 1 {
		avail = 1
	}
	start, end := windowRange(m.searchCursor, len(m.searchOptions), avail)
	for i := start; i < end; i++ {
		opt := clip(m.searchOptions[i], m.width-4)
		if i == m.searchCursor {
			lines = append(lines, lipgloss.NewStyle().
				Bold(true).Foreground(colSelFg).Background(colSelBg).
				Render(" > "+opt+" "))
		} else {
			lines = append(lines, styleMuted.Render("   "+opt))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) playerBarView() string {
	accent := accentForTab(m.tab)
	divider := lipgloss.NewStyle().Foreground(accent).Render(strings.Repeat("─", m.width))

	if m.mpv == nil {
		if m.confirmMode && m.confirmPrompt != "" {
			return divider + "\n" + lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#FCA5A5"}).
				Render("  ⚠ "+m.confirmPrompt)
		}
		if m.filterMode {
			return divider + "\n" + lipgloss.NewStyle().Bold(true).Foreground(accent).
				Render("  filter: "+m.filterInput.Value()+"▌  [esc] clear · [enter] keep")
		}
		line := "  no track  ·  0 home  ·  2 search  ·  3 library  ·  4 playlists  ·  enter to play"
		if m.pomodoro.Running() {
			line = "  " + m.pomodoroOSD() + "  ·  enter to play"
		}
		switch {
		case m.osdVisible && m.osdText != "":
			return divider + "\n" + lipgloss.NewStyle().Bold(true).Foreground(accent).Render("  "+m.osdText)
		case m.status != "":
			return divider + "\n" + styleMuted.Render("  "+m.status)
		}
		return divider + "\n" + styleMuted.Render(clip(line, m.width))
	}

	symbol := "▶"
	symStyle := lipgloss.NewStyle().Foreground(colPlaying)
	if !m.playerState.Playing {
		symbol = "⏸"
		symStyle = styleMuted
	}
	sym := symStyle.Render(symbol)

	pomo := m.pomodoroIndicator()
	title := m.playerState.Title
	if title == "" {
		title = "loading..."
	}
	prefix := "  " + sym + "  "
	prefixW := lipgloss.Width(prefix)
	timeStr := pomo + fmt.Sprintf("%s / %s", formatDuration(m.playerState.Position), formatDuration(m.playerState.Duration))
	timeW := lipgloss.Width(timeStr)

	var line1 string
	if m.width >= prefixW+timeW+8 {
		// Room for the title, a gap, and the elapsed/total time.
		titleW := m.width - prefixW - timeW - 2
		if titleW < 1 {
			titleW = 1
		}
		left := prefix + clip(title, titleW)
		gap := m.width - lipgloss.Width(left) - timeW
		if gap < 1 {
			gap = 1
		}
		line1 = left + strings.Repeat(" ", gap) + styleMuted.Render(timeStr)
	} else {
		// Too narrow for both: keep the title, drop the time.
		titleW := m.width - prefixW - 1
		if titleW < 0 {
			titleW = 0
		}
		line1 = prefix + clip(title, titleW)
	}

	// Progress bar
	barW := m.width - 2
	var bar string
	if m.playerState.Duration > 0 && barW > 4 {
		filled := int(float64(barW) * m.playerState.Position / m.playerState.Duration)
		if filled > barW {
			filled = barW
		}
		if filled < 0 {
			filled = 0
		}
		filledStr := lipgloss.NewStyle().Foreground(colBar).Render(strings.Repeat("█", filled))
		emptyStr := styleMuted.Render(strings.Repeat("╌", barW-filled))
		bar = " " + filledStr + emptyStr
	}

	// The last line is used for the transient OSD when active, otherwise the
	// persistent status message. A pending confirmation takes precedence. Both
	// are clipped so they can never overflow.
	extra := ""
	textW := m.width - 2
	if textW < 0 {
		textW = 0
	}
	switch {
	case m.confirmMode && m.confirmPrompt != "":
		extra = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#FCA5A5"}).
			Render("  ⚠ " + m.confirmPrompt)
	case m.filterMode:
		extra = lipgloss.NewStyle().Bold(true).Foreground(accent).
			Render("  filter: " + m.filterInput.Value() + "▌  [esc] clear · [enter] keep")
	case m.osdVisible && m.osdText != "":
		style := lipgloss.NewStyle().Bold(true).Foreground(accent)
		if m.osdError {
			style = styleErr.Bold(true)
		}
		extra = style.Render("  " + clip(m.osdText, textW))
	case m.status != "":
		extra = styleMuted.Render("  " + clip(m.status, textW))
	}

	out := divider + "\n" + line1
	if bar != "" {
		out += "\n" + bar
	}
	if extra != "" {
		out += "\n" + extra
	}
	return out
}

func (m Model) nowPlayingView(height int) string {
	if m.mpv == nil {
		if m.resumeState != nil && len(m.resumeState.Tracks) > 0 {
			rs := m.resumeState
			idx := rs.Index
			if idx < 0 || idx >= len(rs.Tracks) {
				idx = 0
			}
			cur := rs.Tracks[idx]
			status := fmt.Sprintf("stopped at %s  ·  track %d / %d", formatDuration(rs.Position), idx+1, len(rs.Tracks))
			if rs.Shuffle {
				status += "  ·  ⇄ shuffle"
			}
			lines := []string{
				"",
				"    " + styleTitle.Render(cur.Title),
				"    " + styleMuted.Render(cur.Artist),
				"    " + styleMuted.Render(status),
				"",
				"    " + styleMuted.Render("press  space  or  enter  to resume"),
			}
			if rs.FromPlaylist != "" {
				lines = append([]string{lines[0], "    " + styleMuted.Render("from "+rs.FromPlaylist)}, lines[1:]...)
			}
			return vertCenter(strings.Join(lines, "\n"), height)
		}
		msg := lipgloss.NewStyle().Padding(0, 3).
			Render(styleMuted.Render("Nothing playing  ·  0 home  ·  2 search  ·  3 library  ·  4 playlists"))
		return vertCenter(msg, height)
	}
	if m.queue == nil {
		return vertCenter("  "+styleMuted.Render("loading…"), height)
	}

	cur, _ := m.queue.Current()

	// Wide terminals lay out everything: album art on the left; a right column
	// with the track header on top and, beneath it, lyrics (left) beside the
	// Up Next queue (right). Narrow terminals drop the art and prioritise the
	// Up Next list; lyrics stay toggleable with `y`.
	wide := m.width >= upNextMinWidth

	var artBlock string
	artCols := 0
	if wide {
		var artRows int
		artCols, artRows = artSize(m.width, 1, 1)
		if art, _ := m.currentArt(); art != "" {
			artBlock = art
		} else {
			artBlock = artPlaceholder(artCols, artRows)
		}
	}

	infoW := m.infoWidth(wide, artCols)
	header := m.trackHeader(cur, infoW)
	headerH := lipgloss.Height(header)
	panelH := height - headerH - 2 // header + one blank spacer line
	if panelH < 1 {
		panelH = 1
	}

	var right string
	switch {
	case wide && m.showLyrics:
		// Lyrics and Up Next side by side, each padded to a fixed column so the
		// gutters line up regardless of content width.
		const gap = 3
		upNextW := infoW / 3
		if upNextW < 22 {
			upNextW = 22
		}
		if upNextW > 40 {
			upNextW = 40
		}
		lyricsW := infoW - upNextW - gap
		if lyricsW < 20 {
			lyricsW = 20
			upNextW = infoW - lyricsW - gap
			if upNextW < 1 {
				upNextW = 1
			}
		}
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			padBlock(m.lyricsPanel(lyricsW, panelH), lyricsW, panelH),
			strings.Repeat(" ", gap),
			padBlock(m.upNextPanel(upNextW, panelH), upNextW, panelH),
		)
		right = header + "\n\n" + row
	case wide:
		right = header + "\n\n" + m.upNextPanel(infoW, panelH)
	case m.showLyrics:
		// Narrow: lyrics on top, Up Next beneath; both share the space.
		lyricsH := panelH / 2
		if lyricsH < 1 {
			lyricsH = 1
		}
		upH := panelH - lyricsH
		if upH < 1 {
			upH = 1
		}
		right = header + "\n\n" + m.lyricsPanel(infoW, lyricsH) + "\n" + m.upNextPanel(infoW, upH)
	default:
		right = header + "\n\n" + m.upNextPanel(infoW, panelH)
	}

	body := indentLeft(right, 2)
	if artBlock != "" {
		body = indentLeft(lipgloss.JoinHorizontal(lipgloss.Top, artBlock, "   ", right), 2)
	}
	return vertCenter(body, height)
}

// indentLeft prepends n spaces to every line of s. Plain concatenation would
// only pad the first line, leaving the rest (art rows, panels) unaligned.
func indentLeft(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// padBlock pads every line of s to exactly width display cells and appends
// blank lines until the block is `height` lines tall. This keeps side-by-side
// columns aligned with consistent gutters independent of their content.
func padBlock(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if w := lipgloss.Width(line); w < width {
			lines[i] = line + strings.Repeat(" ", width-w)
		}
	}
	blank := strings.Repeat(" ", width)
	for len(lines) < height {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

// infoWidth returns the width available for the info/lyrics column. artCols is
// the rendered art width (0 when art is hidden).
func (m Model) infoWidth(showArt bool, artCols int) int {
	w := m.width - 4
	if showArt {
		// 2 left gutter + art + 3 inter-column gap + 2 right margin.
		w = m.width - 2 - artCols - 3 - 2
	}
	if w < 20 {
		w = 20
	}
	return w
}

// trackHeader renders the compact, always-visible track header shown in the
// right column of Now Playing: the title (bold accent), artist, album (muted),
// the progress bar with elapsed/total time, and a transport/queue line. It is
// rendered whether or not the lyrics panel is open, so the track's identity and
// position are never lost behind the lyrics.
func (m Model) trackHeader(cur ytmusic.Track, width int) string {
	accent := accentForTab(TabNowPlaying)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(accent)
	artistStyle := lipgloss.NewStyle().Foreground(colText)

	lines := []string{titleStyle.Render(clip(cur.Title, width))}

	artist := cur.Artist
	if artist == "" {
		artist = "Unknown artist"
	}
	lines = append(lines, artistStyle.Render(clip(artist, width)))

	meta := cur.Album
	if m.fromPlaylist != "" {
		if meta != "" {
			meta += "  ·  from " + m.fromPlaylist
		} else {
			meta = "from " + m.fromPlaylist
		}
	}
	if meta == "" {
		meta = " "
	}
	lines = append(lines, styleMuted.Render(clip(meta, width)))

	// Progress bar with elapsed / total time on the same line, right-aligned.
	// On very narrow widths the time is dropped so the bar cannot overflow.
	pos, dur := m.playerState.Position, m.playerState.Duration
	timeStr := fmt.Sprintf("%s / %s", formatDuration(pos), formatDuration(dur))
	timeW := lipgloss.Width(timeStr)
	showTime := width >= timeW+12
	barW := width - 4
	if showTime {
		barW = width - timeW - 4
	}
	if barW < 6 {
		barW = 6
	}
	filled := 0
	if dur > 0 {
		filled = int(float64(barW) * pos / dur)
	}
	if filled > barW {
		filled = barW
	}
	if filled < 0 {
		filled = 0
	}
	filledStr := lipgloss.NewStyle().Foreground(colBar).Render(strings.Repeat("█", filled))
	emptyStr := styleMuted.Render(strings.Repeat("╌", barW-filled))
	if showTime {
		lines = append(lines, "  "+filledStr+emptyStr+"  "+styleMuted.Render(timeStr))
	} else {
		lines = append(lines, "  "+filledStr+emptyStr)
	}

	// Transport / queue line. The optional loop/shuffle indicators are only
	// appended while they fit, so the line never overflows.
	symbol := "▶"
	symStyle := lipgloss.NewStyle().Foreground(colPlaying)
	if !m.playerState.Playing {
		symbol = "⏸"
		symStyle = styleMuted
	}
	loopIcon := styleMuted.Render("↺ off")
	if m.looping {
		loopIcon = lipgloss.NewStyle().Foreground(colPlaying).Render("↺ on")
	}
	shuffleIcon := styleMuted.Render("⇄ off")
	if m.queue != nil && m.queue.Shuffled() {
		shuffleIcon = lipgloss.NewStyle().Foreground(colPlaying).Render("⇄ on")
	}
	likeIcon := styleMuted.Render(ratingIcon(m.ratingOf(cur.VideoID)))
	switch m.ratingOf(cur.VideoID) {
	case ytmusic.RatingLike:
		likeIcon = lipgloss.NewStyle().Foreground(colPlaying).Render("♥")
	case ytmusic.RatingDislike:
		likeIcon = styleMuted.Render("✖")
	}
	symPart := "  " + symStyle.Render(symbol) + "  "
	trackStr := fmt.Sprintf("track %d / %d", m.queue.CurrentIndex()+1, m.queue.Len())
	if lipgloss.Width(symPart)+lipgloss.Width(trackStr) > width {
		trackStr = fmt.Sprintf("%d/%d", m.queue.CurrentIndex()+1, m.queue.Len())
	}
	transport := symPart + styleMuted.Render(trackStr)
	transport = appendFits(transport, "    "+loopIcon, width)
	transport = appendFits(transport, "  "+shuffleIcon, width)
	transport = appendFits(transport, "  "+likeIcon, width)
	lines = append(lines, transport)

	return strings.Join(lines, "\n")
}

// appendFits appends extra to line only when the result still fits in width.
func appendFits(line, extra string, width int) string {
	if lipgloss.Width(line)+lipgloss.Width(extra) <= width {
		return line + extra
	}
	return line
}

// upNextPanel renders the Up Next queue column: the current track is marked
// with a ▶ "now playing" indicator and the following tracks (up to
// maxUpNextRows) are listed beneath. j/k move the selection cursor and Enter
// loads the highlighted track (which starts playing at 0:00).
func (m Model) upNextPanel(width, height int) string {
	accent := accentForTab(TabNowPlaying)
	title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("⏭  Up Next")
	header := title
	// Pick the richest hint that still fits the panel; main added the reorder
	// hint, feat/polish's narrow rework requires nothing overflow.
	fullHint := styleMuted.Render("   j/k move · enter play · J/K reorder · x remove")
	shortHint := styleMuted.Render("   j/k move · enter play")
	switch {
	case width >= 52 && lipgloss.Width(title)+lipgloss.Width(fullHint) <= width:
		header = title + fullHint
	case width >= 30 && lipgloss.Width(title)+lipgloss.Width(shortHint) <= width:
		header = title + shortHint
	}

	if m.queue == nil || m.queue.Len() == 0 {
		return header + "\n\n" + styleMuted.Render("queue empty")
	}
	curTrack, ok := m.queue.Current()
	if !ok {
		return header + "\n\n" + styleMuted.Render("queue empty")
	}
	upcoming := m.queue.UpcomingCount()

	// Reserve the header line and one blank spacer; the rest is the list, with
	// one row always held for the current track.
	avail := height - 2
	if avail < 1 {
		avail = 1
	}
	listRows := avail - 1
	if listRows < 1 {
		listRows = 1
	}
	if listRows > maxUpNextRows {
		listRows = maxUpNextRows
	}

	// Keep the selected row inside the window.
	cursor := m.upNextCursor
	if cursor < 0 {
		cursor = 0
	}
	if upcoming > 0 && cursor >= upcoming {
		cursor = upcoming - 1
	}
	first := 0
	if cursor >= listRows {
		first = cursor - listRows + 1
	}
	last := first + listRows
	if last > upcoming {
		last = upcoming
	}

	// The upcoming track indices in playback order (shuffle-aware).
	upcomingIdx := m.queue.UpcomingIndexes(last)

	lines := []string{header, ""}

	// The current track, always marked so it is clear where we are.
	marker := lipgloss.NewStyle().Foreground(colPlaying).Render("▶ ")
	curLine := marker + lipgloss.NewStyle().Bold(true).Foreground(colText).
		Render(clip(curTrack.Title, width-4))
	lines = append(lines, curLine)

	for r := first; r < last && r < len(upcomingIdx); r++ {
		idx := upcomingIdx[r]
		tr, ok := m.queue.TrackAt(idx)
		if !ok {
			continue
		}
		label := clip(fmt.Sprintf("%d. %s — %s", idx+1, tr.Title, tr.Artist), width-4)
		if r == cursor {
			lines = append(lines, lipgloss.NewStyle().
				Bold(true).Foreground(colSelFg).Background(colSelBg).
				Render(" > "+label+" "))
		} else {
			lines = append(lines, styleMuted.Render("   "+label))
		}
	}
	if last < upcoming {
		lines = append(lines, styleMuted.Render(fmt.Sprintf("   … %d more", upcoming-last)))
	}
	return strings.Join(lines, "\n")
}

// lyricsPanel renders the scrollable lyrics column. height bounds the panel's
// total line count (its own header included). When the lyrics are synced the
// active line is highlighted and, unless the user has taken over scrolling, the
// view follows it.
func (m Model) lyricsPanel(width, height int) string {
	accent := accentForTab(TabNowPlaying)
	header := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("♪  Lyrics")
	if width >= 34 {
		hint := "    y hide · [/] scroll"
		if m.lyricsSynced {
			if m.lyricsFollow {
				hint += " · f follow:on"
			} else {
				hint += " · f follow:off"
			}
		}
		header += styleMuted.Render(hint)
	}

	switch {
	case m.lyricsLoading:
		return header + "\n\n" + styleMuted.Render("loading lyrics…")
	case m.lyricsNoMatch:
		return header + "\n\n" + styleMuted.Render("no lyrics found for this track")
	case len(m.lyrics) == 0:
		return header + "\n\n" + styleMuted.Render("no lyrics")
	}

	avail := height - 3 // panel header, blank spacer, and the "more lines" hint
	if avail < 1 {
		avail = 1
	}
	active := -1
	if m.lyricsSynced {
		active = activeLyricLine(m.lyrics, m.playerState.Position)
	}
	start := m.lyricsStart(avail, active)
	end := minInt(len(m.lyrics), start+avail)

	lines := []string{header, ""}
	for i := start; i < end; i++ {
		lines = append(lines, m.renderLyricLine(i, active, width))
	}
	if end < len(m.lyrics) {
		lines = append(lines, styleMuted.Render(fmt.Sprintf("…  %d more lines", len(m.lyrics)-end)))
	}
	return strings.Join(lines, "\n")
}

// renderLyricLine renders one lyric line, marking and styling the active line
// when it matches. Every line gets a two-cell gutter so highlighting does not
// shift the text.
func (m Model) renderLyricLine(i, active, width int) string {
	text := clip(m.lyrics[i].text, width-2)
	if i == active {
		marker := lipgloss.NewStyle().Foreground(colAccentNP).Bold(true).Render("▶ ")
		return marker + lipgloss.NewStyle().Bold(true).Foreground(colAccentNP).Render(text)
	}
	return "  " + text
}

// lyricsStart returns the index of the first lyric line to render. When
// auto-follow is on and a synced line is active it centres the active line
// (clamped to the list); otherwise it uses the manual scroll offset.
func (m Model) lyricsStart(avail, active int) int {
	total := len(m.lyrics)
	if avail < 1 {
		avail = 1
	}
	maxStart := total - avail
	if maxStart < 0 {
		maxStart = 0
	}
	if m.lyricsFollow && active >= 0 {
		start := active - avail/2
		if start < 0 {
			start = 0
		}
		if start > maxStart {
			start = maxStart
		}
		return start
	}
	start := m.lyricsScroll
	if start < 0 {
		start = 0
	}
	if start > maxStart {
		start = maxStart
	}
	return start
}

// lyricsViewportRows estimates how many lyric lines are visible with the
// current layout. It mirrors the panel sizing in nowPlayingView so a manual
// scroll can anchor at the same position the renderer used.
func (m Model) lyricsViewportRows() int {
	headerH := 5 // track header lines
	panelH := m.height - headerH - 2
	if panelH < 1 {
		panelH = 1
	}
	if m.width < upNextMinWidth {
		panelH /= 2 // narrow layout stacks lyrics above Up Next
	}
	avail := panelH - 3
	if avail < 1 {
		avail = 1
	}
	return avail
}

// scrollLyrics moves the lyrics viewport by delta lines, clamped to range.
// Manual scrolling turns off auto-follow, anchoring at the current on-screen
// position first so the view does not jump.
func (m *Model) scrollLyrics(delta int) {
	if len(m.lyrics) == 0 {
		return
	}
	if m.lyricsFollow && m.lyricsSynced {
		active := activeLyricLine(m.lyrics, m.playerState.Position)
		m.lyricsScroll = m.lyricsStart(m.lyricsViewportRows(), active)
	}
	m.lyricsFollow = false
	m.lyricsScroll += delta
	if m.lyricsScroll < 0 {
		m.lyricsScroll = 0
	}
	if m.lyricsScroll > len(m.lyrics)-1 {
		m.lyricsScroll = len(m.lyrics) - 1
	}
}

// vertCenter pads content with blank lines to center it vertically.
func vertCenter(content string, height int) string {
	contentH := strings.Count(content, "\n") + 1
	pad := (height - contentH) / 2
	if pad < 0 {
		pad = 0
	}
	top := strings.Repeat("\n", pad)
	return top + content
}

// mediaTitle formats a track's display title for mpv/MPRIS: "Title - Artist".
// When the artist is unknown it falls back to just the title; when the title is
// empty it falls back to the artist (or an empty string when both are missing).
func mediaTitle(t ytmusic.Track) string {
	switch {
	case t.Title == "":
		return t.Artist
	case t.Artist == "":
		return t.Title
	default:
		return t.Title + " - " + t.Artist
	}
}

// sleepMenuView renders the sleep-timer chooser.
func (m Model) sleepMenuView() string {
	accent := accentForTab(TabNowPlaying)
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(accent)

	mins := int(m.sleepDefault / time.Minute)
	if mins <= 0 {
		mins = 30
	}

	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(accent).Render("😴  Sleep timer"),
		"",
	}
	if m.sleep.Active() {
		lines = append(lines,
			styleMuted.Render("  active — "+formatDuration(m.sleep.Remaining(time.Now()).Seconds())+" remaining"),
			"")
	}
	lines = append(lines,
		"  "+keyStyle.Render("1")+"   15 minutes",
		"  "+keyStyle.Render("2")+"   30 minutes",
		"  "+keyStyle.Render("3")+"   45 minutes",
		"  "+keyStyle.Render("4")+"   60 minutes",
		"  "+keyStyle.Render("d")+"   "+fmt.Sprintf("%d minutes (default)", mins),
		"",
		"  "+keyStyle.Render("0")+"   turn off",
		"  "+keyStyle.Render("esc")+" cancel",
	)
	return vertCenter(lipgloss.NewStyle().Padding(1, 3).Render(strings.Join(lines, "\n")), m.height)
}

// --- Helpers ---

func (m *Model) resizeLists() {
	m.searchList.SetWidth(m.width)
	m.libraryList.SetWidth(m.width)
	m.playlistList.SetWidth(m.width)
	m.trackList.SetWidth(m.width)
	m.pageList.SetWidth(m.width)
	m.pickList.SetWidth(m.width)
	m.searchInput.Width = m.width - 4
	m.filterInput.Width = m.width - 4
	m.nameInput.Width = m.width - 4
}

// List item adapters.

type trackItem struct {
	track  ytmusic.Track
	marked bool
}

func (t trackItem) Title() string {
	if t.marked {
		return "✔ " + t.track.Title
	}
	return t.track.Title
}
func (t trackItem) Description() string {
	s := t.track.Artist
	if t.track.Duration != "" {
		s += "  " + t.track.Duration
	}
	return s
}
func (t trackItem) FilterValue() string {
	if t.track.Artist != "" {
		return t.track.Title + " " + t.track.Artist
	}
	return t.track.Title
}

type playlistItem struct{ pl ytmusic.Playlist }

func (p playlistItem) Title() string       { return p.pl.Title }
func (p playlistItem) Description() string { return fmt.Sprintf("%d tracks", p.pl.Count) }
func (p playlistItem) FilterValue() string { return p.pl.Title }

// browseItem adapts a navigable album/artist/playlist tile for the list model.
type browseItem struct{ it ytmusic.BrowseItem }

func (b browseItem) Title() string { return b.it.Title }
func (b browseItem) Description() string {
	if b.it.Subtitle != "" {
		return b.it.Subtitle
	}
	return b.it.Kind
}
func (b browseItem) FilterValue() string { return b.it.Title + " " + b.it.Subtitle }

func tracksToItems(tracks []ytmusic.Track) []list.Item {
	items := make([]list.Item, len(tracks))
	for i, t := range tracks {
		items[i] = trackItem{track: t}
	}
	return items
}

func playlistsToItems(pls []ytmusic.Playlist) []list.Item {
	items := make([]list.Item, len(pls))
	for i, p := range pls {
		items[i] = playlistItem{p}
	}
	return items
}

// extractStreamURL and runYtDlp now live in resolve.go, alongside the measured
// fast-path/fallback choice.

func browseToItems(items []ytmusic.BrowseItem) []list.Item {
	out := make([]list.Item, len(items))
	for i, it := range items {
		out[i] = browseItem{it}
	}
	return out
}

// tracksFromItems extracts trackItems (in list order) from a list's items.
func tracksFromItems(items []list.Item) []ytmusic.Track {
	var out []ytmusic.Track
	for _, it := range items {
		if t, ok := it.(trackItem); ok {
			out = append(out, t.track)
		}
	}
	return out
}

// indexOfTrack returns the position of the track with the given videoID among
// the trackItems of items, or -1.
func indexOfTrack(items []list.Item, videoID string) int {
	idx := 0
	for _, it := range items {
		t, ok := it.(trackItem)
		if !ok {
			continue
		}
		if t.track.VideoID == videoID {
			return idx
		}
		idx++
	}
	return -1
}

func formatDuration(sec float64) string {
	if sec <= 0 {
		return "0:00"
	}
	s := int(sec)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
