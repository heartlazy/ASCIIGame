package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

// calcDamage mirrors game_calc_damage (game.c:21-24): atk-def, floored at 1.
func calcDamage(atk, def int) int {
	if d := atk - def; d > 0 {
		return d
	}
	return 1
}

// direction bytes accepted by MOVE (game.c:96-100).
const (
	dirUp    = 'U'
	dirDown  = 'D'
	dirLeft  = 'L'
	dirRight = 'R'
)

// broadcastEvent mirrors game_broadcast_event (game.c:690-696).
func (r *Room) broadcastEvent(eventType, data string) {
	r.broadcast(protocol.BuildGameEvent(eventType, data))
}

// updateBuffs mirrors game_update_buffs (game.c:26-75).
//
// NOTE: the C version stripped the trailing '\n' from these events before
// player_send (which does not re-add it), yielding an unterminated frame — a
// latent framing bug. The Go port sends a properly terminated frame.
func (r *Room) updateBuffs(p *Player) {
	now := nowMS()
	p.mu.Lock()
	if p.atkBuffExpire > 0 {
		remaining := p.atkBuffExpire - now
		if remaining <= 0 {
			p.atk = p.baseATK
			p.atkBuffExpire = 0
			p.atkBuffWarned = false
			id := p.id
			p.mu.Unlock()
			r.broadcastEvent("BUFF_EXPIRED", fmt.Sprintf("%d", id))
			return
		} else if remaining <= 5000 && !p.atkBuffWarned {
			p.atkBuffWarned = true
			seconds := int(remaining / 1000)
			id := p.id
			p.mu.Unlock()
			r.broadcastEvent("BUFF_WARNING", fmt.Sprintf("%d|%d", id, seconds))
			return
		}
	}
	p.mu.Unlock()
}

// handleMove mirrors game_handle_move (game.c:79-140):
//
//	0 ok, -1 on cooldown, -2 invalid direction/blocked.
func (r *Room) handleMove(p *Player, direction byte) int {
	now := nowMS()

	p.mu.Lock()
	if now-p.lastMoveTime < config.MoveCooldownMS {
		p.mu.Unlock()
		return -1
	}
	ox, oy := p.x, p.y
	nx, ny := p.x, p.y
	switch direction {
	case dirUp:
		ny--
	case dirDown:
		ny++
	case dirLeft:
		nx--
	case dirRight:
		nx++
	default:
		p.mu.Unlock()
		return -2
	}
	p.mu.Unlock()

	r.mu.Lock()
	walkable := mapIsWalkable(&r.m, nx, ny)
	r.mu.Unlock()
	if !walkable {
		return -2
	}

	r.wal.write(walMove, fmt.Sprintf("pid=%d,dir=%c,ox=%d,oy=%d,nx=%d,ny=%d", p.id, direction, ox, oy, nx, ny))

	p.mu.Lock()
	p.x = nx
	p.y = ny
	p.lastMoveTime = now
	p.mu.Unlock()

	r.checkItemPickup(p)
	return 0
}

