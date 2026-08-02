package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// generateWAL creates a WAL file with nRecords of game activity for room roomID.
// Returns the directory containing data/wal/.
func generateWAL(tb testing.TB, roomID int, nRecords int) string {
	tb.Helper()
	dir := tb.TempDir()
	walDir := filepath.Join(dir, "data", "wal")
	if err := os.MkdirAll(walDir, 0o755); err != nil {
		tb.Fatal(err)
	}

	var lines []string
	seq := 1
	ts := int64(1000000)

	// GAME_START
	lines = append(lines, fmt.Sprintf("%d|%d|%d|GAME_START|room_name=BenchRoom,max_players=6", ts, seq, roomID))
	seq++
	ts += 50

	// Two players
	lines = append(lines, fmt.Sprintf("%d|%d|%d|PLAYER_JOIN|pid=1,username=alice,x=10,y=10,hp=100,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=0,0,0,0,0", ts, seq, roomID))
	seq++
	ts += 50
	lines = append(lines, fmt.Sprintf("%d|%d|%d|PLAYER_JOIN|pid=2,username=bob,x=30,y=15,hp=100,max_hp=100,atk=15,def=5,base_atk=15,shield=0,atk_buff_remain=0,inv=0,0,0,0,0", ts, seq, roomID))
	seq++
	ts += 50

	// Initial items
	for i := 0; i < 5; i++ {
		lines = append(lines, fmt.Sprintf("%d|%d|%d|ITEM_SPAWN|type=%d,x=%d,y=%d", ts, seq, roomID, (i%3)+1, 5+i*3, 5))
		seq++
		ts += 50
	}

	// Generate game activity records (moves, attacks, pickups, poison shrinks).
	x, y := 10, 10
	for i := 0; i < nRecords; i++ {
		switch i % 5 {
		case 0: // move
			nx := x + 1
			if nx >= 48 {
				nx = 2
			}
			lines = append(lines, fmt.Sprintf("%d|%d|%d|MOVE|pid=1,dir=R,ox=%d,oy=%d,nx=%d,ny=%d", ts, seq, roomID, x, y, nx, y))
			x = nx
		case 1: // attack
			lines = append(lines, fmt.Sprintf("%d|%d|%d|ATTACK|pid=1,x=%d,y=%d,atk=15", ts, seq, roomID, x, y))
		case 2: // damage
			lines = append(lines, fmt.Sprintf("%d|%d|%d|DAMAGE|atk=1,vic=2,dmg=10,hp=90", ts, seq, roomID))
		case 3: // item spawn
			lines = append(lines, fmt.Sprintf("%d|%d|%d|ITEM_SPAWN|type=1,x=%d,y=%d", ts, seq, roomID, (i*3)%45+2, (i*2)%18+1))
		case 4: // poison shrink
			lines = append(lines, fmt.Sprintf("%d|%d|%d|POISON_SHRINK|radius=%d", ts, seq, roomID, 25-i/100))
		}
		seq++
		ts += 50
	}

	walPath := filepath.Join(walDir, fmt.Sprintf("room_%d.wal", roomID))
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(walPath, []byte(content), 0o644); err != nil {
		tb.Fatal(err)
	}
	return dir
}

// BenchmarkRecovery_WALOnly benchmarks recovery from WAL files of varying sizes
// (simulating pure-WAL recovery without snapshots).
func BenchmarkRecovery_WALOnly(b *testing.B) {
	sizes := []int{100, 500, 2000, 6000}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("records=%d", n), func(b *testing.B) {
			dir := generateWAL(b, 1, n)
			walFile := filepath.Join(dir, "data", "wal", "room_1.wal")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := replayWAL(walFile)
				if state == nil {
					b.Fatal("replay returned nil")
				}
			}
		})
	}
}

// BenchmarkRecovery_WithSnapshot benchmarks recovery where the WAL has been
// truncated after a snapshot (only ~20s worth of records remain).
func BenchmarkRecovery_WithSnapshot(b *testing.B) {
	// After a snapshot, the WAL is truncated and only contains a CHECKPOINT +
	// full player state + recent events. Simulate this with a small WAL
	// representing "20 seconds of activity after last snapshot" (~400 records).
	postSnapshotRecords := 400
	dir := generateWAL(b, 1, postSnapshotRecords)
	walFile := filepath.Join(dir, "data", "wal", "room_1.wal")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := replayWAL(walFile)
		if state == nil {
			b.Fatal("replay returned nil")
		}
	}
}

// TestRecoverySpeed_Comparison prints a comparison of recovery times for
// different WAL sizes, illustrating the advantage of periodic snapshots.
func TestRecoverySpeed_Comparison(t *testing.T) {
	sizes := []struct {
		name    string
		records int
	}{
		{"20s_post_snapshot (WAL+Snapshot)", 400},
		{"1min_pure_WAL", 1200},
		{"3min_pure_WAL", 3600},
		{"5min_pure_WAL", 6000},
	}

	t.Log("=== Recovery Speed Comparison ===")
	t.Log("Scenario                         | Records | Time")
	t.Log("---------------------------------|---------|--------")

	for _, s := range sizes {
		dir := generateWAL(t, 1, s.records)
		walFile := filepath.Join(dir, "data", "wal", "room_1.wal")

		start := time.Now()
		iterations := 100
		for i := 0; i < iterations; i++ {
			state := replayWAL(walFile)
			if state == nil {
				t.Fatalf("replay returned nil for %s", s.name)
			}
		}
		elapsed := time.Since(start) / time.Duration(iterations)
		t.Logf("%-33s | %7d | %v", s.name, s.records, elapsed)
	}
}

// TestWALFileSize_Comparison shows WAL file sizes with and without snapshots.
func TestWALFileSize_Comparison(t *testing.T) {
	t.Log("=== WAL File Size Comparison ===")
	t.Log("Scenario                         | Records | WAL Size")
	t.Log("---------------------------------|---------|----------")

	sizes := []struct {
		name    string
		records int
	}{
		{"20s_post_snapshot (truncated)", 400},
		{"1min_no_snapshot", 1200},
		{"3min_no_snapshot", 3600},
		{"5min_no_snapshot", 6000},
	}

	for _, s := range sizes {
		dir := generateWAL(t, 1, s.records)
		walFile := filepath.Join(dir, "data", "wal", "room_1.wal")
		info, _ := os.Stat(walFile)
		t.Logf("%-33s | %7d | %d bytes (%.1f KB)", s.name, s.records, info.Size(), float64(info.Size())/1024)
	}
}
