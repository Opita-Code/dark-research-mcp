package mem

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the central handle to dark.db's research schema. Reads are
// concurrent-safe; writes are serialized via a mutex.
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// Open creates/opens the research tables at path. If path is empty, a
// temp file is used (mainly for tests). If the file does not exist it
// is created. Schema is applied; migrations run.
func Open(path string) (*Store, error) {
	if path == "" {
		f, err := os.CreateTemp("", "dark-research-*.db")
		if err != nil {
			return nil, fmt.Errorf("mem: temp db: %w", err)
		}
		_ = f.Close()
		path = f.Name()
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mem: mkdir: %w", err)
		}
	}
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("mem: open: %w", err)
	}
	s := &Store{db: db}
	if err := s.Migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// applySchema runs the CREATE TABLE statements. Deprecated: use Migrate.
func (s *Store) applySchema() error {
	if _, err := s.db.Exec(schemaV1); err != nil {
		return fmt.Errorf("mem: schema: %w", err)
	}
	return nil
}

// Close flushes and closes the underlying database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB returns the underlying *sql.DB. Exposed for tests and for ad-hoc
// SQL joins against dark-eval's tables in the same SQLite file.
func (s *Store) DB() *sql.DB { return s.db }

// Exec serializes a write through the mutex.
func (s *Store) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.ExecContext(ctx, query, args...)
}

// Query is read-only and concurrent-safe.
func (s *Store) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, query, args...)
}

// QueryRow is read-only convenience.
func (s *Store) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, query, args...)
}

// Now returns the canonical timestamp string used everywhere.
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// jsonMustMarshal is a tiny helper for serializing small blobs.
func jsonMustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}