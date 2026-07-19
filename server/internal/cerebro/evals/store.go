package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound            = errors.New("eval not found")
	ErrImmutable           = errors.New("active eval definitions are immutable; create a new version")
	ErrRunExecutorMissing  = errors.New("server-side eval runner is not configured")
	ErrInvalidRunExecution = errors.New("server-side eval runner returned an invalid result")
)

type Store struct {
	pool        *pgxpool.Pool
	runExecutor RunExecutor
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) WithRunExecutor(executor RunExecutor) *Store {
	s.runExecutor = executor
	return s
}

const evalColumns = `id, workspace_id, eval_key, version, title, description, status,
 owner, objective, target, datasets, graders, thresholds, runner, source,
 created_by_id, created_by_type, created_at, updated_at`

func scanEval(row pgx.Row) (Eval, error) {
	var value Eval
	err := row.Scan(&value.ID, &value.WorkspaceID, &value.Key, &value.Version, &value.Title,
		&value.Description, &value.Status, &value.Owner, &value.Objective, &value.Target,
		&value.Datasets, &value.Graders, &value.Thresholds, &value.Runner, &value.Source,
		&value.CreatedByID, &value.CreatedByType, &value.CreatedAt, &value.UpdatedAt)
	return value, err
}

func (s *Store) List(ctx context.Context, workspaceID uuid.UUID) ([]Eval, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+evalColumns+` FROM cerebro_eval
        WHERE workspace_id=$1 ORDER BY updated_at DESC, title ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Eval, 0)
	for rows.Next() {
		item, err := scanEval(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Get(ctx context.Context, workspaceID, id uuid.UUID) (Eval, error) {
	value, err := scanEval(s.pool.QueryRow(ctx, `SELECT `+evalColumns+` FROM cerebro_eval WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Eval{}, ErrNotFound
	}
	return value, err
}

func (s *Store) Create(ctx context.Context, workspaceID, actorID uuid.UUID, actorType string, input EvalInput) (Eval, error) {
	return scanEval(s.pool.QueryRow(ctx, `INSERT INTO cerebro_eval (
        workspace_id, eval_key, version, title, description, status, owner, objective,
        target, datasets, graders, thresholds, runner, source, created_by_id, created_by_type
      ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING `+evalColumns,
		workspaceID, input.Key, input.Version, input.Title, input.Description, input.Status,
		input.Owner, input.Objective, input.Target, input.Datasets, input.Graders,
		input.Thresholds, input.Runner, input.Source, actorID, actorType))
}

func (s *Store) Update(ctx context.Context, workspaceID, id uuid.UUID, input EvalInput) (Eval, error) {
	value, err := scanEval(s.pool.QueryRow(ctx, `UPDATE cerebro_eval SET
        eval_key=$3, version=$4, title=$5, description=$6, status=$7, owner=$8,
        objective=$9, target=$10, datasets=$11, graders=$12, thresholds=$13,
        runner=$14, source=$15, updated_at=now()
      WHERE workspace_id=$1 AND id=$2 AND (
        status='draft' OR (
          eval_key=$3 AND version=$4 AND title=$5 AND description=$6 AND owner=$8
          AND objective=$9 AND target=$10 AND datasets=$11 AND graders=$12
          AND thresholds=$13 AND runner=$14 AND source=$15
        )
      ) RETURNING `+evalColumns,
		workspaceID, id, input.Key, input.Version, input.Title, input.Description,
		input.Status, input.Owner, input.Objective, input.Target, input.Datasets,
		input.Graders, input.Thresholds, input.Runner, input.Source))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := s.Get(ctx, workspaceID, id); getErr == nil {
			return Eval{}, ErrImmutable
		}
		return Eval{}, ErrNotFound
	}
	return value, err
}

func (s *Store) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM cerebro_eval WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err == nil && result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

const runColumns = `id, workspace_id, eval_id, eval_version, target_version, workflow_id,
 issue_id, status, results, evidence_artifact_id, cost_cents, latency_ms,
 created_by_id, created_by_type, started_at, completed_at, created_at`

