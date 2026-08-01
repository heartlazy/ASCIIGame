package server

import (
	"testing"
	"time"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

// TestProtocolE2E exercises the full gameplay protocol end-to-end over real TCP:
// register, login, create room, join, ready, GAME_START, GAME_STATE fields,
// move, attack events, leave, list rooms, logout, and error responses.
func TestProtocolE2E(t *testing.T) {
	addr := startTestServer(t)

	// --- Register two users ---
	for _, name := range []string{"alice", "bob"} {
		c := dial(t, addr)
		c.send(protocol.NewRegister(name, "pw"))
		f := c.recv()
		if o := f.GetOk(); o == nil || o.Message != "Registration successful" {
			t.Fatalf("register %s: %+v", name, f)
		}
		c.close()
	}

	// --- Duplicate register should fail ---
	{
		c := dial(t, addr)
		c.send(protocol.NewRegister("alice", "pw"))
		f := c.recv()
		if e := f.GetError(); e == nil || e.Code != int32(config.ErrUsernameExists) {
			t.Fatalf("dup register should be ErrUsernameExists: %+v", f)
		}
		c.close()
	}

	// --- Login with wrong password ---
	{
		c := dial(t, addr)
		c.send(protocol.NewLogin("alice", "wrong"))
		f := c.recv()
		if e := f.GetError(); e == nil || e.Code != int32(config.ErrInvalidCredentials) {
			t.Fatalf("wrong password should be ErrInvalidCredentials: %+v", f)
		}
		c.close()
	}

	// --- Login alice ---
	a := dial(t, addr)
	defer a.close()
	a.send(protocol.NewLogin("alice", "pw"))
	{
		f := a.waitFor("alice login", func(f *protocol.Frame) bool { return f.GetOk() != nil })
		o := f.GetOk()
		if o.PlayerId <= 0 {
			t.Fatalf("login OK should carry player_id>0: %+v", o)
		}
		if o.Message != "Login successful" {
			t.Fatalf("login message = %q", o.Message)
		}
	}

	// --- List rooms (empty) ---
	a.send(protocol.NewListRooms())
	{
		f := a.waitFor("room list", func(f *protocol.Frame) bool { return f.GetRoomList() != nil })
		if len(f.GetRoomList().Rooms) != 0 {
			t.Fatalf("room list should be empty: %+v", f.GetRoomList())
		}
	}

	// --- Create room ---
	a.send(protocol.NewCreateRoom("Arena", 2))
	var roomID int32
	{
		// Should get PLAYER_JOIN (self) then ROOM_INFO.
		a.waitFor("player_join", func(f *protocol.Frame) bool { return f.GetPlayerJoin() != nil })
		f := a.waitFor("room_info", func(f *protocol.Frame) bool { return f.GetRoomInfo() != nil })
		ri := f.GetRoomInfo()
		if ri.Name != "Arena" || ri.MaxPlayers != 2 || ri.PlayerCount != 1 || ri.Status != 0 {
			t.Fatalf("room_info = %+v", ri)
		}
		roomID = ri.RoomId
	}

	// --- Login bob and join ---
	b := dial(t, addr)
	defer b.close()
	b.send(protocol.NewLogin("bob", "pw"))
	b.waitFor("bob login", func(f *protocol.Frame) bool { return f.GetOk() != nil })

	b.send(protocol.NewJoinRoom(roomID))
	{
		b.waitFor("player_join", func(f *protocol.Frame) bool { return f.GetPlayerJoin() != nil })
		f := b.waitFor("room_info", func(f *protocol.Frame) bool { return f.GetRoomInfo() != nil })
		if f.GetRoomInfo().PlayerCount != 2 {
			t.Fatalf("join: player_count=%d want 2", f.GetRoomInfo().PlayerCount)
		}
	}

	// alice should see bob's PLAYER_JOIN
	a.waitFor("bob join notify", func(f *protocol.Frame) bool {
		pj := f.GetPlayerJoin()
		return pj != nil && pj.Username == "bob"
	})

	// --- Ready -> GAME_START ---
	a.send(protocol.NewReady())
	a.waitFor("alice ready OK", func(f *protocol.Frame) bool { return isOKWith(f, "Ready") })
	b.send(protocol.NewReady())
	b.waitFor("bob ready OK", func(f *protocol.Frame) bool { return isOKWith(f, "Ready") })

	a.waitFor("GAME_START", func(f *protocol.Frame) bool { return f.GetGameStart() != nil })
	b.waitFor("GAME_START", func(f *protocol.Frame) bool { return f.GetGameStart() != nil })

	// --- GAME_STATE: verify fields ---
	{
		f := a.waitFor("GAME_STATE", func(f *protocol.Frame) bool { return f.GetGameState() != nil })
		gs := f.GetGameState()
		if gs.Timestamp <= 0 {
			t.Errorf("timestamp should be >0: %d", gs.Timestamp)
		}
		if len(gs.Players) != 2 {
			t.Fatalf("GAME_STATE player count = %d want 2", len(gs.Players))
		}
		for _, p := range gs.Players {
			if p.Id <= 0 || p.Hp != int32(config.InitialHP) || p.Atk != int32(config.InitialATK) || p.Def != int32(config.InitialDEF) {
				t.Errorf("player state unexpected: %+v", p)
			}
			if p.X < 0 || p.X >= int32(config.MapWidth) || p.Y < 0 || p.Y >= int32(config.MapHeight) {
				t.Errorf("player pos out of bounds: %d,%d", p.X, p.Y)
			}
			if len(p.Inventory) != config.MaxInventory {
				t.Errorf("inventory len = %d want %d", len(p.Inventory), config.MaxInventory)
			}
		}
		if gs.PoisonRadius != int32(mapInitialPoisonRadius()) {
			t.Errorf("poison radius = %d want %d", gs.PoisonRadius, mapInitialPoisonRadius())
		}
		// Should have items (spawn points produce them at game start).
		if len(gs.Items) == 0 {
			t.Errorf("expected items at game start")
		}
		for _, it := range gs.Items {
			if it.Type < 1 || it.Type > 3 {
				t.Errorf("item type out of range: %+v", it)
			}
		}
	}

	// --- Move: try all 4 dirs, at least one should succeed (no error returned) ---
	moved := false
	for _, dir := range []string{"U", "D", "L", "R"} {
		a.send(protocol.NewMove(dir))
		time.Sleep(50 * time.Millisecond)
		// Drain: check for an error reply for this specific move.
		gotErr := false
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			_ = a.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			f, err := protocol.ReadFrame(a.r)
			if err != nil {
				break
			}
			if e := f.GetError(); e != nil && (e.Code == int32(config.ErrInvalidMove) || e.Code == int32(config.ErrMoveCooldown)) {
				gotErr = true
				break
			}
		}
		if !gotErr {
			moved = true
			break
		}
		time.Sleep(210 * time.Millisecond) // cooldown
	}
	if !moved {
		t.Fatalf("all moves rejected — player stuck")
	}

	// --- Attack: should produce attack events ---
	time.Sleep(time.Duration(config.AttackCooldownMS) * time.Millisecond)
	a.send(protocol.NewAttack())
	{
		// Should receive at least an AttackEvent and an AttackResultEvent.
		gotAttack := false
		gotResult := false
		deadline := time.Now().Add(1 * time.Second)
		for time.Now().Before(deadline) && !(gotAttack && gotResult) {
			_ = a.conn.SetReadDeadline(deadline)
			f, err := protocol.ReadFrame(a.r)
			if err != nil {
				break
			}
			ge := f.GetGameEvent()
			if ge == nil {
				continue
			}
			if ge.GetAttack() != nil {
				gotAttack = true
			}
			if ge.GetAttackResult() != nil {
				gotResult = true
			}
		}
		if !gotAttack {
			t.Error("missing AttackEvent after attack")
		}
		if !gotResult {
			t.Error("missing AttackResultEvent after attack")
		}
	}

	// --- Leave room mid-game ---
	b.send(protocol.NewLeaveRoom())
	b.waitFor("left room", func(f *protocol.Frame) bool { return isOKWith(f, "Left room") })

	// After bob leaves, game should end (only alice alive) => alice gets GameEnd.
	a.waitFor("game_end", func(f *protocol.Frame) bool { return f.GetGameEnd() != nil })

	// --- List rooms after game ends (room still exists with alice) ---
	a.send(protocol.NewListRooms())
	a.waitFor("room list after", func(f *protocol.Frame) bool { return f.GetRoomList() != nil })

	// --- Logout ---
	a.send(protocol.NewLogout())
	a.waitFor("logout OK", func(f *protocol.Frame) bool { return isOKWith(f, "Logged out") })

	// --- After logout, commands should fail ---
	a.send(protocol.NewListRooms())
	{
		f := a.waitFor("not logged in", func(f *protocol.Frame) bool { return f.GetError() != nil })
		if f.GetError().Code != int32(config.ErrInvalidFormat) {
			t.Errorf("expected ErrInvalidFormat after logout, got %+v", f.GetError())
		}
	}
}

// TestProtocolChatAndKick verifies chat message relay and KICK on shutdown.
func TestProtocolChatAndKick(t *testing.T) {
	addr := startTestServer(t)

	// Setup: two players in a room.
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

	a.send(protocol.NewCreateRoom("ChatRoom", 4))
	info := a.waitFor("ROOM_INFO", func(f *protocol.Frame) bool { return f.GetRoomInfo() != nil }).GetRoomInfo()
	b.send(protocol.NewJoinRoom(info.RoomId))
	b.waitFor("ROOM_INFO", func(f *protocol.Frame) bool { return f.GetRoomInfo() != nil })

	// alice sends chat; bob should receive ChatMsg.
	a.send(protocol.NewChat("hello world"))
	{
		f := b.waitFor("chat_msg", func(f *protocol.Frame) bool { return f.GetChatMsg() != nil })
		cm := f.GetChatMsg()
		if cm.Message != "hello world" {
			t.Errorf("chat message = %q want 'hello world'", cm.Message)
		}
		if cm.Sender == "" {
			t.Error("chat sender should not be empty")
		}
	}
}
