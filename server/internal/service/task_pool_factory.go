package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/runtimepool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrPoolPlacementMemberRequired = errors.New("runtime pool placement requires a workspace member")

var errPoolTaskTransactionRequired = errors.New("runtime pool task creation requires a transaction")

// PoolRoutingSnapshot is the immutable routing half of a Pool Task insert.
// Entry callbacks retain their complete entry-specific parameters and replace
// only these fields with the locked factory decision.
type PoolRoutingSnapshot struct {
	Status                   string
	CompletedAt              pgtype.Timestamptz
	RuntimeRequirements      []byte
	PlacementWorkspaceID     pgtype.UUID
	RuntimeRequesterUserID   pgtype.UUID
	RuntimeTriggerUserID     pgtype.UUID
	SessionAffinityState     string
	SessionAffinityRuntimeID pgtype.UUID
	ExplicitFreshSession     bool
	WaitReason               pgtype.Text
}

// PoolTaskCreateInput contains only lock keys, placement inputs, and the
// entry-specific insert callback. The callback must execute through qtx so the
// routing snapshot and the entry payload commit atomically.
type PoolTaskCreateInput struct {
	AgentID          pgtype.UUID
	WorkspaceID      pgtype.UUID
	OriginatorUserID pgtype.UUID
	TriggerUserID    pgtype.UUID
	Placement        PoolPlacementRequest
	Deferred         bool
	BeforePlacement  func(context.Context, *db.Queries, db.Member, db.Agent) error
	Insert           func(context.Context, *db.Queries, PoolRoutingSnapshot) (db.AgentTaskQueue, error)
}

type poolTaskCreateMemberLock struct {
	preview   db.Agent
	requester pgtype.UUID
	member    db.Member
}

func (s *TaskService) createPoolTask(ctx context.Context, input PoolTaskCreateInput) (db.AgentTaskQueue, error) {
	if s == nil || s.Queries == nil || s.TxStarter == nil {
		return db.AgentTaskQueue{}, errPoolTaskTransactionRequired
	}
	if input.Insert == nil {
		return db.AgentTaskQueue{}, errors.New("runtime pool task insert callback is required")
	}

	var created db.AgentTaskQueue
	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		var err error
		created, err = s.createPoolTaskWithQueries(ctx, qtx, input)
		return err
	})
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	if created.Status == "cancelled" || created.Status == "deferred" {
		return created, nil
	}
	if _, err := s.AssignPoolWorkspace(ctx, created.PlacementWorkspaceID, created.ID); err != nil {
		return created, err
	}
	return created, nil
}

// createPoolTaskWithQueries acquires the factory locks and inserts through an
// existing caller-owned transaction. It deliberately performs no commit-time
// publication or allocator work.
func (s *TaskService) createPoolTaskWithQueries(ctx context.Context, qtx *db.Queries, input PoolTaskCreateInput) (db.AgentTaskQueue, error) {
	if qtx == nil || input.Insert == nil {
		return db.AgentTaskQueue{}, errors.New("runtime pool task transaction and insert callback are required")
	}
	memberLock, err := s.lockPoolTaskCreateMember(ctx, qtx, input)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	agent, err := s.lockPoolTaskCreateAgent(ctx, qtx, input, memberLock)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	return s.createPoolTaskAfterLocks(ctx, qtx, input, memberLock.member, agent)
}

