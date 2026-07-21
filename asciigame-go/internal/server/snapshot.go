package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/heartlazyli/asciigame/internal/config"
)

// snapshotFile is the on-disk snapshot. The C server used a binary format; the
// Go port uses JSON (per the port plan) since snapshots are an internal,
// non-wire artifact. It captures enough to rebuild a room after a crash.
type snapshotFile struct {
	Version          int          `json:"version"`
	Timestamp        int64        `json:"timestamp"`
	RoomID           int          `json:"room_id"`
	RoomName         string       `json:"room_name"`
	MaxPlayers       int          `json:"max_players"`
	Status           int          `json:"status"`
	Map              []string     `json:"map"`
	PoisonRadius     int          `json:"poison_radius"`
	Items            []snapItem   `json:"items"`
	GameStartTime    int64        `json:"game_start_time"`
	LastItemSpawn    int64        `json:"last_item_spawn"`
	LastPoisonShrink int64        `json:"last_poison_shrink"`
	Players          []snapPlayer `json:"players"`
}

type snapItem struct {
	X    int      `json:"x"`
	Y    int      `json:"y"`
	Type ItemType `json:"type"`
}

type snapPlayer struct {
	ID            int        `json:"id"`
	Username      string     `json:"username"`
	X             int        `json:"x"`
	Y             int        `json:"y"`
	HP            int        `json:"hp"`
	MaxHP         int        `json:"max_hp"`
	ATK           int        `json:"atk"`
	DEF           int        `json:"def"`
	BaseATK       int        `json:"base_atk"`
	HasShield     bool       `json:"has_shield"`
	Status        int        `json:"status"`
	Inventory     []ItemType `json:"inventory"`
	AtkBuffExpire int64      `json:"atk_buff_expire"`
}

const snapshotVersion = 1

func snapshotPath(roomID int) string {
	return filepath.Join(filepath.FromSlash(config.SnapshotDir), fmt.Sprintf("room_%d.snap", roomID))
}

func snapshotExists(roomID int) bool {
	_, err := os.Stat(snapshotPath(roomID))
	return err == nil
}

func snapshotDelete(roomID int) {
	_ = os.Remove(snapshotPath(roomID))
}

// snapshotShouldSave mirrors snapshot_should_save (snapshot.c:94-101).
func (r *Room) snapshotShouldSave() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status == RoomGaming && nowMS()-r.lastSnapshotTime >= config.SnapshotIntervalMS
}

// snapshotSave writes a JSON snapshot atomically, then truncates the WAL and
// rewrites a self-contained checkpoint (matching snapshot_save, snapshot.c),
// so the WAL alone can still recover the latest state.
func (r *Room) snapshotSave() {
	r.mu.Lock()
	roomID := r.id
	if r.originalRoomID >= 0 {
		roomID = r.originalRoomID
	}
	now := nowMS()
	snap := snapshotFile{
		Version: snapshotVersion, Timestamp: now, RoomID: roomID,
		RoomName: r.name, MaxPlayers: r.maxPlayers, Status: int(r.status),
		PoisonRadius: r.poisonRadius, GameStartTime: r.gameStartTime,
		LastItemSpawn: r.lastItemSpawn, LastPoisonShrink: r.lastPoisonShrink,
	}
	for y := 0; y < config.MapHeight; y++ {
		snap.Map = append(snap.Map, string(r.m[y][:config.MapWidth]))
	}
	for i := 0; i < r.itemCount; i++ {
		if r.items[i].active {
			snap.Items = append(snap.Items, snapItem{X: r.items[i].x, Y: r.items[i].y, Type: r.items[i].typ})
		}
	}
	ids := make([]int, 0, r.playerCount)
	for i := 0; i < config.MaxRoomPlayers; i++ {
		if r.playerIDs[i] >= 0 {
			ids = append(ids, r.playerIDs[i])
		}
	}
	r.mu.Unlock()

	for _, id := range ids {
		p := r.srv.findPlayerByID(id)
		if p == nil {
			continue
		}
		p.mu.Lock()
		inv := make([]ItemType, config.MaxInventory)
		copy(inv, p.inventory[:])
		snap.Players = append(snap.Players, snapPlayer{
			ID: p.id, Username: p.username, X: p.x, Y: p.y, HP: p.hp, MaxHP: p.maxHP,
			ATK: p.atk, DEF: p.def, BaseATK: p.baseATK, HasShield: p.hasShield,
			Status: int(p.status), Inventory: inv, AtkBuffExpire: p.atkBuffExpire,
		})
		p.mu.Unlock()
	}

	if err := writeJSONAtomic(snapshotPath(roomID), &snap); err != nil {
		return
	}

	r.mu.Lock()
	r.lastSnapshotTime = now
	r.mu.Unlock()

	// Keep the WAL small and self-contained: truncate, then rewrite the full
	// current state as a checkpoint (snapshot.c:219-263).
	if r.wal != nil {
		r.wal.truncate()
		r.wal.write(walCheckpoint, fmt.Sprintf("snapshot_time=%d,room_name=%s,poison_radius=%d", now, snap.RoomName, snap.PoisonRadius))
		for _, sp := range snap.Players {
			remain := int64(0)
			if sp.AtkBuffExpire > now {
				remain = sp.AtkBuffExpire - now
			}
			r.wal.write(walPlayerJoin, fmt.Sprintf(
				"pid=%d,username=%s,x=%d,y=%d,hp=%d,max_hp=%d,atk=%d,def=%d,base_atk=%d,shield=%d,atk_buff_remain=%d,inv=%d,%d,%d,%d,%d",
				sp.ID, sp.Username, sp.X, sp.Y, sp.HP, sp.MaxHP, sp.ATK, sp.DEF, sp.BaseATK,
				boolToInt(sp.HasShield), remain,
				int(sp.Inventory[0]), int(sp.Inventory[1]), int(sp.Inventory[2]), int(sp.Inventory[3]), int(sp.Inventory[4])))
		}
		for _, it := range snap.Items {
			r.wal.write(walItemSpawn, fmt.Sprintf("type=%d,x=%d,y=%d", int(it.Type), it.X, it.Y))
		}
		r.wal.write(walPoisonShrink, fmt.Sprintf("radius=%d", snap.PoisonRadius))
		r.wal.sync()
	}
}

// writeJSONAtomic writes v as indented JSON to path via a temp file + rename.
func writeJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// snapshotLoad reads a snapshot file, returning nil if absent/invalid.
func snapshotLoad(roomID int) *snapshotFile {
	data, err := os.ReadFile(snapshotPath(roomID))
	if err != nil {
		return nil
	}
	var snap snapshotFile
	if json.Unmarshal(data, &snap) != nil || snap.Version != snapshotVersion {
		return nil
	}
	return &snap
}
