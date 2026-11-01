package player

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPositionResetOnFileChange verifies that the cached position/duration are
// zeroed when a new file begins, so a freshly loaded track reads 0:00 instead
// of the previous track's position (which is what the UI used to show).
func TestPositionResetOnFileChange(t *testing.T) {
	cases := []struct {
		name  string
		event string
	}{
		{"start-file", `{"event":"start-file"}`},
		{"file-loaded", `{"event":"file-loaded"}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &MPV{}
			// Seed a stale position/duration as the previous track left them.
			m.handleLine([]byte(`{"event":"property-change","name":"time-pos","data":95.5}`))
			m.handleLine([]byte(`{"event":"property-change","name":"duration","data":200}`))
			if st := m.GetState(); st.Position != 95.5 || st.Duration != 200 {
				t.Fatalf("seed failed: pos=%v dur=%v", st.Position, st.Duration)
			}

			// A file transition must reset the cache to zero.
			m.handleLine([]byte(c.event))
			if st := m.GetState(); st.Position != 0 || st.Duration != 0 {
				t.Errorf("%s: cache not reset: pos=%v dur=%v", c.name, st.Position, st.Duration)
			}
			// A pending "ended" from the old track must also be cleared.
			m.ended.Store(true)
			m.handleLine([]byte(c.event))
			if m.TakeEnded() {
				t.Errorf("%s: ended signal must be cleared on a file transition", c.name)
			}

			// mpv's next property-change repopulates the reset cache.
			m.handleLine([]byte(`{"event":"property-change","name":"time-pos","data":0.4}`))
			m.handleLine([]byte(`{"event":"property-change","name":"duration","data":180}`))
			if st := m.GetState(); st.Position != 0.4 || st.Duration != 180 {
				t.Errorf("%s: cache not repopulated: pos=%v dur=%v", c.name, st.Position, st.Duration)
			}
		})
	}
}

// fakeIPC is a minimal mpv JSON-IPC endpoint: it records the commands a client
// sends and replies "success" to each. It lets the LoadURL command sequence be
// asserted without spawning a real mpv.
type fakeIPC struct {
	path string
	ln   net.Listener
	mu   sync.Mutex
	cmds [][]interface{}
	// props answers get_property requests; a missing name replies "success"
	// with no data, matching mpv's unavailable-property behaviour.
	props map[string]interface{}
}

func newFakeIPC(t *testing.T) *fakeIPC {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mpv.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeIPC{path: path, ln: ln}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeIPC) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(c)
	}
}

func (f *fakeIPC) handle(c net.Conn) {
	defer c.Close()
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		var req struct {
			Command   []interface{} `json:"command"`
			RequestID int64         `json:"request_id"`
		}
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		f.mu.Lock()
		f.cmds = append(f.cmds, req.Command)
		var data interface{}
		var hasData bool
		if len(req.Command) >= 2 && req.Command[0] == "get_property" {
			if name, ok := req.Command[1].(string); ok {
				data, hasData = f.props[name]
			}
		}
		f.mu.Unlock()
		body := map[string]interface{}{
			"request_id": req.RequestID,
			"error":      "success",
		}
		if hasData {
			body["data"] = data
		}
		resp, _ := json.Marshal(body)
		_, _ = c.Write(append(resp, '\n'))
	}
}

func (f *fakeIPC) commands() [][]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]interface{}, len(f.cmds))
	copy(out, f.cmds)
	return out
}

// hasCommand reports whether any recorded command renders exactly like want.
func (f *fakeIPC) hasCommand(want ...interface{}) bool {
	target := fmt.Sprintf("%v", want)
	for _, cmd := range f.commands() {
		if fmt.Sprintf("%v", cmd) == target {
			return true
		}
	}
	return false
}

// TestLoadURLUnpauses verifies that loading a new URL always clears mpv's pause
// state, so a skip issued while paused starts the new track playing.
func TestLoadURLUnpauses(t *testing.T) {
	f := newFakeIPC(t)

	conn, err := net.Dial("unix", f.path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	m := &MPV{conn: conn, pending: make(map[int64]chan ipcResponse), done: make(chan struct{})}
	go m.readLoop()
	t.Cleanup(func() { _ = conn.Close() })

	if err := m.LoadURL("http://example/stream", "Song - Artist"); err != nil {
		t.Fatalf("LoadURL: %v", err)
	}

	if !f.hasCommand("loadfile", "http://example/stream", "replace") {
		t.Errorf("LoadURL must issue loadfile replace; got %v", f.commands())
	}
	if !f.hasCommand("set_property", "pause", false) {
		t.Errorf("LoadURL must set pause=false; got %v", f.commands())
	}
	if !f.hasCommand("set_property", "force-media-title", "Song - Artist") {
		t.Errorf("LoadURL must set the media title; got %v", f.commands())
	}
}

// TestEndedSignalOnEOFNotPause drives the real JSON event path (handleLine) and
// asserts that the one-shot "ended" signal is set only for a genuine track end:
// pausing (pause=true, idle-active=true) must NOT produce it, while
// end-file/eof must, and start-file/file-loaded must clear it.
func TestEndedSignalOnEOFNotPause(t *testing.T) {
	m := &MPV{}

	// Pausing must never look like a track end.
	m.handleLine([]byte(`{"event":"property-change","name":"pause","data":true}`))
	if m.TakeEnded() {
		t.Fatal("pause must not set the ended signal")
	}

	// idle-active becomes true while paused with core-idle semantics, but on
	// its own it must not set the ended signal either.
	m.handleLine([]byte(`{"event":"property-change","name":"idle-active","data":true}`))
	if m.TakeEnded() {
		t.Fatal("idle-active property change alone must not set the ended signal")
	}
	if !m.IsIdle() {
		t.Fatal("IsIdle should reflect idle-active=true")
	}

	// A genuine EOF end-file sets the signal.
	m.handleLine([]byte(`{"event":"end-file","reason":"eof"}`))
	if !m.TakeEnded() {
		t.Fatal("end-file/eof must set the ended signal")
	}
	// Consuming is one-shot.
	if m.TakeEnded() {
		t.Fatal("ended signal must be consume-once")
	}

	// Non-eof end-file (manual stop / redirect) must not set it.
	m.handleLine([]byte(`{"event":"end-file","reason":"stop"}`))
	if m.TakeEnded() {
		t.Fatal("end-file with reason stop must not set the ended signal")
	}

	// start-file clears a pending ended signal.
	m.handleLine([]byte(`{"event":"end-file","reason":"eof"}`))
	m.handleLine([]byte(`{"event":"start-file"}`))
	if m.TakeEnded() {
		t.Fatal("start-file must clear the ended signal")
	}

	// file-loaded clears it too.
	m.handleLine([]byte(`{"event":"end-file","reason":"eof"}`))
	m.handleLine([]byte(`{"event":"file-loaded"}`))
	if m.TakeEnded() {
		t.Fatal("file-loaded must clear the ended signal")
	}

	// idle-active=false is reflected by IsIdle.
	m.handleLine([]byte(`{"event":"property-change","name":"idle-active","data":false}`))
	if m.IsIdle() {
		t.Fatal("IsIdle should be false after idle-active=false")
	}
}

func TestClampVolume(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{-10, 0},
		{0, 0},
		{50, 50},
		{100, 100},
		{101, 100},
		{1000, 100},
	}
	for _, c := range cases {
		if got := clampVolume(c.in); got != c.want {
			t.Errorf("clampVolume(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestStartOptionsArgs verifies the mpv command-line flags built for the
// gapless/crossfade/normalisation toggles.
func TestStartOptionsArgs(t *testing.T) {
	cases := []struct {
		name   string
		opts   StartOptions
		want   []string
		absent []string
	}{
		{
			name:   "defaults disable gapless",
			opts:   StartOptions{},
			want:   []string{"--gapless-audio=no"},
			absent: []string{"--crossfade", "--af="},
		},
		{
			name: "gapless only",
			opts: StartOptions{Gapless: true},
			want: []string{"--gapless-audio=yes"},
		},
		{
			name:   "crossfade implies gapless and fades in",
			opts:   StartOptions{CrossfadeSeconds: 4.5},
			want:   []string{"--gapless-audio=yes", "--af=lavfi=[afade=t=in:st=0:d=4.50]"},
			absent: []string{"--crossfade"},
		},
		{
			name: "normalize adds filter",
			opts: StartOptions{NormalizeAudio: true},
			want: []string{"--gapless-audio=no", "--af=lavfi=[dynaudnorm]"},
		},
		{
			name: "normalize and crossfade share one filter chain",
			opts: StartOptions{NormalizeAudio: true, CrossfadeSeconds: 2},
			want: []string{"--af=lavfi=[dynaudnorm,afade=t=in:st=0:d=2.00]"},
		},
		{
			name:   "negative crossfade clamped to off",
			opts:   StartOptions{CrossfadeSeconds: -3},
			want:   []string{"--gapless-audio=no"},
			absent: []string{"afade"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := c.opts.Args()
			joined := strings.Join(args, "\n")
			for _, w := range c.want {
				if !strings.Contains(joined, w) {
					t.Errorf("Args() = %v, want to contain %q", args, w)
				}
			}
			for _, a := range c.absent {
				if strings.Contains(joined, a) {
					t.Errorf("Args() = %v, must not contain %q", args, a)
				}
			}
		})
	}
}

// TestGetStateIsCachedAndUpdates verifies that GetState/IsIdle are served from
// cache (no socket I/O) while still reflecting mpv's observed properties.
func TestGetStateIsCachedAndUpdates(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv binary not found; skipping IPC test")
	}

	// Isolate the IPC socket for this test.
	origSock := sockPath
	sockPath = filepath.Join(t.TempDir(), "ytm-test-mpv.sock")
	defer func() { sockPath = origSock }()

	// Generate a short local tone with ffmpeg when available; otherwise run mpv
	// idle with no file (Start omits the positional URL).
	streamURL := ""
	if ffmpeg, err := exec.LookPath("ffmpeg"); err == nil {
		wav := filepath.Join(t.TempDir(), "tone.wav")
		cmd := exec.Command(ffmpeg, "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-y", wav)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("ffmpeg failed (%v); falling back to idle mpv. output: %s", err, out)
		} else {
			streamURL = wav
		}
	} else {
		t.Log("ffmpeg not installed; testing idle mpv with no file")
	}

	m, err := Start(streamURL, "test tone", 0, StartOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(m.Quit)

	// With a tone file, wait for observed properties to arrive and confirm they
	// become non-zero — proving events actually update the cache.
	if streamURL != "" {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			st := m.GetState()
			if st.Duration > 0 && st.Position > 0 && !m.IsIdle() {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		st := m.GetState()
		if st.Duration <= 0 {
			t.Errorf("expected non-zero duration for tone file, got %v", st.Duration)
		}
		if st.Position <= 0 {
			t.Errorf("expected non-zero position for tone file, got %v", st.Position)
		}
		if m.IsIdle() {
			t.Errorf("expected IsIdle()==false while tone is playing")
		}
	}

	// Cached reads must be effectively instant.
	const n = 100
	start := time.Now()
	for i := 0; i < n; i++ {
		_ = m.GetState()
		_ = m.IsIdle()
	}
	avg := time.Since(start) / (2 * n)
	t.Logf("cached GetState+IsIdle latency: avg %v over %d calls", avg, 2*n)
	if avg > 5*time.Millisecond {
		t.Errorf("cached reads too slow: avg %v (want < 5ms)", avg)
	}

	// Explicit 200-call average for GetState alone.
	const n2 = 200
	start = time.Now()
	for i := 0; i < n2; i++ {
		_ = m.GetState()
	}
	avgState := time.Since(start) / n2
	t.Logf("GetState latency: avg %v over %d calls", avgState, n2)
	if avgState > 5*time.Millisecond {
		t.Errorf("GetState too slow: avg %v (want < 5ms)", avgState)
	}

	// Quit must terminate the process and report it dead.
	m.Quit()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !m.IsAlive() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if m.IsAlive() {
		t.Errorf("mpv still reported alive after Quit")
	}
}

// TestDefaultSockPath checks that the IPC socket is per-user and per-process:
// a fixed /tmp path let a second instance (or another user) delete and steal the
// first one's mpv.
func TestDefaultSockPath(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "run"))
	got := defaultSockPath()
	want := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), fmt.Sprintf("ytm-mpv-%d.sock", os.Getpid()))
	if got != want {
		t.Errorf("defaultSockPath() = %q, want %q", got, want)
	}

	t.Setenv("XDG_RUNTIME_DIR", "")
	if got, want := defaultSockPath(), filepath.Join(os.TempDir(), fmt.Sprintf("ytm-mpv-%d.sock", os.Getpid())); got != want {
		t.Errorf("defaultSockPath() without XDG_RUNTIME_DIR = %q, want %q", got, want)
	}
}

// TestStartArgsResumeAvoidsPersistentStart guards against the regression where
// a resume position was passed as --start: mpv re-applies that per-file option
// to every later loadfile, so every skip/jump started at the old position.
func TestStartArgsResumeAvoidsPersistentStart(t *testing.T) {
	old := sockPath
	sockPath = "/tmp/ytm-test-mpv.sock"
	t.Cleanup(func() { sockPath = old })

	cases := []struct {
		name      string
		startPos  float64
		wantPause bool
	}{
		{"fresh start", 0, false},
		{"resume", 102, true},
		{"sub-second resume ignored", 0.5, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := startArgs("http://example/stream", "Song", c.startPos, StartOptions{})
			joined := strings.Join(args, "\n")
			if strings.Contains(joined, "--start") {
				t.Errorf("startArgs = %v, must not pass --start (it persists across loadfile)", args)
			}
			if got := strings.Contains(joined, "--pause=yes"); got != c.wantPause {
				t.Errorf("startArgs pause = %v, want %v (args=%v)", got, c.wantPause, args)
			}
			if !strings.Contains(joined, "http://example/stream") || !strings.Contains(joined, "--force-media-title=Song") {
				t.Errorf("startArgs = %v, missing URL or title", args)
			}
		})
	}
}

// TestPendingSeekAppliedOnFileLoaded verifies a resume seek is issued exactly
// once, when the initial file has loaded, and then playback is unpaused.
// TestSeekWhenLoadedAppliesPendingSeek covers the startup race: mpv loads the
// command-line file before the IPC client connects, so the file-loaded event is
// lost and the resume seek must be triggered by polling the duration instead.
func TestSeekWhenLoadedAppliesPendingSeek(t *testing.T) {
	f := newFakeIPC(t)
	f.mu.Lock()
	f.props = map[string]interface{}{"duration": 40.0}
	f.mu.Unlock()

	conn, err := net.Dial("unix", f.path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	m := &MPV{conn: conn, pending: make(map[int64]chan ipcResponse), done: make(chan struct{})}
	go m.readLoop()
	t.Cleanup(func() { _ = conn.Close() })

	m.seekPos, m.hasSeek = 42, true
	go m.seekWhenLoaded()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.hasCommand("seek", float64(42), "absolute") && f.hasCommand("set_property", "pause", false) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !f.hasCommand("seek", float64(42), "absolute") {
		t.Errorf("expected absolute seek to 42; got %v", f.commands())
	}
	if !f.hasCommand("set_property", "pause", false) {
		t.Errorf("expected playback to resume; got %v", f.commands())
	}
	if _, ok := m.takePendingSeek(); ok {
		t.Error("pending seek must be consumed after it is applied")
	}
}

func TestPendingSeekAppliedOnFileLoaded(t *testing.T) {
	f := newFakeIPC(t)
	conn, err := net.Dial("unix", f.path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	m := &MPV{conn: conn, pending: make(map[int64]chan ipcResponse), done: make(chan struct{})}
	go m.readLoop()
	t.Cleanup(func() { _ = conn.Close() })

	m.seekPos, m.hasSeek = 42, true
	m.handleLine([]byte(`{"event":"file-loaded"}`))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.hasCommand("seek", float64(42), "absolute") && f.hasCommand("set_property", "pause", false) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !f.hasCommand("seek", float64(42), "absolute") {
		t.Errorf("expected absolute seek to 42; got %v", f.commands())
	}
	if !f.hasCommand("set_property", "pause", false) {
		t.Errorf("expected playback to resume; got %v", f.commands())
	}
	if _, ok := m.takePendingSeek(); ok {
		t.Error("pending seek must be consumed, not re-applied on the next file")
	}
}

// TestLoadURLClearsPendingSeek ensures a skip before the resume seek lands
// cannot seek the newly chosen track to the old session's position.
func TestLoadURLClearsPendingSeek(t *testing.T) {
	f := newFakeIPC(t)
	conn, err := net.Dial("unix", f.path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	m := &MPV{conn: conn, pending: make(map[int64]chan ipcResponse), done: make(chan struct{})}
	go m.readLoop()
	t.Cleanup(func() { _ = conn.Close() })

	m.seekPos, m.hasSeek = 102, true
	if err := m.LoadURL("http://example/other", "Other"); err != nil {
		t.Fatalf("LoadURL: %v", err)
	}
	if _, ok := m.takePendingSeek(); ok {
		t.Fatal("LoadURL must clear the pending resume seek")
	}
	m.handleLine([]byte(`{"event":"file-loaded"}`))
	time.Sleep(100 * time.Millisecond)
	if f.hasCommand("seek", float64(102), "absolute") {
		t.Errorf("a skip must not seek the new track to the old position; got %v", f.commands())
	}
}
