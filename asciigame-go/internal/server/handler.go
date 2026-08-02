package server

import (
	"log"

	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

// handleTCP dispatches a TCP frame from an authenticated player. Only game
// actions (Move/Attack/UseItem) are handled here; all lobby/room operations
// go through the HTTP API.
func (s *Server) handleTCP(p *Player, f *protocol.Frame) {
	switch m := f.Payload.(type) {
	case *protocol.Frame_Move:
		s.handleMoveCmd(p, m.Move)
	case *protocol.Frame_Attack:
		s.handleAttackCmd(p)
	case *protocol.Frame_UseItem:
		s.handleUseItemCmd(p, m.UseItem)
	default:
		p.Send(protocol.NewError(int32(config.ErrUnknownCommand), "Unknown TCP command"))
	}
}

// handleMoveCmd handles a Move frame (TCP only, in-game).
func (s *Server) handleMoveCmd(p *Player, m *protocol.Move) {
	if m == nil || m.Direction == "" {
		p.Send(protocol.NewError(int32(config.ErrInvalidMove), "Invalid move"))
		return
	}
	if p.getStatus() != StatusGaming {
		p.Send(protocol.NewError(int32(config.ErrInvalidFormat), "Not in game"))
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		p.Send(protocol.NewError(int32(config.ErrRoomNotFound), "Room not found"))
		return
	}
	switch room.handleMove(p, m.Direction[0]) {
	case -1:
		p.Send(protocol.NewError(int32(config.ErrMoveCooldown), "Move on cooldown"))
	case -2:
		p.Send(protocol.NewError(int32(config.ErrInvalidMove), "Invalid move"))
	}
}

// handleAttackCmd handles an Attack frame (TCP only, in-game).
func (s *Server) handleAttackCmd(p *Player) {
	if p.getStatus() != StatusGaming {
		p.Send(protocol.NewError(int32(config.ErrInvalidFormat), "Not in game"))
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		p.Send(protocol.NewError(int32(config.ErrRoomNotFound), "Room not found"))
		return
	}
	if room.handleAttack(p) == -1 {
		p.Send(protocol.NewError(int32(config.ErrAttackCooldown), "Attack on cooldown"))
	}
}

// handleUseItemCmd handles a UseItem frame (TCP only, in-game).
func (s *Server) handleUseItemCmd(p *Player, m *protocol.UseItem) {
	if m == nil {
		p.Send(protocol.NewError(int32(config.ErrInvalidArgCount), "Usage: UseItem{index}"))
		return
	}
	if p.getStatus() != StatusGaming {
		p.Send(protocol.NewError(int32(config.ErrInvalidFormat), "Not in game"))
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		p.Send(protocol.NewError(int32(config.ErrRoomNotFound), "Room not found"))
		return
	}
	if room.handleUseItem(p, int(m.Index)) == -1 {
		p.Send(protocol.NewError(int32(config.ErrInvalidItemIndex), "Invalid item index"))
		return
	}
	p.Send(protocol.NewOk("Item used", 0))
}

// sendErr is a convenience to push a typed Error frame to a player (used by
// game.go broadcastEvent / endGame for consistency).
func (s *Server) sendErr(p *Player, code int, message string) {
	p.Send(protocol.NewError(int32(code), message))
}

// sendOK is a convenience to push an Ok frame.
func (s *Server) sendOK(p *Player, message string) {
	p.Send(protocol.NewOk(message, 0))
}

// payloadName returns the oneof payload type name for logging.
func payloadName(f *protocol.Frame) string {
	if f == nil || f.Payload == nil {
		return "empty"
	}
	switch f.Payload.(type) {
	case *protocol.Frame_Move:
		return "Move"
	case *protocol.Frame_Attack:
		return "Attack"
	case *protocol.Frame_UseItem:
		return "UseItem"
	case *protocol.Frame_Auth:
		return "Auth"
	default:
		return "unknown"
	}
}

// Unused sendErr/sendOK need log import only for http.go (which has its own);
// suppress the import here.
var _ = log.Printf
