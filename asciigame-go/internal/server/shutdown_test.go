package server

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGracefulShutdown verifies that cancelling the serve context during a game
// sends KICK to clients and leaves the WAL intact (no GAME_END) so the match
// can be recovered on restart.
func TestGracefulShutdown(t *testing.T) {
	chdirTemp(t)
	usersPath := filepath.FromSlash("data/game.db")
	srv, err := New(usersPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.Serve(ctx, ln); close(done) }()
	addr := ln.Addr().String()

	reg := func(name string) {
		c := dial(t, addr)
		c.send("REGISTER|" + name + "|pw")
		c.readLine()
		c.close()
	}
	reg("alice")
	reg("bob")

	a := dial(t, addr)
	defer a.close()
	b := dial(t, addr)
	defer b.close()
	a.send("LOGIN|alice|pw")
	a.waitFor("OK")
	b.send("LOGIN|bob|pw")
	b.waitFor("OK")
	a.send("CREATE_ROOM|Arena|2")
	info := a.waitFor("ROOM_INFO")
	roomID := strings.Split(info, "|")[1]
	b.send("JOIN_ROOM|" + roomID)
	b.waitFor("ROOM_INFO")
	a.send("READY")
	b.send("READY")
	a.waitFor("GAME_START")
	b.waitFor("GAME_START")
	a.waitFor("GAME_STATE") // game is running; WAL exists

	// Trigger graceful shutdown.
	cancel()

	// Alice should receive a KICK frame.
	if got := a.waitFor("KICK"); !strings.Contains(got, "shutting down") {
		t.Fatalf("expected KICK on shutdown, got %q", got)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}

	// The WAL for the (recovery) room must still exist without a GAME_END, so
	// the in-progress game is recoverable on restart.
	rid := atoi(roomID)
	if !walExistsForRoom(rid) {
		t.Fatalf("WAL for room %d should survive graceful shutdown", rid)
	}
	if walHasGameEnd(walPath(rid)) {
		t.Fatalf("WAL should not contain GAME_END after graceful shutdown")
	}
}
