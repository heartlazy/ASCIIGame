package server

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

// recoveredPlayer is a player's persisted state, reconstructed from the WAL.
type recoveredPlayer struct {
	id                                   int
	username                             string
	x, y, hp, maxHP, atk, def, baseATK   int
	hasShield                            bool
	status                               int
	inventory                            []ItemType
	atkBuffExpire                        int64
}

// pendingRecovery holds a reconstructed, not-yet-resumed room. Players rejoin
// by logging back in; the room waits RecoveryWaitTime for them.
type pendingRecovery struct {
	originalRoomID int
	roomName       string
	poisonRadius   int
	items          []mapItem
	players        []recoveredPlayer // alive players only
	expected       int
}

// RecoverAll scans the WAL directory on startup and rebuilds pending
// recoveries for rooms whose games did not end normally. Mirrors
// recovery_check_and_recover (recovery.c:988-1011). Must be called before
// serving connections.
func (s *Server) RecoverAll() {
	dir := filepath.FromSlash(config.WalDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // no WAL dir yet: nothing to recover
	}
	seen := map[int]bool{}
	maxRoomID := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "room_") || !strings.HasSuffix(name, ".wal") {
			continue
		}
		idStr := strings.TrimSuffix(strings.TrimPrefix(name, "room_"), ".wal")
		roomID, err := strconv.Atoi(idStr)
		if err != nil || seen[roomID] {
			continue
		}
		seen[roomID] = true
		if roomID > maxRoomID {
			maxRoomID = roomID
		}
		s.recoverRoom(roomID)
	}
	// Avoid reusing an id that a recovered room still owns.
	s.rmu.Lock()
	if maxRoomID >= s.nextRoomID {
		s.nextRoomID = maxRoomID + 1
	}
	s.rmu.Unlock()

	s.recMu.Lock()
	n := len(s.pending)
	s.recMu.Unlock()
	if n > 0 {
		log.Printf("recovery: %d room(s) pending player reconnect", n)
	}
}

// recoverRoom reconstructs one room from its WAL, storing a pendingRecovery if
// the game is still in progress with >1 survivor.
func (s *Server) recoverRoom(roomID int) {
	path := walPath(roomID)
	if walHasGameEnd(path) {
		// Game ended normally; drop stale files.
		walDeleteForRoom(roomID)
		snapshotDelete(roomID)
		return
	}
	state := replayWAL(path)
	if state == nil {
		return
	}
	var alive []recoveredPlayer
	for _, p := range state.players {
		if p.status == int(StatusGaming) && p.hp > 0 {
			alive = append(alive, p)
		}
	}
	if len(alive) <= 1 {
		// Nobody left to resume the match.
		walDeleteForRoom(roomID)
		snapshotDelete(roomID)
		return
	}
	if state.roomName == "" {
		state.roomName = fmt.Sprintf("Room_%d", roomID)
	}
	pr := &pendingRecovery{
		originalRoomID: roomID,
		roomName:       state.roomName,
		poisonRadius:   state.poisonRadius,
		items:          state.items,
		players:        alive,
		expected:       len(alive),
	}
	s.recMu.Lock()
	s.pending[roomID] = pr
	for _, p := range alive {
		s.recByUser[p.username] = roomID
	}
	s.recMu.Unlock()
	log.Printf("recovery: room %d reconstructed, %d survivors pending", roomID, len(alive))
}

// checkRecovery reports whether username has a game to rejoin.
func (s *Server) checkRecovery(username string) (int, bool) {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	id, ok := s.recByUser[username]
	return id, ok
}

