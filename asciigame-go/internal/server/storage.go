package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"

	"github.com/heartlazyli/asciigame/internal/config"
)

// userRecord mirrors the C UserRecord (storage.h). The C version stored fixed
// 128-byte binary records in data/users.dat; the Go port uses JSON (per the
// port plan) since the store is internal and not exercised by the wire tests.
type userRecord struct {
	Username string `json:"username"`
	// PasswordHash is a bcrypt hash ("$2..."). Records written by older builds
	// hold a bare SHA-256 hex digest and are upgraded to bcrypt on next login.
	PasswordHash string `json:"password_hash"`
	Wins         int    `json:"wins"`
	Losses       int    `json:"losses"`
	Points       int    `json:"points"`
}

// storage is the user account store, mirroring storage.c behavior.
type storage struct {
	mu    sync.Mutex
	path  string
	users map[string]*userRecord
}

func newStorage(path string) (*storage, error) {
	s := &storage{path: path, users: make(map[string]*userRecord)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// hashPassword returns a bcrypt hash of the password.
func hashPassword(password string) string {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		// bcrypt only errors on >72-byte inputs; passwords are capped well
		// below that (MaxPassword), so this is effectively unreachable.
		return ""
	}
	return string(h)
}

// legacySHA256 returns the old, unsalted SHA-256 hex digest used by earlier
// builds (and the C server), kept only for verifying pre-bcrypt records.
func legacySHA256(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// checkPassword verifies password against stored hash. It returns ok and
// whether the stored hash is a legacy SHA-256 that should be upgraded.
func checkPassword(stored, password string) (ok, legacy bool) {
	if strings.HasPrefix(stored, "$2") { // bcrypt
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)) == nil, false
	}
	// Legacy unsalted SHA-256 hex (constant-time compare).
	match := subtle.ConstantTimeCompare([]byte(stored), []byte(legacySHA256(password))) == 1
	return match, match
}

// register mirrors storage_register_user (storage.c:174-232):
//
//	0  success, -1 username exists, -2 invalid/other.
func (s *storage) register(username, password string) int {
	if username == "" || len(username) >= config.MaxUsername {
		return -2
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[username]; ok {
		return -1
	}
	if len(s.users) >= config.MaxUsers {
		return -2
	}
	s.users[username] = &userRecord{Username: username, PasswordHash: hashPassword(password)}
	_ = s.saveLocked()
	return 0
}

// verify mirrors storage_verify_user (storage.c:234-266):
//
//	0  ok, -1 user not found, -2 wrong password.
//
// On a successful login against a legacy SHA-256 record, the stored hash is
// transparently upgraded to bcrypt.
func (s *storage) verify(username, password string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return -1
	}
	match, legacy := checkPassword(u.PasswordHash, password)
	if !match {
		return -2
	}
	if legacy {
		if h := hashPassword(password); h != "" {
			u.PasswordHash = h
			_ = s.saveLocked()
		}
	}
	return 0
}

// updateStats mirrors storage_update_stats (storage.c:268-304).
func (s *storage) updateStats(username string, win bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return
	}
	if win {
		u.Wins++
		u.Points += 10
	} else {
		u.Losses++
		u.Points++
	}
	_ = s.saveLocked()
}

func (s *storage) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // absent file is normal
		}
		return err
	}
	var list []*userRecord
	if len(data) > 0 {
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range list {
		s.users[u.Username] = u
	}
	return nil
}

func (s *storage) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	list := make([]*userRecord, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
