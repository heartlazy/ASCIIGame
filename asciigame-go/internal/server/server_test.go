package server

import (
	"bufio"
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testClient is a minimal line-framed TCP client for integration tests.
type testClient struct {
	conn net.Conn
	r    *bufio.Reader
	t    *testing.T
}

func dial(t *testing.T, addr string) *testClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return &testClient{conn: conn, r: bufio.NewReader(conn), t: t}
}

func (c *testClient) send(s string) {
	c.t.Helper()
	if _, err := c.conn.Write([]byte(s + "\n")); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// readLine reads one frame with a deadline.
func (c *testClient) readLine() string {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	return strings.TrimRight(line, "\r\n")
}

// waitFor reads frames until one starts with prefix or the timeout elapses.
func (c *testClient) waitFor(prefix string) string {
	c.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.conn.SetReadDeadline(deadline)
		line, err := c.r.ReadString('\n')
		if err != nil {
			c.t.Fatalf("waitFor %q: %v", prefix, err)
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	c.t.Fatalf("timeout waiting for %q", prefix)
	return ""
}

func (c *testClient) close() { _ = c.conn.Close() }

// startTestServer boots a Server on an ephemeral port and returns its address.
func startTestServer(t *testing.T) string {
	t.Helper()
	srv, err := New(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
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

// TestFullMatch drives two players through register/login/room/ready and
// verifies GAME_START, a 13-field GAME_STATE, and a successful move.
func TestFullMatch(t *testing.T) {
	addr := startTestServer(t)

	reg := func(name string) {
		c := dial(t, addr)
		c.send("REGISTER|" + name + "|pw")
		if got := c.readLine(); !strings.HasPrefix(got, "OK") {
			t.Fatalf("register %s: %q", name, got)
		}
		c.close()
	}
	reg("alice")
	reg("bob")

	a := dial(t, addr)
	defer a.close()
	b := dial(t, addr)
	defer b.close()

	a.send("LOGIN|alice|pw")
	if got := a.waitFor("OK"); !strings.Contains(got, "Login successful") {
		t.Fatalf("alice login: %q", got)
	}
	b.send("LOGIN|bob|pw")
	b.waitFor("OK")

	// alice creates a room; parse the room id from ROOM_INFO.
	a.send("CREATE_ROOM|Arena|2")
	info := a.waitFor("ROOM_INFO")
	parts := strings.Split(info, "|")
	roomID := parts[1]

	b.send("JOIN_ROOM|" + roomID)
	b.waitFor("ROOM_INFO")

	// Both ready -> game starts.
	a.send("READY")
	b.send("READY")
	a.waitFor("GAME_START")
	b.waitFor("GAME_START")

	// A GAME_STATE frame must carry 13-field player entries.
	gs := a.waitFor("GAME_STATE")
	fields := strings.Split(gs, "|")
	if len(fields) != 5 {
		t.Fatalf("GAME_STATE should have 5 pipe fields, got %d: %q", len(fields), gs)
	}
	players := strings.Split(fields[2], ";")
	if len(players) < 1 {
		t.Fatalf("no player states in %q", gs)
	}
	if n := len(strings.Split(players[0], ",")); n != 13 {
		t.Fatalf("player entry should have 13 comma fields, got %d: %q", n, players[0])
	}

	// At least one of the four directions must be walkable from any valid
	// spawn (the map has no fully enclosed cells). Respect the 200ms cooldown.
	accepted := false
	for _, dir := range []string{"U", "D", "L", "R"} {
		a.send("MOVE|" + dir)
		rejected := false
		_ = a.conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		for {
			line, err := a.r.ReadString('\n')
			if err != nil {
				break
			}
			if strings.HasPrefix(strings.TrimRight(line, "\r\n"), "ERR|4002") {
				rejected = true
				break
			}
		}
		if !rejected {
			accepted = true
			break
		}
		time.Sleep(210 * time.Millisecond) // move cooldown
	}
	if !accepted {
		t.Fatalf("all four moves rejected as invalid")
	}
}
