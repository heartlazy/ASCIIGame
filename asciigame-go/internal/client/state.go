// Package client implements the ASCII Battle Royale terminal client (Go port of
// client/), rendering with tview/tcell instead of ncurses. It mirrors the state
// model in client/game.c: the client keeps a full local mirror of game state,
// updated from server frames, and holds the map template locally (the server
// only sends MAP_DATA on recovery, not during normal play).
package client

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

const maxMessages = 50

// attackEffectMS is how long the attack-range highlight stays on screen after
// an ATTACK event (mirrors ATTACK_EFFECT_DURATION_MS in client/game.h:16).
const attackEffectMS = 300

// nowMS returns the current time in milliseconds.
func nowMS() int64 { return time.Now().UnixMilli() }

// mapTemplate is the built-in map, identical to client/game.c:17-38 and the
// server's template.
var mapTemplate = [config.MapHeight]string{
	"##################################################",
	"#                    $                           #",
	"#   ##    ##         $         ##    ##    $     #",
	"#   ##    ##    $              ##    ##          #",
	"#              ###        ###              $     #",
	"#   $          # $        $ #          $         #",
	"#              ###        ###                    #",
	"#       $                          $       ##    #",
	"#   ##              ####              ##         #",
	"#   ##     $        #  #        $     ##    $    #",
	"#          $        #  #        $                #",
	"#   ##     $        ####        $     ##         #",
	"#   ##                                ##    $    #",
	"#       $                          $             #",
	"#              ###        ###              $     #",
	"#   $          # $        $ #          $         #",
	"#              ###        ###                    #",
	"#   ##    ##    $              ##    ##          #",
	"#   ##    ##         $         ##    ##    $     #",
	"##################################################",
}

type playerView struct {
	id, x, y, hp, status int
}

type itemView struct {
	x, y, typ int
}

type chatMessage struct {
	sender, text string
}

// State is the client-side mirror of game state, guarded by mu. Mirrors the C
// GameState struct (client/game.h:47-99).
type State struct {
	mu sync.Mutex

	loggedIn bool
	username string

	inRoom   bool
	roomID   int
	roomName string
	isReady  bool

	inGame                              bool
	myID                                int
	myX, myY, myHP, myMaxHP, myATK, myDEF int
	myHasShield                         bool
	inventory                           [config.MaxInventory]int
	inventoryCount                      int

	players     []playerView
	items       []itemView
	poisonRadius int

	// attackEffect: a transient highlight of the last attack's range, cleared
	// after attackEffectMS. Mirrors the C AttackEffect (client/game.h:39-45).
	attackActive   bool
	attackX        int
	attackY        int
	attackRadius   int
	attackExpireMS int64

	messages []chatMessage
}

// NewState returns an initialized client state.
func NewState() *State {
	return &State{myMaxHP: config.InitialHP, poisonRadius: 25, roomID: -1}
}

// snapshot is a lock-free copy of the render-relevant State fields.
type snapshot struct {
	username     string
	inRoom       bool
	roomID       int
	roomName     string
	isReady      bool
	inGame       bool
	myID         int
	myX, myY     int
	myHP, myMaxHP int
	myATK, myDEF int
	myHasShield  bool
	inventory    [config.MaxInventory]int
	poisonRadius int
	players      []playerView
	items        []itemView
	attackActive bool
	attackX      int
	attackY      int
	attackRadius int
	messages     []chatMessage
}

// Snapshot returns a consistent copy of state for rendering, taken under lock.
func (s *State) Snapshot() snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Expire the attack effect if its window has passed.
	attackActive := s.attackActive && nowMS() < s.attackExpireMS
	return snapshot{
		username: s.username, inRoom: s.inRoom, roomID: s.roomID, roomName: s.roomName,
		isReady: s.isReady, inGame: s.inGame, myID: s.myID, myX: s.myX, myY: s.myY,
		myHP: s.myHP, myMaxHP: s.myMaxHP, myATK: s.myATK, myDEF: s.myDEF,
		myHasShield: s.myHasShield, inventory: s.inventory, poisonRadius: s.poisonRadius,
		players:      append([]playerView(nil), s.players...),
		items:        append([]itemView(nil), s.items...),
		attackActive: attackActive, attackX: s.attackX, attackY: s.attackY, attackRadius: s.attackRadius,
		messages: append([]chatMessage(nil), s.messages...),
	}
}

func (s *State) addMessage(sender, text string) {
	s.messages = append(s.messages, chatMessage{sender, text})
	if len(s.messages) > maxMessages {
		s.messages = s.messages[len(s.messages)-maxMessages:]
	}
}

// Thread-safe accessors used by the UI and tests.

