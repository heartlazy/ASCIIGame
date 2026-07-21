package client_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/heartlazyli/asciigame/internal/client"
	"github.com/heartlazyli/asciigame/internal/protocol"
	"github.com/heartlazyli/asciigame/internal/server"
)

// startServer boots a server on an ephemeral port for the client tests.
func startServer(t *testing.T) string {
	t.Helper()
	srv, err := server.New(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Serve(ctx, ln)
	return ln.Addr().String()
}

// clientPeer bundles a connection with the client-side State it feeds.
type clientPeer struct {
	conn  *client.Conn
	state *client.State
}

func newPeer(t *testing.T, addr string) *clientPeer {
	t.Helper()
	c, err := client.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	st := client.NewState()
	go c.ReadLoop(func(raw string) { st.Update(raw) }, func() {})
	return &clientPeer{conn: c, state: st}
}

// TestClientFullMatch drives two client-side States through a full match via
// the real server and asserts the client parsing reflects the game.
func TestClientFullMatch(t *testing.T) {
	addr := startServer(t)

	a := newPeer(t, addr)
	b := newPeer(t, addr)

	// Register + login both (register is idempotent enough here: fresh store).
	_ = a.conn.Send(protocol.BuildRegister("alice", "pw"))
	_ = a.conn.Send(protocol.BuildLogin("alice", "pw"))
	_ = b.conn.Send(protocol.BuildRegister("bob", "pw"))
	_ = b.conn.Send(protocol.BuildLogin("bob", "pw"))

	// alice creates a room; wait for the client to learn the room id.
	_ = a.conn.Send(protocol.BuildCreateRoom("Arena", 2))
	if !waitUntil(func() bool { return a.state.RoomID() > 0 }) {
		t.Fatalf("alice never entered a room")
	}
	roomID := a.state.RoomID()

	_ = b.conn.Send(protocol.BuildJoinRoom(roomID))
	if !waitUntil(func() bool { return b.state.RoomID() == roomID }) {
		t.Fatalf("bob never joined room %d", roomID)
	}

	_ = a.conn.Send(protocol.BuildSimple("READY"))
	_ = b.conn.Send(protocol.BuildSimple("READY"))

	// Both clients should observe the game start and receive player states.
	if !waitUntil(func() bool { return a.state.InGame() && a.state.PlayerCount() >= 2 }) {
		t.Fatalf("alice never saw the game start with players; inGame=%v players=%d",
			a.state.InGame(), a.state.PlayerCount())
	}
	if a.state.MyID() == 0 {
		t.Fatalf("alice's my_id was never set")
	}

	// A move from alice should be reflected in her mirrored position over time
	// (server broadcasts GAME_STATE ~20/s). Just assert no panic and that state
	// stays consistent: player count should remain >= 2 shortly after.
	_ = a.conn.Send(protocol.BuildMove('U'))
	time.Sleep(150 * time.Millisecond)
	if a.state.PlayerCount() < 2 {
		t.Fatalf("player count dropped unexpectedly: %d", a.state.PlayerCount())
	}
}

func waitUntil(cond func() bool) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}
