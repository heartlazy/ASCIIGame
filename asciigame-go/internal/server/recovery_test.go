package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// chdirTemp switches into a fresh temp dir (so data/wal is isolated) and
// restores the original cwd on cleanup.
func chdirTemp(t *testing.T) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func serveOn(t *testing.T, srv *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Serve(ctx, ln)
	return ln.Addr().String()
}

// TestCrashRecovery writes a WAL for an unfinished game, "restarts" the server
// via RecoverAll, and verifies a player who logs back in is placed straight
// into the recovered, in-progress room with restored state.
func TestCrashRecovery(t *testing.T) {
	chdirTemp(t)
	usersPath := filepath.FromSlash("data/users.json")

	// 1. Register alice & bob on a first server instance, then stop it.
	s1, err := New(usersPath)
	if err != nil {
		t.Fatalf("New s1: %v", err)
	}
	addr1 := serveOn(t, s1)
	for _, name := range []string{"alice", "bob"} {
		c := dial(t, addr1)
		c.send("REGISTER|" + name + "|pw")
		if got := c.readLine(); !strings.HasPrefix(got, "OK") {
			t.Fatalf("register %s: %q", name, got)
		}
		c.close()
	}

	// 2. Simulate a crash mid-game by writing a WAL with no GAME_END.
	if err := os.MkdirAll(filepath.FromSlash("data/wal"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	walContent := strings.Join([]string{
		"1000|1|7|GAME_START|room_name=Arena,max_players=2",
		"1001|2|7|PLAYER_JOIN|pid=1,username=alice,x=10,y=10,hp=80,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=1,0,0,0,0",
		"1002|3|7|PLAYER_JOIN|pid=2,username=bob,x=20,y=15,hp=90,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=0,0,0,0,0",
		"1003|4|7|ITEM_SPAWN|type=1,x=5,y=5",
		"1004|5|7|POISON_SHRINK|radius=20",
		"",
	}, "\n")
	if err := os.WriteFile(walPath(7), []byte(walContent), 0o644); err != nil {
		t.Fatalf("write wal: %v", err)
	}

	// 3. "Restart": a fresh server recovers from the WAL.
	s2, err := New(usersPath)
	if err != nil {
		t.Fatalf("New s2: %v", err)
	}
	s2.RecoverAll()
	if _, ok := s2.checkRecovery("alice"); !ok {
		t.Fatalf("alice not marked recoverable after RecoverAll")
	}
	addr2 := serveOn(t, s2)

	// 4. alice logs back in -> rejoins the game directly.
	a := dial(t, addr2)
	defer a.close()
	a.send("LOGIN|alice|pw")
	if got := a.waitFor("OK"); !strings.Contains(got, "Rejoining game") {
		t.Fatalf("alice login should rejoin: %q", got)
	}
	a.waitFor("GAME_START")
	a.waitFor("ROOM_INFO")

	// A GAME_STATE should show alice's restored HP of 80.
	gs := a.waitFor("GAME_STATE")
	if !hasPlayerWithHP(gs, 80) {
		t.Fatalf("recovered GAME_STATE missing restored hp=80: %q", gs)
	}

	// 5. bob logs back in -> rejoins the same recovered room.
	b := dial(t, addr2)
	defer b.close()
	b.send("LOGIN|bob|pw")
	if got := b.waitFor("OK"); !strings.Contains(got, "Rejoining game") {
		t.Fatalf("bob login should rejoin: %q", got)
	}
	b.waitFor("GAME_START")
}

// hasPlayerWithHP reports whether a GAME_STATE frame contains a player entry
// (13 comma fields) whose hp (field index 3) equals want.
func hasPlayerWithHP(gameState string, want int) bool {
	fields := strings.Split(gameState, "|")
	if len(fields) < 3 {
		return false
	}
	for _, entry := range strings.Split(fields[2], ";") {
		f := strings.Split(entry, ",")
		if len(f) >= 4 {
			if hp, err := strconv.Atoi(f[3]); err == nil && hp == want {
				return true
			}
		}
	}
	return false
}
