// Package discordrpc implements a minimal, best-effort Discord Rich Presence
// client over Discord's local IPC socket, using only the standard library.
//
// It is deliberately defensive: connecting, handshaking, and sending updates
// all happen on a background goroutine, so the caller (the TUI) never blocks.
// Every failure is swallowed (logged at debug level) and retried on the next
// update, so a missing or closing Discord client can never crash or stall ytm.
//
// Protocol summary (see Discord's RPC documentation): each message is a little
// endian uint32 opcode, a little endian uint32 payload length, then a JSON
// payload. Opcode 0 is HANDSHAKE, opcode 1 is FRAME (method calls/events).
package discordrpc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// IPC opcodes.
const (
	opHandshake = 0
	opFrame     = 1
)

const (
	dialTimeout  = 500 * time.Millisecond
	writeTimeout = 2 * time.Second
	maxFrameSize = 1 << 20
)

// Activity is a Rich Presence update. An all-zero Activity clears the presence.
type Activity struct {
	// Details is the first line (typically the track title).
	Details string
	// State is the second line (typically the artist).
	State string
	// StartTime, when set, makes Discord render an elapsed timer from it.
	StartTime time.Time

	// clear marks an internal "remove presence" update.
	clear bool
}

// Client is a best-effort Rich Presence connection. A nil *Client is safe to
// use: every method no-ops.
type Client struct {
	appID string

	updates chan Activity
	done    chan struct{}
	once    sync.Once

	mu   sync.Mutex
	conn net.Conn
}

// New returns an unstarted client for the given Discord application id. Call
// Start to launch the background worker.
func New(appID string) *Client {
	return &Client{
		appID:   appID,
		updates: make(chan Activity, 1),
		done:    make(chan struct{}),
	}
}

// Start launches the background worker. It is a no-op when the client is nil or
// has no application id.
func (c *Client) Start() {
	if c == nil || c.appID == "" {
		return
	}
	go c.run()
}

// SetActivity enqueues an update. It never blocks: when an update is already
// pending the newest one replaces it.
func (c *Client) SetActivity(a Activity) {
	if c == nil {
		return
	}
	c.enqueue(a)
}

// Clear removes the presence.
func (c *Client) Clear() {
	if c == nil {
		return
	}
	c.enqueue(Activity{clear: true})
}

// Close stops the worker and closes any open socket. It is idempotent.
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		close(c.done)
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.mu.Unlock()
	})
}

func (c *Client) enqueue(a Activity) {
	select {
	case <-c.done:
		return
	default:
	}
	// Replace any pending update so Discord always converges on the latest.
	select {
	case c.updates <- a:
		return
	default:
	}
	select {
	case <-c.updates:
	default:
	}
	select {
	case c.updates <- a:
	default:
	}
}

func (c *Client) run() {
	for {
		select {
		case <-c.done:
			c.closeConn()
			return
		case a := <-c.updates:
			c.publish(a)
		}
	}
}

func (c *Client) publish(a Activity) {
	conn := c.currentConn()
	if conn == nil {
		if a.clear {
			return // nothing to clear
		}
		var err error
		conn, err = c.connect()
		if err != nil {
			slog.Debug("discord rpc: not available", "err", err)
			return
		}
		c.setConn(conn)
	}
	if err := c.sendActivity(conn, a); err != nil {
		slog.Debug("discord rpc: update failed", "err", err)
		c.closeConn()
	}
}

// connect walks the candidate socket paths and handshakes with the first that
// accepts a connection.
func (c *Client) connect() (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, p := range socketPaths() {
		dctx, dcancel := context.WithTimeout(ctx, dialTimeout)
		conn, err := dialContext(dctx, "unix", p)
		dcancel()
		if err != nil {
			continue
		}
		if err := writeFrame(conn, opHandshake, handshakePayload(c.appID)); err != nil {
			_ = conn.Close()
			continue
		}
		return conn, nil
	}
	return nil, fmt.Errorf("discord rpc: no IPC socket found")
}

func (c *Client) sendActivity(conn net.Conn, a Activity) error {
	payload, err := activityPayload(os.Getpid(), a)
	if err != nil {
		return err
	}
	return writeFrame(conn, opFrame, payload)
}

func (c *Client) currentConn() net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

func (c *Client) setConn(conn net.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
}

func (c *Client) closeConn() {
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()
}

// --- encoding helpers (unit-tested) ---

func handshakePayload(appID string) []byte {
	b, _ := json.Marshal(map[string]any{"v": 1, "client_id": appID})
	return b
}

// activityPayload builds the SET_ACTIVITY frame body. A clear update sends a
// nil activity, which removes the presence.
func activityPayload(pid int, a Activity) ([]byte, error) {
	args := map[string]any{"pid": pid}
	if a.clear {
		args["activity"] = nil
	} else {
		act := map[string]any{}
		if v := clampField(a.Details); v != "" {
			act["details"] = v
		}
		if v := clampField(a.State); v != "" {
			act["state"] = v
		}
		if !a.StartTime.IsZero() {
			act["timestamps"] = map[string]any{"start": a.StartTime.UnixMilli()}
		}
		if len(act) == 0 {
			args["activity"] = nil
		} else {
			act["type"] = 2 // 2 = "Listening"
			args["activity"] = act
		}
	}
	return json.Marshal(map[string]any{
		"cmd":   "SET_ACTIVITY",
		"args":  args,
		"nonce": fmt.Sprintf("%d", time.Now().UnixNano()),
	})
}

// clampField trims s to Discord's 128-character field limit. Discord refuses
// fields shorter than 2 characters, but an invalid-but-present value is worse
// than a short one here, so only the upper bound is enforced.
func clampField(s string) string {
	const max = 128
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// writeFrame writes one length-prefixed protocol message.
func writeFrame(conn net.Conn, op uint32, payload []byte) error {
	buf := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], op)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(payload)))
	copy(buf[8:], payload)
	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	_, err := conn.Write(buf)
	return err
}

// readFrame reads one length-prefixed protocol message. It is used by tests to
// assert the wire format.
func readFrame(r io.Reader) (op uint32, payload []byte, err error) {
	header := make([]byte, 8)
	if _, err = io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	op = binary.LittleEndian.Uint32(header[0:4])
	n := binary.LittleEndian.Uint32(header[4:8])
	if n > maxFrameSize {
		return 0, nil, fmt.Errorf("discord rpc: frame too large: %d", n)
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return op, payload, nil
}

// dialContext is a package var so tests can substitute a fake transport.
var dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// socketPaths is a package var so tests can point at a fake socket.
var socketPaths = defaultSocketPaths

// defaultSocketPaths returns the candidate Discord IPC socket paths in
// preference order: runtime/tmp directories followed by the Flatpak bundle path.
func defaultSocketPaths() []string {
	var dirs []string
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		dirs = append(dirs, v, filepath.Join(v, "app", "com.discordapp.Discord"))
	}
	if v := os.Getenv("TMPDIR"); v != "" {
		dirs = append(dirs, v)
	}
	dirs = append(dirs, "/tmp")

	var out []string
	for _, d := range dirs {
		for i := 0; i < 10; i++ {
			out = append(out, filepath.Join(d, fmt.Sprintf("discord-ipc-%d", i)))
		}
	}
	return out
}
