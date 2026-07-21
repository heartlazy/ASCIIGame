// Package server implements the ASCII Battle Royale game server, ported from
// the C server/ sources. The C design (single epoll thread + one pthread game
// loop per room, global player/room tables under mutexes) maps to: one
// goroutine per TCP connection, one goroutine per room game loop
// (time-sliced 50ms), and player/room registries guarded by RWMutexes. Lock
// order is always room.mu before player.mu; the registry locks are leaf locks.
package server

import (
	"bufio"
	"context"
	"log"
	"net"
	"sync"

	"github.com/heartlazyli/asciigame/internal/protocol"
)

// Server holds the global player and room registries plus the user store,
// replacing the C global singletons in player.c/room.c/storage.c.
type Server struct {
	pmu          sync.RWMutex
	players      map[int]*Player
	nextPlayerID int

	rmu        sync.RWMutex
	rooms      map[int]*Room
	nextRoomID int

	store *storage

	// Recovery registry, populated by RecoverAll on startup and consumed as
	// players log back in.
	recMu         sync.Mutex
	pending       map[int]*pendingRecovery // by original room id
	recByUser     map[string]int           // username -> original room id
	recRoomByOrig map[int]int              // original room id -> live room id
}

// New creates a Server, loading the user store from storagePath.
func New(storagePath string) (*Server, error) {
	st, err := newStorage(storagePath)
	if err != nil {
		return nil, err
	}
	return &Server{
		players:       make(map[int]*Player),
		nextPlayerID:  1,
		rooms:         make(map[int]*Room),
		nextRoomID:    1,
		store:         st,
		pending:       make(map[int]*pendingRecovery),
		recByUser:     make(map[string]int),
		recRoomByOrig: make(map[int]int),
	}, nil
}

// ListenAndServe accepts connections on addr until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("server listening on %s", addr)
	return s.Serve(ctx, ln)
}

// Serve accepts connections on ln until ctx is cancelled. Exposed so tests can
// supply a listener bound to an ephemeral port.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	// Close the listener when the context is cancelled to unblock Accept.
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetNoDelay(true) // match C TCP_NODELAY
		}
		go s.handleConn(ctx, conn)
	}
}

// registerPlayer allocates an id and adds the player to the registry, mirroring
// player_create's slot+id assignment.
func (s *Server) registerPlayer(conn net.Conn) *Player {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	id := s.nextPlayerID
	s.nextPlayerID++
	p := newPlayer(conn, id)
	s.players[id] = p
	return p
}

func (s *Server) unregisterPlayer(p *Player) {
	s.pmu.Lock()
	delete(s.players, p.id)
	s.pmu.Unlock()
}

// findPlayerByID mirrors player_find_by_id. A leaf lock: never held while
// acquiring room/player mutexes.
func (s *Server) findPlayerByID(id int) *Player {
	s.pmu.RLock()
	defer s.pmu.RUnlock()
	return s.players[id]
}

// findPlayerByUsername mirrors player_find_by_username (matches only logged-in
// players, i.e. non-empty username).
func (s *Server) findPlayerByUsername(username string) *Player {
	s.pmu.RLock()
	defer s.pmu.RUnlock()
	for _, p := range s.players {
		p.mu.Lock()
		u := p.username
		p.mu.Unlock()
		if u != "" && u == username {
			return p
		}
	}
	return nil
}

// handleConn runs one connection: a writer goroutine drains the player's out
// channel, and this goroutine reads newline-framed messages and dispatches
// them. Mirrors handle_client_data + handle_disconnect (main.c).
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	p := s.registerPlayer(conn)
	log.Printf("player %d connected", p.id)

	go p.writeLoop()

	defer s.disconnect(p)

	// bufio.Reader (not Scanner) so large frames like MAP_DATA are never capped
	// at the 64KB Scanner token limit.
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		msg, perr := protocol.Parse(line)
		if perr != nil {
			// Malformed frame: mirror main.c's ERR_INVALID_FORMAT reply.
			p.Send(protocol.BuildErr(1001, "Invalid message format"))
			continue
		}
		s.handle(p, msg)
	}
}

// writeLoop serializes all socket writes for a player.
func (p *Player) writeLoop() {
	for {
		select {
		case msg := <-p.out:
			if _, err := p.conn.Write([]byte(msg)); err != nil {
				return
			}
		case <-p.done:
			return
		}
	}
}

// disconnect mirrors handle_disconnect (main.c:84-108): leave any room, drop
// from the registry, close the socket.
func (s *Server) disconnect(p *Player) {
	p.mu.Lock()
	roomID := p.roomID
	p.mu.Unlock()

	if roomID >= 0 {
		if room := s.findRoomByID(roomID); room != nil {
			room.removePlayer(p)
		}
	}
	s.unregisterPlayer(p)
	p.closeOnce()
	_ = p.conn.Close()
	log.Printf("player %d disconnected", p.id)
}
