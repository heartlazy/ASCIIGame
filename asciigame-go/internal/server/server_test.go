package server

import (
	"bufio"
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

// testClient is a minimal protobuf-framed TCP client for integration tests.
type testClient struct {
	conn net.Conn
	r    *bufio.Reader
	t    *testing.T
}

func dial(t *testing.T, addr string) *testClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return &testClient{conn: conn, r: bufio.NewReader(conn), t: t}
}

func (c *testClient) send(f *protocol.Frame) {
	c.t.Helper()
	if err := protocol.WriteFrame(c.conn, f); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// recv reads one frame with a 2s deadline.
func (c *testClient) recv() *protocol.Frame {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	f, err := protocol.ReadFrame(c.r)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	return f
}

// waitFor reads frames until match returns true or the timeout elapses.
func (c *testClient) waitFor(name string, match func(*protocol.Frame) bool) *protocol.Frame {
	c.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.conn.SetReadDeadline(deadline)
		f, err := protocol.ReadFrame(c.r)
		if err != nil {
			c.t.Fatalf("waitFor %s: %v", name, err)
		}
		if match(f) {
			return f
		}
	}
	c.t.Fatalf("timeout waiting for %s", name)
	return nil
}

func (c *testClient) close() { _ = c.conn.Close() }

// isOKWith reports whether f is an Ok frame whose message contains substr.
func isOKWith(f *protocol.Frame, substr string) bool {
	o := f.GetOk()
	return o != nil && strings.Contains(o.Message, substr)
}

// startTestServer boots a Server on an ephemeral port and returns its address.
func startTestServer(t *testing.T) string {
	t.Helper()
	srv, err := New(filepath.Join(t.TempDir(), "game.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Serve(ctx, ln)
	return ln.Addr().String()
}

// TestFullMatch drives two players through register/login/room/ready and
// verifies GAME_START, a GAME_STATE with player entries, and a successful move.
func TestFullMatch(t *testing.T) {
	addr := startTestServer(t)

	reg := func(name string) {
		c := dial(t, addr)
		c.send(protocol.NewRegister(name, "pw"))
		if o := c.recv().GetOk(); o == nil {
			t.Fatalf("register %s: not OK", name)
		}
		c.close()
	}
	reg("alice")
	reg("bob")

	a := dial(t, addr)
	defer a.close()
	b := dial(t, addr)
	defer b.close()

	a.send(protocol.NewLogin("alice", "pw"))
	if f := a.waitFor("login OK", func(f *protocol.Frame) bool { return f.GetOk() != nil }); !isOKWith(f, "Login successful") {
		t.Fatalf("alice login: %v", f.GetOk())
	}
	b.send(protocol.NewLogin("bob", "pw"))
	b.waitFor("login OK", func(f *protocol.Frame) bool { return f.GetOk() != nil })

	// alice creates a room; parse the room id from ROOM_INFO.
	a.send(protocol.NewCreateRoom("Arena", 2))
	info := a.waitFor("ROOM_INFO", func(f *protocol.Frame) bool { return f.GetRoomInfo() != nil }).GetRoomInfo()
	roomID := info.RoomId

	b.send(protocol.NewJoinRoom(roomID))
	b.waitFor("ROOM_INFO", func(f *protocol.Frame) bool { return f.GetRoomInfo() != nil })

	// Both ready -> game starts.
	a.send(protocol.NewReady())
	b.send(protocol.NewReady())
	a.waitFor("GAME_START", func(f *protocol.Frame) bool { return f.GetGameStart() != nil })
	b.waitFor("GAME_START", func(f *protocol.Frame) bool { return f.GetGameStart() != nil })

	// A GAME_STATE frame must carry at least one player entry.
	gs := a.waitFor("GAME_STATE", func(f *protocol.Frame) bool { return f.GetGameState() != nil }).GetGameState()
	if len(gs.Players) < 1 {
		t.Fatalf("GAME_STATE has no players: %+v", gs)
	}
	if gs.Players[0].Id == 0 {
		t.Fatalf("player entry missing id: %+v", gs.Players[0])
	}

	// At least one of the four directions must be walkable from any valid
	// spawn (the map has no fully enclosed cells). Respect the 200ms cooldown.
	accepted := false
	for _, dir := range []string{"U", "D", "L", "R"} {
		a.send(protocol.NewMove(dir))
		rejected := false
		_ = a.conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		for {
			f, err := protocol.ReadFrame(a.r)
			if err != nil {
				break
			}
			if e := f.GetError(); e != nil && e.Code == int32(config.ErrInvalidMove) {
				rejected = true
				break
			}
		}
		if !rejected {
			accepted = true
			break
		}
		time.Sleep(210 * time.Millisecond) // move cooldown
	}
	if !accepted {
		t.Fatalf("all four moves rejected as invalid")
	}
}
