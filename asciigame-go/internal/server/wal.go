package server

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/heartlazyli/asciigame/internal/config"
)

// walAction enumerates the C WalActionType values (wal.h:17-30). The order and
// names must match so WAL files stay format-compatible with the C server.
type walAction int

const (
	walGameStart walAction = iota
	walPlayerJoin
	walPlayerLeave
	walMove
	walAttack
	walPickup
	walUseItem
	walDamage
	walPlayerDeath
	walItemSpawn
	walPoisonShrink
	walGameEnd
	walCheckpoint
)

var walActionNames = [...]string{
	"GAME_START", "PLAYER_JOIN", "PLAYER_LEAVE", "MOVE", "ATTACK", "PICKUP",
	"USE_ITEM", "DAMAGE", "PLAYER_DEATH", "ITEM_SPAWN", "POISON_SHRINK",
	"GAME_END", "CHECKPOINT",
}

func walActionName(a walAction) string {
	if a >= 0 && int(a) < len(walActionNames) {
		return walActionNames[a]
	}
	return "UNKNOWN"
}

func walParseAction(name string) (walAction, bool) {
	for i, n := range walActionNames {
		if n == name {
			return walAction(i), true
		}
	}
	return 0, false
}

// wal is a text Write-Ahead Log, format-compatible with the C version:
//
//	TIMESTAMP|SEQUENCE|ROOM_ID|ACTION_TYPE|ACTION_DATA\n
//
// (wal.c:159-205). Records are appended and fsync'd at most every
// WalSyncIntervalMS. Methods are nil-safe so callers need no nil checks.
type wal struct {
	roomID   int
	mu       sync.Mutex
	f        *os.File
	w        *bufio.Writer
	seq      int64
	lastSync int64
}

func walPath(roomID int) string {
	return filepath.Join(filepath.FromSlash(config.WalDir), fmt.Sprintf("room_%d.wal", roomID))
}

// newWAL opens (append) the room's WAL, continuing the sequence from any
// existing file. Mirrors wal_create_for_room (wal.c:100-136).
func newWAL(roomID int) *wal {
	if err := os.MkdirAll(filepath.FromSlash(config.WalDir), 0o755); err != nil {
		return nil
	}
	path := walPath(roomID)
	last := lastSequence(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return &wal{roomID: roomID, f: f, w: bufio.NewWriter(f), seq: last + 1, lastSync: nowMS()}
}

// lastSequence scans an existing WAL for the highest sequence number.
func lastSequence(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var last int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		fields := strings.SplitN(sc.Text(), "|", 3)
		if len(fields) >= 2 {
			if seq, err := strconv.ParseInt(fields[1], 10, 64); err == nil && seq > last {
				last = seq
			}
		}
	}
	return last
}

func (w *wal) write(action walAction, data string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	ts := nowMS()
	seq := w.seq
	w.seq++
	fmt.Fprintf(w.w, "%d|%d|%d|%s|%s\n", ts, seq, w.roomID, walActionName(action), data)
	if now := nowMS(); now-w.lastSync >= config.WalSyncIntervalMS {
		w.flushLocked()
		w.lastSync = now
	}
}

func (w *wal) sync() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
	w.lastSync = nowMS()
}

func (w *wal) flushLocked() {
	_ = w.w.Flush()
	_ = w.f.Sync() // fsync on Linux / FlushFileBuffers on Windows
}

func (w *wal) close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
	_ = w.f.Close()
	w.f = nil
}

// truncate clears the WAL and resets the sequence, mirroring wal_truncate
// (wal.c:319-348). Used after a snapshot so the WAL stays independently
// recoverable and small.
func (w *wal) truncate() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.w.Flush()
	_ = w.f.Close()
	f, err := os.OpenFile(walPath(w.roomID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		w.f = nil
		return
	}
	w.f = f
	w.w = bufio.NewWriter(f)
	w.seq = 1
}

func walExistsForRoom(roomID int) bool {
	_, err := os.Stat(walPath(roomID))
	return err == nil
}

func walDeleteForRoom(roomID int) {
	_ = os.Remove(walPath(roomID))
}
