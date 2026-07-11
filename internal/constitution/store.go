package constitution

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Persistence layer for the `constitutions` table (added in migration v2).
//
// The Constitution struct (in types.go) is the in-memory shape that
// comes out of the loader and feeds BuildSystemPrompt. The
// ConstitutionRow struct is the persisted shape — same content plus
// DB-only fields (created_at, activated_at, enabled, source label,
// sha256-as-stored, parsed_json-as-stored).
//
// Save is the upsert path. The table has UNIQUE(constitution_id,
// version); saving an existing (id, version) overwrites parsed_json
// and sha256 (the file changed) but preserves created_at (so the
// audit trail shows the original install).
// ---------------------------------------------------------------------------

// ConstitutionRow is the persisted form of a constitution. Use
// ToConstitution() to recover the runtime form, or
// FromConstitution() to construct a Row from a fresh load.
type ConstitutionRow struct {
	ID           string
	Version      string
	Label        string // empty = NULL in DB
	Source       string
	FilePath     string
	ParsedJSON   string
	SHA256       string
	Enabled      bool
	CreatedAt    string // RFC3339Nano; populated by DB on insert
	ActivatedAt  string // RFC3339Nano; empty if never activated
}

// ToConstitution converts the persisted Row to the in-memory
// Constitution used by BuildSystemPrompt. The conversion is one-
// way: the row carries a serialized JSON blob (parsed_json), but
// the in-memory Constitution holds the deserialized fields
// (Identity, Authority, etc.). For now we just copy the metadata
// fields; a future enhancement could re-hydrate from parsed_json.
func (r *ConstitutionRow) ToConstitution() *Constitution {
	if r == nil {
		return nil
	}
	return &Constitution{
		Meta: Meta{
			ID:      r.ID,
			Version: r.Version,
			Label:   r.Label,
		},
		Source:   Source(r.Source),
		FilePath: r.FilePath,
		SHA256:   r.SHA256,
		Builtin:  isBuiltinSource(r.Source),
	}
}

// FromConstitution builds a Row from a freshly-loaded Constitution.
// The CreatedAt is set from the load time; the ActivatedAt is
// stamped to "now" so the row records that the constitution was
// in use at the moment of persistence.
func FromConstitution(c *Constitution) *ConstitutionRow {
	if c == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	loadedAt := now
	if !c.LoadedAt.IsZero() {
		loadedAt = c.LoadedAt.UTC().Format(time.RFC3339Nano)
	}
	return &ConstitutionRow{
		ID:          c.Meta.ID,
		Version:     c.Meta.Version,
		Label:       c.Meta.Label,
		Source:      string(c.Source),
		FilePath:    c.FilePath,
		ParsedJSON:  c.ParsedJSON(),
		SHA256:      c.SHA256,
		Enabled:     true, // freshly loaded constitutions are active by default
		CreatedAt:   loadedAt,
		ActivatedAt: now,
	}
}

func isBuiltinSource(s string) bool {
	return s == string(SourceBuiltinLight) || s == string(SourceBuiltinDark)
}

// Store is the constitution persistence facade. It wraps a
// sqlExec. Methods are safe to call concurrently.
type Store struct {
	db sqlExec
}

// sqlExec is the minimal interface we need from a database. The
// mem.Store satisfies it (it has ExecContext, QueryRowContext,
// QueryContext). Defining the interface here keeps the
// constitution package decoupled from the mem package — tests can
// pass an in-memory fake without dragging the entire dark.db
// schema in.
type sqlExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// NewStore wraps a sqlExec. Typically called with mem.Store.DB()
// or the mem.Store itself (which forwards to its *sql.DB).
func NewStore(db sqlExec) *Store {
	return &Store{db: db}
}