func scanRun(row pgx.Row) (EvalRun, error) {
	var value EvalRun
	err := row.Scan(&value.ID, &value.WorkspaceID, &value.EvalID, &value.EvalVersion,
		&value.TargetVersion, &value.WorkflowID, &value.IssueID, &value.Status, &value.Results,
		&value.EvidenceArtifactID, &value.CostCents, &value.LatencyMS, &value.CreatedByID,
		&value.CreatedByType, &value.StartedAt, &value.CompletedAt, &value.CreatedAt)
	return value, err
}

// formatIssueKey builds a case key ("MUL-123") from the workspace prefix and the
// issue number. Both come from a LEFT JOIN, so either may be absent (a run with
// no issue, or a drifted row); in that case it returns "" and the UI falls back
// to the raw id.
func formatIssueKey(prefix *string, number *int32) string {
	if prefix == nil || number == nil || *prefix == "" {
		return ""
	}
	return fmt.Sprintf("%s-%d", *prefix, *number)
}

func (s *Store) ListRuns(ctx context.Context, workspaceID, evalID uuid.UUID) ([]EvalRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT
        r.id, r.workspace_id, r.eval_id, r.eval_version, r.target_version, r.workflow_id,
        r.issue_id, r.status, r.results, r.evidence_artifact_id, r.cost_cents, r.latency_ms,
        r.created_by_id, r.created_by_type, r.started_at, r.completed_at, r.created_at,
        i.number, w.issue_prefix
      FROM cerebro_eval_run r
      LEFT JOIN issue i ON i.id = r.issue_id
      LEFT JOIN workspace w ON w.id = r.workspace_id
      WHERE r.workspace_id=$1 AND r.eval_id=$2 ORDER BY r.created_at DESC LIMIT 100`, workspaceID, evalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]EvalRun, 0)
	for rows.Next() {
		var run EvalRun
		var number *int32
		var prefix *string
		if err := rows.Scan(&run.ID, &run.WorkspaceID, &run.EvalID, &run.EvalVersion,
			&run.TargetVersion, &run.WorkflowID, &run.IssueID, &run.Status, &run.Results,
			&run.EvidenceArtifactID, &run.CostCents, &run.LatencyMS, &run.CreatedByID,
			&run.CreatedByType, &run.StartedAt, &run.CompletedAt, &run.CreatedAt,
			&number, &prefix); err != nil {
			return nil, err
		}
		run.IssueKey = formatIssueKey(prefix, number)
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) CreateRun(ctx context.Context, workspaceID, actorID, evalID uuid.UUID, actorType string, input EvalRunInput) (EvalRun, error) {
	eval, err := s.Get(ctx, workspaceID, evalID)
	if err != nil {
		return EvalRun{}, err
	}
	if s.runExecutor == nil {
		return EvalRun{}, ErrRunExecutorMissing
	}
	execution, err := s.runExecutor.Execute(ctx, eval)
	if err != nil {
		return EvalRun{}, fmt.Errorf("execute eval: %w", err)
	}
	if (execution.Status != RunStatusPassed && execution.Status != RunStatusFailed && execution.Status != RunStatusError) ||
		!json.Valid(execution.Results) || execution.CostCents < 0 || execution.LatencyMS < 0 {
		return EvalRun{}, ErrInvalidRunExecution
	}
	input.TargetVersion = execution.TargetVersion
	input.Status = execution.Status
	input.Results = execution.Results
	input.CostCents = execution.CostCents
	input.LatencyMS = execution.LatencyMS
	input.StartedAt = execution.StartedAt
	input.CompletedAt = execution.CompletedAt
	run, err := scanRun(s.pool.QueryRow(ctx, `INSERT INTO cerebro_eval_run (
        workspace_id, eval_id, eval_version, target_version, workflow_id, issue_id,
        status, results, evidence_artifact_id, cost_cents, latency_ms, created_by_id,
        created_by_type, started_at, completed_at)
      SELECT $1, e.id, e.version, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
      FROM cerebro_eval e WHERE e.workspace_id=$1 AND e.id=$2 RETURNING `+runColumns,
		workspaceID, evalID, input.TargetVersion, input.WorkflowID, input.IssueID, input.Status,
		input.Results, input.EvidenceArtifactID, input.CostCents, input.LatencyMS, actorID, actorType,
		input.StartedAt, input.CompletedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return EvalRun{}, ErrNotFound
	}
	return run, err
}

// RunForIssue executes evalID against the given issue using the store's wired
// runner (the same server executor as eval Run-now) and persists the run. It
// returns the run id and status as strings so callers outside this package —
// the eval.run hook action — need not import evals types (avoiding the
// evals→loops→workflows import cycle). Fails closed when no runner is wired:
// CreateRun returns ErrRunExecutorMissing, which the hook records as a failure.
func (s *Store) RunForIssue(ctx context.Context, workspaceID, actorID, evalID, issueID uuid.UUID, actorType string) (string, string, error) {
	run, err := s.CreateRun(ctx, workspaceID, actorID, evalID, actorType, EvalRunInput{IssueID: &issueID})
	if err != nil {
		return "", "", err
	}
	return run.ID.String(), run.Status, nil
}

func (s *Store) ListBindings(ctx context.Context, workspaceID uuid.UUID, workflowID *uuid.UUID) ([]Binding, error) {
	query := `SELECT b.id, b.workspace_id, b.workflow_id, b.eval_id, b.phase, b.blocking,
        b.created_by_id, b.created_at, e.eval_key, e.version, e.title
      FROM cerebro_workflow_eval_binding b JOIN cerebro_eval e ON e.id=b.eval_id
      WHERE b.workspace_id=$1`
	args := []any{workspaceID}
	if workflowID != nil {
		query += ` AND b.workflow_id=$2`
		args = append(args, *workflowID)
	}
	query += ` ORDER BY b.created_at DESC`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Binding, 0)
	for rows.Next() {
		var item Binding
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.WorkflowID, &item.EvalID,
			&item.Phase, &item.Blocking, &item.CreatedByID, &item.CreatedAt, &item.EvalKey,
			&item.EvalVersion, &item.EvalTitle); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetBinding(ctx context.Context, workspaceID, id uuid.UUID) (Binding, error) {
	var item Binding
	err := s.pool.QueryRow(ctx, `SELECT b.id, b.workspace_id, b.workflow_id, b.eval_id, b.phase, b.blocking,
		b.created_by_id, b.created_at, e.eval_key, e.version, e.title
		FROM cerebro_workflow_eval_binding b JOIN cerebro_eval e ON e.id=b.eval_id
		WHERE b.workspace_id=$1 AND b.id=$2`, workspaceID, id).Scan(
		&item.ID, &item.WorkspaceID, &item.WorkflowID, &item.EvalID, &item.Phase,
		&item.Blocking, &item.CreatedByID, &item.CreatedAt, &item.EvalKey,
		&item.EvalVersion, &item.EvalTitle)
	if errors.Is(err, pgx.ErrNoRows) {
		return Binding{}, ErrNotFound
	}
	return item, err
}

func (s *Store) CreateBinding(ctx context.Context, workspaceID, actorID uuid.UUID, input BindingInput) (Binding, error) {
	var item Binding
	err := s.pool.QueryRow(ctx, `INSERT INTO cerebro_workflow_eval_binding
        (workspace_id, workflow_id, eval_id, phase, blocking, created_by_id)
      SELECT $1, w.id, e.id, $4, $5, $6 FROM cerebro_workflow w, cerebro_eval e
      WHERE w.workspace_id=$1 AND w.id=$2 AND e.workspace_id=$1 AND e.id=$3
        AND (NOT $5 OR e.status='active')
      RETURNING id, workspace_id, workflow_id, eval_id, phase, blocking, created_by_id, created_at`,
		workspaceID, input.WorkflowID, input.EvalID, input.Phase, input.Blocking, actorID).
		Scan(&item.ID, &item.WorkspaceID, &item.WorkflowID, &item.EvalID, &item.Phase,
			&item.Blocking, &item.CreatedByID, &item.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Binding{}, ErrNotFound
		}
		return Binding{}, err
	}
	eval, err := s.Get(ctx, workspaceID, item.EvalID)
	if err != nil {
		return Binding{}, err
	}
	item.EvalKey, item.EvalVersion, item.EvalTitle = eval.Key, eval.Version, eval.Title
	return item, nil
}

func (s *Store) DeleteBinding(ctx context.Context, workspaceID, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM cerebro_workflow_eval_binding WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err == nil && result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// BlockingEvalsPassed fails closed: every blocking binding for the requested
// phase must have a latest server-scored passing run for this workflow+issue.
func (s *Store) BlockingEvalsPassed(ctx context.Context, workflowID, issueID uuid.UUID, phase string) (bool, error) {
	if !validEvalPhase(phase) {
		return false, fmt.Errorf("invalid eval binding phase %q", phase)
	}
	var allPassed bool
	err := s.pool.QueryRow(ctx, `WITH selected_workflow AS (
      SELECT COALESCE(generated_from_workflow_id, id) AS id
      FROM cerebro_workflow WHERE id=$1
    ) SELECT NOT EXISTS (
      SELECT 1 FROM cerebro_workflow_eval_binding b
      JOIN cerebro_eval e ON e.id=b.eval_id
      JOIN selected_workflow selected ON selected.id=b.workflow_id
      LEFT JOIN LATERAL (
        SELECT r.status FROM cerebro_eval_run r
        WHERE r.workspace_id=b.workspace_id AND r.workflow_id=b.workflow_id
          AND r.issue_id=$2 AND r.eval_id=b.eval_id AND r.eval_version=e.version
        ORDER BY r.created_at DESC LIMIT 1
      ) latest ON TRUE
      WHERE b.phase=$3 AND b.blocking
        AND COALESCE(latest.status, '') <> 'passed'
    )`, workflowID, issueID, phase).Scan(&allPassed)
	return allPassed, err
}

// FailingAdvisoryEvals returns the advisory (non-blocking) bindings for the
// requested phase whose latest server-scored run for this workflow+issue is not
// a pass — the mirror of BlockingEvalsPassed for warn-only bindings. A binding
// with no run counts as failing (warn on missing evidence). The AdvisoryWarner
// turns each returned binding into an owner/admin inbox card; it NEVER affects a
// gate's advance decision.
func (s *Store) FailingAdvisoryEvals(ctx context.Context, workflowID, issueID uuid.UUID, phase string) ([]Binding, error) {
	if !validEvalPhase(phase) {
		return nil, fmt.Errorf("invalid eval binding phase %q", phase)
	}
	rows, err := s.pool.Query(ctx, `WITH selected_workflow AS (
      SELECT COALESCE(generated_from_workflow_id, id) AS id
      FROM cerebro_workflow WHERE id=$1
    )
    SELECT b.id, b.workspace_id, b.workflow_id, b.eval_id, b.phase, b.blocking,
      b.created_by_id, b.created_at, e.eval_key, e.version, e.title
    FROM cerebro_workflow_eval_binding b
    JOIN cerebro_eval e ON e.id=b.eval_id
    JOIN selected_workflow selected ON selected.id=b.workflow_id
    LEFT JOIN LATERAL (
      SELECT r.status FROM cerebro_eval_run r
      WHERE r.workspace_id=b.workspace_id AND r.workflow_id=b.workflow_id
        AND r.issue_id=$2 AND r.eval_id=b.eval_id AND r.eval_version=e.version
      ORDER BY r.created_at DESC LIMIT 1
    ) latest ON TRUE
    WHERE b.phase=$3 AND NOT b.blocking
      AND COALESCE(latest.status, '') <> 'passed'
    ORDER BY b.created_at DESC`, workflowID, issueID, phase)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Binding, 0)
	for rows.Next() {
		var item Binding
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.WorkflowID, &item.EvalID,
			&item.Phase, &item.Blocking, &item.CreatedByID, &item.CreatedAt, &item.EvalKey,
			&item.EvalVersion, &item.EvalTitle); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// LatestRunPassed reports whether the newest run of eval evalID for this issue
// passed. Fails closed: no run → (false, nil). Used by the eval.gate hook action
// and the eval_passed hook condition, which key on eval+issue (not workflow).
func (s *Store) LatestRunPassed(ctx context.Context, workspaceID, evalID, issueID uuid.UUID) (bool, error) {
	var passed bool
	err := s.pool.QueryRow(ctx, `SELECT COALESCE((
      SELECT r.status = 'passed' FROM cerebro_eval_run r
      WHERE r.workspace_id=$1 AND r.eval_id=$2 AND r.issue_id=$3
      ORDER BY r.created_at DESC LIMIT 1
    ), FALSE)`, workspaceID, evalID, issueID).Scan(&passed)
	return passed, err
}

// EvalPassed is an alias of LatestRunPassed used by the eval_passed hook
// condition resolver (U3), where the resolver interface is keyed on eval+issue.
func (s *Store) EvalPassed(ctx context.Context, workspaceID, evalID, issueID uuid.UUID) (bool, error) {
	return s.LatestRunPassed(ctx, workspaceID, evalID, issueID)
}

// VersionPassRate is the pass/total tally for one target_version of an eval.
type VersionPassRate struct {
	TargetVersion string `json:"target_version"`
	Passed        int    `json:"passed"`
	Total         int    `json:"total"`
}

// Rate returns the fraction of runs that passed for this target version, or 0
// when there are no runs (guards a zero divide).
func (v VersionPassRate) Rate() float64 {
	if v.Total == 0 {
		return 0
	}
	return float64(v.Passed) / float64(v.Total)
}

// PassRateByTargetVersion returns the pass/total tally per target_version for an
// eval, newest target_version first (ordered by each version's most recent run).
// Used by the drift alarm to detect a pass-rate regression between the newest
// target version and the one before it.
func (s *Store) PassRateByTargetVersion(ctx context.Context, workspaceID, evalID uuid.UUID) ([]VersionPassRate, error) {
	rows, err := s.pool.Query(ctx, `
      SELECT target_version,
        count(*) FILTER (WHERE status = 'passed') AS passed,
        count(*) AS total
      FROM cerebro_eval_run
      WHERE workspace_id=$1 AND eval_id=$2
      GROUP BY target_version
      ORDER BY max(created_at) DESC`, workspaceID, evalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]VersionPassRate, 0)
	for rows.Next() {
		var v VersionPassRate
		if err := rows.Scan(&v.TargetVersion, &v.Passed, &v.Total); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LatestRunStatusForEval returns the status of the newest run of an eval across
// all issues/versions, and whether any run exists. Used by the drift alarm to
// flag an eval whose most recent run failed.
func (s *Store) LatestRunStatusForEval(ctx context.Context, workspaceID, evalID uuid.UUID) (string, bool, error) {
	var status string
	err := s.pool.QueryRow(ctx, `
      SELECT status FROM cerebro_eval_run
      WHERE workspace_id=$1 AND eval_id=$2
      ORDER BY created_at DESC LIMIT 1`, workspaceID, evalID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return status, true, nil
}

// ListActiveEvals returns every active eval across all workspaces, newest first.
// The drift alarm walks these each tick; a workspace with no active evals simply
// contributes nothing.
func (s *Store) ListActiveEvals(ctx context.Context) ([]Eval, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+evalColumns+` FROM cerebro_eval
        WHERE status='active' ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Eval, 0)
	for rows.Next() {
		eval, err := scanEval(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, eval)
	}
	return items, rows.Err()
}