// lockPoolTaskCreateMember establishes the first relationship lock for Pool
// creation. Callers that already own a transaction may interpose a ChatSession
// lock before lockPoolTaskCreateAgent without duplicating authorization logic.
func (s *TaskService) lockPoolTaskCreateMember(ctx context.Context, qtx *db.Queries, input PoolTaskCreateInput) (poolTaskCreateMemberLock, error) {
	preview, err := qtx.GetAgent(ctx, input.AgentID)
	if err != nil {
		return poolTaskCreateMemberLock{}, fmt.Errorf("load Pool Agent lock keys: %w", err)
	}
	requester := input.OriginatorUserID
	if !requester.Valid {
		requester = preview.OwnerID
	}
	if !requester.Valid {
		return poolTaskCreateMemberLock{}, ErrPoolPlacementMemberRequired
	}

	// The first relationship lock is the placement Member. Agent follows;
	// Runtime selection is deliberately post-commit allocator work.
	member, err := qtx.LockPoolPlacementMember(ctx, db.LockPoolPlacementMemberParams{
		WorkspaceID:     preview.WorkspaceID,
		RequesterUserID: requester,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return poolTaskCreateMemberLock{}, ErrPoolPlacementMemberRequired
	}
	if err != nil {
		return poolTaskCreateMemberLock{}, fmt.Errorf("lock Pool placement Member: %w", err)
	}
	return poolTaskCreateMemberLock{preview: preview, requester: requester, member: member}, nil
}

func (s *TaskService) lockPoolTaskCreateAgent(ctx context.Context, qtx *db.Queries, input PoolTaskCreateInput, locked poolTaskCreateMemberLock) (db.Agent, error) {
	agent, err := qtx.LockPoolAgentForPlacement(ctx, input.AgentID)
	if err != nil {
		return db.Agent{}, fmt.Errorf("lock Pool Agent: %w", err)
	}

	lockedRequester := input.OriginatorUserID
	if !lockedRequester.Valid {
		lockedRequester = agent.OwnerID
	}
	if agent.ID != input.AgentID || agent.WorkspaceID != locked.preview.WorkspaceID ||
		(input.WorkspaceID.Valid && agent.WorkspaceID != input.WorkspaceID) ||
		lockedRequester != locked.requester || locked.member.WorkspaceID != agent.WorkspaceID ||
		locked.member.UserID != lockedRequester || agent.RuntimeBindingMode != runtimepool.BindingPool ||
		agent.ArchivedAt.Valid {
		return db.Agent{}, ErrPoolPlacementMemberRequired
	}
	return agent, nil
}

func (s *TaskService) createPoolTaskAfterLocks(ctx context.Context, qtx *db.Queries, input PoolTaskCreateInput, member db.Member, agent db.Agent) (db.AgentTaskQueue, error) {
	if input.BeforePlacement != nil {
		if err := input.BeforePlacement(ctx, qtx, member, agent); err != nil {
			return db.AgentTaskQueue{}, err
		}
	}
	return s.createPoolTaskLocked(ctx, qtx, input, member, agent)
}

func (s *TaskService) createPoolTaskLocked(
	ctx context.Context,
	qtx *db.Queries,
	input PoolTaskCreateInput,
	member db.Member,
	agent db.Agent,
) (db.AgentTaskQueue, error) {
	requirements, err := runtimepool.ParseRequirements(agent.RuntimeRequirements)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("parse Pool Agent requirements: %w", err)
	}
	canonicalRequirements, err := runtimepool.CanonicalRequirements(requirements)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("canonicalize Pool Agent requirements: %w", err)
	}

	// Placement resolution only consumes Queries. Construct a transaction-bound
	// service instead of copying TaskService, which contains synchronization
	// primitives and must never be copied after first use.
	txService := &TaskService{Queries: qtx}
	placementRequest := input.Placement
	placementRequest.AgentID = agent.ID
	placement, err := txService.ResolvePoolTaskPlacement(ctx, placementRequest)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	routing, err := newPoolRoutingSnapshot(agent, member, input.OriginatorUserID, input.TriggerUserID, placementRequest, placement, canonicalRequirements, input.Deferred)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	created, err := input.Insert(ctx, qtx, routing)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	if err := validatePoolTaskRoutingSnapshot(created, agent.ID, routing); err != nil {
		return db.AgentTaskQueue{}, err
	}
	return created, nil
}

