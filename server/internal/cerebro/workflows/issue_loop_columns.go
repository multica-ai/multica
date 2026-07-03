package workflows

// issue_loop_columns.go reads and writes the workflow_type / loop_spec /
// generated_from_workflow_id columns added to cerebro_workflow for FIR-2283's
// Issue workflow type directly against the pool, bypassing the
// sqlc-generated CerebroWorkflow queries. This mirrors the precedent the
// loops package already set for its own tables (see loops/store.go's package
// doc) — it keeps the three new columns fully functional without a sqlc
// regeneration pass over the rest of this package's queries, which read/write
// the base row unchanged.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
)

const (
	// WorkflowTypeStandard is the default, unchanged workflow: a single
	// trigger -> conditions -> action rule a person edits directly.
	WorkflowTypeStandard = "standard"
	// WorkflowTypeIssueLoop is a Plan/Build/Delivery-gate/Done recipe
	// designed on the Issue workflow surface. Its loop_spec column holds the
	// recipe; IssueLoopCompiler compiles it into generated child rules (see
	// issue_loop.go).
	WorkflowTypeIssueLoop = "issue_loop"
)

// IssueLoopFields is the subset of a cerebro_workflow row that only an
// issue_loop-typed workflow (or one of its generated children) carries.
type IssueLoopFields struct {
	WorkflowType            string
	LoopSpec                json.RawMessage
	GeneratedFromWorkflowID string // empty when this row is not a generated child rule
}

// IssueLoopColumnStore reads/writes the issue_loop columns directly against
// the pool.
type IssueLoopColumnStore struct {
	pool *pgxpool.Pool
}

// NewIssueLoopColumnStore builds an IssueLoopColumnStore over the given pool.
func NewIssueLoopColumnStore(pool *pgxpool.Pool) *IssueLoopColumnStore {
	return &IssueLoopColumnStore{pool: pool}
}

// Get returns one row's issue_loop columns.
func (s *IssueLoopColumnStore) Get(ctx context.Context, id pgtype.UUID) (IssueLoopFields, error) {
	var f IssueLoopFields
	var loopSpec []byte
	var generatedFrom pgtype.UUID
	if err := s.pool.QueryRow(ctx,
		`SELECT workflow_type, loop_spec, generated_from_workflow_id FROM cerebro_workflow WHERE id = $1`,
		id,
	).Scan(&f.WorkflowType, &loopSpec, &generatedFrom); err != nil {
		return IssueLoopFields{}, fmt.Errorf("load issue-loop columns: %w", err)
	}
	if len(loopSpec) > 0 {
		f.LoopSpec = loopSpec
	}
	if generatedFrom.Valid {
		f.GeneratedFromWorkflowID = util.UUIDToString(generatedFrom)
	}
	return f, nil
}

// GetMany returns the issue_loop columns for every id in ids, keyed by the
// string id — used by List to filter generated rows out of the main listing
// and to attach workflow_type/loop_spec to every response row in one query.
func (s *IssueLoopColumnStore) GetMany(ctx context.Context, workspaceID pgtype.UUID) (map[string]IssueLoopFields, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, workflow_type, loop_spec, generated_from_workflow_id
		 FROM cerebro_workflow WHERE workspace_id = $1`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list issue-loop columns: %w", err)
	}
	defer rows.Close()

	out := make(map[string]IssueLoopFields)
	for rows.Next() {
		var id pgtype.UUID
		var f IssueLoopFields
		var loopSpec []byte
		var generatedFrom pgtype.UUID
		if err := rows.Scan(&id, &f.WorkflowType, &loopSpec, &generatedFrom); err != nil {
			return nil, fmt.Errorf("scan issue-loop columns: %w", err)
		}
		if len(loopSpec) > 0 {
			f.LoopSpec = loopSpec
		}
		if generatedFrom.Valid {
			f.GeneratedFromWorkflowID = util.UUIDToString(generatedFrom)
		}
		out[util.UUIDToString(id)] = f
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issue-loop columns: %w", err)
	}
	return out, nil
}

// Set writes workflow_type and loop_spec on an existing row. Called after the
// normal sqlc Create/Update of the base row.
func (s *IssueLoopColumnStore) Set(ctx context.Context, id pgtype.UUID, workflowType string, loopSpec json.RawMessage) error {
	spec := []byte(loopSpec)
	if len(spec) == 0 {
		spec = nil
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE cerebro_workflow SET workflow_type = $2, loop_spec = $3 WHERE id = $1`,
		id, workflowType, spec,
	); err != nil {
		return fmt.Errorf("set issue-loop columns: %w", err)
	}
	return nil
}

// SetGeneratedFrom marks a row as one of parentID's compiled child rules —
// the PROJECT-WIDE compile of the recipe (generated_for_issue_id stays NULL).
// Use SetGeneratedFromForIssue for a per-issue activation.
func (s *IssueLoopColumnStore) SetGeneratedFrom(ctx context.Context, id, parentID pgtype.UUID) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE cerebro_workflow SET generated_from_workflow_id = $2 WHERE id = $1`,
		id, parentID,
	); err != nil {
		return fmt.Errorf("set generated-from-workflow-id: %w", err)
	}
	return nil
}

// SetGeneratedFromForIssue marks a row as one of parentID's compiled child
// rules for one specific issue activation (FIR-2283 v2 point 8) — the same
// recipe compiled again for a DIFFERENT issue gets its own independent set
// of rows, distinguished by issueID.
func (s *IssueLoopColumnStore) SetGeneratedFromForIssue(ctx context.Context, id, parentID, issueID pgtype.UUID) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE cerebro_workflow SET generated_from_workflow_id = $2, generated_for_issue_id = $3 WHERE id = $1`,
		id, parentID, issueID,
	); err != nil {
		return fmt.Errorf("set generated-from-workflow-id for issue: %w", err)
	}
	return nil
}

