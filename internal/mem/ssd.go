package mem

import (
	"context"
	"database/sql"
	"fmt"
)

// ---------------------------------------------------------------------------
// dark-ssd CRUD. The LLM-as-judge tools in dark-research-mcp wrap these
// methods: fetch context from mem, call the LLM, then persist the
// verdict so it's auditable.
// ---------------------------------------------------------------------------

// SaveSDDEvaluation inserts an LLM-as-judge verdict and returns its ID.
func (s *Store) SaveSDDEvaluation(ctx context.Context, e *SDDEvaluation) (int64, error) {
	if e == nil || e.EvalType == "" || e.TargetType == "" || e.TargetID == "" {
		return 0, fmt.Errorf("mem: sdd_evaluation requires eval_type + target_type + target_id")
	}
	if e.CreatedAt == "" {
		e.CreatedAt = Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sdd_evaluations (eval_type, target_type, target_id, verdict_json, confidence, prompt_version, model, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.EvalType, e.TargetType, e.TargetID, e.VerdictJSON, e.Confidence,
		nullString(e.PromptVersion), nullString(e.Model), e.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("mem: insert sdd_evaluation: %w", err)
	}
	return res.LastInsertId()
}

// LatestSDDEvaluation returns the most recent evaluation for a target.
func (s *Store) LatestSDDEvaluation(ctx context.Context, evalType, targetType, targetID string) (*SDDEvaluation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, eval_type, target_type, target_id, verdict_json, confidence, prompt_version, model, created_at
		 FROM sdd_evaluations
		 WHERE eval_type = ? AND target_type = ? AND target_id = ?
		 ORDER BY id DESC LIMIT 1`, evalType, targetType, targetID)
	var e SDDEvaluation
	var promptVersion, model sql.NullString
	if err := row.Scan(&e.ID, &e.EvalType, &e.TargetType, &e.TargetID, &e.VerdictJSON, &e.Confidence, &promptVersion, &model, &e.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("mem: scan sdd_evaluation: %w", err)
	}
	if promptVersion.Valid {
		e.PromptVersion = promptVersion.String
	}
	if model.Valid {
		e.Model = model.String
	}
	return &e, nil
}

// ListSDDEvaluations returns up to limit evaluations for a target type
// (newest first). Used by the agent to audit past judgments.
func (s *Store) ListSDDEvaluations(ctx context.Context, evalType, targetType string, limit int) ([]SDDEvaluation, error) {
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT id, eval_type, target_type, target_id, verdict_json, confidence, prompt_version, model, created_at
	      FROM sdd_evaluations WHERE 1=1`
	args := []any{}
	if evalType != "" {
		q += ` AND eval_type = ?`
		args = append(args, evalType)
	}
	if targetType != "" {
		q += ` AND target_type = ?`
		args = append(args, targetType)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mem: list sdd_evaluations: %w", err)
	}
	defer rows.Close()

	out := []SDDEvaluation{}
	for rows.Next() {
		var e SDDEvaluation
		var promptVersion, model sql.NullString
		if err := rows.Scan(&e.ID, &e.EvalType, &e.TargetType, &e.TargetID, &e.VerdictJSON, &e.Confidence, &promptVersion, &model, &e.CreatedAt); err != nil {
			return nil, err
		}
		if promptVersion.Valid {
			e.PromptVersion = promptVersion.String
		}
		if model.Valid {
			e.Model = model.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}