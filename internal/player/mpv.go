package player

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// sockPath is the unix socket mpv exposes its JSON IPC on. It is a var so tests
// can point at an isolated path in a temp dir.
var sockPath = "/tmp/ytm-mpv.sock"

// requestTimeout bounds how long a caller waits for a single IPC response.
const requestTimeout = 2 * time.Second

// writeTimeout bounds a single socket write.
const writeTimeout = 2 * time.Second

// State represents current playback state.
type State struct {
	Playing  bool
	Title    string
	Position float64 // seconds
	Duration float64 // seconds
	Volume   int
}

// MPV controls an mpv subprocess via JSON IPC.
//
// All socket reads happen in a single background goroutine that dispatches
// responses to waiters by request_id and folds property-change events into a
// cached snapshot. GetState/IsIdle read only that cache, so they are non-blocking
// and safe to call from the UI thread.
type MPV struct {
	cmd  *exec.Cmd
	conn net.Conn

	writeMu sync.Mutex // serializes writes to conn
	pendMu  sync.Mutex // guards pending
	pending map[int64]chan ipcResponse
	nextID  atomic.Int64

	// seekMu guards the one-shot resume seek applied to the first loaded file.
	// It is taken by the reader goroutine on file-loaded and cleared by
	// LoadURL/Stop so it can never leak into a skip or a later load.
	seekMu  sync.Mutex
	seekPos float64
	hasSeek bool

	stateMu  sync.RWMutex
	playing  bool
	title    string
	position float64
	duration float64
	volume   int
	idle     bool

	// ended is a one-shot signal set on end-file/eof and cleared on
	// start-file/file-loaded. Unlike idle-active it is true only for a real
	// track end, never for a pause, so the UI advances exactly once.
	ended atomic.Bool

	// failed is a one-shot signal set when mpv cannot open/play the current
	// file (end-file with reason "error"/"unknown", e.g. an expired googlevideo
	// URL returning 403). It lets the UI evict the cached URL and retry once.
	failed atomic.Bool

	exited    atomic.Bool
	closeOnce sync.Once
	done      chan struct{} // closed when the reader goroutine exits
}

type ipcRequest struct {
	Command   []interface{} `json:"command"`
	RequestID int64         `json:"request_id"`
}

