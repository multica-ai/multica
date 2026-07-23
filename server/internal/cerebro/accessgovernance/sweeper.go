package accessgovernance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DefaultInterval    = 24 * time.Hour
	DefaultUnusedAfter = 90 * 24 * time.Hour
)

type GrantSource interface {
	ListGrants(context.Context) ([]Grant, error)
}

type FindingReporter interface {
	Report(context.Context, []Finding) error
}

// AccessTightener expires the existing binding behind a finding. It receives
// no API that can create a role, assignment, or Allow row.
type AccessTightener interface {
	Tighten(context.Context, []Finding, time.Time) error
}

// Sweeper is the live governance module. Its small interface reads access,
// expires affected bindings, and reports tighten-only findings; it deliberately
// exposes no write that can create or widen access.
type Sweeper struct {
	Source      GrantSource
	Tightener   AccessTightener
	Reporter    FindingReporter
	Now         func() time.Time
	UnusedAfter time.Duration
}

func NewSweeper(source GrantSource, tightener AccessTightener, reporter FindingReporter) *Sweeper {
	return &Sweeper{
		Source:      source,
		Tightener:   tightener,
		Reporter:    reporter,
		Now:         time.Now,
		UnusedAfter: DefaultUnusedAfter,
	}
}

// NewDatabaseSweeper wires the production role-binding source and operational
// reporter used by server startup.
func NewDatabaseSweeper(pool *pgxpool.Pool) *Sweeper {
	return NewSweeper(&databaseGrantSource{pool: pool}, &databaseAccessTightener{pool: pool}, logFindingReporter{})
}

