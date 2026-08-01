package client

import (
	"testing"

	"github.com/heartlazyli/asciigame/internal/protocol"
)

// TestLeaveMidGameResetsView verifies that receiving "OK|Left room" while in a
// game clears inGame/inRoom so the client returns to the lobby view.
func TestLeaveMidGameResetsView(t *testing.T) {
	s := NewState()
	// Simulate being mid-game.
	s.inGame = true
	s.inRoom = true
	s.roomID = 5
	s.players = []playerView{{id: 1, x: 3, y: 3, hp: 100}}

	s.Update(protocol.NewOk("Left room", 0))

	if s.InGame() {
		t.Error("inGame should be false after leaving")
	}
	if s.RoomID() != -1 {
		t.Errorf("roomID=%d, want -1", s.RoomID())
	}
	s.mu.Lock()
	inRoom := s.inRoom
	nplayers := len(s.players)
	s.mu.Unlock()
	if inRoom {
		t.Error("inRoom should be false after leaving")
	}
	if nplayers != 0 {
		t.Errorf("players should be cleared, got %d", nplayers)
	}
}
