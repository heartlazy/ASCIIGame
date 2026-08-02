package server

import (
	"bufio"
	"context"
	"net"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heartlazyli/asciigame/internal/protocol"
)

// countingConn wraps a net.Conn and counts bytes read (for traffic measurement).
type countingConn struct {
	net.Conn
	bytesRead int64
}

func (c *countingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	atomic.AddInt64(&c.bytesRead, int64(n))
	return n, err
}

// setupBenchGame creates a server, registers+logs in two players, creates a
// room, joins, readies, and returns the two authenticated TCP connections (with
// counting wrappers) and a cancel func. The game is already running when this
// returns.
func setupBenchGame(t *testing.T) (ta, tb *countingConn, raTCP, rbTCP *bufio.Reader, cancel context.CancelFunc) {
	t.Helper()
	srv, err := New(filepath.Join(t.TempDir(), "game.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	engine := srv.SetupHTTP()
	hs := httptest.NewServer(engine)
	t.Cleanup(hs.Close)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelFn := context.WithCancel(context.Background())
	go srv.Serve(ctx, ln)

	hc := &httpHelper{t: t, client: hs.Client(), base: hs.URL}
	hc.register("alice", "pw")
	hc.register("bob", "pw")

	// Login alice
	ha := &httpHelper{t: t, client: hs.Client(), base: hs.URL}
	ha.login("alice", "pw")
	// Login bob
	hb := &httpHelper{t: t, client: hs.Client(), base: hs.URL}
	hb.login("bob", "pw")

	// TCP connect alice
	connA, _ := net.Dial("tcp", ln.Addr().String())
	ta = &countingConn{Conn: connA}
	raTCP = bufio.NewReader(ta)
	_ = protocol.WriteFrame(connA, protocol.NewAuth(ha.token))
	protocol.ReadFrame(raTCP) // Ok

	// TCP connect bob
	connB, _ := net.Dial("tcp", ln.Addr().String())
	tb = &countingConn{Conn: connB}
	rbTCP = bufio.NewReader(tb)
	_ = protocol.WriteFrame(connB, protocol.NewAuth(hb.token))
	protocol.ReadFrame(rbTCP) // Ok

	// Create room + join + ready
	ha.post("/api/rooms", map[string]any{"name": "Bench", "max_players": 2})
	hb.post("/api/rooms/1/join", nil)

	// Drain PlayerJoin notifications on TCP before ready
	drainUntilTimeout(raTCP, connA, 200*time.Millisecond)
	drainUntilTimeout(rbTCP, connB, 200*time.Millisecond)

	ha.post("/api/rooms/ready", nil)
	hb.post("/api/rooms/ready", nil)

	// Wait for GAME_START on both
	waitForFrame(t, raTCP, connA, "GAME_START", func(f *protocol.Frame) bool { return f.GetGameStart() != nil })
	waitForFrame(t, rbTCP, connB, "GAME_START", func(f *protocol.Frame) bool { return f.GetGameStart() != nil })

	// Reset byte counters after setup
	atomic.StoreInt64(&ta.bytesRead, 0)
	atomic.StoreInt64(&tb.bytesRead, 0)

	return ta, tb, raTCP, rbTCP, cancelFn
}

func drainUntilTimeout(r *bufio.Reader, conn net.Conn, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		protocol.ReadFrame(r)
	}
}

func waitForFrame(t *testing.T, r *bufio.Reader, conn net.Conn, name string, match func(*protocol.Frame) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		f, err := protocol.ReadFrame(r)
		if err != nil {
			t.Fatalf("waitFor %s: %v", name, err)
		}
		if match(f) {
			return
		}
	}
	t.Fatalf("timeout waiting for %s", name)
}

func countFrames(r *bufio.Reader, conn net.Conn, duration time.Duration, match func(*protocol.Frame) bool) int {
	count := 0
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		f, err := protocol.ReadFrame(r)
		if err != nil {
			continue
		}
		if match(f) {
			count++
		}
	}
	return count
}

// --- Test 1.1: Idle scenario (no actions) ---

func TestDirtyDetection_Idle(t *testing.T) {
	ta, _, raTCP, _, cancel := setupBenchGame(t)
	defer cancel()
	defer ta.Close()

	// Wait for the first GAME_STATE (always sent).
	waitForFrame(t, raTCP, ta, "first GAME_STATE", func(f *protocol.Frame) bool { return f.GetGameState() != nil })

	// Count GAME_STATE frames over 2 seconds of pure idleness.
	count := countFrames(raTCP, ta, 2*time.Second, func(f *protocol.Frame) bool { return f.GetGameState() != nil })

	// With dirty detection: should be 0 (nothing changed).
	// Without: would be ~40 (2s * 20 tick/s).
	t.Logf("Idle 2s: received %d GAME_STATE frames (expect 0 with dirty detection, ~40 without)", count)
	if count > 2 {
		t.Errorf("dirty detection not working: got %d frames in idle period, expected <=2", count)
	}
}

// --- Test 1.2: Active scenario (one player moves every 200ms) ---

func TestDirtyDetection_Active(t *testing.T) {
	ta, _, raTCP, _, cancel := setupBenchGame(t)
	defer cancel()
	defer ta.Close()

	// Drain initial state.
	waitForFrame(t, raTCP, ta, "first GAME_STATE", func(f *protocol.Frame) bool { return f.GetGameState() != nil })

	// Move alice every 210ms for 2 seconds (will try ~9 moves).
	go func() {
		for i := 0; i < 9; i++ {
			dirs := []string{"U", "D", "L", "R"}
			_ = protocol.WriteFrame(ta.Conn, protocol.NewMove(dirs[i%4]))
			time.Sleep(210 * time.Millisecond)
		}
	}()

	// Count GAME_STATE frames over 2 seconds.
	count := countFrames(raTCP, ta, 2*time.Second, func(f *protocol.Frame) bool { return f.GetGameState() != nil })

	// With dirty detection: roughly 9-18 frames (each move triggers one broadcast,
	// plus possibly a few from poison/item changes).
	// Without: would be ~40.
	t.Logf("Active 2s (9 moves): received %d GAME_STATE frames (expect ~9-20 with dirty, ~40 without)", count)
	if count > 30 {
		t.Errorf("dirty detection seems ineffective: %d frames with active movement", count)
	}
}

// --- Test 1.3: Traffic bytes comparison ---

func TestDirtyDetection_TrafficBytes(t *testing.T) {
	ta, _, raTCP, _, cancel := setupBenchGame(t)
	defer cancel()
	defer ta.Close()

	// Measure bytes over 2 seconds of idleness.
	waitForFrame(t, raTCP, ta, "first GAME_STATE", func(f *protocol.Frame) bool { return f.GetGameState() != nil })
	atomic.StoreInt64(&ta.bytesRead, 0)

	// Idle for 2 seconds.
	time.Sleep(2 * time.Second)
	_ = ta.Conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	// Drain any remaining.
	for {
		_, err := protocol.ReadFrame(raTCP)
		if err != nil {
			break
		}
	}

	idleBytes := atomic.LoadInt64(&ta.bytesRead)
	t.Logf("Idle 2s traffic: %d bytes (with dirty detection; without would be ~%d bytes)",
		idleBytes, 40*200) // rough estimate: 40 frames * ~200 bytes each

	// With dirty detection, idle traffic should be near zero (maybe a few event
	// frames from buff/poison timers, but no GAME_STATE flood).
	if idleBytes > 2000 {
		t.Errorf("idle traffic too high: %d bytes (dirty detection may not be working)", idleBytes)
	}
}
