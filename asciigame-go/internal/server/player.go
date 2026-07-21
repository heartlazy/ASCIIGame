package server

import (
	"net"
	"sync"
	"time"

	"github.com/heartlazyli/asciigame/internal/config"
)

// PlayerStatus mirrors the C PlayerStatus enum (player.h:14-22).
type PlayerStatus int

const (
	StatusDisconnected PlayerStatus = iota
	StatusConnected                 // connected, not logged in
	StatusLobby                     // logged in, in lobby
	StatusInRoom                    // in a room, not ready
	StatusReady                     // in a room, ready
	StatusGaming                    // in game
	StatusDead                      // dead in game
)

// ItemType mirrors the C ItemType/MapItemType enums (player.h:24-30, map.h:25-30).
type ItemType int

const (
	ItemNone ItemType = iota
	ItemHealth
	ItemAttack
	ItemShield
)

// nowMS returns the current time in milliseconds, matching player_get_time_ms.
func nowMS() int64 { return time.Now().UnixMilli() }

// Player holds a connected player's state. Fields are guarded by mu, mirroring
// the C Player.lock. Network writes are serialized through the out channel by a
// dedicated writer goroutine (started in Server.handleConn), so Send never
// blocks on socket I/O while a caller holds a lock.
type Player struct {
	conn net.Conn
	id   int

	mu       sync.Mutex
	username string
	roomID   int
	status   PlayerStatus

	x, y                        int
	hp, maxHP, atk, def, baseATK int

	lastMoveTime, lastAttackTime int64

	inventory     [config.MaxInventory]ItemType
	inventoryCount int
	hasShield     bool
	atkBuffExpire int64
	atkBuffWarned bool

	out  chan string
	done chan struct{}
	once sync.Once
}

func newPlayer(conn net.Conn, id int) *Player {
	return &Player{
		conn:    conn,
		id:      id,
		roomID:  -1,
		status:  StatusConnected,
		hp:      config.InitialHP,
		maxHP:   config.InitialHP,
		atk:     config.InitialATK,
		baseATK: config.InitialATK,
		def:     config.InitialDEF,
		out:     make(chan string, 256),
		done:    make(chan struct{}),
	}
}

// Send enqueues a raw frame (already terminated with '\n') for delivery. Safe
// for concurrent use; drops silently once the player is closed.
func (p *Player) Send(msg string) {
	select {
	case p.out <- msg:
	case <-p.done:
	}
}

// closeOnce signals the writer goroutine to stop. Idempotent.
func (p *Player) closeOnce() {
	p.once.Do(func() { close(p.done) })
}

// resetGameState mirrors player_reset_game_state (player.c:208-234).
func (p *Player) resetGameState() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hp = config.InitialHP
	p.maxHP = config.InitialHP
	p.atk = config.InitialATK
	p.baseATK = config.InitialATK
	p.def = config.InitialDEF
	p.lastMoveTime = 0
	p.lastAttackTime = 0
	p.inventoryCount = 0
	for i := range p.inventory {
		p.inventory[i] = ItemNone
	}
	p.hasShield = false
	p.atkBuffExpire = 0
	p.atkBuffWarned = false
}

// addItem mirrors player_add_item (player.c:267-284); returns false if full.
func (p *Player) addItem(t ItemType) bool {
	if t == ItemNone {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inventoryCount >= config.MaxInventory {
		return false
	}
	p.inventory[p.inventoryCount] = t
	p.inventoryCount++
	return true
}

// useItem mirrors player_use_item (player.c:286-309); removes and returns the
// item at index, shifting the rest down. Returns ItemNone on invalid index.
func (p *Player) useItem(index int) ItemType {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= p.inventoryCount {
		return ItemNone
	}
	t := p.inventory[index]
	for i := index; i < p.inventoryCount-1; i++ {
		p.inventory[i] = p.inventory[i+1]
	}
	p.inventoryCount--
	p.inventory[p.inventoryCount] = ItemNone
	return t
}

// Locked accessors for fields the game loop mutates concurrently, so handler
// goroutines read them race-free (the C code read these without locks).

func (p *Player) getStatus() PlayerStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *Player) getRoomID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.roomID
}

func (p *Player) getUsername() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.username
}

func (p *Player) setUsername(u string) {
	p.mu.Lock()
	p.username = truncate(u, config.MaxUsername-1)
	p.mu.Unlock()
}
