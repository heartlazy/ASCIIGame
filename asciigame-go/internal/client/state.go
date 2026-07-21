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

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

const maxMessages = 50

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
	messages     []chatMessage
}

// Snapshot returns a consistent copy of state for rendering, taken under lock.
func (s *State) Snapshot() snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshot{
		username: s.username, inRoom: s.inRoom, roomID: s.roomID, roomName: s.roomName,
		isReady: s.isReady, inGame: s.inGame, myID: s.myID, myX: s.myX, myY: s.myY,
		myHP: s.myHP, myMaxHP: s.myMaxHP, myATK: s.myATK, myDEF: s.myDEF,
		myHasShield: s.myHasShield, inventory: s.inventory, poisonRadius: s.poisonRadius,
		players:  append([]playerView(nil), s.players...),
		items:    append([]itemView(nil), s.items...),
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

func (s *State) RoomID() int      { s.mu.Lock(); defer s.mu.Unlock(); return s.roomID }
func (s *State) InGame() bool     { s.mu.Lock(); defer s.mu.Unlock(); return s.inGame }
func (s *State) MyID() int        { s.mu.Lock(); defer s.mu.Unlock(); return s.myID }
func (s *State) PlayerCount() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.players) }

// Update parses one server frame and mutates state, mirroring
// game_update_from_server (client/game.c:603-653). It returns true if the UI
// should redraw.
func (s *State) Update(raw string) bool {
	msg, err := protocol.Parse(raw)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.Type {
	case protocol.CmdOK:
		s.handleOK(msg)
	case protocol.CmdErr:
		s.handleErr(msg)
	case protocol.CmdRoomList:
		s.handleRoomList(msg)
	case protocol.CmdRoomInfo:
		s.handleRoomInfo(msg)
	case protocol.CmdPlayerJoin:
		if len(msg.Args) >= 2 {
			s.addMessage("System", msg.Args[1]+" joined the room")
		}
	case protocol.CmdPlayerLeave:
		if len(msg.Args) >= 1 {
			s.addMessage("System", "Player "+msg.Args[0]+" left the room")
		}
	case protocol.CmdGameStart:
		s.handleGameStart()
	case protocol.CmdGameState:
		s.handleGameState(msg)
	case protocol.CmdGameEvent:
		s.handleGameEvent(msg)
	case protocol.CmdGameEnd:
		s.handleGameEnd(msg)
	case protocol.CmdChatMsg:
		if len(msg.Args) >= 2 {
			s.addMessage(msg.Args[0], msg.Args[1])
		}
	}
	return true
}

func (s *State) handleOK(msg protocol.Message) {
	if len(msg.Args) == 0 {
		return
	}
	m := msg.Args[0]
	if strings.Contains(m, "Login successful") && len(msg.Args) > 1 {
		s.myID = atoi(msg.Args[1])
	}
	if strings.Contains(m, "Left room") {
		s.inRoom = false
		s.isReady = false
		s.roomID = -1
		s.roomName = ""
	}
	switch m {
	case "Ready":
		s.isReady = true
	case "Not ready":
		s.isReady = false
	}
	s.addMessage("Server", m)
}

func (s *State) handleErr(msg protocol.Message) {
	if len(msg.Args) < 2 {
		return
	}
	e := msg.Args[1]
	// Suppress spammy in-game errors, matching client/game.c:225-232.
	switch e {
	case "Invalid move", "Move on cooldown", "Attack on cooldown":
		return
	}
	s.addMessage("Error", e)
}

func (s *State) handleRoomList(msg protocol.Message) {
	if len(msg.Args) < 1 || msg.Args[0] == "" {
		s.addMessage("System", "No rooms available. Press C to create one.")
		return
	}
	s.addMessage("System", "=== Room List ===")
	for _, entry := range strings.Split(msg.Args[0], ";") {
		f := strings.Split(entry, ",")
		if len(f) < 5 {
			continue
		}
		status := "Other"
		switch f[4] {
		case "0":
			status = "Waiting"
		case "2":
			status = "Gaming"
		}
		s.addMessage("", "  ["+f[0]+"] "+f[1]+" ("+f[2]+"/"+f[3]+") - "+status)
	}
	s.addMessage("System", "Press J to join a room by ID")
}

func (s *State) handleRoomInfo(msg protocol.Message) {
	if len(msg.Args) < 5 {
		return
	}
	s.roomID = atoi(msg.Args[0])
	s.roomName = msg.Args[1]
	s.inRoom = true
	s.addMessage("System", "Joined room "+msg.Args[1]+" (ID: "+msg.Args[0]+", Players: "+msg.Args[2]+"/"+msg.Args[3]+")")
}

func (s *State) handleGameStart() {
	s.inGame = true
	s.myHP = s.myMaxHP
	s.myATK = config.InitialATK
	s.myDEF = config.InitialDEF
	s.addMessage("System", "Game started! WASD to move, J/Space to attack!")
}

