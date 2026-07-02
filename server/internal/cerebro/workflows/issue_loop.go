package workflows

// issue_loop.go wires the FIR-2283 "Issue workflow" type (workflow_type ==
// "issue_loop") to the loop engine. An issue_loop row's loop_spec column
// holds the recipe designed on the Issue workflow surface (goal,
// definition_of_done, verification[], caps, planning) — this file is the
// bridge that turns "save this recipe" into "the engine actually runs it":
// on Create/Update, Handler calls IssueLoopCompiler.SyncIssueLoop, which
// parses the spec, calls loops.Compile, and persists the resulting
// dispatch/gate/escalate rules as their own generated cerebro_workflow rows
// (workflow_type stays "standard" on those — they are literally standard
// rules, just machine-authored).
//
// workflows cannot import loops here either — loops already imports
// workflows to build Rules, so the reverse edge would be a cycle (see
// check_gate.go and loop_planning.go for the identical problem on the
// delivery gate and the planning-dispatch materializer). The dependency is
// inverted the same way: workflows declares the IssueLoopCompiler interface
// it needs, the loops package supplies a concrete implementation
// (loops.IssueLoopBridge), and router.go plugs it in via
// Handler.WithIssueLoopCompiler.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// IssueLoopCompiler compiles an issue_loop workflow's loop_spec into the
// engine's dispatch/gate/escalate rules and keeps them in sync with the
// recipe. It is the seam Handler.Create/Update calls; the loops package
// implements it (loops.Compile, persisted via cerebrodb — see
// loops/materialize_issue_loop.go).
type IssueLoopCompiler interface {
	// SyncIssueLoop (re)compiles workflowID's loop_spec and replaces its
	// previously-generated child rules with the fresh set — called after
	// every create/update of an issue_loop workflow so the compiled rules
	// never drift from the saved recipe. loopSpecJSON is the loop_spec
	// column's raw JSON. Returns a user-facing error (invalid spec) as a
	// plain error; the caller surfaces it as a 400.
	SyncIssueLoop(ctx context.Context, workspaceID, workflowID, projectID, createdByID pgtype.UUID, createdByType string, loopSpecJSON []byte) error
}

// WithIssueLoopCompiler plugs in the issue-loop bridge implementation.
// Optional and nil-safe: without it, an issue_loop workflow's loop_spec is
// persisted but never compiled onto the engine — the recipe is stored, ready
// to sync once this is wired. Returns the receiver for chainable init.
func (h *Handler) WithIssueLoopCompiler(c IssueLoopCompiler) *Handler {
	h.issueLoopCompiler = c
	return h
}
