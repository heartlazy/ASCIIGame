package server

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heartlazyli/asciigame/internal/protocol"
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
		c.send(protocol.NewRegister(name, "pw"))
		c.recv()
		c.close()
	}
	reg("alice")
	reg("bob")

	a := dial(t, addr)
	defer a.close()
	b := dial(t, addr)
	defer b.close()
	a.send(protocol.NewLogin("alice", "pw"))
	a.waitFor("OK", func(f *protocol.Frame) bool { return f.GetOk() != nil })
	b.send(protocol.NewLogin("bob", "pw"))
	b.waitFor("OK", func(f *protocol.Frame) bool { return f.GetOk() != nil })
	a.send(protocol.NewCreateRoom("Arena", 2))
	info := a.waitFor("ROOM_INFO", func(f *protocol.Frame) bool { return f.GetRoomInfo() != nil }).GetRoomInfo()
	roomID := info.RoomId
	b.send(protocol.NewJoinRoom(roomID))
	b.waitFor("ROOM_INFO", func(f *protocol.Frame) bool { return f.GetRoomInfo() != nil })
	a.send(protocol.NewReady())
	b.send(protocol.NewReady())
	a.waitFor("GAME_START", func(f *protocol.Frame) bool { return f.GetGameStart() != nil })
	b.waitFor("GAME_START", func(f *protocol.Frame) bool { return f.GetGameStart() != nil })
	a.waitFor("GAME_STATE", func(f *protocol.Frame) bool { return f.GetGameState() != nil })

	// Trigger graceful shutdown.
	cancel()

	// Alice should receive a KICK frame.
	kick := a.waitFor("KICK", func(f *protocol.Frame) bool { return f.GetKick() != nil }).GetKick()
	if !strings.Contains(kick.Reason, "shutting down") {
		t.Fatalf("expected KICK on shutdown, got %q", kick.Reason)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}

	// The WAL for the (recovery) room must still exist without a GAME_END, so
	// the in-progress game is recoverable on restart.
	rid := int(roomID)
	if !walExistsForRoom(rid) {
		t.Fatalf("WAL for room %d should survive graceful shutdown", rid)
	}
	if walHasGameEnd(walPath(rid)) {
		t.Fatalf("WAL should not contain GAME_END after graceful shutdown")
	}
}
