package discordrpc

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// TestClientHandshakeAndActivity drives the client against a fake Discord IPC
// socket and asserts the handshake then a SET_ACTIVITY frame on the wire.
func TestClientHandshakeAndActivity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discord-ipc-0")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	origPaths := socketPaths
	socketPaths = func() []string { return []string{path} }
	t.Cleanup(func() { socketPaths = origPaths })

	type frame struct {
		op      uint32
		payload []byte
	}
	frames := make(chan frame, 4)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			op, p, err := readFrame(conn)
			if err != nil {
				return
			}
			frames <- frame{op, p}
		}
	}()

	c := New("1234567890")
	c.Start()
	t.Cleanup(c.Close)
	c.SetActivity(Activity{Details: "Song", State: "Artist", StartTime: time.Unix(1000, 0)})

	next := func() frame {
		select {
		case f := <-frames:
			return f
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for an IPC frame")
			return frame{}
		}
	}

	hs := next()
	if hs.op != opHandshake {
		t.Fatalf("first frame op = %d, want handshake %d", hs.op, opHandshake)
	}
	var hello map[string]any
	if err := json.Unmarshal(hs.payload, &hello); err != nil {
		t.Fatalf("handshake payload: %v", err)
	}
	if hello["client_id"] != "1234567890" {
		t.Errorf("handshake client_id = %v, want 1234567890", hello["client_id"])
	}
	if v, _ := hello["v"].(float64); v != 1 {
		t.Errorf("handshake v = %v, want 1", hello["v"])
	}

	act := next()
	if act.op != opFrame {
		t.Fatalf("second frame op = %d, want frame %d", act.op, opFrame)
	}
	var msg struct {
		Cmd  string `json:"cmd"`
		Args struct {
			PID      int            `json:"pid"`
			Activity map[string]any `json:"activity"`
		} `json:"args"`
	}
	if err := json.Unmarshal(act.payload, &msg); err != nil {
		t.Fatalf("activity payload: %v", err)
	}
	if msg.Cmd != "SET_ACTIVITY" {
		t.Errorf("cmd = %q, want SET_ACTIVITY", msg.Cmd)
	}
	if msg.Args.PID == 0 {
		t.Error("pid should be set")
	}
	if msg.Args.Activity["details"] != "Song" || msg.Args.Activity["state"] != "Artist" {
		t.Errorf("activity = %v, want details Song / state Artist", msg.Args.Activity)
	}
	ts, ok := msg.Args.Activity["timestamps"].(map[string]any)
	if !ok || ts["start"] != float64(1000*1000) {
		t.Errorf("timestamps = %v, want start 1000000", msg.Args.Activity["timestamps"])
	}
}

func TestActivityPayloadClear(t *testing.T) {
	p, err := activityPayload(42, Activity{clear: true})
	if err != nil {
		t.Fatalf("activityPayload: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(p, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	args := msg["args"].(map[string]any)
	if v, present := args["activity"]; !present || v != nil {
		t.Errorf("clear activity = %v (present=%v), want nil", v, present)
	}
}

func TestClampField(t *testing.T) {
	short := "ok"
	if got := clampField(short); got != short {
		t.Errorf("clampField(%q) = %q, want unchanged", short, got)
	}
	long := make([]rune, 200)
	for i := range long {
		long[i] = 'x'
	}
	if got := clampField(string(long)); len([]rune(got)) != 128 {
		t.Errorf("clampField truncated to %d runes, want 128", len([]rune(got)))
	}
}

func TestNilClientSafe(t *testing.T) {
	var c *Client
	c.Start()
	c.SetActivity(Activity{Details: "x"})
	c.Clear()
	c.Close() // must not panic
}

func TestSocketPathsNonEmpty(t *testing.T) {
	if len(defaultSocketPaths()) == 0 {
		t.Error("defaultSocketPaths() returned nothing")
	}
}
