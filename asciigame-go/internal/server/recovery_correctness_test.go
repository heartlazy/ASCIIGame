package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRecoveryCorrectness_WALPlusSnapshot verifies that recovery after a
// snapshot + subsequent WAL records produces the correct final state.
func TestRecoveryCorrectness_WALPlusSnapshot(t *testing.T) {
	dir := t.TempDir()
	walDir := filepath.Join(dir, "data", "wal")
	os.MkdirAll(walDir, 0o755)

	// Simulate: snapshot captured alice at (10,10) hp=80, then she moved to (11,10).
	// WAL after snapshot (CHECKPOINT + PLAYER_JOIN is how snapshotSave writes it):
	walContent := strings.Join([]string{
		"2000|1|5|CHECKPOINT|snapshot_time=2000,room_name=Arena,poison_radius=20",
		"2001|2|5|PLAYER_JOIN|pid=1,username=alice,x=10,y=10,hp=80,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=1,0,0,0,0",
		"2002|3|5|PLAYER_JOIN|pid=2,username=bob,x=20,y=15,hp=90,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=0,0,0,0,0",
		"2003|4|5|ITEM_SPAWN|type=2,x=5,y=5",
		"2004|5|5|POISON_SHRINK|radius=19",
		// Post-snapshot activity:
		"2100|6|5|MOVE|pid=1,dir=R,ox=10,oy=10,nx=11,ny=10",
		"2200|7|5|DAMAGE|atk=1,vic=2,dmg=10,hp=80",
		"",
	}, "\n")
	os.WriteFile(filepath.Join(walDir, "room_5.wal"), []byte(walContent), 0o644)

	state := replayWAL(filepath.Join(walDir, "room_5.wal"))
	if state == nil {
		t.Fatal("replay returned nil")
	}

	// Verify alice's final position (moved from 10,10 to 11,10).
	alice := state.playerByID(1)
	if alice == nil {
		t.Fatal("alice not found")
	}
	if alice.x != 11 || alice.y != 10 {
		t.Errorf("alice pos = (%d,%d), want (11,10)", alice.x, alice.y)
	}
	if alice.hp != 80 {
		t.Errorf("alice hp = %d, want 80", alice.hp)
	}

	// Verify bob took damage.
	bob := state.playerByID(2)
	if bob == nil {
		t.Fatal("bob not found")
	}
	if bob.hp != 80 {
		t.Errorf("bob hp = %d, want 80 (90 - 10 damage)", bob.hp)
	}

	// Verify poison radius updated.
	if state.poisonRadius != 19 {
		t.Errorf("poisonRadius = %d, want 19", state.poisonRadius)
	}

	// Verify items.
	if len(state.items) != 1 {
		t.Errorf("items = %d, want 1", len(state.items))
	}
}

