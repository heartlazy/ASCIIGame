package protocol

import (
	"testing"

	"github.com/heartlazyli/asciigame/internal/config"
)

// TestBuildGoldenVectors asserts every builder emits byte-for-byte the same
// frame the C snprintf formats produce. These are the compatibility red line.
func TestBuildGoldenVectors(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ok", BuildOK("success"), "OK|success\n"},
		{"ok-login", BuildOK("Login successful|42"), "OK|Login successful|42\n"},
		{"ok-empty", BuildOK(""), "OK|\n"},
		{"err", BuildErr(1001, "Invalid format"), "ERR|1001|Invalid format\n"},
		{"room-list", BuildRoomList("1,MyRoom,2,4,0;2,Game1,3,6,1"), "ROOM_LIST|1,MyRoom,2,4,0;2,Game1,3,6,1\n"},
		{"room-info", BuildRoomInfo(1, "Battle Arena", 3, 10, 0), "ROOM_INFO|1|Battle Arena|3|10|0\n"},
		{"player-join", BuildPlayerJoin(42, "Alice"), "PLAYER_JOIN|42|Alice\n"},
		{"player-leave", BuildPlayerLeave(42), "PLAYER_LEAVE|42\n"},
		{"game-start", BuildGameStart(), "GAME_START\n"},
		{"game-state", BuildGameState(1000, "1,10,20,100,15,5,5,0,0,0,0,0,0", "5,5,1;6,6,2", 25), "GAME_STATE|1000|1,10,20,100,15,5,5,0,0,0,0,0,0|5,5,1;6,6,2|25\n"},
		{"game-state-empty", BuildGameState(1000, "", "", 25), "GAME_STATE|1000|||25\n"},
		{"game-event", BuildGameEvent("DAMAGE", "1|2|10|90"), "GAME_EVENT|DAMAGE|1|2|10|90\n"},
		{"game-end", BuildGameEnd(-1, "kills=3"), "GAME_END|-1|kills=3\n"},
		{"chat-msg", BuildChatMsg("Alice", "hi all"), "CHAT_MSG|Alice|hi all\n"},
		{"kick", BuildKick("shutting down"), "KICK|shutting down\n"},
		{"login", BuildLogin("alice", "pass123"), "LOGIN|alice|pass123\n"},
		{"register", BuildRegister("bob", "pass456"), "REGISTER|bob|pass456\n"},
		{"create-room", BuildCreateRoom("MyRoom", 4), "CREATE_ROOM|MyRoom|4\n"},
		{"join-room", BuildJoinRoom(1), "JOIN_ROOM|1\n"},
		{"move", BuildMove('U'), "MOVE|U\n"},
		{"attack", BuildAttack(), "ATTACK|\n"},
		{"use-item", BuildUseItem(0), "USE_ITEM|0\n"},
		{"chat", BuildChat("hello"), "CHAT|hello\n"},
		{"simple-list", BuildSimple("LIST_ROOMS"), "LIST_ROOMS|\n"},
		{"simple-ready", BuildSimple("READY"), "READY|\n"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestBuildMapData verifies MAP_DATA framing: 20 rows joined by ',', 50 chars
// each, NUL rendered as space.
func TestBuildMapData(t *testing.T) {
	var m [config.MapHeight][config.MapWidth + 1]byte
	// Fill row 0 with '#', leave the rest as NUL (=> spaces on the wire).
	for x := 0; x < config.MapWidth; x++ {
		m[0][x] = '#'
	}
	got := BuildMapData(&m)

	// Expected: "MAP_DATA|" + 50*'#' + then 19 * ("," + 50*' ') + "\n"
	want := "MAP_DATA|"
	for x := 0; x < config.MapWidth; x++ {
		want += "#"
	}
	for y := 1; y < config.MapHeight; y++ {
		want += ","
		for x := 0; x < config.MapWidth; x++ {
			want += " "
		}
	}
	want += "\n"
	if got != want {
		t.Errorf("map data mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestParse verifies strtok_r-equivalent parsing: trailing CRLF stripped, split
// on '|', empty tokens skipped.
func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantCmd Command
		wantArg []string
		wantErr bool
	}{
		{"login", "LOGIN|alice|pass\n", CmdLogin, []string{"alice", "pass"}, false},
		{"crlf", "LOGIN|alice|pass\r\n", CmdLogin, []string{"alice", "pass"}, false},
		{"no-newline", "LOGIN|alice|pass", CmdLogin, []string{"alice", "pass"}, false},
		{"attack-empty-arg", "ATTACK|\n", CmdAttack, nil, false}, // empty token dropped => argc 0
		{"ready-simple", "READY|\n", CmdReady, nil, false},
		{"list-rooms", "LIST_ROOMS|\n", CmdListRooms, nil, false},
		{"move", "MOVE|U\n", CmdMove, []string{"U"}, false},
		{"collapsed-delims", "A||B", CmdUnknown, []string{"B"}, false}, // "A" unknown, empty skipped
		{"unknown-cmd", "FOOBAR|x\n", CmdUnknown, []string{"x"}, false},
		{"empty", "", CmdUnknown, nil, true},
		{"only-newline", "\n", CmdUnknown, nil, true},
	}
	for _, c := range cases {
		msg, err := Parse(c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got none", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
			continue
		}
		if msg.Type != c.wantCmd {
			t.Errorf("%s: cmd got %v (%s), want %v (%s)", c.name, msg.Type, CmdName(msg.Type), c.wantCmd, CmdName(c.wantCmd))
		}
		if len(msg.Args) != len(c.wantArg) {
			t.Errorf("%s: argc got %d %q, want %d %q", c.name, len(msg.Args), msg.Args, len(c.wantArg), c.wantArg)
			continue
		}
		for i := range c.wantArg {
			if msg.Args[i] != c.wantArg[i] {
				t.Errorf("%s: arg[%d] got %q, want %q", c.name, i, msg.Args[i], c.wantArg[i])
			}
		}
	}
}

// TestCommandEnumOrder guards the enum order against accidental reordering,
// which would silently break the wire protocol.
func TestCommandEnumOrder(t *testing.T) {
	if CmdLogin != 0 || CmdKick != 24 || CmdUnknown != 25 {
		t.Fatalf("command enum drifted: LOGIN=%d KICK=%d UNKNOWN=%d", CmdLogin, CmdKick, CmdUnknown)
	}
	if CmdName(CmdGameState) != "GAME_STATE" {
		t.Errorf("CmdName(CmdGameState)=%q", CmdName(CmdGameState))
	}
	if ParseCmd("GAME_STATE") != CmdGameState {
		t.Errorf("ParseCmd(GAME_STATE)=%v", ParseCmd("GAME_STATE"))
	}
	if ParseCmd("UNKNOWN") != CmdUnknown {
		t.Errorf("ParseCmd(UNKNOWN) should map to CmdUnknown")
	}
}
