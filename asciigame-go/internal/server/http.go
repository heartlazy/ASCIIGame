package server

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
)

// SetupHTTP returns a Gin engine with all HTTP API routes.
func (s *Server) SetupHTTP() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	api := r.Group("/api")
	api.POST("/register", s.httpRegister)
	api.POST("/login", s.httpLogin)

	auth := api.Group("", s.authMiddleware())
	auth.GET("/rooms", s.httpListRooms)
	auth.POST("/rooms", s.httpCreateRoom)
	auth.POST("/rooms/:id/join", s.httpJoinRoom)
	auth.POST("/rooms/leave", s.httpLeaveRoom)
	auth.POST("/rooms/ready", s.httpReady)
	auth.POST("/chat", s.httpChat)
	auth.POST("/logout", s.httpLogout)

	return r
}

// authMiddleware validates the Bearer token and injects the Player into context.
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			c.Abort()
			return
		}
		token := strings.TrimPrefix(h, "Bearer ")
		p := s.tokens.Validate(token)
		if p == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}
		c.Set("player", p)
		c.Next()
	}
}

func getPlayer(c *gin.Context) *Player {
	v, _ := c.Get("player")
	return v.(*Player)
}

// --- Handlers ---

func (s *Server) httpRegister(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}
	if len(req.Username) >= config.MaxUsername {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username too long"})
		return
	}
	switch s.store.register(req.Username, req.Password) {
	case 0:
		log.Printf("user registered: %s", req.Username)
		c.JSON(http.StatusOK, gin.H{"message": "Registration successful"})
	case -1:
		c.JSON(http.StatusConflict, gin.H{"error": "Username already exists"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Registration failed"})
	}
}

func (s *Server) httpLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}

	switch s.store.verify(req.Username, req.Password) {
	case -1:
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	case -2:
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	// Create or reuse a Player object for this user.
	p := s.findPlayerByUsername(req.Username)
	if p != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User already online"})
		return
	}
	p = s.createPlayer(req.Username)
	token := s.tokens.Issue(p)
	log.Printf("player %s logged in (id=%d)", req.Username, p.id)
	c.JSON(http.StatusOK, gin.H{
		"message":   "Login successful",
		"token":     token,
		"player_id": p.id,
	})
}

func (s *Server) httpListRooms(c *gin.Context) {
	p := getPlayer(c)
	log.Printf("player %s listed rooms", p.label())

	s.rmu.RLock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.rmu.RUnlock()

	type roomDTO struct {
		RoomID      int    `json:"room_id"`
		Name        string `json:"name"`
		PlayerCount int    `json:"player_count"`
		MaxPlayers  int    `json:"max_players"`
		Status      int    `json:"status"`
	}
	list := make([]roomDTO, 0, len(rooms))
	for _, r := range rooms {
		r.mu.Lock()
		list = append(list, roomDTO{
			RoomID: r.id, Name: r.name, PlayerCount: r.playerCount,
			MaxPlayers: r.maxPlayers, Status: int(r.status),
		})
		r.mu.Unlock()
	}
	c.JSON(http.StatusOK, list)
}

func (s *Server) httpCreateRoom(c *gin.Context) {
	p := getPlayer(c)
	var req struct {
		Name       string `json:"name" binding:"required"`
		MaxPlayers int    `json:"max_players"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	if p.getStatus() != StatusLobby {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Must be in lobby"})
		return
	}
	if req.MaxPlayers < config.MinRoomPlayers || req.MaxPlayers > config.MaxRoomPlayers {
		req.MaxPlayers = config.MaxRoomPlayers
	}
	room := s.createRoom(req.Name, req.MaxPlayers)
	if room == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create room"})
		return
	}
	if room.addPlayer(p) < 0 {
		s.destroyRoom(room)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to join room"})
		return
	}
	room.mu.Lock()
	resp := gin.H{
		"room_id": room.id, "name": room.name,
		"player_count": room.playerCount, "max_players": room.maxPlayers,
		"status": int(room.status),
	}
	room.mu.Unlock()
	c.JSON(http.StatusOK, resp)
}

func (s *Server) httpJoinRoom(c *gin.Context) {
	p := getPlayer(c)
	if p.getStatus() != StatusLobby {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Must be in lobby"})
		return
	}
	roomID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid room id"})
		return
	}
	room := s.findRoomByID(roomID)
	if room == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Room not found"})
		return
	}
	switch room.addPlayer(p) {
	case -1:
		c.JSON(http.StatusConflict, gin.H{"error": "Room is full"})
		return
	case -2:
		c.JSON(http.StatusConflict, gin.H{"error": "Game in progress"})
		return
	}
	room.mu.Lock()
	resp := gin.H{
		"room_id": room.id, "name": room.name,
		"player_count": room.playerCount, "max_players": room.maxPlayers,
		"status": int(room.status),
	}
	room.mu.Unlock()
	c.JSON(http.StatusOK, resp)
}

func (s *Server) httpLeaveRoom(c *gin.Context) {
	p := getPlayer(c)
	if p.getStatus() < StatusInRoom {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Not in a room"})
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Room not found"})
		return
	}
	room.removePlayer(p)
	c.JSON(http.StatusOK, gin.H{"message": "Left room"})
}

func (s *Server) httpReady(c *gin.Context) {
	p := getPlayer(c)
	st := p.getStatus()
	if st != StatusInRoom && st != StatusReady {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Not in a room"})
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Room not found"})
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

	msg := "Not ready"
	if newStatus == StatusReady {
		msg = "Ready"
	}

	gameStarted := false
	if room.allReady() {
		room.mu.Lock()
		room.wal = newWAL(room.id)
		room.mu.Unlock()
		room.startGame()
		go room.gameLoop()
		gameStarted = true
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "game_started": gameStarted})
}

func (s *Server) httpChat(c *gin.Context) {
	p := getPlayer(c)
	var req struct {
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message required"})
		return
	}
	if p.getRoomID() < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Not in a room"})
		return
	}
	room := s.findRoomByID(p.getRoomID())
	if room == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Room not found"})
		return
	}
	sender := fmt.Sprintf("%s#%d", p.getUsername(), p.id)
	room.broadcast(protocol.NewChatMsg(sender, req.Message))
	c.JSON(http.StatusOK, gin.H{"message": "sent"})
}

func (s *Server) httpLogout(c *gin.Context) {
	p := getPlayer(c)
	if rid := p.getRoomID(); rid >= 0 {
		if room := s.findRoomByID(rid); room != nil {
			room.removePlayer(p)
		}
	}
	s.tokens.Revoke(p.id)
	s.unregisterPlayer(p)
	p.mu.Lock()
	p.username = ""
	p.status = StatusDisconnected
	p.mu.Unlock()
	log.Printf("player %s logged out", p.label())
	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}