// GeneratedChildIDByName returns the id of one of parentID's PROJECT-WIDE
// generated child rules by its rule name (e.g. "loop:delivery-gate") — this
// IS the "gate" key loops.GateEvaluator uses (gate = the delivery-gate child
// workflow's own id), so the control strip and the approve-human-check
// endpoint resolve it once per request rather than the client having to know
// or store it. Use GeneratedChildIDByNameForIssue for a per-issue activation.
func (s *IssueLoopColumnStore) GeneratedChildIDByName(ctx context.Context, parentID pgtype.UUID, name string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := s.pool.QueryRow(ctx,
		`SELECT id FROM cerebro_workflow WHERE generated_from_workflow_id = $1 AND generated_for_issue_id IS NULL AND name = $2 LIMIT 1`,
		parentID, name,
	).Scan(&id); err != nil {
		return pgtype.UUID{}, fmt.Errorf("find generated child %q: %w", name, err)
	}
	return id, nil
}

// GeneratedChildIDByNameForIssue mirrors GeneratedChildIDByName, scoped to
// one issue's activation of the recipe.
func (s *IssueLoopColumnStore) GeneratedChildIDByNameForIssue(ctx context.Context, parentID, issueID pgtype.UUID, name string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := s.pool.QueryRow(ctx,
		`SELECT id FROM cerebro_workflow WHERE generated_from_workflow_id = $1 AND generated_for_issue_id = $2 AND name = $3 LIMIT 1`,
		parentID, issueID, name,
	).Scan(&id); err != nil {
		return pgtype.UUID{}, fmt.Errorf("find generated child %q for issue: %w", name, err)
	}
	return id, nil
}

// DeleteGeneratedChildren removes every PROJECT-WIDE row previously
// generated from parentID (generated_for_issue_id IS NULL), so re-syncing a
// recipe (on Update) never leaves a stale rule behind — the bridge always
// deletes-then-recreates rather than trying to diff the old and new rule
// sets. Scoped to NULL so this never touches another issue's independent
// activation of the same recipe — see DeleteGeneratedChildrenForIssue.
func (s *IssueLoopColumnStore) DeleteGeneratedChildren(ctx context.Context, parentID pgtype.UUID) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM cerebro_workflow WHERE generated_from_workflow_id = $1 AND generated_for_issue_id IS NULL`,
		parentID,
	); err != nil {
		return fmt.Errorf("delete generated issue-loop rules: %w", err)
	}
	return nil
}

// DeleteGeneratedChildrenForIssue removes every row previously generated
// from parentID for issueID's activation specifically — re-activating the
// same recipe on the same issue deletes-then-recreates just that issue's
// rows, leaving every OTHER issue's activation (and the project-wide compile)
// untouched.
func (s *IssueLoopColumnStore) DeleteGeneratedChildrenForIssue(ctx context.Context, parentID, issueID pgtype.UUID) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM cerebro_workflow WHERE generated_from_workflow_id = $1 AND generated_for_issue_id = $2`,
		parentID, issueID,
	); err != nil {
		return fmt.Errorf("delete generated issue-loop rules for issue: %w", err)
	}
	return nil
}

// DeleteAllGeneratedChildrenForIssue removes every issue-scoped generated row
// for issueID, regardless of which recipe produced it. Use this before
// activating a recipe from the issue sidebar: one issue can only have one
// active Issue workflow recipe at a time, so switching recipes must remove
// the old recipe's rules as well as any stale copy of the new recipe's rules.
func (s *IssueLoopColumnStore) DeleteAllGeneratedChildrenForIssue(ctx context.Context, issueID pgtype.UUID) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM cerebro_workflow WHERE generated_for_issue_id = $1`,
		issueID,
	); err != nil {
		return fmt.Errorf("delete generated issue-loop rules for issue: %w", err)
	}
	return nil
}

// ActiveWorkflowForIssue returns the recipe (workflow) id currently activated
// on issueID, if any. The binding is fully derivable from the materialized
// rows — no separate "issue -> workflow" table — because every rule
// SyncIssueLoop compiles for an activation carries both
// generated_from_workflow_id (which recipe) and generated_for_issue_id
// (which issue).
func (s *IssueLoopColumnStore) ActiveWorkflowForIssue(ctx context.Context, issueID pgtype.UUID) (pgtype.UUID, bool, error) {
	var id pgtype.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT generated_from_workflow_id FROM cerebro_workflow
		 WHERE generated_for_issue_id = $1 AND generated_from_workflow_id IS NOT NULL
		 GROUP BY generated_from_workflow_id
		 ORDER BY max(created_at) DESC
		 LIMIT 1`,
		issueID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, false, nil
	}
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("load active workflow for issue: %w", err)
	}
	return id, true, nil
}