// restorePlayerToGame rejoins a player to their recovered room, creating the
// live room on the first reconnect. Mirrors recovery_restore_player_to_game
// (recovery.c:1419-1549). Returns the live room, or nil on failure.
func (s *Server) restorePlayerToGame(p *Player, origID int) *Room {
	s.recMu.Lock()
	pr := s.pending[origID]
	if pr == nil {
		s.recMu.Unlock()
		return nil
	}
	// Find the recovered state for this user.
	var saved *recoveredPlayer
	for i := range pr.players {
		if pr.players[i].username == p.getUsername() {
			saved = &pr.players[i]
			break
		}
	}
	if saved == nil {
		s.recMu.Unlock()
		return nil
	}
	// Find or create the live recovery room.
	var room *Room
	if liveID, ok := s.recRoomByOrig[pr.originalRoomID]; ok {
		room = s.findRoomByID(liveID)
	}
	if room == nil {
		room = s.createRecoveryRoom(pr)
		if room == nil {
			s.recMu.Unlock()
			return nil
		}
		s.recRoomByOrig[pr.originalRoomID] = room.id
		go room.gameLoop()
	}
	delete(s.recByUser, p.getUsername())
	s.recMu.Unlock()

	// Place the player in a free slot.
	room.mu.Lock()
	slot := -1
	for i := 0; i < config.MaxRoomPlayers; i++ {
		if room.playerIDs[i] < 0 {
			slot = i
			break
		}
	}
	if slot < 0 {
		room.mu.Unlock()
		return nil
	}
	room.playerIDs[slot] = p.id
	room.playerCount++
	room.mu.Unlock()

	// Restore player fields.
	p.mu.Lock()
	p.roomID = room.id
	p.status = StatusGaming
	p.x, p.y = saved.x, saved.y
	p.hp, p.maxHP = saved.hp, saved.maxHP
	p.atk, p.def, p.baseATK = saved.atk, saved.def, saved.baseATK
	p.hasShield = saved.hasShield
	p.lastMoveTime, p.lastAttackTime = 0, 0
	if saved.atkBuffExpire > nowMS() {
		p.atkBuffExpire = saved.atkBuffExpire
	} else {
		p.atkBuffExpire = 0
	}
	p.atkBuffWarned = false
	p.inventoryCount = 0
	for _, it := range saved.inventory {
		if it > 0 && p.inventoryCount < config.MaxInventory {
			p.inventory[p.inventoryCount] = it
			p.inventoryCount++
		}
	}
	p.mu.Unlock()

	log.Printf("recovery: player %s rejoined room %d (orig %d)", p.getUsername(), room.id, pr.originalRoomID)
	return room
}

// createRecoveryRoom builds a live, gaming room from a pendingRecovery and
// seeds a fresh WAL with the full state (recovery.c:1298-1408).
func (s *Server) createRecoveryRoom(pr *pendingRecovery) *Room {
	room := s.createRoom(pr.roomName, config.MaxRoomPlayers)
	if room == nil {
		return nil
	}
	now := nowMS()
	room.mu.Lock()
	room.status = RoomGaming
	room.poisonRadius = pr.poisonRadius
	room.gameStartTime = now
	room.lastItemSpawn = now
	room.lastPoisonShrink = now
	room.isRecovery = true
	room.expectedPlayers = pr.expected
	room.recoveryStart = now
	room.originalRoomID = pr.originalRoomID
	mapGenerate(&room.m) // map is the static template
	room.itemCount = 0
	for _, it := range pr.items {
		if room.itemCount >= config.MaxMapItems {
			break
		}
		room.items[room.itemCount] = mapItem{x: it.x, y: it.y, typ: it.typ, active: true}
		room.itemCount++
	}
	room.mu.Unlock()

	// Fresh WAL seeded with full recovery state so repeated crashes still work.
	walDeleteForRoom(pr.originalRoomID)
	w := newWAL(pr.originalRoomID)
	room.mu.Lock()
	room.wal = w
	room.mu.Unlock()
	w.write(walGameStart, fmt.Sprintf("room_name=%s,max_players=%d,poison_radius=%d", pr.roomName, config.MaxRoomPlayers, pr.poisonRadius))
	for _, sp := range pr.players {
		remain := int64(0)
		if sp.atkBuffExpire > now {
			remain = sp.atkBuffExpire - now
		}
		inv := padInv(sp.inventory)
		w.write(walPlayerJoin, fmt.Sprintf(
			"pid=%d,username=%s,x=%d,y=%d,hp=%d,max_hp=%d,atk=%d,def=%d,base_atk=%d,shield=%d,atk_buff_remain=%d,inv=%d,%d,%d,%d,%d",
			sp.id, sp.username, sp.x, sp.y, sp.hp, sp.maxHP, sp.atk, sp.def, sp.baseATK,
			boolToInt(sp.hasShield), remain, inv[0], inv[1], inv[2], inv[3], inv[4]))
	}
	for _, it := range pr.items {
		w.write(walItemSpawn, fmt.Sprintf("type=%d,x=%d,y=%d", int(it.typ), it.x, it.y))
	}
	w.write(walPoisonShrink, fmt.Sprintf("radius=%d", pr.poisonRadius))
	w.sync()
	return room
}