// handleAttack mirrors game_handle_attack (game.c:144-293): scan players within
// ATTACK_RANGE (Manhattan), apply shield/damage/death. Events are collected
// under lock and broadcast off-lock, preserving ATTACK -> per-hit -> RESULT
// ordering.
//
//	0 ok, -1 on cooldown.
func (r *Room) handleAttack(p *Player) int {
	now := nowMS()

	p.mu.Lock()
	if now-p.lastAttackTime < config.AttackCooldownMS {
		p.mu.Unlock()
		return -1
	}
	atkX, atkY, atkPower, attackerID := p.x, p.y, p.atk, p.id
	p.lastAttackTime = now
	p.mu.Unlock()

	r.wal.write(walAttack, fmt.Sprintf("pid=%d,x=%d,y=%d,atk=%d", attackerID, atkX, atkY, atkPower))
	r.broadcastEvent("ATTACK", fmt.Sprintf("%d|%d|%d", attackerID, atkX, atkY))

	hitCount := 0
	var events []string

	r.mu.Lock()
	for i := 0; i < config.MaxRoomPlayers; i++ {
		id := r.playerIDs[i]
		if id < 0 || id == attackerID {
			continue
		}
		target := r.srv.findPlayerByID(id)
		if target == nil {
			continue
		}
		target.mu.Lock()
		dist := mapDistance(atkX, atkY, target.x, target.y)
		if dist <= config.AttackRange && target.status == StatusGaming && target.hp > 0 {
			if target.hasShield {
				target.hasShield = false
				r.wal.write(walDamage, fmt.Sprintf("atk=%d,vic=%d,dmg=0,hp=%d,shield_broken=1", attackerID, target.id, target.hp))
				events = append(events, protocol.BuildGameEvent("SHIELD",
					fmt.Sprintf("%d|%d", attackerID, target.id)))
			} else {
				dmg := calcDamage(atkPower, target.def)
				target.hp -= dmg
				hitCount++
				r.wal.write(walDamage, fmt.Sprintf("atk=%d,vic=%d,dmg=%d,hp=%d", attackerID, target.id, dmg, target.hp))
				events = append(events, protocol.BuildGameEvent("DAMAGE",
					fmt.Sprintf("%d|%d|%d|%d", attackerID, target.id, dmg, target.hp)))
				if target.hp <= 0 {
					target.hp = 0
					target.status = StatusDead
					r.wal.write(walPlayerDeath, fmt.Sprintf("pid=%d,killer=%d", target.id, attackerID))
					events = append(events, protocol.BuildGameEvent("KILL",
						fmt.Sprintf("%d|%d", attackerID, target.id)))
				}
			}
		}
		target.mu.Unlock()
	}
	r.mu.Unlock()

	for _, e := range events {
		r.broadcast(e)
	}
	r.broadcastEvent("ATTACK_RESULT", fmt.Sprintf("%d|%d", attackerID, hitCount))
	return 0
}

// checkItemPickup mirrors game_check_item_pickup (game.c:297-343): pick up at
// most one item on the player's cell, if the inventory has room.
func (r *Room) checkItemPickup(p *Player) {
	p.mu.Lock()
	px, py, invCount := p.x, p.y, p.inventoryCount
	p.mu.Unlock()
	if invCount >= config.MaxInventory {
		return
	}

	r.mu.Lock()
	var picked ItemType = ItemNone
	for i := 0; i < r.itemCount; i++ {
		it := &r.items[i]
		if it.active && it.x == px && it.y == py {
			picked = it.typ
			it.active = false
			break
		}
	}
	r.mu.Unlock()

	if picked == ItemNone {
		return
	}
	r.wal.write(walPickup, fmt.Sprintf("pid=%d,item=%d,x=%d,y=%d", p.id, int(picked), px, py))
	p.addItem(picked)
	r.broadcastEvent("PICKUP", fmt.Sprintf("%d|%d", p.id, int(picked)))
}

// handleUseItem mirrors game_handle_use_item (game.c:345-392):
//
//	0 ok, -1 invalid index.
func (r *Room) handleUseItem(p *Player, index int) int {
	t := p.useItem(index)
	if t == ItemNone {
		return -1
	}
	r.wal.write(walUseItem, fmt.Sprintf("pid=%d,item=%d,idx=%d", p.id, int(t), index))

	p.mu.Lock()
	switch t {
	case ItemHealth:
		p.hp += config.HealthRestore
		if p.hp > p.maxHP {
			p.hp = p.maxHP
		}
	case ItemAttack:
		p.atk = p.baseATK + config.AtkBuffAmount
		p.atkBuffExpire = nowMS() + config.AtkBuffDuration
		p.atkBuffWarned = false
	case ItemShield:
		p.hasShield = true
	}
	p.mu.Unlock()
	return 0
}

// spawnItems mirrors game_spawn_items (game.c:394-448): every ItemSpawnInterval,
// fill one free slot with a random item at a random spawn/empty cell.
func (r *Room) spawnItems() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := nowMS()
	if now-r.lastItemSpawn < config.ItemSpawnInterval {
		return
	}
	r.lastItemSpawn = now

	slot := -1
	for i := 0; i < config.MaxMapItems; i++ {
		if !r.items[i].active {
			slot = i
			break
		}
	}
	if slot < 0 {
		return
	}
	x, y := mapRandomItemPosition(&r.m)
	r.items[slot] = mapItem{x: x, y: y, typ: randItemType(), active: true}
	if slot >= r.itemCount {
		r.itemCount = slot + 1
	}
	r.wal.write(walItemSpawn, fmt.Sprintf("type=%d,x=%d,y=%d", int(r.items[slot].typ), x, y))
}

