package mem

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// SaveRun persists a research run + its items in one transaction.
// Returns the assigned run ID. If the run is empty (no items, no
// backend_used), it is still saved for audit purposes.
func (s *Store) SaveRun(ctx context.Context, run *ResearchRun) (int64, error) {
	if run == nil {
		return 0, fmt.Errorf("mem: nil run")
	}
	if run.CreatedAt == "" {
		run.CreatedAt = Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("mem: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	btJSON := jsonMustMarshal(run.BackendsTried)
	errsJSON := jsonMustMarshal(run.Errors)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO research_runs
		 (session_id, query, intent, backend_used, backends_tried,
		  took_ms, confidence_avg, items_count, errors, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullString(run.SessionID), run.Query, run.Intent,
		nullString(run.BackendUsed), btJSON, run.TookMs,
		run.ConfidenceAvg, len(run.Items), errsJSON, run.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("mem: insert run: %w", err)
	}
	runID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("mem: last id: %w", err)
	}

	for i := range run.Items {
		item := &run.Items[i]
		if item.CreatedAt == "" {
			item.CreatedAt = run.CreatedAt
		}
		item.RunID = runID
		rawJSON := item.Raw
		if rawJSON == "" && item.Raw != "" {
			rawJSON = jsonMustMarshal(item.Raw)
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO research_items
			 (run_id, title, url, snippet, source, confidence,
			  freshness_at, lang, raw, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			runID, item.Title, nullString(item.URL), nullString(item.Snippet),
			item.Source, item.Confidence, nullString(item.FreshnessAt),
			nullString(item.Lang), nullString(rawJSON), item.CreatedAt,
		)
		if err != nil {
			return 0, fmt.Errorf("mem: insert item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("mem: commit: %w", err)
	}
	return runID, nil
}

// Recall returns items whose title, snippet, or source contains the
// query substring (case-insensitive). Optional filters narrow by intent
// or source. Results are ordered by recency desc.
func (s *Store) Recall(ctx context.Context, query string, filterIntent, filterSource string, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 20
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	like := "%" + strings.ToLower(q) + "%"

	var (
		rows *rows
		err  error
	)
	_ = rows
	_ = err

	sqlStr := `SELECT i.id, i.run_id, i.title, i.url, i.snippet, i.source,
	                 i.confidence, i.freshness_at, i.lang, i.raw, i.created_at
	          FROM research_items i
	          JOIN research_runs r ON r.id = i.run_id
	          WHERE (LOWER(i.title) LIKE ? OR LOWER(i.snippet) LIKE ? OR LOWER(i.source) LIKE ?)`
	args := []any{like, like, like}
	if filterIntent != "" {
		sqlStr += ` AND r.intent = ?`
		args = append(args, filterIntent)
	}
	if filterSource != "" {
		sqlStr += ` AND i.source = ?`
		args = append(args, filterSource)
	}
	sqlStr += ` ORDER BY i.id DESC LIMIT ?`
	args = append(args, limit)

	r, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("mem: recall query: %w", err)
	}
	defer r.Close()

	out := []Item{}
	for r.Next() {
		var it Item
		var (
			urlNS, snippetNS, freshNS, langNS, rawNS, createdNS sql.NullString
			conf                                                sql.NullFloat64
		)
		if err := r.Scan(&it.ID, &it.RunID, &it.Title, &urlNS, &snippetNS, &it.Source,
			&conf, &freshNS, &langNS, &rawNS, &createdNS); err != nil {
			return nil, fmt.Errorf("mem: recall scan: %w", err)
		}
		if urlNS.Valid {
			it.URL = urlNS.String
		}
		if snippetNS.Valid {
			it.Snippet = snippetNS.String
		}
		if conf.Valid {
			it.Confidence = float32(conf.Float64)
		}
		if freshNS.Valid {
			it.FreshnessAt = freshNS.String
		}
		if langNS.Valid {
			it.Lang = langNS.String
		}
		if rawNS.Valid {
			it.Raw = rawNS.String
		}
		if createdNS.Valid {
			it.CreatedAt = createdNS.String
		}
		out = append(out, it)
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Status returns aggregate stats over the research tables.
func (s *Store) Status(ctx context.Context) (*Status, error) {
	st := &Status{
		IntentHistogram: map[string]int{},
		SourceHistogram: map[string]int{},
	}

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM research_runs`).Scan(&st.RunsTotal); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM research_items`).Scan(&st.ItemsTotal); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM research_links`).Scan(&st.LinksTotal); err != nil {
		return nil, err
	}

	r, err := s.db.QueryContext(ctx, `SELECT intent, COUNT(*) FROM research_runs GROUP BY intent`)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var k string
		var n int
		if err := r.Scan(&k, &n); err != nil {
			r.Close()
			return nil, err
		}
		st.IntentHistogram[k] = n
	}
	r.Close()

	r, err = s.db.QueryContext(ctx, `SELECT source, COUNT(*) FROM research_items GROUP BY source`)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var k string
		var n int
		if err := r.Scan(&k, &n); err != nil {
			r.Close()
			return nil, err
		}
		st.SourceHistogram[k] = n
	}
	r.Close()

	r, err = s.db.QueryContext(ctx, `SELECT MIN(created_at), MAX(created_at) FROM research_runs`)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var oldest, newest sql.NullString
		if err := r.Scan(&oldest, &newest); err != nil {
			r.Close()
			return nil, err
		}
		if oldest.Valid {
			st.OldestRun = oldest.String
		}
		if newest.Valid {
			st.NewestRun = newest.String
		}
	}
	r.Close()

	return st, nil
}

// LinkResearchToAttack creates a research_link row connecting a
// persisted research item to a dark-eval attack. target_type can be
// 'attack', 'cve', 'technique', 'paper'.
func (s *Store) LinkResearchToAttack(ctx context.Context, itemID int64, targetType, targetID, note string) error {
	if itemID == 0 || targetType == "" || targetID == "" {
		return fmt.Errorf("mem: link requires item_id, target_type, target_id")
	}
	_, err := s.Exec(ctx,
		`INSERT INTO research_links (research_item_id, target_type, target_id, note, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		itemID, targetType, targetID, nullString(note), Now())
	return err
}

// LatestRunByQuery returns the most recent run for a query string, or
// nil if not found. Used by the router for cache hits.
func (s *Store) LatestRunByQuery(ctx context.Context, query, intent string) (*ResearchRun, error) {
	var row = s.db.QueryRowContext
	r := row(ctx,
		`SELECT id, session_id, query, intent, backend_used, backends_tried,
		        took_ms, confidence_avg, errors, created_at
		 FROM research_runs
		 WHERE query = ? AND intent = ?
		 ORDER BY id DESC LIMIT 1`, query, intent)
	var run ResearchRun
	var btJSON, errsJSON sql.NullString
	var sessionID, backendUsed sql.NullString
	if err := r.Scan(&run.ID, &sessionID, &run.Query, &run.Intent, &backendUsed, &btJSON,
		&run.TookMs, &run.ConfidenceAvg, &errsJSON, &run.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if sessionID.Valid {
		run.SessionID = sessionID.String
	}
	if backendUsed.Valid {
		run.BackendUsed = backendUsed.String
	}
	if btJSON.Valid && btJSON.String != "" {
		_ = json.Unmarshal([]byte(btJSON.String), &run.BackendsTried)
	}
	if errsJSON.Valid && errsJSON.String != "" {
		_ = json.Unmarshal([]byte(errsJSON.String), &run.Errors)
	}
	return &run, nil
}

// ListResearchRuns returns research runs newest-first with optional
// intent filter. Limit defaults to 50. Used by the agent to audit prior
// research threads (e.g. "show me the last 20 dark_research runs").
func (s *Store) ListResearchRuns(ctx context.Context, intent string, limit int) ([]ResearchRun, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, session_id, query, intent, backend_used, backends_tried,
	             took_ms, confidence_avg, errors, created_at
	      FROM research_runs WHERE 1=1`
	args := []any{}
	if intent != "" {
		q += ` AND intent = ?`
		args = append(args, intent)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mem: list research_runs: %w", err)
	}
	defer rows.Close()

	out := []ResearchRun{}
	for rows.Next() {
		var run ResearchRun
		var btJSON, errsJSON, sessionID, backendUsed sql.NullString
		if err := rows.Scan(&run.ID, &sessionID, &run.Query, &run.Intent, &backendUsed, &btJSON,
			&run.TookMs, &run.ConfidenceAvg, &errsJSON, &run.CreatedAt); err != nil {
			return nil, fmt.Errorf("mem: scan research_run row: %w", err)
		}
		if sessionID.Valid {
			run.SessionID = sessionID.String
		}
		if backendUsed.Valid {
			run.BackendUsed = backendUsed.String
		}
		if btJSON.Valid && btJSON.String != "" {
			_ = json.Unmarshal([]byte(btJSON.String), &run.BackendsTried)
		}
		if errsJSON.Valid && errsJSON.String != "" {
			_ = json.Unmarshal([]byte(errsJSON.String), &run.Errors)
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// ListResearchItems returns research items newest-first, optionally
// filtered by source (e.g. 'osv.dev') and/or run_id. Limit defaults to
// 50. Useful for "show me every CVE I found earlier today" without
// running the recall substring query.
func (s *Store) ListResearchItems(ctx context.Context, runID int64, source string, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, run_id, title, url, snippet, source, confidence,
	             freshness_at, lang, raw, created_at
	      FROM research_items WHERE 1=1`
	args := []any{}
	if runID > 0 {
		q += ` AND run_id = ?`
		args = append(args, runID)
	}
	if source != "" {
		q += ` AND source = ?`
		args = append(args, source)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mem: list research_items: %w", err)
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		var it Item
		var (
			urlNS, snippetNS, freshNS, langNS, rawNS, createdNS sql.NullString
			conf                                                 sql.NullFloat64
		)
		if err := rows.Scan(&it.ID, &it.RunID, &it.Title, &urlNS, &snippetNS, &it.Source,
			&conf, &freshNS, &langNS, &rawNS, &createdNS); err != nil {
			return nil, fmt.Errorf("mem: scan research_item row: %w", err)
		}
		if urlNS.Valid {
			it.URL = urlNS.String
		}
		if snippetNS.Valid {
			it.Snippet = snippetNS.String
		}
		if conf.Valid {
			it.Confidence = float32(conf.Float64)
		}
		if freshNS.Valid {
			it.FreshnessAt = freshNS.String
		}
		if langNS.Valid {
			it.Lang = langNS.String
		}
		if rawNS.Valid {
			it.Raw = rawNS.String
		}
		if createdNS.Valid {
			it.CreatedAt = createdNS.String
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// nullString returns the string itself or "" if empty (so the DB
// driver stores NULL cleanly).
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// rows is an alias kept for symmetry with future typed-row helpers.
type rows = interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}