// ---- WAL replay ----

// recoveryState accumulates game state while replaying a WAL.
type recoveryState struct {
	roomName     string
	poisonRadius int
	players      []recoveredPlayer
	items        []mapItem
}

func (rs *recoveryState) playerByID(id int) *recoveredPlayer {
	for i := range rs.players {
		if rs.players[i].id == id {
			return &rs.players[i]
		}
	}
	return nil
}

// replayWAL reconstructs game state from a WAL file, mirroring parse_wal_file
// (recovery.c:456-583). Returns nil if the file is unreadable/empty or ends
// with GAME_END.
func replayWAL(path string) *recoveryState {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	rs := &recoveryState{poisonRadius: mapInitialPoisonRadius()}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	records := 0
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "|", 5)
		if len(parts) < 5 {
			continue
		}
		action, ok := walParseAction(parts[3])
		if !ok {
			continue
		}
		data := parts[4]
		switch action {
		case walGameStart:
			kv := parseKV(data)
			if v, ok := kv["room_name"]; ok {
				rs.roomName = v
			}
			if v, ok := kv["poison_radius"]; ok {
				rs.poisonRadius = atoi(v)
			}
		case walPlayerJoin:
			rs.applyPlayerJoin(data)
		case walCheckpoint:
			kv := parseKV(data)
			if v, ok := kv["room_name"]; ok && v != "" {
				rs.roomName = v
			}
			if v, ok := kv["poison_radius"]; ok {
				rs.poisonRadius = atoi(v)
			}
		case walMove:
			kv := parseKV(data)
			if p := rs.playerByID(atoi(kv["pid"])); p != nil {
				p.x, p.y = atoi(kv["nx"]), atoi(kv["ny"])
			}
		case walDamage:
			kv := parseKV(data)
			if p := rs.playerByID(atoi(kv["vic"])); p != nil {
				p.hp = atoi(kv["hp"])
				if kv["shield_broken"] == "1" {
					p.hasShield = false
				}
			}
		case walPlayerDeath:
			kv := parseKV(data)
			if p := rs.playerByID(atoi(kv["pid"])); p != nil {
				p.hp = 0
				p.status = int(StatusDead)
			}
		case walPoisonShrink:
			rs.poisonRadius = atoi(parseKV(data)["radius"])
		case walItemSpawn:
			kv := parseKV(data)
			if len(rs.items) < config.MaxMapItems {
				rs.items = append(rs.items, mapItem{x: atoi(kv["x"]), y: atoi(kv["y"]), typ: ItemType(atoi(kv["type"])), active: true})
			}
		case walPickup:
			kv := parseKV(data)
			px, py := atoi(kv["x"]), atoi(kv["y"])
			for i := range rs.items {
				if rs.items[i].active && rs.items[i].x == px && rs.items[i].y == py {
					rs.items[i].active = false
					break
				}
			}
			if p := rs.playerByID(atoi(kv["pid"])); p != nil {
				p.inventory = append(p.inventory, ItemType(atoi(kv["item"])))
			}
		case walUseItem:
			kv := parseKV(data)
			if p := rs.playerByID(atoi(kv["pid"])); p != nil {
				idx := atoi(kv["idx"])
				if idx >= 0 && idx < len(p.inventory) {
					p.inventory = append(p.inventory[:idx], p.inventory[idx+1:]...)
				}
				switch ItemType(atoi(kv["item"])) {
				case ItemHealth:
					p.hp += config.HealthRestore
					if p.hp > p.maxHP {
						p.hp = p.maxHP
					}
				case ItemAttack:
					p.atk = config.InitialATK + config.AtkBuffAmount
				case ItemShield:
					p.hasShield = true
				}
			}
		case walGameEnd:
			return nil
		}
		records++
	}
	if records == 0 {
		return nil
	}
	// Drop inactive items so recovery rooms only seed live ones.
	active := rs.items[:0]
	for _, it := range rs.items {
		if it.active {
			active = append(active, it)
		}
	}
	rs.items = active
	return rs
}