// updatePoison mirrors game_update_poison (game.c:452-492).
func (r *Room) updatePoison() {
	r.mu.Lock()
	now := nowMS()
	if now-r.gameStartTime < config.PoisonStartTime {
		r.mu.Unlock()
		return
	}
	if now-r.lastPoisonShrink < config.PoisonShrinkInterval {
		r.mu.Unlock()
		return
	}
	if r.poisonRadius > 1 {
		r.poisonRadius--
		r.lastPoisonShrink = now
		r.wal.write(walPoisonShrink, fmt.Sprintf("radius=%d", r.poisonRadius))
		r.mu.Unlock()
		r.broadcastEvent("POISON", "")
		return
	}
	r.mu.Unlock()
}

// applyPoisonDamage mirrors game_apply_poison_damage (game.c:494-539): players
// outside the safe zone take PoisonDamage*Tick/1000 (>=1) per tick.
func (r *Room) applyPoisonDamage() {
	r.mu.Lock()
	radius := r.poisonRadius
	ids := make([]int, 0, r.playerCount)
	for i := 0; i < config.MaxRoomPlayers; i++ {
		if r.playerIDs[i] >= 0 {
			ids = append(ids, r.playerIDs[i])
		}
	}
	r.mu.Unlock()

	dmg := config.PoisonDamage * config.TickIntervalMS / 1000
	if dmg < 1 {
		dmg = 1
	}
	for _, id := range ids {
		p := r.srv.findPlayerByID(id)
		if p == nil {
			continue
		}
		p.mu.Lock()
		if p.status == StatusGaming && p.hp > 0 && mapIsInPoison(p.x, p.y, radius) {
			p.hp -= dmg
			if p.hp <= 0 {
				p.hp = 0
				p.status = StatusDead
				r.wal.write(walPlayerDeath, fmt.Sprintf("pid=%d,killer=-1", p.id))
			}
		}
		p.mu.Unlock()
	}
}

// checkEnd mirrors game_check_end (game.c:543-618) for the MVP (recovery-room
// handling is added in phase 3):
//
//	>=0 winner id, -1 draw/timeout, -2 continue.
func (r *Room) checkEnd() int {
	r.mu.Lock()
	now := nowMS()
	if now-r.gameStartTime >= config.GameMaxDuration {
		r.mu.Unlock()
		return -1
	}
	// Recovery wait: don't judge a winner until the expected players reconnect
	// or the wait window elapses (game.c:558-587).
	if r.isRecovery && r.expectedPlayers > 0 {
		if r.playerCount >= r.expectedPlayers {
			r.isRecovery = false
			r.expectedPlayers = 0
		} else if now-r.recoveryStart < config.RecoveryWaitTime {
			r.mu.Unlock()
			return -2
		} else {
			r.isRecovery = false
			r.expectedPlayers = 0
		}
	}
	ids := make([]int, 0, r.playerCount)
	for i := 0; i < config.MaxRoomPlayers; i++ {
		if r.playerIDs[i] >= 0 {
			ids = append(ids, r.playerIDs[i])
		}
	}
	r.mu.Unlock()

	alive := 0
	lastAlive := -1
	for _, id := range ids {
		p := r.srv.findPlayerByID(id)
		if p == nil {
			continue
		}
		p.mu.Lock()
		if p.status == StatusGaming && p.hp > 0 {
			alive++
			lastAlive = p.id
		}
		p.mu.Unlock()
	}
	if alive == 0 {
		return -1
	}
	if alive == 1 {
		return lastAlive
	}
	return -2
}