// ipcResponse covers both command replies (request_id/error/data) and events
// (event/name/reason). Only one shape is populated per message.
type ipcResponse struct {
	RequestID int64           `json:"request_id"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`

	Event  string `json:"event"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// observedProps are the mpv properties we subscribe to on connect. Their values
// are pushed to us via property-change events and cached for instant reads.
//
// idle-active is true only when mpv has no file loaded (i.e. the track truly
// ended); core-idle is deliberately NOT used because it is also true while
// paused, which would make a pause look like a track end.
var observedProps = []string{"pause", "time-pos", "duration", "volume", "media-title", "idle-active"}

// StartOptions configures mpv's audio pipeline at startup. The zero value
// disables gapless/crossfade/normalisation and is what tests use.
type StartOptions struct {
	// Gapless enables mpv's gapless-audio mode. Crossfade forces it on.
	Gapless bool
	// CrossfadeSeconds sets a per-track fade-in. mpv has no native crossfade
	// (the option was removed upstream), so this is approximated with a lavfi
	// fade-in combined with gapless playback. 0 disables it.
	CrossfadeSeconds float64
	// NormalizeAudio applies mpv's dynaudnorm filter for level matching.
	NormalizeAudio bool
}

// Args returns the mpv command-line flags implied by the options. It is
// separated from Start so the construction can be unit-tested without spawning
// mpv.
func (o StartOptions) Args() []string {
	var args []string

	crossfade := o.CrossfadeSeconds
	if crossfade < 0 {
		crossfade = 0
	}
	// Crossfade only makes sense with gapless audio, so it implies it.
	gapless := o.Gapless || crossfade > 0
	if gapless {
		args = append(args, "--gapless-audio=yes")
	} else {
		args = append(args, "--gapless-audio=no")
	}

	// Build a single lavfi filter chain so normalisation and the crossfade
	// approximation can coexist (mpv accepts only one --af value).
	var filters []string
	if o.NormalizeAudio {
		// dynaudnorm evens out track loudness without a per-file scan; a good
		// fit for streamed audio.
		filters = append(filters, "dynaudnorm")
	}
	if crossfade > 0 {
		filters = append(filters, fmt.Sprintf("afade=t=in:st=0:d=%.2f", crossfade))
	}
	if len(filters) > 0 {
		args = append(args, "--af=lavfi=["+strings.Join(filters, ",")+"]")
	}
	return args
}

// startArgs builds mpv's command line.
//
// A resume position is deliberately NOT passed as --start: mpv treats that as a
// per-file option and re-applies it to every subsequent loadfile, which would
// make each skip/jump start mid-track. Instead the file is loaded paused and
// handleEvent applies a one-shot absolute seek on file-loaded.
func startArgs(streamURL, title string, startPos float64, opts StartOptions) []string {
	args := []string{
		"--no-video",
		"--idle=yes",       // stay alive after track ends so socket remains open
		"--volume-max=100", // hard cap: volume can never be amplified above 100%
		"--input-ipc-server=" + sockPath,
		"--really-quiet",
	}
	args = append(args, opts.Args()...)
	if title != "" {
		// Surfaces through media-title, so MPRIS/media widgets and the TUI bar
		// show a real name instead of the raw stream URL.
		args = append(args, "--force-media-title="+title)
	}
	if startPos > 1 {
		args = append(args, "--pause=yes")
	}
	if streamURL != "" {
		args = append(args, streamURL)
	}
	return args
}

// Start spawns mpv with the given stream URL and human-readable title.
// startPos > 0 resumes that many seconds into the track: mpv is started paused
// and a one-shot seek is applied once the file finishes loading. opts tunes
// mpv's audio pipeline (gapless, crossfade, normalisation).
func Start(streamURL, title string, startPos float64, opts StartOptions) (*MPV, error) {
	// Clean up stale socket
	os.Remove(sockPath)

	args := startArgs(streamURL, title, startPos, opts)

	cmd := exec.Command("mpv", args...)
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mpv not found or failed to start: %w", err)
	}
	slog.Info("mpv started", "pid", cmd.Process.Pid, "startPos", startPos)

	m := &MPV{
		cmd:     cmd,
		pending: make(map[int64]chan ipcResponse),
		done:    make(chan struct{}),
	}
	if startPos > 1 {
		m.seekPos = startPos
		m.hasSeek = true
	}

	// Wait for socket to appear (up to 3 seconds)
	var conn net.Conn
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			conn = c
			break
		}
	}
	if conn == nil {
		cmd.Process.Kill()
		slog.Error("mpv socket did not appear", "sock", sockPath)
		return nil, fmt.Errorf("mpv socket did not appear at %s", sockPath)
	}
	m.conn = conn
	slog.Debug("mpv IPC socket connected", "sock", sockPath)

	// Reader owns all reads from here on; waiter reaps the process so IsAlive
	// can report real liveness.
	go m.readLoop()
	go m.waitLoop()

	// Subscribe to the properties we poll. Property-change events then keep the
	// cache fresh in the background, with no per-read round trip.
	for i, name := range observedProps {
		m.observe(i+1, name)
	}

	// mpv opens the command-line file as soon as it starts, usually before the
	// IPC socket above is accepted, so its file-loaded event can be missed.
	// Apply a pending resume seek by polling instead of trusting that event.
	if m.hasPendingSeek() {
		go m.seekWhenLoaded()
	}

	return m, nil
}

// LoadURL loads a new URL into the running mpv instance, first setting the
// human-readable media title (shown by MPRIS/media widgets) when provided.
//
// mpv's pause state survives `loadfile replace`, so a skip issued while paused
// would otherwise leave the new track paused. We explicitly unpause after the
// load so every skip/jump starts playing; the user can re-pause afterwards.
func (m *MPV) LoadURL(url, title string) error {
	// A skip/jump supersedes an unapplied resume seek: clearing it keeps the
	// new track at 0:00 instead of the old session's position.
	m.clearPendingSeek()
	if title != "" {
		if err := m.setProperty("force-media-title", title); err != nil {
			slog.Debug("mpv: set force-media-title failed", "err", err)
		}
	}
	if _, err := m.command("loadfile", url, "replace"); err != nil {
		return err
	}
	if err := m.setProperty("pause", false); err != nil {
		slog.Debug("mpv: unpause after load failed", "err", err)
	}
	return nil
}

// PlayPause toggles play/pause.
func (m *MPV) PlayPause() error {
	_, err := m.command("cycle", "pause")
	return err
}

// Pause pauses playback.
func (m *MPV) Pause() error {
	return m.setProperty("pause", true)
}

// Play resumes playback.
func (m *MPV) Play() error {
	return m.setProperty("pause", false)
}

// Seek seeks forward/backward by delta seconds.
func (m *MPV) Seek(delta float64) error {
	_, err := m.command("seek", delta, "relative")
	return err
}

// SetVolume sets volume, clamped to 0–100.
func (m *MPV) SetVolume(vol int) error {
	return m.setProperty("volume", clampVolume(vol))
}

// VolumeUp increases volume by 5, never exceeding 100.
func (m *MPV) VolumeUp() error {
	return m.nudgeVolume(5)
}

// VolumeDown decreases volume by 5, never below 0.
func (m *MPV) VolumeDown() error {
	return m.nudgeVolume(-5)
}

// VolumeBy moves the volume by delta percent relative to mpv's own value,
// clamped to 0–100. It is used when the step is configurable.
func (m *MPV) VolumeBy(delta int) error {
	return m.nudgeVolume(delta)
}

// nudgeVolume moves the volume by delta relative to mpv's own value (so rapid
// steps are never lost to a stale local cache). mpv's --volume-max caps the
// result; we additionally clamp the locally cached value so GetState/the OSD
// can never report more than 100%.
func (m *MPV) nudgeVolume(delta int) error {
	if _, err := m.command("add", "volume", delta); err != nil {
		return err
	}
	m.stateMu.Lock()
	m.volume = clampVolume(m.volume)
	m.stateMu.Unlock()
	return nil
}

// clampVolume bounds v to the 0–100 range.
func clampVolume(v int) int {
	if v > 100 {
		return 100
	}
	if v < 0 {
		return 0
	}
	return v
}

// GetState returns a snapshot of current playback state. It performs no I/O.
func (m *MPV) GetState() State {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return State{
		Playing:  m.playing,
		Title:    m.title,
		Position: m.position,
		Duration: m.duration,
		Volume:   m.volume,
	}
}

// IsIdle returns true when mpv has no file loaded (idle-active), i.e. the
// current track has genuinely finished and mpv is waiting. It reads the cached
// value and performs no I/O. Note: this is true while a track is just paused
// only if idle-active says so — core-idle (which is true while paused) is not
// used. Use TakeEnded for auto-advance decisions.
func (m *MPV) IsIdle() bool {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return m.idle
}

// TakeEnded atomically consumes and returns the one-shot "track ended" signal,
// which is set only on end-file with reason eof and cleared on start-file /
// file-loaded. Returning it consumes it, so a caller advances exactly once per
// real track end and never on a pause.
func (m *MPV) TakeEnded() bool {
	return m.ended.Swap(false)
}

// TakeFailed atomically consumes and returns the one-shot "playback failed"
// signal, set when mpv could not open/play the file (e.g. an expired stream URL
// returning 403). Like TakeEnded it is cleared on a new file so it only fires
// once per failed load.
func (m *MPV) TakeFailed() bool {
	return m.failed.Swap(false)
}

// Stop halts playback and unloads the current file, leaving mpv idle.
func (m *MPV) Stop() error {
	m.clearPendingSeek()
	_, err := m.command("stop")
	return err
}

// Quit terminates mpv.
func (m *MPV) Quit() {
	m.closeOnce.Do(func() {
		slog.Debug("mpv quit")
		m.command("quit")
		if m.conn != nil {
			m.conn.Close()
		}
		if m.cmd != nil && m.cmd.Process != nil {
			m.cmd.Process.Kill()
		}
	})
}

// IsAlive checks if the mpv process is still running.
func (m *MPV) IsAlive() bool {
	if m == nil || m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	return !m.exited.Load()
}

// SetLoop toggles single-track looping.
func (m *MPV) SetLoop(loop bool) error {
	val := "no"
	if loop {
		val = "inf"
	}
	return m.setProperty("loop-file", val)
}

// --- internal helpers ---

func (m *MPV) command(args ...interface{}) (json.RawMessage, error) {
	id := m.nextID.Add(1)
	req := ipcRequest{Command: args, RequestID: id}
	return m.send(req)
}

func (m *MPV) setProperty(name string, value interface{}) error {
	_, err := m.command("set_property", name, value)
	return err
}

func (m *MPV) observe(observeID int, name string) {
	if _, err := m.command("observe_property", observeID, name); err != nil {
		slog.Debug("mpv: observe_property failed", "name", name, "err", err)
	}
}

// send registers a response channel for req.RequestID, writes the request, and
// waits for the matching reply. It never reads the socket itself.
func (m *MPV) send(req ipcRequest) (json.RawMessage, error) {
	ch := make(chan ipcResponse, 1)

	m.pendMu.Lock()
	m.pending[req.RequestID] = ch
	m.pendMu.Unlock()

	defer func() {
		m.pendMu.Lock()
		delete(m.pending, req.RequestID)
		m.pendMu.Unlock()
	}()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	m.writeMu.Lock()
	if m.conn == nil {
		m.writeMu.Unlock()
		return nil, fmt.Errorf("mpv: not connected")
	}
	_ = m.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, werr := m.conn.Write(data)
	m.writeMu.Unlock()
	if werr != nil {
		return nil, werr
	}

	select {
	case resp := <-ch:
		if resp.Error != "" && resp.Error != "success" {
			return nil, fmt.Errorf("mpv: %s", resp.Error)
		}
		return resp.Data, nil
	case <-time.After(requestTimeout):
		return nil, fmt.Errorf("mpv: timeout waiting for response to %v", req.Command)
	case <-m.done:
		return nil, fmt.Errorf("mpv: connection closed")
	}
}

// readLoop is the single owner of reads. It buffers across reads so a response
// split over multiple TCP segments is never lost, and runs until the socket
// closes.
func (m *MPV) readLoop() {
	defer close(m.done)
	r := bufio.NewReader(m.conn)
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			m.handleLine([]byte(line))
		}
		if err != nil {
			m.failPending(err)
			return
		}
	}
}

// waitLoop reaps the child process so IsAlive reports real liveness.
func (m *MPV) waitLoop() {
	err := m.cmd.Wait()
	m.exited.Store(true)
	slog.Debug("mpv process exited", "err", err)
	if m.conn != nil {
		m.conn.Close()
	}
}

func (m *MPV) handleLine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var msg ipcResponse
	if err := json.Unmarshal(line, &msg); err != nil {
		slog.Debug("mpv: bad IPC line", "err", err)
		return
	}
	if msg.Event != "" {
		m.handleEvent(msg)
		return
	}
	m.deliver(msg)
}

// handleEvent folds mpv events into the cached state.
func (m *MPV) handleEvent(msg ipcResponse) {
	switch msg.Event {
	case "property-change":
		m.handlePropertyChange(msg)
	case "start-file", "file-loaded":
		m.setIdle(false)
		// A new file began: any prior end/failure signal belongs to the old
		// track and must not trigger an action for this one.
		m.ended.Store(false)
		m.failed.Store(false)
		// The cached time-pos/duration describe the OLD file until mpv pushes a
		// fresh property-change for the new one. Zero them now so a freshly
		// loaded track reads 0:00 instead of the previous track's position
		// (which would otherwise stick, notably while paused).
		m.resetTimeline()
		if msg.Event == "file-loaded" {
			if pos, ok := m.takePendingSeek(); ok {
				// Run off the reader goroutine: command() waits for a reply that
				// only this goroutine can deliver.
				go m.seekAndResume(pos)
			}
		}
	case "end-file":
		// "eof" means the track genuinely finished; "error"/"unknown" mean mpv
		// could not open or decode the stream (e.g. an expired URL 403s). Other
		// reasons (stop, redirect) are transitions where idle-active settles on
		// its own.
		switch msg.Reason {
		case "eof":
			m.setIdle(true)
			m.ended.Store(true)
		case "error", "unknown":
			m.setIdle(true)
			m.failed.Store(true)
		}
	case "idle":
		m.setIdle(true)
	}
}

func (m *MPV) handlePropertyChange(msg ipcResponse) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	switch msg.Name {
	case "pause":
		var v bool
		if json.Unmarshal(msg.Data, &v) == nil {
			m.playing = !v
		}
	case "time-pos":
		var v float64
		if json.Unmarshal(msg.Data, &v) == nil {
			m.position = v
		}
	case "duration":
		var v float64
		if json.Unmarshal(msg.Data, &v) == nil {
			m.duration = v
		}
	case "volume":
		var v float64
		if json.Unmarshal(msg.Data, &v) == nil {
			m.volume = int(v)
		}
	case "media-title":
		var v string
		if json.Unmarshal(msg.Data, &v) == nil {
			m.title = v
		}
	case "idle-active":
		var v bool
		if json.Unmarshal(msg.Data, &v) == nil {
			m.idle = v
		}
	}
}

func (m *MPV) setIdle(idle bool) {
	m.stateMu.Lock()
	m.idle = idle
	m.stateMu.Unlock()
}

// resetTimeline zeroes the cached position and duration. It is called when a new
// file starts so the UI reads 0:00; mpv's subsequent property-change events
// repopulate both.
func (m *MPV) resetTimeline() {
	m.stateMu.Lock()
	m.position = 0
	m.duration = 0
	m.stateMu.Unlock()
}

// takePendingSeek returns and clears the one-shot resume position, if any.
func (m *MPV) takePendingSeek() (float64, bool) {
	m.seekMu.Lock()
	defer m.seekMu.Unlock()
	pos, ok := m.seekPos, m.hasSeek
	m.seekPos = 0
	m.hasSeek = false
	return pos, ok
}

// clearPendingSeek drops a resume seek that has not been applied yet. A skip or
// stop supersedes it, so it must never seek a track the user chose later.
func (m *MPV) clearPendingSeek() {
	_, _ = m.takePendingSeek()
}

// hasPendingSeek reports whether a resume seek is still waiting to be applied.
func (m *MPV) hasPendingSeek() bool {
	m.seekMu.Lock()
	defer m.seekMu.Unlock()
	return m.hasSeek
}

// seekWhenLoaded applies a pending resume seek once mpv reports a duration for
// the file. It covers the startup case where the command-line file loads before
// the IPC client connects and its file-loaded event is therefore lost.
func (m *MPV) seekWhenLoaded() {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := m.command("get_property", "duration")
		if err == nil {
			var d float64
			if json.Unmarshal(data, &d) == nil && d > 0 {
				if pos, ok := m.takePendingSeek(); ok {
					m.seekAndResume(pos)
				}
				return
			}
		}
		select {
		case <-m.done:
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	slog.Debug("mpv: resume seek abandoned, file never reported a duration")
}

// seekAndResume applies the resume position once the initial file has loaded
// and then unpauses playback (Start loads resume tracks paused so the opening
// bars are not audible before the seek lands).
func (m *MPV) seekAndResume(pos float64) {
	if _, err := m.command("seek", pos, "absolute"); err != nil {
		slog.Debug("mpv: resume seek failed", "pos", pos, "err", err)
	}
	if err := m.setProperty("pause", false); err != nil {
		slog.Debug("mpv: unpause after resume failed", "err", err)
	}
}

// deliver routes a response to its waiter, if any. Buffered channels mean this
// never blocks the reader goroutine.
func (m *MPV) deliver(resp ipcResponse) {
	m.pendMu.Lock()
	ch := m.pending[resp.RequestID]
	delete(m.pending, resp.RequestID)
	m.pendMu.Unlock()
	if ch != nil {
		ch <- resp
	}
}

// failPending unblocks every waiter after the socket dies.
func (m *MPV) failPending(err error) {
	m.pendMu.Lock()
	pend := m.pending
	m.pending = make(map[int64]chan ipcResponse)
	m.pendMu.Unlock()
	for _, ch := range pend {
		ch <- ipcResponse{Error: err.Error()}
	}
}
