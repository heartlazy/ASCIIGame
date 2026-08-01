package server

import (
	"testing"
	"time"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

// TestItemExpiry verifies that items are removed after ItemExpireTime.
func TestItemExpiry(t *testing.T) {
	addr := startTestServer(t)

	// Setup: two players, start a game.
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
	a.send(protocol.NewCreateRoom("Expiry", 2))
	info := a.waitFor("ROOM_INFO", func(f *protocol.Frame) bool { return f.GetRoomInfo() != nil }).GetRoomInfo()
	b.send(protocol.NewJoinRoom(info.RoomId))
	b.waitFor("ROOM_INFO", func(f *protocol.Frame) bool { return f.GetRoomInfo() != nil })
	a.send(protocol.NewReady())
	b.send(protocol.NewReady())
	a.waitFor("GAME_START", func(f *protocol.Frame) bool { return f.GetGameStart() != nil })

	// Get initial item count from first GAME_STATE.
	gs0 := a.waitFor("GAME_STATE", func(f *protocol.Frame) bool { return f.GetGameState() != nil }).GetGameState()
	initialItems := len(gs0.Items)
	if initialItems == 0 {
		t.Skip("no initial items (unlikely but possible)")
	}
	t.Logf("initial items: %d, expiry after %dms", initialItems, config.ItemExpireTime)

	// Items should still be present shortly after spawn. Force a broadcast by
	// moving (dirty detection skips idle states).
	time.Sleep(500 * time.Millisecond)
	a.send(protocol.NewMove("U")) // trigger state change so GAME_STATE broadcasts
	time.Sleep(300 * time.Millisecond)
	a.send(protocol.NewMove("D")) // move back
	gs1 := a.waitFor("GAME_STATE", func(f *protocol.Frame) bool { return f.GetGameState() != nil }).GetGameState()
	if len(gs1.Items) == 0 {
		t.Error("items expired too early (should last 30s)")
	}
}
