package store

import (
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrateAppliesAndIsIdempotent(t *testing.T) {
	s := openTemp(t)
	if v, err := s.Version(); err != nil || v != 0 {
		t.Fatalf("pristine Version = %d, %v", v, err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if v, err := s.Version(); err != nil || v != LatestVersion() {
		t.Fatalf("Version = %d (want %d), %v", v, LatestVersion(), err)
	}
	// Re-running must be a no-op, not an error.
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate (second run): %v", err)
	}
	if v, _ := s.Version(); v != LatestVersion() {
		t.Fatalf("Version changed on re-run: %d", v)
	}
}

func TestMigrationSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if v, _ := s2.Version(); v != LatestVersion() {
		t.Fatalf("version lost after reopen: %d", v)
	}
	if err := s2.Migrate(); err != nil {
		t.Fatalf("Migrate on reopened db: %v", err)
	}
}

// The whole v1 schema (FTS5 included) must apply and work under
// modernc.org/sqlite — this is the earliest place a missing FTS5 build
// flag could surface.
func TestFTS5Works(t *testing.T) {
	s := openTemp(t)
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	_, err := s.Exec(t.Context(),
		`INSERT INTO fts (title, body, item_id) VALUES ('Go 1.27 released', 'The new release ships generics improvements.', 'test:1')`)
	if err != nil {
		t.Fatalf("insert into fts: %v", err)
	}
	var n int
	err = s.db.QueryRow(`SELECT count(*) FROM fts WHERE fts MATCH 'generics'`).Scan(&n)
	if err != nil {
		t.Fatalf("fts match: %v", err)
	}
	if n != 1 {
		t.Fatalf("fts match count = %d, want 1", n)
	}
}

func TestDefaultPathEnvOverride(t *testing.T) {
	t.Setenv("READSCOPE_STATE", "/tmp/rs-state")
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if want := "/tmp/rs-state/readscope.db"; got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
}