func newPoolRoutingSnapshot(
	agent db.Agent,
	member db.Member,
	originator pgtype.UUID,
	triggerUserID pgtype.UUID,
	request PoolPlacementRequest,
	placement PoolPlacement,
	requirements []byte,
	deferred bool,
) (PoolRoutingSnapshot, error) {
	requester := originator
	if !requester.Valid {
		requester = agent.OwnerID
	}
	if triggerUserID.Valid && triggerUserID != member.UserID {
		return PoolRoutingSnapshot{}, ErrPoolPlacementMemberRequired
	}
	snapshot := PoolRoutingSnapshot{
		Status:                 runtimepool.StatusWaitingRuntime,
		RuntimeRequirements:    requirements,
		PlacementWorkspaceID:   member.WorkspaceID,
		RuntimeRequesterUserID: requester,
		RuntimeTriggerUserID:   triggerUserID,
		SessionAffinityState:   placement.State,
		ExplicitFreshSession:   request.ExplicitFreshSession,
		WaitReason:             pgtype.Text{String: "no_eligible_runtime", Valid: true},
	}
	if deferred {
		snapshot.Status = "deferred"
	}
	switch placement.State {
	case runtimepool.SessionAffinityNone:
		if placement.RuntimeID.Valid {
			return PoolRoutingSnapshot{}, errors.New("affinity-none placement has a Runtime")
		}
	case runtimepool.SessionAffinityPinned:
		if !placement.RuntimeID.Valid {
			return PoolRoutingSnapshot{}, errors.New("pinned placement has no Runtime")
		}
		snapshot.SessionAffinityRuntimeID = placement.RuntimeID
	case runtimepool.SessionAffinityRemoved:
		if placement.RuntimeID.Valid {
			return PoolRoutingSnapshot{}, errors.New("removed placement has a Runtime")
		}
		snapshot.Status = "cancelled"
		snapshot.CompletedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
		snapshot.WaitReason = pgtype.Text{String: "session_runtime_removed", Valid: true}
	default:
		return PoolRoutingSnapshot{}, fmt.Errorf("unsupported Pool affinity state %q", placement.State)
	}
	return snapshot, nil
}

func validatePoolTaskRoutingSnapshot(created db.AgentTaskQueue, agentID pgtype.UUID, expected PoolRoutingSnapshot) error {
	parsed, err := runtimepool.ParseRequirements(created.RuntimeRequirements)
	if err != nil {
		return fmt.Errorf("persisted Pool requirements: %w", err)
	}
	canonical, err := runtimepool.CanonicalRequirements(parsed)
	if err != nil {
		return fmt.Errorf("persisted Pool requirements: %w", err)
	}
	if created.AgentID != agentID || created.Status != expected.Status || created.RuntimeID.Valid ||
		created.RuntimeBindingMode != runtimepool.BindingPool || !bytes.Equal(canonical, expected.RuntimeRequirements) ||
		created.PlacementWorkspaceID != expected.PlacementWorkspaceID ||
		created.RuntimeRequesterUserID != expected.RuntimeRequesterUserID ||
		created.RuntimeTriggerUserID != expected.RuntimeTriggerUserID ||
		created.SessionAffinityState != expected.SessionAffinityState ||
		created.SessionAffinityRuntimeID != expected.SessionAffinityRuntimeID ||
		created.ExplicitFreshSession != expected.ExplicitFreshSession ||
		created.WaitReason != expected.WaitReason || created.CompletedAt.Valid != expected.CompletedAt.Valid {
		return errors.New("Pool task insert callback changed the locked routing snapshot")
	}
	return nil
}

// insertPoolAgentTask preserves the complete CreateAgentTask entry payload and
// substitutes only the routing columns produced under the factory locks.
func insertPoolAgentTask(
	ctx context.Context,
	qtx *db.Queries,
	params db.CreateAgentTaskParams,
	routing PoolRoutingSnapshot,
) (db.AgentTaskQueue, error) {
	return qtx.CreatePoolAgentTask(ctx, db.CreatePoolAgentTaskParams{
		AgentID:                  params.AgentID,
		IssueID:                  params.IssueID,
		Status:                   routing.Status,
		CompletedAt:              routing.CompletedAt,
		Priority:                 params.Priority,
		TriggerCommentID:         params.TriggerCommentID,
		CoalescedCommentIds:      params.CoalescedCommentIds,
		TriggerSummary:           params.TriggerSummary,
		ForceFreshSession:        params.ForceFreshSession,
		IsLeaderTask:             params.IsLeaderTask,
		HandoffNote:              params.HandoffNote,
		SquadID:                  params.SquadID,
		HeadSha:                  params.HeadSha,
		OriginatorUserID:         params.OriginatorUserID,
		AccountableUserID:        params.AccountableUserID,
		RuntimeMcpOverlay:        params.RuntimeMcpOverlay,
		RuntimeConnectedApps:     params.RuntimeConnectedApps,
		OriginatorSource:         params.OriginatorSource,
		DelegatedFromTaskID:      params.DelegatedFromTaskID,
		RuleVersionID:            params.RuleVersionID,
		RerunOfTaskID:            params.RerunOfTaskID,
		TriggerEvidenceKind:      params.TriggerEvidenceKind,
		TriggerEvidenceRefID:     params.TriggerEvidenceRefID,
		RuntimeRequirements:      routing.RuntimeRequirements,
		PlacementWorkspaceID:     routing.PlacementWorkspaceID,
		RuntimeRequesterUserID:   routing.RuntimeRequesterUserID,
		RuntimeTriggerUserID:     routing.RuntimeTriggerUserID,
		SessionAffinityState:     routing.SessionAffinityState,
		SessionAffinityRuntimeID: routing.SessionAffinityRuntimeID,
		ExplicitFreshSession:     routing.ExplicitFreshSession,
		WaitReason:               routing.WaitReason,
	})
}

