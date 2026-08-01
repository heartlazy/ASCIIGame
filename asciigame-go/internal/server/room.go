package server

import (
	"fmt"
	"log"
	"math/rand/v2"
	"sync"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

// RoomStatus mirrors the C RoomStatus enum (room.h:19-24).
type RoomStatus int

const (
	RoomWaiting RoomStatus = iota
	RoomStarting
	RoomGaming
	RoomEnded
)

// mapItem mirrors the C MapItem (map.h:34-38), extended with spawn time for
// expiration.
type mapItem struct {
	x, y      int
	typ       ItemType
	active    bool
	spawnTime int64 // millisecond timestamp when this item appeared
}

// Room mirrors the C Room (room.h:27-63). Fields are guarded by mu. The
// per-room game loop goroutine and connection goroutines both touch these
// fields, exactly as the C game_thread and handler threads did.
type Room struct {
	srv        *Server
	id         int
	name       string
	maxPlayers int

	mu          sync.Mutex
	members     [config.MaxRoomPlayers]*Player // nil == empty slot
	playerCount int
	status      RoomStatus
	m           gameMap
	items       [config.MaxMapItems]mapItem
	itemCount   int
	poisonRadius int

	gameStartTime    int64
	lastItemSpawn    int64
	lastPoisonShrink int64
	lastSnapshotTime int64
	running          bool

	// lastStateSig is the marshaled GameState signature (timestamp excluded)
	// from the last broadcastState; used to skip identical broadcasts. Reset on
	// game start/end so the first state of each game always sends.
	lastStateSig []byte

	// Recovery bookkeeping (recovery.c). When a room is reconstructed after a
	// crash it waits for the expected players to reconnect before judging the
	// winner.
	isRecovery      bool
	expectedPlayers int
	recoveryStart   int64
	originalRoomID  int

	wal *wal
}

// createRoom mirrors room_create (room.c:83-166); max_players out of range is
// clamped to MaxRoomPlayers.
func (s *Server) createRoom(name string, maxPlayers int) *Room {
	if name == "" {
		return nil
	}
	if maxPlayers < config.MinRoomPlayers || maxPlayers > config.MaxRoomPlayers {
		maxPlayers = config.MaxRoomPlayers
	}
	s.rmu.Lock()
	defer s.rmu.Unlock()
	if len(s.rooms) >= config.MaxRooms {
		return nil
	}
	r := &Room{
		srv:            s,
		id:             s.nextRoomID,
		name:           truncate(name, config.MaxRoomName-1),
		maxPlayers:     maxPlayers,
		status:         RoomWaiting,
		poisonRadius:   mapInitialPoisonRadius(),
		originalRoomID: -1,
	}
	s.nextRoomID++
	s.rooms[r.id] = r
	log.Printf("room created: id=%d name=%s max=%d", r.id, name, maxPlayers)
	return r
}

// snapshotMembers returns the room's live players under the room lock, so
// callers can iterate without holding the lock or re-looking-up by id.
func (r *Room) snapshotMembers() []*Player {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.membersLocked()
}

// membersLocked returns the live players; the caller must hold r.mu.
func (r *Room) membersLocked() []*Player {
	out := make([]*Player, 0, r.playerCount)
	for _, p := range r.members {
		if p != nil {
			out = append(out, p)
		}
	}
	return out
}

// findRoomByID mirrors room_find_by_id (leaf lock).
func (s *Server) findRoomByID(id int) *Room {
	s.rmu.RLock()
	defer s.rmu.RUnlock()
	return s.rooms[id]
}

// destroyRoom mirrors room_destroy (room.c:168-202): stop the game loop, drop
// from the registry, close the WAL.
func (s *Server) destroyRoom(r *Room) {
	s.rmu.Lock()
	delete(s.rooms, r.id)
	s.rmu.Unlock()

	r.mu.Lock()
	r.running = false
	r.status = RoomEnded
	r.wal.close()
	r.wal = nil
	r.mu.Unlock()
	log.Printf("room destroyed: id=%d", r.id)
}

// addPlayer mirrors room_add_player (room.c:218-271):
//
//	0 ok, -1 full/no slot, -2 game in progress.
func (r *Room) addPlayer(p *Player) int {
	r.mu.Lock()
	if r.status == RoomGaming {
		r.mu.Unlock()
		return -2
	}
	if r.playerCount >= r.maxPlayers {
		r.mu.Unlock()
		return -1
	}
	slot := -1
	for i := 0; i < config.MaxRoomPlayers; i++ {
		if r.members[i] == nil {
			slot = i
			break
		}
	}
	if slot < 0 {
		r.mu.Unlock()
		return -1
	}
	r.members[slot] = p
	r.playerCount++
	r.mu.Unlock()

	p.mu.Lock()
	p.roomID = r.id
	p.status = StatusInRoom
	p.mu.Unlock()

	log.Printf("player %s joined room %d", p.label(), r.id)
	r.broadcast(protocol.NewPlayerJoin(int32(p.id), p.username))
	return 0
}

// removePlayer mirrors room_remove_player (room.c:273-324): remove, set player
// to lobby, broadcast leave, and destroy the room if it becomes empty.
func (r *Room) removePlayer(p *Player) {
	r.mu.Lock()
	found := false
	for i := 0; i < config.MaxRoomPlayers; i++ {
		if r.members[i] != nil && r.members[i].id == p.id {
			r.members[i] = nil
			r.playerCount--
			found = true
			break
		}
	}
	if !found {
		r.mu.Unlock()
		return
	}
	remaining := r.playerCount
	r.mu.Unlock()

	p.mu.Lock()
	p.roomID = -1
	p.status = StatusLobby
	p.mu.Unlock()

	if remaining > 0 {
		r.broadcast(protocol.NewPlayerLeave(int32(p.id)))
	} else {
		r.srv.destroyRoom(r)
	}
}

// broadcast mirrors room_broadcast (room.c:558-585): snapshot members under the
// room lock, then send off-lock so no socket write happens under a mutex.
func (r *Room) broadcast(f *protocol.Frame) {
	for _, p := range r.snapshotMembers() {
		p.Send(f)
	}
}

// allReady mirrors room_all_ready (room.c:641-679): at least MinRoomPlayers and
// every member in StatusReady.
func (r *Room) allReady() bool {
	r.mu.Lock()
	if r.playerCount < config.MinRoomPlayers {
		r.mu.Unlock()
		return false
	}
	members := r.membersLocked()
	r.mu.Unlock()

	for _, p := range members {
		p.mu.Lock()
		ready := p.status == StatusReady
		p.mu.Unlock()
		if !ready {
			return false
		}
	}
	return true
}

// startGame mirrors room_start_game (room.c:326-452): generate the map, seed
// items at spawn points, reset and place players, then broadcast GAME_START.
func (r *Room) startGame() int {
	r.mu.Lock()
	if r.status != RoomWaiting && r.status != RoomStarting {
		r.mu.Unlock()
		return -1
	}
	if r.playerCount < config.MinRoomPlayers {
		r.mu.Unlock()
		return -1
	}

	mapGenerate(&r.m)
	r.poisonRadius = mapInitialPoisonRadius()

	now := nowMS()
	r.itemCount = 0
	for y := 0; y < config.MapHeight && r.itemCount < config.MaxMapItems; y++ {
		for x := 0; x < config.MapWidth && r.itemCount < config.MaxMapItems; x++ {
			if r.m[y][x] == cellSpawn {
				r.items[r.itemCount] = mapItem{x: x, y: y, typ: ItemType(rand.IntN(3) + 1), active: true, spawnTime: now}
				r.itemCount++
			}
		}
	}

	r.gameStartTime = now
	r.lastItemSpawn = now
	r.lastPoisonShrink = now

	ids := r.membersLocked()
	r.status = RoomGaming
	r.lastStateSig = nil // first broadcastState of the game always sends
	r.mu.Unlock()

	// Reset and place players (not holding room lock while touching players,
	// except the brief map read under room lock, matching C).
	for _, p := range ids {
		p.resetGameState()
		r.mu.Lock()
		x, y := mapRandomPosition(&r.m)
		r.mu.Unlock()
		p.mu.Lock()
		p.x = x
		p.y = y
		p.status = StatusGaming
		p.mu.Unlock()
	}

	// Persist initial game state to the WAL (room_start_game, room.c:406-442).
	r.wal.write(walGameStart, fmt.Sprintf("room_name=%s,max_players=%d", r.name, r.maxPlayers))
	for _, p := range ids {
		p.mu.Lock()
		rec := fmt.Sprintf("pid=%d,username=%s,x=%d,y=%d,hp=%d,max_hp=%d,atk=%d,def=%d,shield=%d,inv=%d,%d,%d,%d,%d",
			p.id, p.username, p.x, p.y, p.hp, p.maxHP, p.atk, p.def, boolToInt(p.hasShield),
			int(p.inventory[0]), int(p.inventory[1]), int(p.inventory[2]), int(p.inventory[3]), int(p.inventory[4]))
		p.mu.Unlock()
		r.wal.write(walPlayerJoin, rec)
	}
	r.mu.Lock()
	for i := 0; i < r.itemCount; i++ {
		if r.items[i].active {
			r.wal.write(walItemSpawn, fmt.Sprintf("type=%d,x=%d,y=%d", int(r.items[i].typ), r.items[i].x, r.items[i].y))
		}
	}
	r.mu.Unlock()
	r.wal.sync()

	log.Printf("game started in room %d", r.id)
	r.broadcast(protocol.NewGameStart())
	return 0
}

// getRoomList mirrors room_get_list (room.c:587-639), returning a ROOM_LIST
// frame carrying one RoomInfo per live room.
func (s *Server) getRoomList() *protocol.Frame {
	s.rmu.RLock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.rmu.RUnlock()

	infos := make([]*protocol.RoomInfo, 0, len(rooms))
	for _, r := range rooms {
		r.mu.Lock()
		infos = append(infos, &protocol.RoomInfo{
			RoomId: int32(r.id), Name: r.name,
			PlayerCount: int32(r.playerCount), MaxPlayers: int32(r.maxPlayers),
			Status: int32(r.status),
		})
		r.mu.Unlock()
	}
	return protocol.NewRoomList(infos)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