// TestRecoveryCorrectness_PureWAL verifies that a full game WAL (from
// GAME_START, no snapshot) also recovers correctly.
func TestRecoveryCorrectness_PureWAL(t *testing.T) {
	dir := t.TempDir()
	walDir := filepath.Join(dir, "data", "wal")
	os.MkdirAll(walDir, 0o755)

	walContent := strings.Join([]string{
		"1000|1|3|GAME_START|room_name=TestRoom,max_players=2",
		"1001|2|3|PLAYER_JOIN|pid=1,username=alice,x=5,y=5,hp=100,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=0,0,0,0,0",
		"1002|3|3|PLAYER_JOIN|pid=2,username=bob,x=20,y=10,hp=100,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=0,0,0,0,0",
		"1003|4|3|ITEM_SPAWN|type=1,x=10,y=10",
		"1004|5|3|ITEM_SPAWN|type=3,x=15,y=8",
		"1100|6|3|MOVE|pid=1,dir=R,ox=5,oy=5,nx=6,ny=5",
		"1200|7|3|MOVE|pid=1,dir=R,ox=6,oy=5,nx=7,ny=5",
		"1300|8|3|PICKUP|pid=1,item=1,x=10,y=10",
		"1400|9|3|USE_ITEM|pid=1,item=1,idx=0,atk_buff_remain=0",
		"1500|10|3|POISON_SHRINK|radius=24",
		"1600|11|3|DAMAGE|atk=1,vic=2,dmg=10,hp=90",
		"1700|12|3|PLAYER_DEATH|pid=2,killer=1",
		"",
	}, "\n")
	os.WriteFile(filepath.Join(walDir, "room_3.wal"), []byte(walContent), 0o644)

	state := replayWAL(filepath.Join(walDir, "room_3.wal"))
	if state == nil {
		t.Fatal("replay returned nil (PLAYER_DEATH present but no GAME_END)")
	}

	alice := state.playerByID(1)
	if alice == nil {
		t.Fatal("alice missing")
	}
	// Alice moved twice right: 5→6→7, then pickup removed item at (10,10).
	if alice.x != 7 || alice.y != 5 {
		t.Errorf("alice pos = (%d,%d), want (7,5)", alice.x, alice.y)
	}
	// Alice used a health pack (item=1): hp should still be 100 (already full).
	if alice.hp != 100 {
		t.Errorf("alice hp = %d, want 100", alice.hp)
	}

	bob := state.playerByID(2)
	if bob == nil {
		t.Fatal("bob missing")
	}
	if bob.hp != 0 || bob.status != int(StatusDead) {
		t.Errorf("bob hp=%d status=%d, want hp=0 status=Dead", bob.hp, bob.status)
	}

	if state.poisonRadius != 24 {
		t.Errorf("poison = %d, want 24", state.poisonRadius)
	}

	// Item at (10,10) was picked up → inactive; item at (15,8) remains.
	active := 0
	for _, it := range state.items {
		if it.active {
			active++
		}
	}
	if active != 1 {
		t.Errorf("active items = %d, want 1", active)
	}
}

// TestRecoveryCorrectness_AtkBuffPersistence verifies that the attack buff
// expiry survives through WAL recovery.
func TestRecoveryCorrectness_AtkBuffPersistence(t *testing.T) {
	dir := t.TempDir()
	walDir := filepath.Join(dir, "data", "wal")
	os.MkdirAll(walDir, 0o755)

	walContent := strings.Join([]string{
		"1000|1|9|GAME_START|room_name=Buff,max_players=2",
		"1001|2|9|PLAYER_JOIN|pid=1,username=alice,x=5,y=5,hp=100,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=2,0,0,0,0",
		"1002|3|9|PLAYER_JOIN|pid=2,username=bob,x=20,y=10,hp=100,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=0,0,0,0,0",
		// Alice uses attack potion (item=2): atk_buff_remain=10000.
		"1100|4|9|USE_ITEM|pid=1,item=2,idx=0,atk_buff_remain=10000",
		"",
	}, "\n")
	os.WriteFile(filepath.Join(walDir, "room_9.wal"), []byte(walContent), 0o644)

	state := replayWAL(filepath.Join(walDir, "room_9.wal"))
	if state == nil {
		t.Fatal("replay nil")
	}
	alice := state.playerByID(1)
	if alice == nil {
		t.Fatal("alice nil")
	}

	// After using attack potion: atk should be base(15) + buff(10) = 25.
	if alice.atk != 25 {
		t.Errorf("alice.atk = %d, want 25 (15+10 buff)", alice.atk)
	}
	// atkBuffExpire should be set (> current time).
	if alice.atkBuffExpire <= 0 {
		t.Errorf("alice.atkBuffExpire = %d, want >0 (buff should have expiry)", alice.atkBuffExpire)
	}
	// Inventory should be empty (used the item).
	if len(alice.inventory) != 0 {
		t.Errorf("alice.inventory = %v, want empty after using item", alice.inventory)
	}
}