// broadcastState mirrors game_broadcast_state (game.c:622-688). Player entry:
// id,x,y,hp,atk,def,status,shield,inv0..inv4 (13 fields); item entry: x,y,type.
func (r *Room) broadcastState() {
	r.mu.Lock()
	timestamp := nowMS()
	poison := r.poisonRadius

	var players []string
	for i := 0; i < config.MaxRoomPlayers; i++ {
		id := r.playerIDs[i]
		if id < 0 {
			continue
		}
		p := r.srv.findPlayerByID(id)
		if p == nil {
			continue
		}
		p.mu.Lock()
		shield := 0
		if p.hasShield {
			shield = 1
		}
		players = append(players, fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d",
			p.id, p.x, p.y, p.hp, p.atk, p.def, int(p.status), shield,
			int(p.inventory[0]), int(p.inventory[1]), int(p.inventory[2]),
			int(p.inventory[3]), int(p.inventory[4])))
		p.mu.Unlock()
	}

	var items []string
	for i := 0; i < r.itemCount; i++ {
		if !r.items[i].active {
			continue
		}
		items = append(items, fmt.Sprintf("%d,%d,%d", r.items[i].x, r.items[i].y, int(r.items[i].typ)))
	}
	r.mu.Unlock()

	r.broadcast(protocol.BuildGameState(timestamp, strings.Join(players, ";"), strings.Join(items, ";"), poison))
}

// gameLoop is the per-room game goroutine, mirroring game_thread_func
// (game.c:700-762). It runs the tick pipeline every TickIntervalMS.
func (r *Room) gameLoop() {
	r.mu.Lock()
	r.running = true
	r.mu.Unlock()

	for {
		r.mu.Lock()
		running := r.running
		status := r.status
		ids := make([]int, 0, r.playerCount)
		for i := 0; i < config.MaxRoomPlayers; i++ {
			if r.playerIDs[i] >= 0 {
				ids = append(ids, r.playerIDs[i])
			}
		}
		r.mu.Unlock()
		if !running || status != RoomGaming {
			break
		}

		for _, id := range ids {
			if p := r.srv.findPlayerByID(id); p != nil {
				r.updateBuffs(p)
			}
		}
		r.updatePoison()
		r.applyPoisonDamage()
		r.spawnItems()
		if r.snapshotShouldSave() {
			r.snapshotSave()
		}
		if winner := r.checkEnd(); winner != -2 {
			r.endGame(winner)
			break
		}
		r.broadcastState()

		time.Sleep(config.TickIntervalMS * time.Millisecond)
	}
}

// endGame mirrors room_end_game (room.c:454-556): broadcast GAME_END, reset
// players to InRoom, reset the room to Waiting, close the WAL.
func (r *Room) endGame(winnerID int) {
	r.mu.Lock()
	r.status = RoomEnded
	r.running = false
	ids := make([]int, 0, r.playerCount)
	for i := 0; i < config.MaxRoomPlayers; i++ {
		if r.playerIDs[i] >= 0 {
			ids = append(ids, r.playerIDs[i])
		}
	}
	r.wal.write(walGameEnd, fmt.Sprintf("winner=%d", winnerID))
	r.wal.sync()
	r.mu.Unlock()

	r.broadcast(protocol.BuildGameEnd(winnerID, ""))

	for _, id := range ids {
		p := r.srv.findPlayerByID(id)
		if p == nil {
			continue
		}
		p.mu.Lock()
		p.status = StatusInRoom
		p.hp = p.maxHP
		p.x = 0
		p.y = 0
		p.hasShield = false
		p.atk = p.baseATK
		p.atkBuffExpire = 0
		p.atkBuffWarned = false
		p.inventoryCount = 0
		p.mu.Unlock()
	}

	r.mu.Lock()
	r.status = RoomWaiting
	for y := 0; y < config.MapHeight; y++ {
		for x := 0; x <= config.MapWidth; x++ {
			r.m[y][x] = 0
		}
	}
	r.itemCount = 0
	for i := range r.items {
		r.items[i].active = false
	}
	r.poisonRadius = mapInitialPoisonRadius()
	r.gameStartTime = 0
	r.lastItemSpawn = 0
	r.lastPoisonShrink = 0
	// Game ended normally: drop persistence so it is not treated as
	// recoverable on the next startup (room_end_game, room.c:531-553).
	walRoomID := r.id
	if r.originalRoomID >= 0 {
		walRoomID = r.originalRoomID
	}
	r.wal.close()
	r.wal = nil
	walDeleteForRoom(walRoomID)
	snapshotDelete(walRoomID)
	r.isRecovery = false
	r.expectedPlayers = 0
	r.recoveryStart = 0
	r.originalRoomID = -1
	r.lastSnapshotTime = 0
	r.mu.Unlock()
}
