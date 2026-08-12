// Package service: MemoryHub claim gate wiring (Plan v1.3 A1 + v1.4 V4-3.2).
// Owner: ALL-16.
//
// The gate runs BEFORE ClaimAgentTask changes the queue row to dispatched.
// Required failures keep the queue queued (no token, no running, no claim
// response); optional degradation is permitted only when the execution
// snapshot explicitly says optional AND all three durable fields are present.
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// resolveMemoryHubClaimGateInput resolves the four DB-backed prerequisite
// classes for a task at claim time: binding, credential, docket and attachment.
// The daemon-capability dimension is a request-level fact carried by the daemon
// handler; the service treats a task's runtime as claim-v1 capable (the
// handler-level X-Client-Capabilities gating belongs to T10).
func (s *TaskService) resolveMemoryHubClaimGateInput(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, workspaceID pgtype.UUID) (ClaimGateInput, error) {
	in := ClaimGateInput{
		MemoryPolicy:            MemoryPolicy(task.MemoryPolicy),
		ProviderNeedsCredential: true,
		DaemonSupportsClaimV1:   true,
		ControlPlaneHealthy:     true,
	}
	if in.MemoryPolicy == "" {
		in.MemoryPolicy = PolicyRequired
	}

	// Binding: resolved when a memoryhub_binding for this task's scope exists in
	// a usable (bound) state. Workspace scope stores scope_id NULL (migration
	// 273 constraint); project scope stores the project id.
	scopeKind, scopeID := "workspace", pgtype.UUID{}
	if task.IssueID.Valid {
		issue, err := q.GetIssue(ctx, task.IssueID)
		if err == nil && issue.ProjectID.Valid {
			scopeKind, scopeID = "project", issue.ProjectID
		}
	}
	bindings, err := q.ListBoundMemoryHubBindingsForClaim(ctx, db.ListBoundMemoryHubBindingsForClaimParams{
		WorkspaceID: workspaceID,
		ScopeKind:   scopeKind,
		ScopeID:     scopeID,
	})
	if err == nil && len(bindings) > 0 {
		in.BindingsResolved = true
	}

	// Credential: resolved when an active secret exists for the workspace.
	if _, err := q.GetSecretForClaim(ctx, workspaceID.String()); err == nil {
		in.CredentialResolved = true
	}

	// Docket: resolved when a docket exists for the subject. Workspace scope
	// stores scope_id NULL; project scope stores the project id.
	if task.IssueID.Valid {
		issue, err := q.GetIssue(ctx, task.IssueID)
		if err == nil {
			subjScopeKind, subjScopeID := "workspace", pgtype.UUID{}
			if issue.ProjectID.Valid {
				subjScopeKind, subjScopeID = "project", issue.ProjectID
			}
			if _, derr := q.GetMemoryDocketBySubject(ctx, db.GetMemoryDocketBySubjectParams{
				WorkspaceID: workspaceID,
				ScopeKind:   subjScopeKind,
				ScopeID:     subjScopeID,
				SubjectType: "issue",
				SubjectID:   task.IssueID,
			}); derr == nil {
				in.DocketResolved = true
			}
		}
	}

	// Attachment: resolved when the task carries a memory_attachment_ref.
	in.AttachmentResolved = task.MemoryAttachmentRef.Valid

	return in, nil
}

// prepareMemoryHubClaim runs the gate for a candidate MemoryHub task BEFORE
// dispatch. A non-nil error is infra; a GateOutcome with State blocked_required
// or blocked_control_plane means the caller must keep the row queued.
func (s *TaskService) prepareMemoryHubClaim(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (GateOutcome, error) {
	workspaceID := pgtype.UUID{}
	if task.IssueID.Valid {
		if issue, err := q.GetIssue(ctx, task.IssueID); err == nil {
			workspaceID = issue.WorkspaceID
		}
	}
	in, err := s.resolveMemoryHubClaimGateInput(ctx, q, task, workspaceID)
	if err != nil {
		return GateOutcome{}, err
	}
	return EvaluateMemoryHubClaimGate(in), nil
}

// claimWithMemoryGate attempts a memory-gated claim for the agent. It returns:
//   - (task, true, nil): a gate-approved task was reserved and committed
//     (queued -> dispatched).
//   - (nil, true, nil): a MemoryHub candidate existed but the gate blocked it;
//     the row stays queued with a durable gate outcome.
//   - (nil, false, nil): no MemoryHub candidate is claimable right now.
//   - (nil, false, err): infrastructure failure.
func (s *TaskService) claimWithMemoryGate(ctx context.Context, qtx *db.Queries, agentID pgtype.UUID) (*db.AgentTaskQueue, bool, error) {
	candidate, err := qtx.SelectQueuedMemoryClaimCandidateForAgent(ctx, agentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}

	leaseID := "gate-" + uuid.NewString()

	// Reserve the candidate for gate preflight (keeps it queued, takes a lease)
	// BEFORE evaluating the gate, so the commit step can CAS on the same lease.
	reserved, err := qtx.ReserveQueuedTaskForMemoryGate(ctx, db.ReserveQueuedTaskForMemoryGateParams{
		MemoryGateLeaseID: pgtype.Text{String: leaseID, Valid: true},
		Column2:           pgtype.Interval{Microseconds: int64(prepareLeaseDuration.Microseconds()), Valid: true},
		ID:                candidate.ID,
	})
	if err != nil {
		// A concurrent claimer won the reserve race on this exact candidate
		// (live lease held elsewhere). That is contention, not infrastructure
		// failure: skip it and let the caller move to the next candidate.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	candidate = reserved

	outcome, err := s.prepareMemoryHubClaim(ctx, qtx, candidate)
	if err != nil {
		return nil, false, err
	}

	switch outcome.State {
	case GateReady, GateDegraded:
		executionID := uuid.NewString()
		runID := "run-" + uuid.NewString()
		claimed, cerr := qtx.CommitReservedTaskClaim(ctx, db.CommitReservedTaskClaimParams{
			ID:                candidate.ID,
			MemoryGateLeaseID: pgtype.Text{String: leaseID, Valid: true},
			ExecutionID:       util.MustParseUUID(executionID),
			MemoryhubRunID:    pgtype.Text{String: runID, Valid: true},
		})
		if cerr != nil {
			return nil, false, cerr
		}
		return &claimed, true, nil
	default:
		// blocked_required / blocked_control_plane: keep the row queued and
		// record the durable outcome.
		failure := outcome.Failure
		code := "memoryhub_gate_blocked"
		if failure != nil {
			code = failure.ErrorCode
		}
		if _, oerr := qtx.SetMemoryGateOutcome(ctx, db.SetMemoryGateOutcomeParams{
			ID:                  candidate.ID,
			MemoryGateState:     pgtype.Text{String: string(outcome.State), Valid: true},
			MemoryGateErrorCode: pgtype.Text{String: code, Valid: true},
		}); oerr != nil {
			return nil, false, oerr
		}
		return nil, true, nil
	}
}