func (s *State) RoomID() int        { s.mu.Lock(); defer s.mu.Unlock(); return s.roomID }
func (s *State) InGame() bool       { s.mu.Lock(); defer s.mu.Unlock(); return s.inGame }
func (s *State) MyID() int          { s.mu.Lock(); defer s.mu.Unlock(); return s.myID }
func (s *State) PlayerCount() int   { s.mu.Lock(); defer s.mu.Unlock(); return len(s.players) }
func (s *State) MessageCount() int  { s.mu.Lock(); defer s.mu.Unlock(); return len(s.messages) }

// HasMessage reports whether any message contains substr in its text.
func (s *State) HasMessage(substr string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.messages {
		if strings.Contains(m.text, substr) {
			return true
		}
	}
	return false
}

// Update applies one server frame and mutates state, mirroring
// game_update_from_server (client/game.c:603-653). Returns true if the UI
// should redraw.
func (s *State) Update(f *protocol.Frame) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch m := f.Payload.(type) {
	case *protocol.Frame_Ok:
		s.handleOK(m.Ok)
	case *protocol.Frame_Error:
		s.handleErr(m.Error)
	case *protocol.Frame_RoomList:
		s.handleRoomList(m.RoomList)
	case *protocol.Frame_RoomInfo:
		s.handleRoomInfo(m.RoomInfo)
	case *protocol.Frame_PlayerJoin:
		if m.PlayerJoin != nil {
			s.addMessage("System", m.PlayerJoin.Username+" joined the room")
		}
	case *protocol.Frame_PlayerLeave:
		if m.PlayerLeave != nil {
			s.addMessage("System", "Player "+strconv.Itoa(int(m.PlayerLeave.PlayerId))+" left the room")
		}
	case *protocol.Frame_GameStart:
		s.handleGameStart()
	case *protocol.Frame_GameState:
		s.handleGameState(m.GameState)
	case *protocol.Frame_GameEvent:
		s.handleGameEvent(m.GameEvent)
	case *protocol.Frame_GameEnd:
		s.handleGameEnd(m.GameEnd)
	case *protocol.Frame_ChatMsg:
		if m.ChatMsg != nil {
			s.addMessage(m.ChatMsg.Sender, m.ChatMsg.Message)
		}
	}
	return true
}

func (s *State) handleOK(o *protocol.Ok) {
	if o == nil {
		return
	}
	m := o.Message
	if strings.Contains(m, "Login successful") && o.PlayerId > 0 {
		s.myID = int(o.PlayerId)
	}
	if strings.Contains(m, "Left room") {
		s.inRoom = false
		s.isReady = false
		s.inGame = false // leaving mid-game must return to the lobby view
		s.roomID = -1
		s.roomName = ""
		s.players = nil
		s.items = nil
		s.attackActive = false
	}
	switch m {
	case "Ready":
		s.isReady = true
	case "Not ready":
		s.isReady = false
	}
	s.addMessage("Server", m)
}

func (s *State) handleErr(e *protocol.Error) {
	if e == nil {
		return
	}
	msg := e.Message
	// Suppress spammy in-game errors, matching client/game.c:225-232.
	switch msg {
	case "Invalid move", "Move on cooldown", "Attack on cooldown":
		return
	}
	s.addMessage("Error", msg)
}

func (s *State) handleRoomList(rl *protocol.RoomList) {
	if rl == nil || len(rl.Rooms) == 0 {
		s.addMessage("System", "No rooms available. Press C to create one.")
		return
	}
	s.addMessage("System", "=== Room List ===")
	for _, r := range rl.Rooms {
		status := "Other"
		switch r.Status {
		case 0:
			status = "Waiting"
		case 2:
			status = "Gaming"
		}
		s.addMessage("", "  ID="+strconv.Itoa(int(r.RoomId))+"  name='"+r.Name+"'  ("+
			strconv.Itoa(int(r.PlayerCount))+"/"+strconv.Itoa(int(r.MaxPlayers))+")  "+status)
	}
	s.addMessage("System", "Press J and enter the ID number (not the name) to join")
}

func (s *State) handleRoomInfo(ri *protocol.RoomInfo) {
	if ri == nil {
		return
	}
	s.roomID = int(ri.RoomId)
	s.roomName = ri.Name
	s.inRoom = true
	s.addMessage("System", "Joined room "+ri.Name+" (ID: "+strconv.Itoa(int(ri.RoomId))+
		", Players: "+strconv.Itoa(int(ri.PlayerCount))+"/"+strconv.Itoa(int(ri.MaxPlayers))+")")
}

func (s *State) handleGameStart() {
	s.inGame = true
	s.myHP = s.myMaxHP
	s.myATK = config.InitialATK
	s.myDEF = config.InitialDEF
	s.addMessage("System", "Game started! WASD to move, J/Space to attack!")
}

