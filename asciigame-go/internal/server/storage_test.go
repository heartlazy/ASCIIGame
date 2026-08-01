package server

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.db")
	st, err := newStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.register("alice", "pw") != 0 {
		t.Fatal("register alice failed")
	}
	// A win: +1 win, +10 points. A loss: +1 loss, +1 point.
	st.updateStats("alice", true)
	st.updateStats("alice", false)
	if err := st.close(); err != nil {
		t.Fatal(err)
	}

	// Reopen to confirm persistence.
	st2, err := newStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.close()
	u := st2.getUser("alice")
	if u == nil {
		t.Fatal("alice not persisted")
	}
	if u.Wins != 1 || u.Losses != 1 || u.Points != 11 {
		t.Errorf("stats = wins:%d losses:%d points:%d, want 1/1/11", u.Wins, u.Losses, u.Points)
	}
	// Unknown user is a no-op (no panic, no rows changed).
	st2.updateStats("ghost", true)
	if st2.getUser("ghost") != nil {
		t.Error("updateStats must not create users")
	}
}

func TestStorageRegisterDuplicate(t *testing.T) {
	st, err := newStorage(filepath.Join(t.TempDir(), "game.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.close()
	if st.register("alice", "pw") != 0 {
		t.Fatal("first register should succeed")
	}
	if got := st.register("alice", "other"); got != -1 {
		t.Errorf("duplicate register = %d, want -1", got)
	}
}

func TestPasswordBcryptAndLegacyUpgrade(t *testing.T) {
	st, err := newStorage(filepath.Join(t.TempDir(), "game.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.close()

	// New registration uses bcrypt.
	if st.register("alice", "secret") != 0 {
		t.Fatal("register failed")
	}
	if h := st.getUser("alice").PasswordHash; !strings.HasPrefix(h, "$2") {
		t.Fatalf("new password not bcrypt: %q", h)
	}
	if st.verify("alice", "secret") != 0 {
		t.Error("correct password should verify")
	}
	if st.verify("alice", "wrong") != -2 {
		t.Error("wrong password should fail")
	}
	if st.verify("ghost", "x") != -1 {
		t.Error("unknown user should return -1")
	}

	// Legacy SHA-256 record: verify succeeds AND upgrades to bcrypt.
	if _, err := st.db.Exec(`INSERT INTO users(username,password_hash) VALUES(?,?)`,
		"bob", legacySHA256("pw")); err != nil {
		t.Fatal(err)
	}
	if st.verify("bob", "pw") != 0 {
		t.Fatal("legacy password should verify")
	}
	if h := st.getUser("bob").PasswordHash; !strings.HasPrefix(h, "$2") {
		t.Fatalf("legacy hash not upgraded to bcrypt: %q", h)
	}
	if st.verify("bob", "pw") != 0 {
		t.Error("upgraded password should still verify")
	}
}
