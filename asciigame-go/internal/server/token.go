package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// tokenStore manages session tokens. Each token maps to a Player who is
// authenticated but may or may not have a TCP connection yet.
type tokenStore struct {
	mu     sync.RWMutex
	tokens map[string]*Player // token -> player
	byID   map[int]string     // player_id -> token (for cleanup)
}

func newTokenStore() *tokenStore {
	return &tokenStore{tokens: make(map[string]*Player), byID: make(map[int]string)}
}

// Issue creates a token for the player and stores it.
func (ts *tokenStore) Issue(p *Player) string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	// Revoke any previous token for this player.
	if old, ok := ts.byID[p.id]; ok {
		delete(ts.tokens, old)
	}
	tok := generateToken(p.id)
	ts.tokens[tok] = p
	ts.byID[p.id] = tok
	return tok
}

// Validate returns the player for a token, or nil if invalid.
func (ts *tokenStore) Validate(token string) *Player {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.tokens[token]
}

// Revoke removes the token for a player.
func (ts *tokenStore) Revoke(playerID int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if tok, ok := ts.byID[playerID]; ok {
		delete(ts.tokens, tok)
		delete(ts.byID, playerID)
	}
}

func generateToken(playerID int) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%d:%s", playerID, hex.EncodeToString(b))
}