func deferredChannelIssueTaskParams(params db.CreateAgentTaskParams, fireAt pgtype.Timestamptz) db.CreateDeferredChannelIssueTaskParams {
	return db.CreateDeferredChannelIssueTaskParams{
		AgentID:              params.AgentID,
		RuntimeID:            params.RuntimeID,
		IssueID:              params.IssueID,
		Priority:             params.Priority,
		TriggerCommentID:     params.TriggerCommentID,
		CoalescedCommentIds:  params.CoalescedCommentIds,
		TriggerSummary:       params.TriggerSummary,
		ForceFreshSession:    params.ForceFreshSession,
		IsLeaderTask:         params.IsLeaderTask,
		HandoffNote:          params.HandoffNote,
		SquadID:              params.SquadID,
		HeadSha:              params.HeadSha,
		OriginatorUserID:     params.OriginatorUserID,
		AccountableUserID:    params.AccountableUserID,
		RuntimeMcpOverlay:    params.RuntimeMcpOverlay,
		RuntimeConnectedApps: params.RuntimeConnectedApps,
		OriginatorSource:     params.OriginatorSource,
		DelegatedFromTaskID:  params.DelegatedFromTaskID,
		RuleVersionID:        params.RuleVersionID,
		RerunOfTaskID:        params.RerunOfTaskID,
		TriggerEvidenceKind:  params.TriggerEvidenceKind,
		TriggerEvidenceRefID: params.TriggerEvidenceRefID,
		FireAt:               fireAt,
	}
}

func insertPoolDeferredChannelIssueTask(
	ctx context.Context,
	qtx *db.Queries,
	params db.CreateDeferredChannelIssueTaskParams,
	routing PoolRoutingSnapshot,
) (db.AgentTaskQueue, error) {
	return qtx.CreatePoolDeferredChannelIssueTask(ctx, db.CreatePoolDeferredChannelIssueTaskParams{
		AgentID:                  params.AgentID,
		IssueID:                  params.IssueID,
		Status:                   routing.Status,
		CompletedAt:              routing.CompletedAt,
		Priority:                 params.Priority,
		TriggerCommentID:         params.TriggerCommentID,
		CoalescedCommentIds:      params.CoalescedCommentIds,
		TriggerSummary:           params.TriggerSummary,
		ForceFreshSession:        params.ForceFreshSession,
		IsLeaderTask:             params.IsLeaderTask,
		HandoffNote:              params.HandoffNote,
		SquadID:                  params.SquadID,
		HeadSha:                  params.HeadSha,
		OriginatorUserID:         params.OriginatorUserID,
		AccountableUserID:        params.AccountableUserID,
		RuntimeMcpOverlay:        params.RuntimeMcpOverlay,
		RuntimeConnectedApps:     params.RuntimeConnectedApps,
		OriginatorSource:         params.OriginatorSource,
		DelegatedFromTaskID:      params.DelegatedFromTaskID,
		RuleVersionID:            params.RuleVersionID,
		RerunOfTaskID:            params.RerunOfTaskID,
		TriggerEvidenceKind:      params.TriggerEvidenceKind,
		TriggerEvidenceRefID:     params.TriggerEvidenceRefID,
		FireAt:                   params.FireAt,
		RuntimeRequirements:      routing.RuntimeRequirements,
		PlacementWorkspaceID:     routing.PlacementWorkspaceID,
		RuntimeRequesterUserID:   routing.RuntimeRequesterUserID,
		RuntimeTriggerUserID:     routing.RuntimeTriggerUserID,
		SessionAffinityState:     routing.SessionAffinityState,
		SessionAffinityRuntimeID: routing.SessionAffinityRuntimeID,
		ExplicitFreshSession:     routing.ExplicitFreshSession,
		WaitReason:               routing.WaitReason,
	})
}