// handleGameState parses GAME_STATE, mirroring parse_player_states /
// parse_item_states (client/game.c). Player entries are 13 comma fields.
func (s *State) handleGameState(msg protocol.Message) {
	if len(msg.Args) < 4 {
		return
	}
	s.players = s.players[:0]
	for _, tok := range strings.Split(msg.Args[1], ";") {
		if tok == "" {
			continue
		}
		f := strings.Split(tok, ",")
		if len(f) < 4 {
			continue
		}
		pv := playerView{id: atoi(f[0]), x: atoi(f[1]), y: atoi(f[2]), hp: atoi(f[3])}
		if len(f) >= 7 {
			pv.status = atoi(f[6])
		}
		if s.myID == 0 {
			s.myID = pv.id
		}
		if pv.id == s.myID {
			s.myX, s.myY, s.myHP = pv.x, pv.y, pv.hp
			if len(f) >= 13 {
				s.myATK = atoi(f[4])
				s.myDEF = atoi(f[5])
				s.myHasShield = f[7] == "1"
				s.inventoryCount = 0
				for i := 0; i < config.MaxInventory; i++ {
					s.inventory[i] = atoi(f[8+i])
					if s.inventory[i] > 0 {
						s.inventoryCount++
					}
				}
			}
		}
		s.players = append(s.players, pv)
	}

	s.items = s.items[:0]
	for _, tok := range strings.Split(msg.Args[2], ";") {
		if tok == "" {
			continue
		}
		f := strings.Split(tok, ",")
		if len(f) < 3 {
			continue
		}
		s.items = append(s.items, itemView{x: atoi(f[0]), y: atoi(f[1]), typ: atoi(f[2])})
	}

	s.poisonRadius = atoi(msg.Args[3])
}

// handleGameEvent surfaces combat/system events as messages, mirroring
// handle_game_event (client/game.c:403-537).
func (s *State) handleGameEvent(msg protocol.Message) {
	if len(msg.Args) < 1 {
		return
	}
	switch msg.Args[0] {
	case "ATTACK_RESULT":
		if len(msg.Args) >= 3 && atoi(msg.Args[1]) == s.myID {
			if atoi(msg.Args[2]) == 0 {
				s.addMessage("Combat", "Attack missed!")
			} else {
				s.addMessage("Combat", "Attack hit "+msg.Args[2]+" target(s)!")
			}
		}
	case "DAMAGE":
		if len(msg.Args) >= 5 {
			attacker, victim := atoi(msg.Args[1]), atoi(msg.Args[2])
			if victim == s.myID {
				s.myHP = atoi(msg.Args[4])
				s.addMessage("Combat", "You took "+msg.Args[3]+" damage! HP: "+msg.Args[4])
			} else if attacker == s.myID {
				s.addMessage("Combat", "You dealt "+msg.Args[3]+" damage!")
			}
		}
	case "KILL":
		if len(msg.Args) >= 3 {
			killer, victim := atoi(msg.Args[1]), atoi(msg.Args[2])
			switch {
			case victim == s.myID:
				s.addMessage("Combat", "You were killed by player "+msg.Args[1]+"!")
			case killer == s.myID:
				s.addMessage("Combat", "You killed player "+msg.Args[2]+"!")
			}
		}
	case "SHIELD":
		if len(msg.Args) >= 3 {
			if atoi(msg.Args[2]) == s.myID {
				s.addMessage("Combat", "Your shield blocked an attack!")
			} else if atoi(msg.Args[1]) == s.myID {
				s.addMessage("Combat", "Your attack was blocked by a shield!")
			}
		}
	case "POISON":
		s.addMessage("System", "Poison zone shrinking!")
	case "PICKUP":
		if len(msg.Args) >= 3 && atoi(msg.Args[1]) == s.myID {
			s.addMessage("Item", "Picked up "+itemName(atoi(msg.Args[2]))+"!")
		}
	case "BUFF_WARNING":
		if len(msg.Args) >= 3 && atoi(msg.Args[1]) == s.myID {
			s.addMessage("Buff", "Attack buff expires in "+msg.Args[2]+" seconds!")
		}
	case "BUFF_EXPIRED":
		if len(msg.Args) >= 2 && atoi(msg.Args[1]) == s.myID {
			s.addMessage("Buff", "Attack buff has expired!")
		}
	}
}

func (s *State) handleGameEnd(msg protocol.Message) {
	s.inGame = false
	s.isReady = false
	s.myHP = s.myMaxHP
	s.players = nil
	s.poisonRadius = 25
	if len(msg.Args) > 0 {
		winner := atoi(msg.Args[0])
		switch {
		case winner == s.myID:
			s.addMessage("System", "You win!")
		case winner < 0:
			s.addMessage("System", "Game ended - Draw!")
		default:
			s.addMessage("System", "Game ended - Player "+msg.Args[0]+" wins!")
		}
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

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
