package server

import (
	"fmt"
	"log"
	"strings"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

// sendOK / sendErr mirror the static helpers in handler.c:20-31.
func (s *Server) sendOK(p *Player, message string) { p.Send(protocol.NewOk(message, 0)) }
func (s *Server) sendErr(p *Player, code int, message string) {
	p.Send(protocol.NewError(int32(code), message))
}

// handle dispatches a frame, mirroring handler_process (handler.c:33-70).
func (s *Server) handle(p *Player, f *protocol.Frame) {
	// Log every command except the high-frequency in-game ones.
	switch f.Payload.(type) {
	case *protocol.Frame_Move, *protocol.Frame_Attack, *protocol.Frame_UseItem:
		// skip: fire many times per second during play
	default:
		log.Printf("player %s cmd=%s", p.label(), payloadName(f))
	}

	switch m := f.Payload.(type) {
	case *protocol.Frame_Login:
		s.handleLogin(p, m.Login)
	case *protocol.Frame_Register:
		s.handleRegister(p, m.Register)
	case *protocol.Frame_ListRooms:
		s.handleListRooms(p)
	case *protocol.Frame_CreateRoom:
		s.handleCreateRoom(p, m.CreateRoom)
	case *protocol.Frame_JoinRoom:
		s.handleJoinRoom(p, m.JoinRoom)
	case *protocol.Frame_LeaveRoom:
		s.handleLeaveRoom(p)
	case *protocol.Frame_Ready:
		s.handleReady(p)
	case *protocol.Frame_Move:
		s.handleMoveCmd(p, m.Move)
	case *protocol.Frame_Attack:
		s.handleAttackCmd(p)
	case *protocol.Frame_UseItem:
		s.handleUseItemCmd(p, m.UseItem)
	case *protocol.Frame_Chat:
		s.handleChat(p, m.Chat)
	case *protocol.Frame_Logout:
		s.handleLogout(p)
	default:
		s.sendErr(p, config.ErrUnknownCommand, "Unknown command")
	}
}

// payloadName returns the oneof payload type name for logging.
func payloadName(f *protocol.Frame) string {
	if f.Payload == nil {
		return "unknown"
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", f.Payload), "*protocol.Frame_")
}

// handleLogin mirrors handler_login (handler.c:72-177), including recovery
// reconnection (rejoining an in-progress game after a crash).
func (s *Server) handleLogin(p *Player, m *protocol.Login) {
	if m == nil || m.Username == "" || m.Password == "" {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: LOGIN username password")
		return
	}
	username, password := m.Username, m.Password

	if p.getStatus() != StatusConnected {
		s.sendErr(p, config.ErrUserLoggedIn, "Already logged in")
		return
	}
	if s.findPlayerByUsername(username) != nil {
		s.sendErr(p, config.ErrUserLoggedIn, "User already online")
		return
	}
	switch s.store.verify(username, password) {
	case -1:
		s.sendErr(p, config.ErrInvalidCredentials, "User not found")
		return
	case -2:
		s.sendErr(p, config.ErrInvalidCredentials, "Invalid password")
		return
	}

	p.setUsername(username)
	p.mu.Lock()
	p.status = StatusLobby
	p.mu.Unlock()

	// Crash recovery: if this user has an unfinished game, rejoin it instead of
	// entering the lobby (handler_login recovery branch, handler.c:112-169).
	if origID, ok := s.checkRecovery(username); ok {
		if room := s.restorePlayerToGame(p, origID); room != nil {
			s.sendRecoveryRejoin(p, room)
			return
		}
	}

	p.Send(protocol.NewOk("Login successful", int32(p.id)))
}

// handleRegister mirrors handler_register (handler.c:179-208).
func (s *Server) handleRegister(p *Player, m *protocol.Register) {
	if m == nil || m.Username == "" || m.Password == "" {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: REGISTER username password")
		return
	}
	username, password := m.Username, m.Password
	if len(username) < 1 || len(username) >= config.MaxUsername {
		s.sendErr(p, config.ErrInvalidArgFormat, "Invalid username length")
		return
	}
	switch s.store.register(username, password) {
	case -1:
		s.sendErr(p, config.ErrUsernameExists, "Username already exists")
		return
	case 0:
		s.sendOK(p, "Registration successful")
	default:
		s.sendErr(p, config.ErrInvalidFormat, "Registration failed")
	}
}

// handleListRooms mirrors handler_list_rooms (handler.c:210-223).
func (s *Server) handleListRooms(p *Player) {
	if p.getStatus() < StatusLobby {
		s.sendErr(p, config.ErrInvalidFormat, "Not logged in")
		return
	}
	list := s.getRoomList()
	s.rmu.RLock()
	n := len(s.rooms)
	s.rmu.RUnlock()
	log.Printf("player %s listed rooms: %d room(s)", p.label(), n)
	p.Send(list)
}

// handleCreateRoom mirrors handler_create_room (handler.c:225-266).
func (s *Server) handleCreateRoom(p *Player, m *protocol.CreateRoom) {
	if m == nil || m.Name == "" {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: CREATE_ROOM name max_players")
		return
	}
	if p.getStatus() != StatusLobby {
		s.sendErr(p, config.ErrInvalidFormat, "Must be in lobby")
		return
	}
	maxPlayers := int(m.MaxPlayers)
	if maxPlayers < config.MinRoomPlayers || maxPlayers > config.MaxRoomPlayers {
		s.sendErr(p, config.ErrInvalidArgFormat, "Invalid max players")
		return
	}
	room := s.createRoom(m.Name, maxPlayers)
	if room == nil {
		s.sendErr(p, config.ErrInvalidFormat, "Failed to create room")
		return
	}
	if room.addPlayer(p) < 0 {
		s.destroyRoom(room)
		s.sendErr(p, config.ErrInvalidFormat, "Failed to join room")
		return
	}
	room.mu.Lock()
	info := protocol.NewRoomInfo(int32(room.id), room.name, int32(room.playerCount), int32(room.maxPlayers), int32(room.status))
	room.mu.Unlock()
	p.Send(info)
}

// handleJoinRoom mirrors handler_join_room (handler.c:268-305).
func (s *Server) handleJoinRoom(p *Player, m *protocol.JoinRoom) {
	if m == nil {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: JOIN_ROOM room_id")
		return
	}
	if p.getStatus() != StatusLobby {
		s.sendErr(p, config.ErrInvalidFormat, "Must be in lobby")
		return
	}
	room := s.findRoomByID(int(m.RoomId))
	if room == nil {
		log.Printf("player %s join failed: room %d not found", p.label(), m.RoomId)
		s.sendErr(p, config.ErrRoomNotFound, "Room not found")
		return
	}
	switch room.addPlayer(p) {
	case -1:
		log.Printf("player %s join room %d failed: full", p.label(), room.id)
		s.sendErr(p, config.ErrRoomFull, "Room is full")
		return
	case -2:
		log.Printf("player %s join room %d failed: game in progress", p.label(), room.id)
		s.sendErr(p, config.ErrGameInProgress, "Game in progress")
		return
	}
	room.mu.Lock()
	info := protocol.NewRoomInfo(int32(room.id), room.name, int32(room.playerCount), int32(room.maxPlayers), int32(room.status))
	room.mu.Unlock()
	p.Send(info)
}

// handleLeaveRoom mirrors handler_leave_room (handler.c:307-327).
func (s *Server) handleLeaveRoom(p *Player) {
	if p.getStatus() < StatusInRoom {
		s.sendErr(p, config.ErrNotInRoom, "Not in a room")
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		s.sendErr(p, config.ErrRoomNotFound, "Room not found")
		return
	}
	room.removePlayer(p)
	s.sendOK(p, "Left room")
}

// handleReady mirrors handler_ready (handler.c:329-371): toggle ready, and if
// everyone is ready, create the WAL, start the game, and launch the game loop.
func (s *Server) handleReady(p *Player) {
	st := p.getStatus()
	if st != StatusInRoom && st != StatusReady {
		s.sendErr(p, config.ErrNotInRoom, "Not in a room")
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		s.sendErr(p, config.ErrRoomNotFound, "Room not found")
		return
	}
	p.mu.Lock()
	if p.status == StatusInRoom {
		p.status = StatusReady
	} else {
		p.status = StatusInRoom
	}
	newStatus := p.status
	p.mu.Unlock()

	if newStatus == StatusReady {
		s.sendOK(p, "Ready")
	} else {
		s.sendOK(p, "Not ready")
	}

	if room.allReady() {
		room.mu.Lock()
		room.wal = newWAL(room.id)
		room.mu.Unlock()
		room.startGame()
		go room.gameLoop()
	}
}

// handleMoveCmd mirrors handler_move (handler.c:373-402).
func (s *Server) handleMoveCmd(p *Player, m *protocol.Move) {
	if m == nil || m.Direction == "" {
		s.sendErr(p, config.ErrInvalidMove, "Invalid move")
		return
	}
	if p.getStatus() != StatusGaming {
		s.sendErr(p, config.ErrInvalidFormat, "Not in game")
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		s.sendErr(p, config.ErrRoomNotFound, "Room not found")
		return
	}
	switch room.handleMove(p, m.Direction[0]) {
	case -1:
		s.sendErr(p, config.ErrMoveCooldown, "Move on cooldown")
	case -2:
		s.sendErr(p, config.ErrInvalidMove, "Invalid move")
	}
}

// handleAttackCmd mirrors handler_attack (handler.c:404-426).
func (s *Server) handleAttackCmd(p *Player) {
	if p.getStatus() != StatusGaming {
		s.sendErr(p, config.ErrInvalidFormat, "Not in game")
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		s.sendErr(p, config.ErrRoomNotFound, "Room not found")
		return
	}
	if room.handleAttack(p) == -1 {
		s.sendErr(p, config.ErrAttackCooldown, "Attack on cooldown")
	}
}

// handleUseItemCmd mirrors handler_use_item (handler.c:428-455).
func (s *Server) handleUseItemCmd(p *Player, m *protocol.UseItem) {
	if m == nil {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: USE_ITEM index")
		return
	}
	if p.getStatus() != StatusGaming {
		s.sendErr(p, config.ErrInvalidFormat, "Not in game")
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		s.sendErr(p, config.ErrRoomNotFound, "Room not found")
		return
	}
	if room.handleUseItem(p, int(m.Index)) == -1 {
		s.sendErr(p, config.ErrInvalidItemIndex, "Invalid item index")
		return
	}
	s.sendOK(p, "Item used")
}

// handleChat mirrors handler_chat (handler.c:457-498): ignore empty/whitespace,
// require a room, then broadcast.
func (s *Server) handleChat(p *Player, m *protocol.Chat) {
	if m == nil {
		return
	}
	message := m.Message
	if strings.TrimSpace(message) == "" {
		return
	}
	if p.getRoomID() < 0 {
		s.sendErr(p, config.ErrNotInRoom, "Not in a room")
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		s.sendErr(p, config.ErrRoomNotFound, "Room not found")
		return
	}
	// Include the sender's player id so recipients can tell players apart.
	sender := fmt.Sprintf("%s#%d", p.getUsername(), p.id)
	room.broadcast(protocol.NewChatMsg(sender, message))
}

// handleLogout mirrors handler_logout (handler.c:500-521).
func (s *Server) handleLogout(p *Player) {
	if rid := p.getRoomID(); rid >= 0 {
		if room := s.findRoomByID(rid); room != nil {
			room.removePlayer(p)
		}
	}
	p.mu.Lock()
	p.username = ""
	p.status = StatusConnected
	p.mu.Unlock()
	s.sendOK(p, "Logged out")
}