// handleGameState mirrors parse_player_states / parse_item_states (client/game.c).
func (s *State) handleGameState(gs *protocol.GameState) {
	if gs == nil {
		return
	}
	s.players = s.players[:0]
	for _, p := range gs.Players {
		pv := playerView{id: int(p.Id), x: int(p.X), y: int(p.Y), hp: int(p.Hp), status: int(p.Status)}
		if s.myID == 0 {
			s.myID = pv.id
		}
		if pv.id == s.myID {
			s.myX, s.myY, s.myHP = pv.x, pv.y, pv.hp
			s.myATK = int(p.Atk)
			s.myDEF = int(p.Def)
			s.myHasShield = p.HasShield
			s.inventoryCount = 0
			for i := 0; i < config.MaxInventory; i++ {
				var t int32
				if i < len(p.Inventory) {
					t = p.Inventory[i]
				}
				s.inventory[i] = int(t)
				if s.inventory[i] > 0 {
					s.inventoryCount++
				}
			}
		}
		s.players = append(s.players, pv)
	}

	s.items = s.items[:0]
	for _, it := range gs.Items {
		s.items = append(s.items, itemView{x: int(it.X), y: int(it.Y), typ: int(it.Type)})
	}
	s.poisonRadius = int(gs.PoisonRadius)
}

// handleGameEvent surfaces combat/system events as messages, mirroring
// handle_game_event (client/game.c:403-537).
func (s *State) handleGameEvent(ge *protocol.GameEvent) {
	if ge == nil {
		return
	}
	switch ev := ge.Event.(type) {
	case *protocol.GameEvent_Attack:
		// Highlight the attacked area for a short time.
		s.attackActive = true
		s.attackX = int(ev.Attack.X)
		s.attackY = int(ev.Attack.Y)
		s.attackRadius = config.AttackRange
		s.attackExpireMS = nowMS() + attackEffectMS
	case *protocol.GameEvent_AttackResult:
		if int(ev.AttackResult.AttackerId) == s.myID {
			if ev.AttackResult.HitCount == 0 {
				s.addMessage("Combat", "Attack missed!")
			} else {
				s.addMessage("Combat", "Attack hit "+strconv.Itoa(int(ev.AttackResult.HitCount))+" target(s)!")
			}
		}
	case *protocol.GameEvent_Damage:
		attacker, victim := int(ev.Damage.AttackerId), int(ev.Damage.VictimId)
		dmg := strconv.Itoa(int(ev.Damage.Damage))
		hp := strconv.Itoa(int(ev.Damage.Hp))
		switch {
		case victim == s.myID:
			s.myHP = int(ev.Damage.Hp)
			s.addMessage("Combat", "You took "+dmg+" damage! HP: "+hp)
		case attacker == s.myID:
			s.addMessage("Combat", "You dealt "+dmg+" damage!")
		}
	case *protocol.GameEvent_Kill:
		killer, victim := int(ev.Kill.KillerId), int(ev.Kill.VictimId)
		switch {
		case victim == s.myID:
			s.addMessage("Combat", "You were killed by player "+strconv.Itoa(killer)+"!")
		case killer == s.myID:
			s.addMessage("Combat", "You killed player "+strconv.Itoa(victim)+"!")
		}
	case *protocol.GameEvent_Shield:
		if int(ev.Shield.DefenderId) == s.myID {
			s.addMessage("Combat", "Your shield blocked an attack!")
		} else if int(ev.Shield.AttackerId) == s.myID {
			s.addMessage("Combat", "Your attack was blocked by a shield!")
		}
	case *protocol.GameEvent_Poison:
		s.addMessage("System", "Poison zone shrinking!")
	case *protocol.GameEvent_Pickup:
		if int(ev.Pickup.PlayerId) == s.myID {
			s.addMessage("Item", "Picked up "+itemName(int(ev.Pickup.ItemType))+"!")
		}
	case *protocol.GameEvent_BuffWarning:
		if int(ev.BuffWarning.PlayerId) == s.myID {
			s.addMessage("Buff", "Attack buff expires in "+strconv.Itoa(int(ev.BuffWarning.Seconds))+" seconds!")
		}
	case *protocol.GameEvent_BuffExpired:
		if int(ev.BuffExpired.PlayerId) == s.myID {
			s.addMessage("Buff", "Attack buff has expired!")
		}
	}
}

func (s *State) handleGameEnd(g *protocol.GameEnd) {
	s.inGame = false
	s.isReady = false
	s.myHP = s.myMaxHP
	s.players = nil
	s.poisonRadius = 25
	if g == nil {
		return
	}
	winner := int(g.WinnerId)
	switch {
	case winner == s.myID:
		s.addMessage("System", "You win!")
	case winner < 0:
		s.addMessage("System", "Game ended - Draw!")
	default:
		s.addMessage("System", "Game ended - Player "+strconv.Itoa(winner)+" wins!")
	}
}

func itemName(t int) string {
	switch t {
	case 1:
		return "Health Pack"
	case 2:
		return "Attack Potion"
	case 3:
		return "Shield"
	default:
		return "Item"
	}
}
