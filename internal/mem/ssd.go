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
//
// v3 added five optional columns (constitution_id, constitution_version,
// active_mods_json, refused_attempts, refusal_pattern). Pre-v3 callers
// (no fields set) continue to work — the new columns are populated
// with NULL / 0. Post-v3 callers (the constitution-aware SSD tools in
// Fase 1+) populate them from the active constitution + mod context.
// refused_attempts defaults to 0 at the DB layer; the int zero value
// maps cleanly to that.
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
		`INSERT INTO sdd_evaluations (
			eval_type, target_type, target_id, verdict_json, confidence,
			prompt_version, model, created_at,
			constitution_id, constitution_version, active_mods_json,
			refused_attempts, refusal_pattern
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.EvalType, e.TargetType, e.TargetID, e.VerdictJSON, e.Confidence,
		nullString(e.PromptVersion), nullString(e.Model), e.CreatedAt,
		nullString(e.ConstitutionID), nullString(e.ConstitutionVersion),
		nullString(e.ActiveModsJSON), e.RefusedAttempts, nullString(e.RefusalPattern),
	)
	if err != nil {
		return 0, fmt.Errorf("mem: insert sdd_evaluation: %w", err)
	}
	return res.LastInsertId()
}

// LatestSDDEvaluation returns the most recent evaluation for a target.
func (s *Store) LatestSDDEvaluation(ctx context.Context, evalType, targetType, targetID string) (*SDDEvaluation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, eval_type, target_type, target_id, verdict_json, confidence,
		        prompt_version, model, created_at,
		        constitution_id, constitution_version, active_mods_json,
		        refused_attempts, refusal_pattern
		 FROM sdd_evaluations
		 WHERE eval_type = ? AND target_type = ? AND target_id = ?
		 ORDER BY id DESC LIMIT 1`, evalType, targetType, targetID)
	var e SDDEvaluation
	var promptVersion, model, constitutionID, constitutionVersion, activeModsJSON, refusalPattern sql.NullString
	if err := row.Scan(
		&e.ID, &e.EvalType, &e.TargetType, &e.TargetID, &e.VerdictJSON, &e.Confidence,
		&promptVersion, &model, &e.CreatedAt,
		&constitutionID, &constitutionVersion, &activeModsJSON,
		&e.RefusedAttempts, &refusalPattern,
	); err != nil {
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
	if constitutionID.Valid {
		e.ConstitutionID = constitutionID.String
	}
	if constitutionVersion.Valid {
		e.ConstitutionVersion = constitutionVersion.String
	}
	if activeModsJSON.Valid {
		e.ActiveModsJSON = activeModsJSON.String
	}
	if refusalPattern.Valid {
		e.RefusalPattern = refusalPattern.String
	}
	return &e, nil
}

// ListSDDEvaluations returns up to limit evaluations for a target type
// (newest first). Used by the agent to audit past judgments.
func (s *Store) ListSDDEvaluations(ctx context.Context, evalType, targetType string, limit int) ([]SDDEvaluation, error) {
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT id, eval_type, target_type, target_id, verdict_json, confidence,
	             prompt_version, model, created_at,
	             constitution_id, constitution_version, active_mods_json,
	             refused_attempts, refusal_pattern
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
		var promptVersion, model, constitutionID, constitutionVersion, activeModsJSON, refusalPattern sql.NullString
		if err := rows.Scan(
			&e.ID, &e.EvalType, &e.TargetType, &e.TargetID, &e.VerdictJSON, &e.Confidence,
			&promptVersion, &model, &e.CreatedAt,
			&constitutionID, &constitutionVersion, &activeModsJSON,
			&e.RefusedAttempts, &refusalPattern,
		); err != nil {
			return nil, err
		}
		if promptVersion.Valid {
			e.PromptVersion = promptVersion.String
		}
		if model.Valid {
			e.Model = model.String
		}
		if constitutionID.Valid {
			e.ConstitutionID = constitutionID.String
		}
		if constitutionVersion.Valid {
			e.ConstitutionVersion = constitutionVersion.String
		}
		if activeModsJSON.Valid {
			e.ActiveModsJSON = activeModsJSON.String
		}
		if refusalPattern.Valid {
			e.RefusalPattern = refusalPattern.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}