// insertPoolDeferredAgentTask preserves the delayed escalation payload while
// substituting only the routing columns decided under the factory locks.
func insertPoolDeferredAgentTask(
	ctx context.Context,
	qtx *db.Queries,
	params db.CreateDeferredAgentTaskParams,
	routing PoolRoutingSnapshot,
) (db.AgentTaskQueue, error) {
	return qtx.CreatePoolDeferredAgentTask(ctx, db.CreatePoolDeferredAgentTaskParams{
		AgentID:                  params.AgentID,
		IssueID:                  params.IssueID,
		Status:                   routing.Status,
		CompletedAt:              routing.CompletedAt,
		Priority:                 params.Priority,
		TriggerCommentID:         params.TriggerCommentID,
		TriggerSummary:           params.TriggerSummary,
		IsLeaderTask:             params.IsLeaderTask,
		SquadID:                  params.SquadID,
		EscalationForTaskID:      params.EscalationForTaskID,
		FireAt:                   params.FireAt,
		OriginatorUserID:         params.OriginatorUserID,
		AccountableUserID:        params.AccountableUserID,
		OriginatorSource:         params.OriginatorSource,
		DelegatedFromTaskID:      params.DelegatedFromTaskID,
		TriggerEvidenceKind:      params.TriggerEvidenceKind,
		TriggerEvidenceRefID:     params.TriggerEvidenceRefID,
		RuntimeRequirements:      routing.RuntimeRequirements,
		PlacementWorkspaceID:     routing.PlacementWorkspaceID,
		RuntimeRequesterUserID:   routing.RuntimeRequesterUserID,
		RuntimeTriggerUserID:     routing.RuntimeTriggerUserID,
		SessionAffinityState:     routing.SessionAffinityState,
		SessionAffinityRuntimeID: routing.SessionAffinityRuntimeID,
		ExplicitFreshSession:     routing.ExplicitFreshSession,
		WaitReason:               routing.WaitReason,
	})
}

func insertPoolQuickCreateTask(
	ctx context.Context,
	qtx *db.Queries,
	params db.CreateQuickCreateTaskParams,
	routing PoolRoutingSnapshot,
) (db.AgentTaskQueue, error) {
	return qtx.CreatePoolQuickCreateTask(ctx, db.CreatePoolQuickCreateTaskParams{
		AgentID:                  params.AgentID,
		Status:                   routing.Status,
		CompletedAt:              routing.CompletedAt,
		Priority:                 params.Priority,
		Context:                  params.Context,
		OriginatorUserID:         params.OriginatorUserID,
		AccountableUserID:        params.AccountableUserID,
		RuntimeMcpOverlay:        params.RuntimeMcpOverlay,
		RuntimeConnectedApps:     params.RuntimeConnectedApps,
		OriginatorSource:         params.OriginatorSource,
		TriggerEvidenceKind:      params.TriggerEvidenceKind,
		TriggerEvidenceRefID:     params.TriggerEvidenceRefID,
		RuntimeRequirements:      routing.RuntimeRequirements,
		PlacementWorkspaceID:     routing.PlacementWorkspaceID,
		RuntimeRequesterUserID:   routing.RuntimeRequesterUserID,
		RuntimeTriggerUserID:     routing.RuntimeTriggerUserID,
		SessionAffinityState:     routing.SessionAffinityState,
		SessionAffinityRuntimeID: routing.SessionAffinityRuntimeID,
		ExplicitFreshSession:     routing.ExplicitFreshSession,
		WaitReason:               routing.WaitReason,
	})
}