// applyPlayerJoin upserts a player from a PLAYER_JOIN record, keyed by username
// (recovery.c:163-276).
func (rs *recoveryState) applyPlayerJoin(data string) {
	kv := parseKV(data)
	username := kv["username"]
	if username == "" {
		return
	}
	var p *recoveredPlayer
	for i := range rs.players {
		if rs.players[i].username == username {
			p = &rs.players[i]
			break
		}
	}
	if p == nil {
		rs.players = append(rs.players, recoveredPlayer{})
		p = &rs.players[len(rs.players)-1]
	}
	p.id = atoi(kv["pid"])
	p.username = username
	p.x, p.y = atoi(kv["x"]), atoi(kv["y"])
	p.hp = atoiOr(kv, "hp", config.InitialHP)
	p.maxHP = atoiOr(kv, "max_hp", config.InitialHP)
	p.atk = atoiOr(kv, "atk", config.InitialATK)
	p.def = atoiOr(kv, "def", config.InitialDEF)
	p.baseATK = atoiOr(kv, "base_atk", config.InitialATK)
	p.hasShield = kv["shield"] == "1"
	p.status = int(StatusGaming)
	if remain := int64(atoi(kv["atk_buff_remain"])); remain > 0 {
		p.atkBuffExpire = nowMS() + remain
	}
	p.inventory = nil
	if inv, ok := kv["inv"]; ok {
		for _, s := range strings.Split(inv, ",") {
			if t := atoi(s); t > 0 {
				p.inventory = append(p.inventory, ItemType(t))
			}
		}
	}
}

// walHasGameEnd reports whether the WAL contains a GAME_END record.
func walHasGameEnd(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if strings.Contains(sc.Text(), "|GAME_END|") {
			return true
		}
	}
	return false
}

// parseKV parses "k=v,k=v,..." into a map. The special "inv" key keeps its
// full comma-separated remainder (it is always last, per the WAL format).
func parseKV(data string) map[string]string {
	m := make(map[string]string)
	fields := strings.Split(data, ",")
	for i := 0; i < len(fields); i++ {
		kv := strings.SplitN(fields[i], "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == "inv" {
			m["inv"] = strings.Join(append([]string{kv[1]}, fields[i+1:]...), ",")
			break
		}
		m[kv[0]] = kv[1]
	}
	return m
}

func atoiOr(m map[string]string, key string, def int) int {
	if v, ok := m[key]; ok {
		return atoi(v)
	}
	return def
}

func padInv(inv []ItemType) [config.MaxInventory]int {
	var out [config.MaxInventory]int
	for i := 0; i < config.MaxInventory && i < len(inv); i++ {
		out[i] = int(inv[i])
	}
	return out
}

// sendRecoveryRejoin sends the reconnect handshake, mirroring handler_login's
// recovery branch (handler.c:120-149).
func (s *Server) sendRecoveryRejoin(p *Player, room *Room) {
	p.Send(protocol.BuildOK(fmt.Sprintf("Login successful - Rejoining game|%d", p.id)))
	p.Send(protocol.BuildGameStart())
	room.mu.Lock()
	info := protocol.BuildRoomInfo(room.id, room.name, room.playerCount, room.maxPlayers, int(room.status))
	room.mu.Unlock()
	p.Send(info)
}
