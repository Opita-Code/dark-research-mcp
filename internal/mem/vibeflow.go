package mem

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// vibe-flow CRUD. The pattern matches research_* methods: write methods use
// the store mutex; read methods are concurrent-safe; SQL is plain with
// IF NOT EXISTS guarantees from schema.go.
// ---------------------------------------------------------------------------

// SaveSpec inserts a spec and returns its assigned ID. UpdatedAt is
// stamped automatically if empty.
func (s *Store) SaveSpec(ctx context.Context, sp *Spec) (int64, error) {
	if sp == nil || sp.VibeCase == "" {
		return 0, fmt.Errorf("mem: spec requires vibe_case")
	}
	if sp.CreatedAt == "" {
		sp.CreatedAt = Now()
	}
	if sp.UpdatedAt == "" {
		sp.UpdatedAt = sp.CreatedAt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO vibe_specs (vibe_case, session_id, constitution_json, spec_json, tasks_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sp.VibeCase, nullString(sp.SessionID), nullString(sp.Constitution),
		nullString(sp.Spec), nullString(sp.Tasks), sp.CreatedAt, sp.UpdatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("mem: insert spec: %w", err)
	}
	return res.LastInsertId()
}

// GetSpec loads a spec by ID.
func (s *Store) GetSpec(ctx context.Context, id int64) (*Spec, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, vibe_case, session_id, constitution_json, spec_json, tasks_json, created_at, updated_at
		 FROM vibe_specs WHERE id = ?`, id)
	var sp Spec
	var sessionID, constitution, specJSON, tasks, updatedAt sql.NullString
	if err := row.Scan(&sp.ID, &sp.VibeCase, &sessionID, &constitution, &specJSON, &tasks, &sp.CreatedAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("mem: scan spec: %w", err)
	}
	if sessionID.Valid {
		sp.SessionID = sessionID.String
	}
	if constitution.Valid {
		sp.Constitution = constitution.String
	}
	if specJSON.Valid {
		sp.Spec = specJSON.String
	}
	if tasks.Valid {
		sp.Tasks = tasks.String
	}
	if updatedAt.Valid {
		sp.UpdatedAt = updatedAt.String
	}
	return &sp, nil
}

// SaveBrandGuide upserts a brand guide (PRIMARY KEY on brand_id).
func (s *Store) SaveBrandGuide(ctx context.Context, b *BrandGuide) error {
	if b == nil || b.BrandID == "" {
		return fmt.Errorf("mem: brand guide requires brand_id")
	}
	if b.CreatedAt == "" {
		b.CreatedAt = Now()
	}
	b.UpdatedAt = Now()
	_, err := s.Exec(ctx,
		`INSERT INTO vibe_brands (brand_id, voice_json, visual_json, narrative_json, compliance_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(brand_id) DO UPDATE SET
		   voice_json = excluded.voice_json,
		   visual_json = excluded.visual_json,
		   narrative_json = excluded.narrative_json,
		   compliance_json = excluded.compliance_json,
		   updated_at = excluded.updated_at`,
		b.BrandID, nullString(b.Voice), nullString(b.Visual),
		nullString(b.Narrative), nullString(b.Compliance), b.CreatedAt, b.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("mem: upsert brand_guide: %w", err)
	}
	return nil
}

// GetBrandGuide loads a brand guide by brand_id.
func (s *Store) GetBrandGuide(ctx context.Context, brandID string) (*BrandGuide, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT brand_id, voice_json, visual_json, narrative_json, compliance_json, created_at, updated_at
		 FROM vibe_brands WHERE brand_id = ?`, brandID)
	var b BrandGuide
	var voice, visual, narrative, compliance, updatedAt sql.NullString
	if err := row.Scan(&b.BrandID, &voice, &visual, &narrative, &compliance, &b.CreatedAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("mem: scan brand_guide: %w", err)
	}
	if voice.Valid {
		b.Voice = voice.String
	}
	if visual.Valid {
		b.Visual = visual.String
	}
	if narrative.Valid {
		b.Narrative = narrative.String
	}
	if compliance.Valid {
		b.Compliance = compliance.String
	}
	if updatedAt.Valid {
		b.UpdatedAt = updatedAt.String
	}
	return &b, nil
}

// SaveComplianceRule upserts a rule (PRIMARY KEY on jurisdiction).
func (s *Store) SaveComplianceRule(ctx context.Context, r *ComplianceRule) error {
	if r == nil || r.Jurisdiction == "" || r.Rules == "" {
		return fmt.Errorf("mem: compliance rule requires jurisdiction + rules")
	}
	if r.CreatedAt == "" {
		r.CreatedAt = Now()
	}
	_, err := s.Exec(ctx,
		`INSERT INTO vibe_compliance (jurisdiction, rules_json, effective_at, source_url, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(jurisdiction) DO UPDATE SET
		   rules_json = excluded.rules_json,
		   effective_at = excluded.effective_at,
		   source_url = excluded.source_url`,
		r.Jurisdiction, r.Rules, nullString(r.EffectiveAt), nullString(r.SourceURL), r.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("mem: upsert compliance_rule: %w", err)
	}
	return nil
}

// GetComplianceRule loads a rule by jurisdiction.
func (s *Store) GetComplianceRule(ctx context.Context, jurisdiction string) (*ComplianceRule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT jurisdiction, rules_json, effective_at, source_url, created_at
		 FROM vibe_compliance WHERE jurisdiction = ?`, jurisdiction)
	var r ComplianceRule
	var rules, effectiveAt, sourceURL sql.NullString
	if err := row.Scan(&r.Jurisdiction, &rules, &effectiveAt, &sourceURL, &r.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("mem: scan compliance_rule: %w", err)
	}
	if rules.Valid {
		r.Rules = rules.String
	}
	if effectiveAt.Valid {
		r.EffectiveAt = effectiveAt.String
	}
	if sourceURL.Valid {
		r.SourceURL = sourceURL.String
	}
	return &r, nil
}

// SaveArtifact inserts an artifact and returns its ID.
func (s *Store) SaveArtifact(ctx context.Context, a *Artifact) (int64, error) {
	if a == nil || a.VibeCase == "" || a.ArtifactType == "" {
		return 0, fmt.Errorf("mem: artifact requires vibe_case + artifact_type")
	}
	if a.CreatedAt == "" {
		a.CreatedAt = Now()
	}
	if a.ValidationStatus == "" {
		a.ValidationStatus = "pending"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO vibe_artifacts (session_id, vibe_case, spec_id, artifact_url, artifact_type, brand_id, jurisdiction, has_disclosure, validation_status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullString(a.SessionID), a.VibeCase, nullInt(a.SpecID), nullString(a.ArtifactURL),
		a.ArtifactType, nullString(a.BrandID), nullString(a.Jurisdiction),
		boolToInt(a.HasDisclosure), a.ValidationStatus, a.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("mem: insert artifact: %w", err)
	}
	return res.LastInsertId()
}

// GetArtifact loads an artifact by ID.
func (s *Store) GetArtifact(ctx context.Context, id int64) (*Artifact, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, vibe_case, spec_id, artifact_url, artifact_type, brand_id, jurisdiction, has_disclosure, validation_status, created_at
		 FROM vibe_artifacts WHERE id = ?`, id)
	var a Artifact
	var (
		sessionID, artifactURL, brandID, jurisdiction sql.NullString
		specID, hasDisclosure                              sql.NullInt64
	)
	if err := row.Scan(&a.ID, &sessionID, &a.VibeCase, &specID, &artifactURL, &a.ArtifactType,
		&brandID, &jurisdiction, &hasDisclosure, &a.ValidationStatus, &a.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("mem: scan artifact: %w", err)
	}
	if sessionID.Valid {
		a.SessionID = sessionID.String
	}
	if specID.Valid {
		a.SpecID = specID.Int64
	}
	if artifactURL.Valid {
		a.ArtifactURL = artifactURL.String
	}
	if brandID.Valid {
		a.BrandID = brandID.String
	}
	if jurisdiction.Valid {
		a.Jurisdiction = jurisdiction.String
	}
	if hasDisclosure.Valid && hasDisclosure.Int64 != 0 {
		a.HasDisclosure = true
	}
	return &a, nil
}