// Save inserts or updates a constitution row. The created_at
// column is preserved on conflict; enabled is refreshed and
// activated_at is bumped to the current time so the audit trail
// shows the most recent activation.
func (s *Store) Save(ctx context.Context, row *ConstitutionRow) (int64, error) {
	if row == nil {
		return 0, fmt.Errorf("constitution: Save: nil")
	}
	if row.ID == "" || row.Version == "" {
		return 0, fmt.Errorf("constitution: Save: id and version are required")
	}
	if row.ParsedJSON == "" {
		return 0, fmt.Errorf("constitution: Save: parsed_json is empty (load the file first)")
	}
	if row.CreatedAt == "" {
		row.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if row.ActivatedAt == "" {
		row.ActivatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	const q = `
		INSERT INTO constitutions
			(constitution_id, version, label, source, file_path, parsed_json, sha256,
			 enabled, created_at, activated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(constitution_id, version) DO UPDATE SET
			label         = excluded.label,
			source        = excluded.source,
			file_path     = excluded.file_path,
			parsed_json   = excluded.parsed_json,
			sha256        = excluded.sha256,
			enabled       = excluded.enabled,
			activated_at  = excluded.activated_at
	`
	res, err := s.db.ExecContext(ctx, q,
		row.ID, row.Version,
		nullString(row.Label), row.Source, row.FilePath,
		row.ParsedJSON, row.SHA256, boolToInt(row.Enabled),
		row.CreatedAt, row.ActivatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("constitution: save: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the constitution Row for (id, version) or nil if absent.
func (s *Store) Get(ctx context.Context, id, version string) (*ConstitutionRow, error) {
	const q = `
		SELECT constitution_id, version, label, source, file_path,
		       parsed_json, sha256, enabled, created_at, activated_at
		FROM constitutions
		WHERE constitution_id = ? AND version = ?
	`
	row := s.db.QueryRowContext(ctx, q, id, version)
	return scanRow(row)
}

// List returns every constitution row, newest-first by created_at.
func (s *Store) List(ctx context.Context, limit int) ([]*ConstitutionRow, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
		SELECT constitution_id, version, label, source, file_path,
		       parsed_json, sha256, enabled, created_at, activated_at
		FROM constitutions
		ORDER BY created_at DESC
		LIMIT ?
	`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("constitution: list: %w", err)
	}
	defer rows.Close()

	var out []*ConstitutionRow
	for rows.Next() {
		r, err := scanRowImpl(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListByID returns every version of a given constitution_id,
// newest-first. Used to show the agent "this id has versions
// 1.0.0, 1.1.0, 2.0.0 — which do you want?".
func (s *Store) ListByID(ctx context.Context, id string) ([]*ConstitutionRow, error) {
	const q = `
		SELECT constitution_id, version, label, source, file_path,
		       parsed_json, sha256, enabled, created_at, activated_at
		FROM constitutions
		WHERE constitution_id = ?
		ORDER BY version DESC
	`
	rows, err := s.db.QueryContext(ctx, q, id)
	if err != nil {
		return nil, fmt.Errorf("constitution: list_by_id: %w", err)
	}
	defer rows.Close()

	var out []*ConstitutionRow
	for rows.Next() {
		r, err := scanRowImpl(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkActivated touches the activated_at column. Called when a
// constitution is selected as the active one so the audit trail
// shows "this is the constitution that was actually in use at
// startup time".
func (s *Store) MarkActivated(ctx context.Context, id, version string) error {
	const q = `UPDATE constitutions SET activated_at = ? WHERE constitution_id = ? AND version = ?`
	_, err := s.db.ExecContext(ctx, q, time.Now().UTC().Format(time.RFC3339Nano), id, version)
	if err != nil {
		return fmt.Errorf("constitution: mark_activated: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Row scanning.
// ---------------------------------------------------------------------------

// rowScanner is the common subset of *sql.Row and *sql.Rows we
// need for Scan. Lets the same code path handle Get and List.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(row *sql.Row) (*ConstitutionRow, error) {
	r, err := scanRowImpl(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func scanRowImpl(s rowScanner) (*ConstitutionRow, error) {
	var (
		r              ConstitutionRow
		label          sql.NullString
		activatedAtRaw sql.NullString
		enabledInt     int
		createdAt      string
	)
	err := s.Scan(
		&r.ID, &r.Version, &label, &r.Source, &r.FilePath,
		&r.ParsedJSON, &r.SHA256, &enabledInt, &createdAt, &activatedAtRaw,
	)
	if err != nil {
		return nil, err
	}
	if label.Valid {
		r.Label = label.String
	}
	r.Enabled = enabledInt != 0
	r.CreatedAt = createdAt
	if activatedAtRaw.Valid {
		r.ActivatedAt = activatedAtRaw.String
	}
	return &r, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