// Tick performs one complete live pass and returns the exact findings that were
// tightened and reported. This is the public seam used by tests and operations.
func (s *Sweeper) Tick(ctx context.Context) ([]Finding, error) {
	if s == nil || s.Source == nil || s.Tightener == nil || s.Reporter == nil {
		return nil, fmt.Errorf("accessgovernance: sweeper is not configured")
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	grants, err := s.Source.ListGrants(ctx)
	if err != nil {
		return nil, fmt.Errorf("accessgovernance: list grants: %w", err)
	}
	findings := Scan(now, s.UnusedAfter, grants)
	if err := s.Tightener.Tighten(ctx, findings, now); err != nil {
		return findings, fmt.Errorf("accessgovernance: tighten access: %w", err)
	}
	if err := s.Reporter.Report(ctx, findings); err != nil {
		return nil, fmt.Errorf("accessgovernance: report findings: %w", err)
	}
	return findings, nil
}

// Run scans once at server startup and then on the configured cadence. A scan
// failure is observable but never changes access decisions.
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	run := func() {
		if _, err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("access governance scan failed", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

type databaseGrantSource struct {
	pool *pgxpool.Pool
}

func (s *databaseGrantSource) ListGrants(ctx context.Context) ([]Grant, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("database source is not configured")
	}
	// LastUsedAt is deliberately binding-wide: use of any allowed capability in
	// the role protects the entire assignment from the unused-access action.
	// Tightening a whole role because one of its permissions was quiet would be
	// broader than the evidence supports.
	rows, err := s.pool.Query(ctx, `
		SELECT
			r.id::text || ':' || ra.subject_type || ':' || ra.subject_id::text || ':' || permission.key AS grant_id,
			r.id::text,
			ra.subject_type,
			ra.subject_id::text,
			permission.key,
			COALESCE(r.created_by::text, ''),
			ra.added_at,
			ra.expires_at,
			MAX(ledger.created_at) AS last_used_at,
			CASE ra.subject_type
				WHEN 'agent' THEN EXISTS (
					SELECT 1 FROM agent a
					WHERE a.id=ra.subject_id AND a.workspace_id=r.workspace_id
				)
				WHEN 'member' THEN EXISTS (
					SELECT 1 FROM member m
					WHERE m.user_id=ra.subject_id AND m.workspace_id=r.workspace_id
				)
				ELSE false
			END AS subject_present
		FROM cerebro_role_assignment ra
		JOIN cerebro_role r ON r.id=ra.role_id
		CROSS JOIN LATERAL jsonb_each(r.permissions) AS permission(key, value)
		LEFT JOIN cerebro_access_decision_ledger ledger
		  ON ledger.workspace_id=r.workspace_id
		 AND EXISTS (
			SELECT 1
			FROM jsonb_each(r.permissions) AS role_permission(key, value)
			WHERE role_permission.key=COALESCE(ledger.canonical_capability_id, ledger.observed_tool_name)
			  AND role_permission.value ->> 'setting' IN ('allow', 'ask')
		 )
		 AND ((ra.subject_type='agent' AND ledger.agent_id=ra.subject_id)
		   OR (ra.subject_type='member' AND ledger.on_behalf_of_user_id=ra.subject_id))
		WHERE permission.value ->> 'setting' IN ('allow', 'ask')
		GROUP BY r.id, ra.subject_type, ra.subject_id, permission.key,
		         r.created_by, ra.added_at, ra.expires_at, r.workspace_id
		ORDER BY grant_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	grants := make([]Grant, 0)
	for rows.Next() {
		var grant Grant
		var expiresAt, lastUsedAt pgtype.Timestamptz
		if err := rows.Scan(
			&grant.ID,
			&grant.RoleID,
			&grant.SubjectType,
			&grant.SubjectID,
			&grant.CapabilityID,
			&grant.OwnerID,
			&grant.AddedAt,
			&expiresAt,
			&lastUsedAt,
			&grant.SubjectPresent,
		); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			value := expiresAt.Time
			grant.ExpiresAt = &value
		}
		if lastUsedAt.Valid {
			value := lastUsedAt.Time
			grant.LastUsedAt = &value
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

type databaseAccessTightener struct {
	pool *pgxpool.Pool
}

type bindingTarget struct {
	RoleID      string
	SubjectType string
	SubjectID   string
	AddedAt     time.Time
}

func bindingTargetsForTightening(findings []Finding) ([]bindingTarget, error) {
	targets := make([]bindingTarget, 0, len(findings))
	seen := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		if !finding.TightenOnly {
			return nil, fmt.Errorf("finding %q is not tighten-only", finding.GrantID)
		}
		if finding.RoleID == "" || finding.SubjectID == "" || finding.BindingAddedAt.IsZero() || (finding.SubjectType != "agent" && finding.SubjectType != "member") {
			return nil, fmt.Errorf("finding %q has an invalid binding identity", finding.GrantID)
		}
		key := finding.RoleID + ":" + finding.SubjectType + ":" + finding.SubjectID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, bindingTarget{
			RoleID: finding.RoleID, SubjectType: finding.SubjectType,
			SubjectID: finding.SubjectID, AddedAt: finding.BindingAddedAt,
		})
	}
	return targets, nil
}

// Tighten expires each affected role assignment at or before the sweep time.
// LEAST makes retries idempotent and monotonic: a later sweep can never extend
// an existing expiry or open access again. Matching AddedAt prevents a stale
// scan from expiring a binding that was removed and freshly re-created.
func (t *databaseAccessTightener) Tighten(ctx context.Context, findings []Finding, at time.Time) error {
	if t == nil || t.pool == nil {
		return fmt.Errorf("database tightener is not configured")
	}
	targets, err := bindingTargetsForTightening(findings)
	if err != nil {
		return err
	}
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, target := range targets {
		if _, err := tx.Exec(ctx, `
			UPDATE cerebro_role_assignment
			SET expires_at = LEAST(COALESCE(expires_at, $5), $5)
			WHERE role_id = $1::uuid AND subject_type = $2 AND subject_id = $3::uuid
			  AND added_at = $4
		`, target.RoleID, target.SubjectType, target.SubjectID, target.AddedAt, at); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type logFindingReporter struct{}

func (logFindingReporter) Report(_ context.Context, findings []Finding) error {
	for _, finding := range findings {
		slog.Warn("access governance finding",
			"grant_id", finding.GrantID,
			"severity", finding.Severity,
			"reason", finding.Reason,
			"tighten_only", finding.TightenOnly,
		)
	}
	return nil
}