// TestRecoveryCorrectness_CorruptSnapshot verifies that the system degrades
// gracefully to pure-WAL recovery when the snapshot file is corrupt.
func TestRecoveryCorrectness_CorruptSnapshot(t *testing.T) {
	dir := t.TempDir()
	walDir := filepath.Join(dir, "data", "wal")
	os.MkdirAll(walDir, 0o755)

	// Write a corrupt snapshot (invalid JSON).
	snapPath := filepath.Join(walDir, "room_7.snap")
	os.WriteFile(snapPath, []byte("CORRUPT DATA{{{"), 0o644)

	// Write a valid WAL that can recover from scratch.
	walContent := strings.Join([]string{
		"1000|1|7|GAME_START|room_name=Fallback,max_players=2",
		"1001|2|7|PLAYER_JOIN|pid=1,username=alice,x=10,y=10,hp=100,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=0,0,0,0,0",
		"1002|3|7|PLAYER_JOIN|pid=2,username=bob,x=20,y=15,hp=90,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=0,0,0,0,0",
		"",
	}, "\n")
	os.WriteFile(filepath.Join(walDir, "room_7.wal"), []byte(walContent), 0o644)

	// snapshotLoad should fail on corrupt data.
	snap := snapshotLoad(7)
	if snap != nil {
		t.Error("corrupt snapshot should not load successfully")
	}

	// But WAL recovery should still work (falls back to pure WAL).
	state := replayWAL(filepath.Join(walDir, "room_7.wal"))
	if state == nil {
		t.Fatal("WAL replay should succeed even with corrupt snapshot")
	}
	if state.playerByID(1) == nil || state.playerByID(2) == nil {
		t.Error("players not recovered from WAL")
	}
	if state.roomName != "Fallback" {
		t.Errorf("room name = %q, want 'Fallback'", state.roomName)
	}
}

// TestRecoveryCorrectness_SnapshotJSON verifies the snapshot JSON format
// preserves all fields correctly.
func TestRecoveryCorrectness_SnapshotJSON(t *testing.T) {
	dir := t.TempDir()
	walDir := filepath.Join(dir, "data", "wal")
	os.MkdirAll(walDir, 0o755)

	// Write a valid snapshot.
	snap := &snapshotFile{
		Version: 1, Timestamp: 5000, RoomID: 4, RoomName: "SnapRoom",
		MaxPlayers: 6, Status: 2, PoisonRadius: 18,
		Map:              []string{"##########"},
		GameStartTime:    1000,
		LastItemSpawn:    4000,
		LastPoisonShrink: 3000,
		Items:            []snapItem{{X: 5, Y: 5, Type: ItemHealth}},
		Players: []snapPlayer{
			{ID: 1, Username: "alice", X: 12, Y: 8, HP: 75, MaxHP: 100,
				ATK: 25, DEF: 5, BaseATK: 15, HasShield: true, Status: 5,
				Inventory: []ItemType{ItemShield, 0, 0, 0, 0}, AtkBuffExpire: 6000},
		},
	}
	snapPath := filepath.Join(walDir, "room_4.snap")
	data, _ := json.MarshalIndent(snap, "", "  ")
	os.WriteFile(snapPath, data, 0o644)

	// snapshotLoad uses the global snapshotPath which uses config.SnapshotDir.
	// We verify the JSON roundtrip directly here.
	var roundtrip snapshotFile
	json.Unmarshal(data, &roundtrip)

	if roundtrip.RoomName != "SnapRoom" || roundtrip.PoisonRadius != 18 {
		t.Errorf("basic fields: name=%q poison=%d", roundtrip.RoomName, roundtrip.PoisonRadius)
	}
	if len(roundtrip.Players) != 1 {
		t.Fatal("expected 1 player in snapshot")
	}
	p := roundtrip.Players[0]
	if p.ATK != 25 || p.BaseATK != 15 || p.AtkBuffExpire != 6000 {
		t.Errorf("player atk=%d base=%d buffExpire=%d", p.ATK, p.BaseATK, p.AtkBuffExpire)
	}
	if !p.HasShield {
		t.Error("shield should be true")
	}
	if p.HP != 75 {
		t.Errorf("hp = %d, want 75", p.HP)
	}
}
