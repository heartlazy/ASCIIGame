package server

import (
	"bytes"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

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

// updateBuffs mirrors game_update_buffs (game.c:26-75).
func (r *Room) updateBuffs(p *Player) {
	now := nowMS()
	p.mu.Lock()
	if p.atkBuffExpire > 0 {
		remaining := p.atkBuffExpire - now
		if remaining <= 0 {
			p.atk = p.baseATK
			p.atkBuffExpire = 0
			p.atkBuffWarned = false
			id := int32(p.id)
			p.mu.Unlock()
			r.broadcast(protocol.NewBuffExpiredEvent(id))
			return
		} else if remaining <= 5000 && !p.atkBuffWarned {
			p.atkBuffWarned = true
			seconds := int32(remaining / 1000)
			id := int32(p.id)
			p.mu.Unlock()
			r.broadcast(protocol.NewBuffWarningEvent(id, seconds))
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
	r.broadcast(protocol.NewAttackEvent(int32(attackerID), int32(atkX), int32(atkY)))

	hitCount := 0
	var events []*protocol.Frame

	r.mu.Lock()
	for i := 0; i < config.MaxRoomPlayers; i++ {
		target := r.members[i]
		if target == nil || target.id == attackerID {
			continue
		}
		target.mu.Lock()
		dist := mapDistance(atkX, atkY, target.x, target.y)
		if dist <= config.AttackRange && target.status == StatusGaming && target.hp > 0 {
			if target.hasShield {
				target.hasShield = false
				r.wal.write(walDamage, fmt.Sprintf("atk=%d,vic=%d,dmg=0,hp=%d,shield_broken=1", attackerID, target.id, target.hp))
				events = append(events, protocol.NewShieldEvent(int32(attackerID), int32(target.id)))
			} else {
				dmg := calcDamage(atkPower, target.def)
				target.hp -= dmg
				hitCount++
				r.wal.write(walDamage, fmt.Sprintf("atk=%d,vic=%d,dmg=%d,hp=%d", attackerID, target.id, dmg, target.hp))
				events = append(events, protocol.NewDamageEvent(int32(attackerID), int32(target.id), int32(dmg), int32(target.hp)))
				if target.hp <= 0 {
					target.hp = 0
					target.status = StatusDead
					r.wal.write(walPlayerDeath, fmt.Sprintf("pid=%d,killer=%d", target.id, attackerID))
					events = append(events, protocol.NewKillEvent(int32(attackerID), int32(target.id)))
				}
			}
		}
		target.mu.Unlock()
	}
	r.mu.Unlock()

	for _, e := range events {
		r.broadcast(e)
	}
	r.broadcast(protocol.NewAttackResultEvent(int32(attackerID), int32(hitCount)))
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
	r.broadcast(protocol.NewPickupEvent(int32(p.id), int32(picked)))
}

// handleUseItem mirrors game_handle_use_item (game.c:345-392):
//
//	0 ok, -1 invalid index.
func (r *Room) handleUseItem(p *Player, index int) int {
	t := p.useItem(index)
	if t == ItemNone {
		return -1
	}

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

	// WAL: include atk_buff_remain so recovery can restore the expiry.
	buffRemain := int64(0)
	if t == ItemAttack {
		buffRemain = config.AtkBuffDuration
	}
	r.wal.write(walUseItem, fmt.Sprintf("pid=%d,item=%d,idx=%d,atk_buff_remain=%d", p.id, int(t), index, buffRemain))
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
	r.items[slot] = mapItem{x: x, y: y, typ: randItemType(), active: true, spawnTime: now}
	if slot >= r.itemCount {
		r.itemCount = slot + 1
	}
	r.wal.write(walItemSpawn, fmt.Sprintf("type=%d,x=%d,y=%d", int(r.items[slot].typ), x, y))
}

// expireItems removes items that have been on the map longer than
// ItemExpireTime without being picked up.
func (r *Room) expireItems() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := nowMS()
	for i := 0; i < r.itemCount; i++ {
		if r.items[i].active && now-r.items[i].spawnTime >= config.ItemExpireTime {
			r.items[i].active = false
		}
	}
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
		r.broadcast(protocol.NewPoisonEvent())
		return
	}
	r.mu.Unlock()
}

// applyPoisonDamage mirrors game_apply_poison_damage (game.c:494-539): players
// outside the safe zone take PoisonDamage*Tick/1000 (>=1) per tick.
func (r *Room) applyPoisonDamage() {
	r.mu.Lock()
	radius := r.poisonRadius
	members := r.membersLocked()
	r.mu.Unlock()

	dmg := config.PoisonDamage * config.TickIntervalMS / 1000
	if dmg < 1 {
		dmg = 1
	}
	for _, p := range members {
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

// checkEnd mirrors game_check_end (game.c:543-618), including recovery-room
// wait handling:
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
	members := r.membersLocked()
	r.mu.Unlock()

	alive := 0
	lastAlive := -1
	for _, p := range members {
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

// broadcastState mirrors game_broadcast_state (game.c:622-688), sending one
// GameState frame per tick. Dirty detection (layer-1 optimization): if the
// observable state (players/items/poison, excluding the ever-changing
// timestamp) is byte-identical to the last broadcast, the frame is skipped —
// so idle periods don't flood clients at 20 Hz. Any real change (move, damage,
// pickup, poison shrink, join/leave) alters the signature and sends.
func (r *Room) broadcastState() {
	r.mu.Lock()
	timestamp := nowMS()
	poison := r.poisonRadius

	players := make([]*protocol.PlayerState, 0, r.playerCount)
	for i := 0; i < config.MaxRoomPlayers; i++ {
		p := r.members[i]
		if p == nil {
			continue
		}
		p.mu.Lock()
		inv := make([]int32, config.MaxInventory)
		for j := 0; j < config.MaxInventory; j++ {
			inv[j] = int32(p.inventory[j])
		}
		players = append(players, &protocol.PlayerState{
			Id: int32(p.id), X: int32(p.x), Y: int32(p.y), Hp: int32(p.hp),
			Atk: int32(p.atk), Def: int32(p.def), Status: int32(p.status),
			HasShield: p.hasShield, Inventory: inv,
		})
		p.mu.Unlock()
	}

	items := make([]*protocol.ItemState, 0, r.itemCount)
	for i := 0; i < r.itemCount; i++ {
		if !r.items[i].active {
			continue
		}
		items = append(items, &protocol.ItemState{
			X: int32(r.items[i].x), Y: int32(r.items[i].y), Type: int32(r.items[i].typ),
		})
	}
	r.mu.Unlock()

	// Signature excludes timestamp so an idle room produces a stable sig.
	sig, err := proto.Marshal(&protocol.GameState{
		Players: players, Items: items, PoisonRadius: int32(poison),
	})
	if err == nil {
		if r.lastStateSig != nil && bytes.Equal(sig, r.lastStateSig) {
			return // nothing observable changed since the last broadcast
		}
		r.lastStateSig = sig
	}

	r.broadcast(protocol.NewGameState(timestamp, players, items, int32(poison)))
}

// gameLoop is the per-room game goroutine, mirroring game_thread_func
// (game.c:700-762). It runs the tick pipeline every TickIntervalMS.
func (r *Room) gameLoop() {
	r.mu.Lock()
	r.running = true
	r.mu.Unlock()

	// A Ticker keeps a steady 20 tick/s cadence: unlike Sleep(50ms), the period
	// does not drift by the per-tick processing time, and ticks missed under
	// load are dropped rather than piling up.
	ticker := time.NewTicker(config.TickIntervalMS * time.Millisecond)
	defer ticker.Stop()

	for {
		r.mu.Lock()
		running := r.running
		status := r.status
		members := r.membersLocked()
		r.mu.Unlock()
		if !running || status != RoomGaming {
			break
		}

		for _, p := range members {
			r.updateBuffs(p)
		}
		r.updatePoison()
		r.applyPoisonDamage()
		r.spawnItems()
		r.expireItems()
		if r.snapshotShouldSave() {
			r.snapshotSave()
		}
		if winner := r.checkEnd(); winner != -2 {
			r.endGame(winner)
			break
		}
		r.broadcastState()

		<-ticker.C
	}
}

// endGame mirrors room_end_game (room.c:454-556): broadcast GAME_END, reset
// players to InRoom, reset the room to Waiting, close the WAL.
func (r *Room) endGame(winnerID int) {
	r.mu.Lock()
	r.status = RoomEnded
	r.running = false
	members := r.membersLocked()
	r.wal.write(walGameEnd, fmt.Sprintf("winner=%d", winnerID))
	r.wal.sync()
	r.mu.Unlock()

	r.broadcast(protocol.NewGameEnd(int32(winnerID), ""))

	for _, p := range members {
		p.mu.Lock()
		username := p.username
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

		// Record win/loss stats (storage_update_stats). winnerID == -1 is a
		// draw/timeout, so everyone takes a loss.
		if username != "" {
			r.srv.store.updateStats(username, p.id == winnerID)
		}
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
	r.lastStateSig = nil
	r.mu.Unlock()
}
