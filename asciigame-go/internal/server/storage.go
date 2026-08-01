package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)

	"github.com/heartlazyli/asciigame/internal/config"
)

// userRecord is one account row. Earlier builds stored these as JSON; accounts
// now live in SQLite (see newStorage) so per-account updates are atomic and
// loading is O(1) instead of rewriting the whole file each change.
type userRecord struct {
	Username string
	// PasswordHash is a bcrypt hash ("$2..."). Records imported from the old
	// JSON store (or the C server) may hold a bare SHA-256 hex digest and are
	// upgraded to bcrypt on the next successful login.
	PasswordHash string
	Wins         int
	Losses       int
	Points       int
}

// storage is the SQLite-backed user account store. *sql.DB is safe for
// concurrent use; SQLite serializes writes internally, which is ample for the
// low account-write rate (register / login upgrade / end-of-match stats).
type storage struct {
	db *sql.DB
}

// newStorage opens (creating if needed) the SQLite database at path, ensures
// the schema exists, and performs a one-time import from a sibling users.json
// left by older builds.
func newStorage(path string) (*storage, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	// busy_timeout lets concurrent writers wait for the lock instead of failing.
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		username      TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL,
		wins          INTEGER NOT NULL DEFAULT 0,
		losses        INTEGER NOT NULL DEFAULT 0,
		points        INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, err
	}
	s := &storage{db: db}
	s.migrateFromJSON(path)
	return s, nil
}

// close releases the database handle.
func (s *storage) close() error { return s.db.Close() }

// migrateFromJSON imports accounts from a legacy users.json sitting next to the
// database, once, when the users table is still empty. The JSON file is then
// renamed so the import never repeats.
func (s *storage) migrateFromJSON(dbPath string) {
	jsonPath := filepath.Join(filepath.Dir(dbPath), "users.json")
	if jsonPath == dbPath {
		return // the DB file itself is not a migration source
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return // no legacy file: nothing to migrate
	}
	var list []*userRecord
	if json.Unmarshal(data, &list) != nil {
		return
	}
	var n int
	if s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); n > 0 {
		return // already populated
	}
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	for _, u := range list {
		_, _ = tx.Exec(`INSERT OR IGNORE INTO users(username,password_hash,wins,losses,points)
			VALUES(?,?,?,?,?)`, u.Username, u.PasswordHash, u.Wins, u.Losses, u.Points)
	}
	if tx.Commit() == nil {
		_ = os.Rename(jsonPath, jsonPath+".migrated")
		log.Printf("storage: migrated %d users from %s to SQLite", len(list), jsonPath)
	}
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
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return -2
	}
	if n >= config.MaxUsers {
		return -2
	}
	_, err := s.db.Exec(`INSERT INTO users(username,password_hash) VALUES(?,?)`,
		username, hashPassword(password))
	if err != nil {
		if strings.Contains(strings.ToUpper(err.Error()), "UNIQUE") {
			return -1 // username already exists
		}
		return -2
	}
	return 0
}

// verify mirrors storage_verify_user (storage.c:234-266):
//
//	0  ok, -1 user not found, -2 wrong password.
//
// On a successful login against a legacy SHA-256 record, the stored hash is
// transparently upgraded to bcrypt.
func (s *storage) verify(username, password string) int {
	var stored string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE username=?`, username).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return -1
	}
	if err != nil {
		return -2
	}
	match, legacy := checkPassword(stored, password)
	if !match {
		return -2
	}
	if legacy {
		if h := hashPassword(password); h != "" {
			_, _ = s.db.Exec(`UPDATE users SET password_hash=? WHERE username=?`, h, username)
		}
	}
	return 0
}

// updateStats mirrors storage_update_stats (storage.c:268-304). A single atomic
// UPDATE — no full-file rewrite. Unknown users are a no-op (0 rows affected).
func (s *storage) updateStats(username string, win bool) {
	if win {
		_, _ = s.db.Exec(`UPDATE users SET wins=wins+1, points=points+10 WHERE username=?`, username)
	} else {
		_, _ = s.db.Exec(`UPDATE users SET losses=losses+1, points=points+1 WHERE username=?`, username)
	}
}

// getUser returns the account row, or nil if it does not exist.
func (s *storage) getUser(username string) *userRecord {
	u := &userRecord{Username: username}
	err := s.db.QueryRow(`SELECT password_hash,wins,losses,points FROM users WHERE username=?`, username).
		Scan(&u.PasswordHash, &u.Wins, &u.Losses, &u.Points)
	if err != nil {
		return nil
	}
	return u
}
