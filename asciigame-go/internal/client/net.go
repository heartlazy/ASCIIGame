package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/heartlazyli/asciigame/internal/protocol"
)

// --- HTTP Client (lobby/room operations) ---

// HTTPClient handles all request-response operations via the Gin HTTP API.
type HTTPClient struct {
	base   string // e.g. "http://127.0.0.1:8080"
	token  string
	client *http.Client
}

// NewHTTPClient creates a client pointing at the given base URL.
func NewHTTPClient(base string) *HTTPClient {
	return &HTTPClient{base: base, client: &http.Client{}}
}

// Token returns the current auth token (for TCP auth).
func (h *HTTPClient) Token() string { return h.token }

func (h *HTTPClient) postJSON(path string, body any) (map[string]any, int, error) {
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest("POST", h.base+path, r)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return m, resp.StatusCode, nil
}

func (h *HTTPClient) getJSON(path string) (any, int, error) {
	req, err := http.NewRequest("GET", h.base+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var v any
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return v, resp.StatusCode, nil
}

// Register sends POST /api/register. Returns error message or "".
func (h *HTTPClient) Register(username, password string) (string, error) {
	m, code, err := h.postJSON("/api/register", map[string]string{"username": username, "password": password})
	if err != nil {
		return "", err
	}
	if code != 200 {
		return getStr(m, "error"), nil
	}
	return "", nil
}

// Login sends POST /api/login. On success sets the token and returns playerID.
func (h *HTTPClient) Login(username, password string) (int, string, error) {
	m, code, err := h.postJSON("/api/login", map[string]string{"username": username, "password": password})
	if err != nil {
		return 0, "", err
	}
	if code != 200 {
		return 0, getStr(m, "error"), nil
	}
	h.token = getStr(m, "token")
	pid := int(getFloat(m, "player_id"))
	return pid, "", nil
}

// ListRooms returns the room list as raw JSON array.
func (h *HTTPClient) ListRooms() ([]map[string]any, error) {
	v, _, err := h.getJSON("/api/rooms")
	if err != nil {
		return nil, err
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, nil
	}
	var rooms []map[string]any
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			rooms = append(rooms, m)
		}
	}
	return rooms, nil
}

// CreateRoom sends POST /api/rooms.
func (h *HTTPClient) CreateRoom(name string, maxPlayers int) (map[string]any, string, error) {
	m, code, err := h.postJSON("/api/rooms", map[string]any{"name": name, "max_players": maxPlayers})
	if err != nil {
		return nil, "", err
	}
	if code != 200 {
		return nil, getStr(m, "error"), nil
	}
	return m, "", nil
}

// JoinRoom sends POST /api/rooms/:id/join.
func (h *HTTPClient) JoinRoom(roomID int) (map[string]any, string, error) {
	m, code, err := h.postJSON(fmt.Sprintf("/api/rooms/%d/join", roomID), nil)
	if err != nil {
		return nil, "", err
	}
	if code != 200 {
		return nil, getStr(m, "error"), nil
	}
	return m, "", nil
}

// LeaveRoom sends POST /api/rooms/leave.
func (h *HTTPClient) LeaveRoom() (string, error) {
	m, code, err := h.postJSON("/api/rooms/leave", nil)
	if err != nil {
		return "", err
	}
	if code != 200 {
		return getStr(m, "error"), nil
	}
	return "", nil
}

// Ready sends POST /api/rooms/ready. Returns (message, gameStarted, errMsg).
func (h *HTTPClient) Ready() (string, bool, string, error) {
	m, code, err := h.postJSON("/api/rooms/ready", nil)
	if err != nil {
		return "", false, "", err
	}
	if code != 200 {
		return "", false, getStr(m, "error"), nil
	}
	started, _ := m["game_started"].(bool)
	return getStr(m, "message"), started, "", nil
}

// Chat sends POST /api/chat.
func (h *HTTPClient) Chat(message string) error {
	_, _, err := h.postJSON("/api/chat", map[string]string{"message": message})
	return err
}

// Logout sends POST /api/logout.
func (h *HTTPClient) Logout() error {
	_, _, err := h.postJSON("/api/logout", nil)
	h.token = ""
	return err
}

func getStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func getFloat(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	v, _ := m[key].(float64)
	return v
}

// --- TCP Connection (game real-time stream) ---

// Conn is the TCP connection for real-time game frames.
type Conn struct {
	conn net.Conn
	r    *bufio.Reader
}

// Dial connects to the TCP game server.
func Dial(addr string) (*Conn, error) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Conn{conn: c, r: bufio.NewReader(c)}, nil
}

// Authenticate sends the Auth frame and reads the Ok acknowledgement.
func (c *Conn) Authenticate(token string) error {
	if err := protocol.WriteFrame(c.conn, protocol.NewAuth(token)); err != nil {
		return err
	}
	f, err := protocol.ReadFrame(c.r)
	if err != nil {
		return err
	}
	if e := f.GetError(); e != nil {
		return fmt.Errorf("auth failed: %s", e.Message)
	}
	return nil
}

// Send writes one frame.
func (c *Conn) Send(f *protocol.Frame) error {
	return protocol.WriteFrame(c.conn, f)
}

// ReadLoop reads frames and invokes onMsg for each, until the connection closes.
func (c *Conn) ReadLoop(onMsg func(*protocol.Frame), onClose func()) {
	defer onClose()
	for {
		f, err := protocol.ReadFrame(c.r)
		if err != nil {
			return
		}
		onMsg(f)
	}
}

// Close closes the connection.
func (c *Conn) Close() error { return c.conn.Close() }
