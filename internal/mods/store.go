package mods

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Store is the persistence facade for the `mods` and `mod_loads`
// tables (added in migration v2). The registry writes mod_loads
// rows on every Activate/Deactivate; mod rows are upserted when a
// mod is first loaded (so the manifest and SHA-256 are
// auditable).
//
// The store wraps a sqlExec (any *sql.DB satisfies the interface)
// so tests can pass an in-memory fake without dragging the
// full dark.db schema in.
type Store struct {
	db    sqlExec
	ctx   context.Context
	clock func() time.Time
}

// sqlExec is the minimal interface we need from a database.
// Defined here rather than imported so the mods package stays
// decoupled from internal/mem.
type sqlExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// NewStore wraps a sqlExec. The ctx is used for every subsequent
// call; pass context.Background() if no parent is available.
// The clock is injectable for tests; production uses time.Now.
func NewStore(ctx context.Context, db sqlExec) *Store {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Store{
		db:    db,
		ctx:   ctx,
		clock: time.Now,
	}
}

// SetClock overrides the time source. Tests use this to make
// timestamps deterministic.
func (s *Store) SetClock(c func() time.Time) { s.clock = c }

// nowMillis is a helper used by the registry and store for
// monotonic millisecond durations.
func nowMillis() int64 {
	return time.Now().UnixMilli()
}

// SaveMod upserts a mod row. The (mod_id) is the natural key;
// version is recorded for audit but does not uniquely identify
// a row (replacing a mod means replacing the row).
func (s *Store) SaveMod(ctx context.Context, m *Loaded) error {
	if m == nil {
		return fmt.Errorf("mods: SaveMod: nil")
	}
	const q = `
		INSERT INTO mods (mod_id, name, version, source, manifest_json, sha256,
		                  risk_class, target_scope, requires_tor, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(mod_id) DO UPDATE SET
			name          = excluded.name,
			version       = excluded.version,
			source        = excluded.source,
			manifest_json = excluded.manifest_json,
			sha256        = excluded.sha256,
			risk_class    = excluded.risk_class,
			target_scope  = excluded.target_scope,
			requires_tor  = excluded.requires_tor,
			updated_at    = excluded.updated_at
	`
	now := s.clock().UTC().Format(time.RFC3339Nano)
	manifestJSON, _ := jsonMarshal(m.Manifest)
	_, err := s.db.ExecContext(ctx, q,
		m.Manifest.Meta.ID,
		m.Manifest.Meta.Name,
		m.Manifest.Meta.Version,
		string(m.Source),
		manifestJSON,
		m.SHA256,
		m.Manifest.Risk.Class,
		m.Manifest.Risk.TargetScope,
		boolToInt(m.Manifest.Risk.RequiresTor),
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("mods: save mod: %w", err)
	}
	return nil
}

// RecordLoad writes a mod_loads row. Called by the registry on
// every Activate (success or failure). Errors are returned to
// the caller; the registry logs them and continues.
//
// The capabilities_count is the total number of knowledge +
// directive files the mod contributed; duration_ms is the
// wall-clock time the loader took.
func (s *Store) RecordLoad(ctx context.Context, modID, sessionID string, durationMs, capabilities int, errStr, constitutionID string) error {
	const q = `
		INSERT INTO mod_loads (mod_id, session_id, loaded_at, duration_ms,
		                        capabilities_count, error, constitution_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	now := s.clock().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, q,
		modID,
		nullString(sessionID),
		now,
		durationMs,
		capabilities,
		nullString(errStr),
		nullString(constitutionID),
	)
	if err != nil {
		return fmt.Errorf("mods: record_load: %w", err)
	}
	return nil
}

// ListMods returns every mod row newest-first.
func (s *Store) ListMods(ctx context.Context, limit int) ([]*ModRow, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
		SELECT mod_id, name, version, source, manifest_json, sha256,
		       risk_class, target_scope, requires_tor, created_at, updated_at
		FROM mods
		ORDER BY created_at DESC
		LIMIT ?
	`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("mods: list: %w", err)
	}
	defer rows.Close()

	var out []*ModRow
	for rows.Next() {
		m, err := scanModRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListModLoads returns the most recent load events, newest-first.
// Optional filter by mod_id.
func (s *Store) ListModLoads(ctx context.Context, modID string, limit int) ([]*ModLoadRow, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `
		SELECT id, mod_id, session_id, loaded_at, duration_ms,
		       capabilities_count, error, constitution_id
		FROM mod_loads
	WHERE 1=1
	`
	args := []any{}
	if modID != "" {
		q += ` AND mod_id = ?`
		args = append(args, modID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mods: list_loads: %w", err)
	}
	defer rows.Close()

	var out []*ModLoadRow
	for rows.Next() {
		m, err := scanModLoadRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Row types — the persisted shape, separate from the in-memory
// Loaded type which carries the loaded content. The Row types
// are the minimum data the audit surface needs.
// ---------------------------------------------------------------------------

// ModRow is one row in the `mods` table.
type ModRow struct {
	ModID       string
	Name        string
	Version     string
	Source      string
	ManifestJSON string
	SHA256      string
	RiskClass   string
	TargetScope string
	RequiresTor bool
	CreatedAt   string
	UpdatedAt   string
}

// ModLoadRow is one row in the `mod_loads` table.
type ModLoadRow struct {
	ID                int64
	ModID             string
	SessionID         string
	LoadedAt          string
	DurationMs        int64
	CapabilitiesCount int
	Error             string
	ConstitutionID    string
}

// ---------------------------------------------------------------------------
// Row scanning.
// ---------------------------------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanModRow(s rowScanner) (*ModRow, error) {
	var (
		m          ModRow
		riskClass  sql.NullString
		scope      sql.NullString
		requiresT  int
		createdAt  string
		updatedAt  sql.NullString
	)
	if err := s.Scan(
		&m.ModID, &m.Name, &m.Version, &m.Source, &m.ManifestJSON, &m.SHA256,
		&riskClass, &scope, &requiresT, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	if riskClass.Valid {
		m.RiskClass = riskClass.String
	}
	if scope.Valid {
		m.TargetScope = scope.String
	}
	m.RequiresTor = requiresT != 0
	m.CreatedAt = createdAt
	if updatedAt.Valid {
		m.UpdatedAt = updatedAt.String
	}
	return &m, nil
}

func scanModLoadRow(s rowScanner) (*ModLoadRow, error) {
	var (
		m          ModLoadRow
		sessionID  sql.NullString
		errStr     sql.NullString
		constID    sql.NullString
	)
	if err := s.Scan(
		&m.ID, &m.ModID, &sessionID, &m.LoadedAt, &m.DurationMs,
		&m.CapabilitiesCount, &errStr, &constID,
	); err != nil {
		return nil, err
	}
	if sessionID.Valid {
		m.SessionID = sessionID.String
	}
	if errStr.Valid {
		m.Error = errStr.String
	}
	if constID.Valid {
		m.ConstitutionID = constID.String
	}
	return &m, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b != true {
		return 0
	}
	return 1
}

// jsonMarshal serializes v to a JSON string. Used to populate
// the manifest_json column. Returns "" on Marshal failure
// (caller treats as error).
func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
