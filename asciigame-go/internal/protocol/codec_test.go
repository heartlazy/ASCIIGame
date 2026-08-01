package protocol

import (
	"bytes"
	"testing"
)

// TestFrameRoundtrip verifies a frame survives marshal -> WriteFrame -> ReadFrame
// -> unmarshal with all fields intact, for a representative payload.
func TestFrameRoundtrip(t *testing.T) {
	original := NewGameState(1234567890,
		[]*PlayerState{
			{Id: 1, X: 10, Y: 20, Hp: 100, Atk: 15, Def: 5, Status: 5, HasShield: true, Inventory: []int32{1, 2, 0, 0, 0}},
			{Id: 2, X: 30, Y: 15, Hp: 80, Atk: 25, Def: 5, Status: 5, Inventory: []int32{3, 0, 0, 0, 0}},
		},
		[]*ItemState{{X: 5, Y: 5, Type: 1}, {X: 6, Y: 6, Type: 2}},
		25,
	)

	var buf bytes.Buffer
	if err := WriteFrame(&buf, original); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	gs := got.GetGameState()
	if gs == nil {
		t.Fatal("payload is not GameState")
	}
	if gs.Timestamp != 1234567890 || gs.PoisonRadius != 25 {
		t.Errorf("header fields = %d/%d", gs.Timestamp, gs.PoisonRadius)
	}
	if len(gs.Players) != 2 {
		t.Fatalf("players = %d, want 2", len(gs.Players))
	}
	if gs.Players[0].Id != 1 || gs.Players[0].Hp != 100 || !gs.Players[0].HasShield {
		t.Errorf("player0 = %+v", gs.Players[0])
	}
	if len(gs.Players[0].Inventory) != 5 || gs.Players[0].Inventory[0] != 1 || gs.Players[0].Inventory[1] != 2 {
		t.Errorf("player0 inventory = %v", gs.Players[0].Inventory)
	}
	if len(gs.Items) != 2 || gs.Items[1].Type != 2 {
		t.Errorf("items = %+v", gs.Items)
	}
}

// TestEventOneofRoundtrip checks the nested GameEvent oneof round-trips.
func TestEventOneofRoundtrip(t *testing.T) {
	cases := []*Frame{
		NewAttackEvent(7, 12, 4),
		NewDamageEvent(7, 3, 10, 90),
		NewKillEvent(7, 3),
		NewShieldEvent(7, 3),
		NewAttackResultEvent(7, 1),
		NewPickupEvent(7, 2),
		NewPoisonEvent(),
		NewBuffWarningEvent(7, 5),
		NewBuffExpiredEvent(7),
	}
	for _, original := range cases {
		var buf bytes.Buffer
		if err := WriteFrame(&buf, original); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		if got.GetGameEvent() == nil {
			t.Fatalf("payload is not GameEvent for %T", original.Payload)
		}
	}
}