func insertPoolRetryTask(
	ctx context.Context,
	qtx *db.Queries,
	params db.CreateRetryTaskParams,
	routing PoolRoutingSnapshot,
) (db.AgentTaskQueue, error) {
	return qtx.CreatePoolRetryTask(ctx, db.CreatePoolRetryTaskParams{
		Status:                   routing.Status,
		CompletedAt:              routing.CompletedAt,
		MaxAttempts:              params.MaxAttempts,
		RuntimeMcpOverlay:        params.RuntimeMcpOverlay,
		RuntimeConnectedApps:     params.RuntimeConnectedApps,
		FireAt:                   params.FireAt,
		RuntimeRequirements:      routing.RuntimeRequirements,
		PlacementWorkspaceID:     routing.PlacementWorkspaceID,
		RuntimeRequesterUserID:   routing.RuntimeRequesterUserID,
		RuntimeTriggerUserID:     routing.RuntimeTriggerUserID,
		SessionAffinityState:     routing.SessionAffinityState,
		SessionAffinityRuntimeID: routing.SessionAffinityRuntimeID,
		ExplicitFreshSession:     routing.ExplicitFreshSession,
		WaitReason:               routing.WaitReason,
		ID:                       params.ID,
	})
}

func newPoolRetryTaskCreateInput(parent db.AgentTaskQueue, params db.CreateRetryTaskParams) PoolTaskCreateInput {
	return PoolTaskCreateInput{
		AgentID:          parent.AgentID,
		WorkspaceID:      parent.PlacementWorkspaceID,
		OriginatorUserID: parent.RuntimeRequesterUserID,
		TriggerUserID:    parent.RuntimeTriggerUserID,
		Placement:        PoolPlacementRequest{RetryOfTaskID: parent.ID},
		Deferred:         params.FireAt.Valid,
		BeforePlacement: func(ctx context.Context, qtx *db.Queries, member db.Member, agent db.Agent) error {
			locked, err := qtx.LockPoolRetrySourceTask(ctx, parent.ID)
			if err != nil {
				return fmt.Errorf("lock Pool retry source Task: %w", err)
			}
			if !samePoolRetrySourceSnapshot(parent, locked) || locked.Status != "failed" ||
				locked.AgentID != agent.ID || locked.PlacementWorkspaceID != member.WorkspaceID ||
				locked.RuntimeRequesterUserID != member.UserID {
				return errors.New("Pool retry source changed before creation")
			}
			return nil
		},
		Insert: func(ctx context.Context, qtx *db.Queries, routing PoolRoutingSnapshot) (db.AgentTaskQueue, error) {
			return insertPoolRetryTask(ctx, qtx, params, routing)
		},
	}
}

func (s *TaskService) createPoolRetryTaskLocked(
	ctx context.Context,
	qtx *db.Queries,
	input PoolTaskCreateInput,
	member db.Member,
	agent db.Agent,
) (db.AgentTaskQueue, error) {
	return s.createPoolTaskAfterLocks(ctx, qtx, input, member, agent)
}

func samePoolRetrySourceSnapshot(before, after db.AgentTaskQueue) bool {
	return before.ID == after.ID && before.AgentID == after.AgentID &&
		before.IssueID == after.IssueID && before.ChatSessionID == after.ChatSessionID &&
		before.AutopilotRunID == after.AutopilotRunID && before.Status == after.Status &&
		before.RuntimeID == after.RuntimeID && before.Attempt == after.Attempt &&
		before.MaxAttempts == after.MaxAttempts && before.FailureReason == after.FailureReason &&
		before.RuntimeBindingMode == after.RuntimeBindingMode &&
		before.PlacementWorkspaceID == after.PlacementWorkspaceID &&
		before.RuntimeRequesterUserID == after.RuntimeRequesterUserID &&
		before.RuntimeTriggerUserID == after.RuntimeTriggerUserID &&
		before.SessionAffinityState == after.SessionAffinityState &&
		before.SessionAffinityRuntimeID == after.SessionAffinityRuntimeID
}