// SetArtifactValidation updates the validation_status of an artifact
// (e.g., after a drift check resolves).
func (s *Store) SetArtifactValidation(ctx context.Context, id int64, status string) error {
	_, err := s.Exec(ctx, `UPDATE vibe_artifacts SET validation_status = ? WHERE id = ?`, status, id)
	return err
}

// SaveDriftReport inserts a drift report and returns its ID. Sets
// reconciled_at on the linked artifact when verdict is "aligned".
func (s *Store) SaveDriftReport(ctx context.Context, d *DriftReport) (int64, error) {
	if d == nil || d.ArtifactID == 0 || d.Verdict == "" {
		return 0, fmt.Errorf("mem: drift report requires artifact_id + verdict")
	}
	if d.CreatedAt == "" {
		d.CreatedAt = Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO vibe_drift_reports (artifact_id, spec_id, verdict, spec_diff_json, judge_reasoning, reconciled_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.ArtifactID, nullInt(d.SpecID), d.Verdict,
		nullString(d.SpecDiff), nullString(d.JudgeReasoning),
		nullString(d.ReconciledAt), d.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("mem: insert drift_report: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	// Side effect: when drift is reconciled, mark artifact as passed.
	if d.ReconciledAt != "" {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE vibe_artifacts SET validation_status = 'passed' WHERE id = ?`,
			d.ArtifactID,
		); err != nil {
			return id, fmt.Errorf("mem: update artifact status: %w", err)
		}
	}

	return id, nil
}

// UpdateSpec mutates an existing spec by ID. Any non-empty field is
// updated; empty fields are left unchanged. updated_at is bumped to now.
//
// Returns ErrNotFound (sql.ErrNoRows from Exec) if no row matches id.
func (s *Store) UpdateSpec(ctx context.Context, id int64, sp *Spec) error {
	if id == 0 {
		return fmt.Errorf("mem: spec id required")
	}
	if sp == nil {
		return fmt.Errorf("mem: nil spec")
	}
	sp.UpdatedAt = Now()
	res, err := s.Exec(ctx,
		`UPDATE vibe_specs SET
		   vibe_case = COALESCE(NULLIF(?, ''), vibe_case),
		   session_id = COALESCE(NULLIF(?, ''), session_id),
		   constitution_json = COALESCE(NULLIF(?, ''), constitution_json),
		   spec_json = COALESCE(NULLIF(?, ''), spec_json),
		   tasks_json = COALESCE(NULLIF(?, ''), tasks_json),
		   updated_at = ?
		 WHERE id = ?`,
		sp.VibeCase, sp.SessionID, sp.Constitution, sp.Spec, sp.Tasks, sp.UpdatedAt, id,
	)
	if err != nil {
		return fmt.Errorf("mem: update spec: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteSpec removes a spec by ID. Drift reports that reference this
// spec have their spec_id set to NULL (ON DELETE SET NULL).
func (s *Store) DeleteSpec(ctx context.Context, id int64) error {
	res, err := s.Exec(ctx, `DELETE FROM vibe_specs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mem: delete spec: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ArtifactUpdate is a partial update for an artifact. Nil pointer = leave
// unchanged; non-nil pointer = set to that value. This lets the caller
// distinguish "leave alone" from "set to false" or "set to 0", which a
// plain Artifact struct cannot do.
type ArtifactUpdate struct {
	SessionID        *string
	SpecID           *int64
	ArtifactURL      *string
	BrandID          *string
	Jurisdiction     *string
	HasDisclosure    *bool
	ValidationStatus *string
}

// UpdateArtifact mutates an existing artifact by ID. Each field is
// optional; nil pointer means "leave unchanged". Useful to fix a typo'd
// URL, attach a missing jurisdiction, or mark has_disclosure=true after
// the fact.
func (s *Store) UpdateArtifact(ctx context.Context, id int64, u *ArtifactUpdate) error {
	if id == 0 {
		return fmt.Errorf("mem: artifact id required")
	}
	if u == nil {
		return fmt.Errorf("mem: nil update")
	}
	res, err := s.Exec(ctx,
		`UPDATE vibe_artifacts SET
		   session_id = COALESCE(?, session_id),
		   spec_id = COALESCE(?, spec_id),
		   artifact_url = COALESCE(?, artifact_url),
		   brand_id = COALESCE(?, brand_id),
		   jurisdiction = COALESCE(?, jurisdiction),
		   has_disclosure = COALESCE(?, has_disclosure),
		   validation_status = COALESCE(?, validation_status)
		 WHERE id = ?`,
		u.SessionID, u.SpecID, u.ArtifactURL, u.BrandID, u.Jurisdiction,
		u.HasDisclosure, u.ValidationStatus, id,
	)
	if err != nil {
		return fmt.Errorf("mem: update artifact: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteArtifact removes an artifact by ID. Drift reports referencing
// this artifact are removed via ON DELETE CASCADE.
func (s *Store) DeleteArtifact(ctx context.Context, id int64) error {
	res, err := s.Exec(ctx, `DELETE FROM vibe_artifacts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mem: delete artifact: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteBrandGuide removes a brand guide by id. Idempotent: removing an
// absent brand returns nil (vs DeleteSpec/DeleteArtifact which return
// sql.ErrNoRows). This is intentional — brand guides are reference data
// the agent registers up-front; "ensure absent" is a common operation.
func (s *Store) DeleteBrandGuide(ctx context.Context, brandID string) error {
	if err := validateName(brandID); err != nil {
		return err
	}
	_, err := s.Exec(ctx, `DELETE FROM vibe_brands WHERE brand_id = ?`, brandID)
	if err != nil {
		return fmt.Errorf("mem: delete brand_guide: %w", err)
	}
	return nil
}

// validateName rejects names that would break our brand_id / jurisdiction
// primary keys (no slashes, no null bytes, no whitespace). Used by both
// SaveBrandGuide and DeleteBrandGuide.
func validateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("mem: name is empty")
	}
	if strings.ContainsAny(name, "/\\\x00\n\r") {
		return fmt.Errorf("mem: name contains invalid characters")
	}
	return nil
}

// LatestDriftForArtifact returns the most recent drift report for an
// artifact, or nil if none exists.
func (s *Store) LatestDriftForArtifact(ctx context.Context, artifactID int64) (*DriftReport, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, artifact_id, spec_id, verdict, spec_diff_json, judge_reasoning, reconciled_at, created_at
		 FROM vibe_drift_reports WHERE artifact_id = ? ORDER BY id DESC LIMIT 1`, artifactID)
	var d DriftReport
	var (
		specID, specDiff, reasoning, reconciled               sql.NullString
	)
	if err := row.Scan(&d.ID, &d.ArtifactID, &specID, &d.Verdict, &specDiff, &reasoning, &reconciled, &d.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("mem: scan drift_report: %w", err)
	}
	if specID.Valid {
		d.SpecID = parseInt(specID.String)
	}
	if specDiff.Valid {
		d.SpecDiff = specDiff.String
	}
	if reasoning.Valid {
		d.JudgeReasoning = reasoning.String
	}
	if reconciled.Valid {
		d.ReconciledAt = reconciled.String
	}
	return &d, nil
}

// ---------------------------------------------------------------------------
// List endpoints. Newest-first, with optional filter by case / session /
// verdict / status. Used by the agent to audit prior work (e.g. "show me
// every artifact flagged drift_detected this session").
//
// We use string concatenation with WHERE 1=1 + AND ? fragments; this is
// safe because all user inputs are bound as parameters (never string-
// substituted). SQL injection is structurally impossible.
// ---------------------------------------------------------------------------

// ListSpecs returns specs newest-first, optionally filtered by vibe_case
// and/or session_id. Limit defaults to 50 when zero.
func (s *Store) ListSpecs(ctx context.Context, vibeCase, sessionID string, limit int) ([]Spec, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, vibe_case, session_id, constitution_json, spec_json, tasks_json, created_at, updated_at
	      FROM vibe_specs WHERE 1=1`
	args := []any{}
	if vibeCase != "" {
		q += ` AND vibe_case = ?`
		args = append(args, vibeCase)
	}
	if sessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mem: list specs: %w", err)
	}
	defer rows.Close()

	out := []Spec{}
	for rows.Next() {
		var sp Spec
		var sessionIDNS, constitution, specJSON, tasks, updatedAt sql.NullString
		if err := rows.Scan(&sp.ID, &sp.VibeCase, &sessionIDNS, &constitution, &specJSON, &tasks, &sp.CreatedAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("mem: scan spec row: %w", err)
		}
		if sessionIDNS.Valid {
			sp.SessionID = sessionIDNS.String
		}
		if constitution.Valid {
			sp.Constitution = constitution.String
		}
		if specJSON.Valid {
			sp.Spec = specJSON.String
		}
		if tasks.Valid {
			sp.Tasks = tasks.String
		}
		if updatedAt.Valid {
			sp.UpdatedAt = updatedAt.String
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ListBrandGuides returns all brand guides newest-first (no filter — the
// table is small). Limit defaults to 100 when zero.
func (s *Store) ListBrandGuides(ctx context.Context, limit int) ([]BrandGuide, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT brand_id, voice_json, visual_json, narrative_json, compliance_json, created_at, updated_at
		 FROM vibe_brands ORDER BY updated_at DESC, brand_id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("mem: list brand_guides: %w", err)
	}
	defer rows.Close()

	out := []BrandGuide{}
	for rows.Next() {
		var b BrandGuide
		var voice, visual, narrative, compliance, updatedAt sql.NullString
		if err := rows.Scan(&b.BrandID, &voice, &visual, &narrative, &compliance, &b.CreatedAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("mem: scan brand_guide row: %w", err)
		}
		if voice.Valid {
			b.Voice = voice.String
		}
		if visual.Valid {
			b.Visual = visual.String
		}
		if narrative.Valid {
			b.Narrative = narrative.String
		}
		if compliance.Valid {
			b.Compliance = compliance.String
		}
		if updatedAt.Valid {
			b.UpdatedAt = updatedAt.String
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListComplianceRules returns all compliance rules newest-first (table is
// small — one row per jurisdiction). Limit defaults to 50.
func (s *Store) ListComplianceRules(ctx context.Context, limit int) ([]ComplianceRule, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT jurisdiction, rules_json, effective_at, source_url, created_at
		 FROM vibe_compliance ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("mem: list compliance_rules: %w", err)
	}
	defer rows.Close()

	out := []ComplianceRule{}
	for rows.Next() {
		var r ComplianceRule
		var effectiveAt, sourceURL sql.NullString
		if err := rows.Scan(&r.Jurisdiction, &r.Rules, &effectiveAt, &sourceURL, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("mem: scan compliance row: %w", err)
		}
		if effectiveAt.Valid {
			r.EffectiveAt = effectiveAt.String
		}
		if sourceURL.Valid {
			r.SourceURL = sourceURL.String
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListArtifacts returns artifacts newest-first with optional filters. The
// most useful filter in practice is vibe_case + status (e.g. "every
// pending image" or "every passed C4 video").
func (s *Store) ListArtifacts(ctx context.Context, vibeCase, brandID, jurisdiction, sessionID, status string, limit int) ([]Artifact, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, session_id, vibe_case, spec_id, artifact_url, artifact_type, brand_id, jurisdiction, has_disclosure, validation_status, created_at
	      FROM vibe_artifacts WHERE 1=1`
	args := []any{}
	if vibeCase != "" {
		q += ` AND vibe_case = ?`
		args = append(args, vibeCase)
	}
	if brandID != "" {
		q += ` AND brand_id = ?`
		args = append(args, brandID)
	}
	if jurisdiction != "" {
		q += ` AND jurisdiction = ?`
		args = append(args, jurisdiction)
	}
	if sessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	if status != "" {
		q += ` AND validation_status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mem: list artifacts: %w", err)
	}
	defer rows.Close()

	out := []Artifact{}
	for rows.Next() {
		var a Artifact
		var (
			sessionIDNS, artifactURL, brandIDNS, jurisdictionNS sql.NullString
			specID, hasDisclosure                                  sql.NullInt64
		)
		if err := rows.Scan(&a.ID, &sessionIDNS, &a.VibeCase, &specID, &artifactURL,
			&a.ArtifactType, &brandIDNS, &jurisdictionNS, &hasDisclosure,
			&a.ValidationStatus, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("mem: scan artifact row: %w", err)
		}
		if sessionIDNS.Valid {
			a.SessionID = sessionIDNS.String
		}
		if specID.Valid {
			a.SpecID = specID.Int64
		}
		if artifactURL.Valid {
			a.ArtifactURL = artifactURL.String
		}
		if brandIDNS.Valid {
			a.BrandID = brandIDNS.String
		}
		if jurisdictionNS.Valid {
			a.Jurisdiction = jurisdictionNS.String
		}
		if hasDisclosure.Valid && hasDisclosure.Int64 != 0 {
			a.HasDisclosure = true
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListDriftReports returns drift reports newest-first with optional
// filters by artifact_id and verdict (aligned | drift_detected |
// needs_human).
func (s *Store) ListDriftReports(ctx context.Context, artifactID int64, verdict string, limit int) ([]DriftReport, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, artifact_id, spec_id, verdict, spec_diff_json, judge_reasoning, reconciled_at, created_at
	      FROM vibe_drift_reports WHERE 1=1`
	args := []any{}
	if artifactID > 0 {
		q += ` AND artifact_id = ?`
		args = append(args, artifactID)
	}
	if verdict != "" {
		q += ` AND verdict = ?`
		args = append(args, verdict)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mem: list drift_reports: %w", err)
	}
	defer rows.Close()

	out := []DriftReport{}
	for rows.Next() {
		var d DriftReport
		var specID, specDiff, reasoning, reconciled sql.NullString
		if err := rows.Scan(&d.ID, &d.ArtifactID, &specID, &d.Verdict, &specDiff, &reasoning, &reconciled, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("mem: scan drift_report row: %w", err)
		}
		if specID.Valid {
			d.SpecID = parseInt(specID.String)
		}
		if specDiff.Valid {
			d.SpecDiff = specDiff.String
		}
		if reasoning.Valid {
			d.JudgeReasoning = reasoning.String
		}
		if reconciled.Valid {
			d.ReconciledAt = reconciled.String
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// nullInt returns nil for 0 so the DB stores NULL cleanly.
func nullInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// boolToInt converts a bool to 0/1 for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// parseInt is a small helper for nullable int64 string columns.
func parseInt(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}