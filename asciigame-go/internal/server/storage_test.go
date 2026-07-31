package server

import (
	"path/filepath"
	"testing"
)

func TestStorageStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
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

	// Reload from disk to confirm persistence.
	st2, err := newStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	u := st2.users["alice"]
	if u == nil {
		t.Fatal("alice not persisted")
	}
	if u.Wins != 1 || u.Losses != 1 || u.Points != 11 {
		t.Errorf("stats = wins:%d losses:%d points:%d, want 1/1/11", u.Wins, u.Losses, u.Points)
	}
	// Unknown user is a no-op (no panic).
	st2.updateStats("ghost", true)
}
