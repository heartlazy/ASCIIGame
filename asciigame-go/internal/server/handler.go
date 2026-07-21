package server

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

// sendOK / sendErr mirror the static helpers in handler.c:20-31.
func (s *Server) sendOK(p *Player, message string) { p.Send(protocol.BuildOK(message)) }
func (s *Server) sendErr(p *Player, code int, message string) {
	p.Send(protocol.BuildErr(code, message))
}

// handle dispatches a parsed message, mirroring handler_process (handler.c:33-70).
func (s *Server) handle(p *Player, msg protocol.Message) {
	switch msg.Type {
	case protocol.CmdLogin:
		s.handleLogin(p, msg)
	case protocol.CmdRegister:
		s.handleRegister(p, msg)
	case protocol.CmdListRooms:
		s.handleListRooms(p, msg)
	case protocol.CmdCreateRoom:
		s.handleCreateRoom(p, msg)
	case protocol.CmdJoinRoom:
		s.handleJoinRoom(p, msg)
	case protocol.CmdLeaveRoom:
		s.handleLeaveRoom(p, msg)
	case protocol.CmdReady:
		s.handleReady(p, msg)
	case protocol.CmdMove:
		s.handleMoveCmd(p, msg)
	case protocol.CmdAttack:
		s.handleAttackCmd(p, msg)
	case protocol.CmdUseItem:
		s.handleUseItemCmd(p, msg)
	case protocol.CmdChat:
		s.handleChat(p, msg)
	case protocol.CmdLogout:
		s.handleLogout(p, msg)
	default:
		s.sendErr(p, config.ErrUnknownCommand, "Unknown command")
	}
}

// handleLogin mirrors handler_login (handler.c:72-177). Recovery reconnection
// is added in phase 3.
func (s *Server) handleLogin(p *Player, msg protocol.Message) {
	if len(msg.Args) < 2 {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: LOGIN|username|password")
		return
	}
	username, password := msg.Args[0], msg.Args[1]

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

	s.sendOK(p, fmt.Sprintf("Login successful|%d", p.id))
}

// handleRegister mirrors handler_register (handler.c:179-208).
func (s *Server) handleRegister(p *Player, msg protocol.Message) {
	if len(msg.Args) < 2 {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: REGISTER|username|password")
		return
	}
	username, password := msg.Args[0], msg.Args[1]
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
func (s *Server) handleListRooms(p *Player, _ protocol.Message) {
	if p.getStatus() < StatusLobby {
		s.sendErr(p, config.ErrInvalidFormat, "Not logged in")
		return
	}
	p.Send(s.getRoomList())
}

// handleCreateRoom mirrors handler_create_room (handler.c:225-266). The bounds
// check uses MinRoomPlayers (=2); the C error text still reads "(6-10)".
func (s *Server) handleCreateRoom(p *Player, msg protocol.Message) {
	if len(msg.Args) < 2 {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: CREATE_ROOM|name|max_players")
		return
	}
	if p.getStatus() != StatusLobby {
		s.sendErr(p, config.ErrInvalidFormat, "Must be in lobby")
		return
	}
	name := msg.Args[0]
	maxPlayers := atoi(msg.Args[1])
	if maxPlayers < config.MinRoomPlayers || maxPlayers > config.MaxRoomPlayers {
		s.sendErr(p, config.ErrInvalidArgFormat, "Invalid max players (6-10)")
		return
	}
	room := s.createRoom(name, maxPlayers)
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
	info := protocol.BuildRoomInfo(room.id, room.name, room.playerCount, room.maxPlayers, int(room.status))
	room.mu.Unlock()
	p.Send(info)
}

// handleJoinRoom mirrors handler_join_room (handler.c:268-305).
func (s *Server) handleJoinRoom(p *Player, msg protocol.Message) {
	if len(msg.Args) < 1 {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: JOIN_ROOM|room_id")
		return
	}
	if p.getStatus() != StatusLobby {
		s.sendErr(p, config.ErrInvalidFormat, "Must be in lobby")
		return
	}
	room := s.findRoomByID(atoi(msg.Args[0]))
	if room == nil {
		s.sendErr(p, config.ErrRoomNotFound, "Room not found")
		return
	}
	switch room.addPlayer(p) {
	case -1:
		s.sendErr(p, config.ErrRoomFull, "Room is full")
		return
	case -2:
		s.sendErr(p, config.ErrGameInProgress, "Game in progress")
		return
	}
	room.mu.Lock()
	info := protocol.BuildRoomInfo(room.id, room.name, room.playerCount, room.maxPlayers, int(room.status))
	room.mu.Unlock()
	p.Send(info)
}

// handleLeaveRoom mirrors handler_leave_room (handler.c:307-327).
func (s *Server) handleLeaveRoom(p *Player, _ protocol.Message) {
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
func (s *Server) handleReady(p *Player, _ protocol.Message) {
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
func (s *Server) handleMoveCmd(p *Player, msg protocol.Message) {
	if len(msg.Args) < 1 {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: MOVE|direction")
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
	if msg.Args[0] == "" {
		s.sendErr(p, config.ErrInvalidMove, "Invalid move")
		return
	}
	switch room.handleMove(p, msg.Args[0][0]) {
	case -1:
		s.sendErr(p, config.ErrMoveCooldown, "Move on cooldown")
	case -2:
		s.sendErr(p, config.ErrInvalidMove, "Invalid move")
	}
}

// handleAttackCmd mirrors handler_attack (handler.c:404-426).
func (s *Server) handleAttackCmd(p *Player, _ protocol.Message) {
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
func (s *Server) handleUseItemCmd(p *Player, msg protocol.Message) {
	if len(msg.Args) < 1 {
		s.sendErr(p, config.ErrInvalidArgCount, "Usage: USE_ITEM|index")
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
	if room.handleUseItem(p, atoi(msg.Args[0])) == -1 {
		s.sendErr(p, config.ErrInvalidItemIndex, "Invalid item index")
		return
	}
	s.sendOK(p, "Item used")
}

// handleChat mirrors handler_chat (handler.c:457-498): ignore empty/whitespace,
// require a room, then broadcast.
func (s *Server) handleChat(p *Player, msg protocol.Message) {
	if len(msg.Args) < 1 {
		return
	}
	message := msg.Args[0]
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
	room.broadcast(protocol.BuildChatMsg(p.getUsername(), message))
}

// handleLogout mirrors handler_logout (handler.c:500-521).
func (s *Server) handleLogout(p *Player, _ protocol.Message) {
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

// atoi mirrors C atoi: non-numeric input yields 0.
func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
