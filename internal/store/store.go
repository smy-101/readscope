// Package store owns readscope's persistent state: a single SQLite
// database (modernc.org/sqlite, pure Go — no cgo) holding cached items,
// read state, favorites, EPUB library metadata, reading progress and the
// unified FTS5 index. Schema changes go through embedded, ordered
// migrations.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the SQLite database.
type Store struct {
	db   *sql.DB
	path string
}

// DefaultPath returns the database location: $READSCOPE_STATE/readscope.db
// when the env override is set, otherwise the XDG state home
// (~/.local/state/readscope/readscope.db on Linux). Parent directories are
// created on Open, not here.
func DefaultPath() (string, error) {
	if dir := os.Getenv("READSCOPE_STATE"); dir != "" {
		return filepath.Join(dir, "readscope.db"), nil
	}
	base, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "readscope", "readscope.db"), nil
}

// stateDir resolves the XDG state home (~/.local/state on Linux); other
// systems reuse the config dir. READSCOPE_STATE above overrides everything.
func stateDir() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return xdg, nil
	}
	switch runtime.GOOS {
	case "windows", "darwin", "plan9":
		return os.UserConfigDir()
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state"), nil
	}
}

// Open opens (creating if needed) the database at path. It does not
// migrate; call Migrate explicitly so doctor can distinguish "can open"
// from "schema healthy". WAL + busy timeout are set via DSN pragmas.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// modernc.org/sqlite serializes writes; one connection avoids
	// SQLITE_BUSY surprises under the TUI's async refreshes.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

// Path reports the database file path (for doctor).
func (s *Store) Path() string { return s.path }

// Ping verifies the connection is alive.
func (s *Store) Ping() error { return s.db.Ping() }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Version returns the currently applied schema version (0 = pristine).
func (s *Store) Version() (int, error) {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at TIMESTAMP NOT NULL)`); err != nil {
		return 0, err
	}
	var v int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v)
	return v, err
}

// LatestVersion returns the newest embedded migration version.
func LatestVersion() int {
	vs, _ := migrationVersions()
	if len(vs) == 0 {
		return 0
	}
	return vs[len(vs)-1]
}

// Migrate applies pending migrations in order, each inside a transaction.
func (s *Store) Migrate() error {
	versions, files := migrationVersions()
	current, err := s.Version()
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	for _, v := range versions {
		if v <= current {
			continue
		}
		body, err := migrationsFS.ReadFile(files[v])
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", filepath.Base(files[v]), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, v, time.Now().UTC()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", v, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

var numPrefix = regexp.MustCompile(`^(\d+)_`)

func migrationVersions() ([]int, map[int]string) {
	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return nil, nil
	}
	versions := make([]int, 0, len(entries))
	files := make(map[int]string, len(entries))
	for _, e := range entries {
		m := numPrefix.FindStringSubmatch(filepath.Base(e))
		if m == nil {
			continue
		}
		v, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		versions = append(versions, v)
		files[v] = e
	}
	sort.Ints(versions)
	return versions, files
}

// Exec exposes the underlying DB for sibling packages (M2+ DAOs live in
// store too; this is the escape hatch until then).
func (s *Store) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}
