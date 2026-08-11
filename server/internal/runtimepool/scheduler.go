package runtimepool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	WorkspaceSweepLimit    = 32
	DeferredPromotionLimit = 64
	runtimeHeartbeatWindow = 150 * time.Second
)

type TxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

type Scheduler struct {
	q        *db.Queries
	tx       TxStarter
	liveness LivenessReader
	now      func() time.Time
	getTasks func(context.Context, []pgtype.UUID) ([]db.AgentTaskQueue, error)

	sweepMu     sync.Mutex
	sweepCursor pgtype.UUID
}

func NewScheduler(q *db.Queries, tx TxStarter, liveness LivenessReader) *Scheduler {
	scheduler := &Scheduler{q: q, tx: tx, liveness: liveness, now: time.Now}
	if q != nil {
		scheduler.getTasks = q.ListPoolTasksByIDs
	}
	return scheduler
}

func filterAliveInOrder(in []db.ListPoolRuntimeCandidatesRow, alive map[string]bool, authoritative bool, now time.Time) []db.ListPoolRuntimeCandidatesRow {
	out := make([]db.ListPoolRuntimeCandidatesRow, 0, len(in))
	for _, candidate := range in {
		if runtimeIsAlive(candidate.AgentRuntime, alive, authoritative, now) {
			out = append(out, candidate)
		}
	}
	return out
}

func runtimeIsAlive(runtime db.AgentRuntime, alive map[string]bool, authoritative bool, now time.Time) bool {
	if authoritative {
		return alive[util.UUIDToString(runtime.ID)]
	}
	return runtime.LastSeenAt.Valid && !runtime.LastSeenAt.Time.Before(now.Add(-runtimeHeartbeatWindow))
}

// runtimeSupportsPoolQuickCreate treats ordinary Tasks as version-agnostic,
// while a recognized Quick Create marker requires the selected Runtime's CLI
// to meet the feature floor. Malformed markers and metadata fail closed.
func runtimeSupportsPoolQuickCreate(task db.AgentTaskQueue, runtime db.AgentRuntime) bool {
	quickCreate, recognized, err := ParseQuickCreateContext(task.Context)
	if err != nil {
		return false
	}
	if !recognized {
		return true
	}
	minimum := agentpkg.MinQuickCreateCLIVersion
	if quickCreate.Priority != "" || quickCreate.DueDate != "" {
		minimum = agentpkg.MinQuickCreateFieldsCLIVersion
	}
	return agentpkg.CheckMinCLIVersionFor(agentpkg.ReadRuntimeCLIVersion(runtime.Metadata), minimum) == nil
}

type placementPlan struct {
	task         db.AgentTaskQueue
	requirements Requirements
	candidates   []db.ListPoolRuntimeCandidatesRow
	pinnedReason string
}

func (s *Scheduler) AssignWaiting(ctx context.Context, request AssignRequest) (AssignResult, error) {
	var result AssignResult
	if s == nil || s.q == nil || s.tx == nil {
		return result, errors.New("runtime pool scheduler is not configured")
	}
	if !request.WorkspaceID.Valid {
		return result, errors.New("runtime pool assignment requires a Workspace")
	}
	limit := request.Limit
	if limit <= 0 || limit > AssignmentBatchLimit {
		limit = AssignmentBatchLimit
	}

	tasks, err := s.q.ListWaitingPoolTasks(ctx, db.ListWaitingPoolTasksParams{
		PlacementWorkspaceID: request.WorkspaceID,
		ScanLimit:            WaitingTaskScanLimit,
	})
	if err != nil {
		return result, fmt.Errorf("list waiting Pool Tasks: %w", err)
	}

	plans := make([]placementPlan, 0, len(tasks))
	runtimeIDs := make([]string, 0)
	seenRuntimeIDs := make(map[string]struct{})
	for _, task := range tasks {
		requirements, parseErr := ParseRequirements(task.RuntimeRequirements)
		if parseErr != nil {
			return result, fmt.Errorf("parse Runtime requirements for Task %s: %w", util.UUIDToString(task.ID), parseErr)
		}
		plan := placementPlan{task: task, requirements: requirements}
		switch task.SessionAffinityState {
		case SessionAffinityNone:
			plan.candidates, err = s.q.ListPoolRuntimeCandidates(ctx, db.ListPoolRuntimeCandidatesParams{
				RequesterUserID: task.RuntimeRequesterUserID,
				TriggerUserID:   task.RuntimeTriggerUserID,
				WorkspaceID:     request.WorkspaceID,
				RequirementsAll: requirements.CapabilitiesAll,
				RuntimeLimit:    RuntimeScanLimit,
			})
		case SessionAffinityPinned:
			if !task.SessionAffinityRuntimeID.Valid {
				plan.pinnedReason = "session_runtime_offline"
				break
			}
			var candidate db.AgentRuntime
			candidate, err = s.q.GetPinnedPoolRuntimeCandidate(ctx, db.GetPinnedPoolRuntimeCandidateParams{
				RequesterUserID: task.RuntimeRequesterUserID,
				TriggerUserID:   task.RuntimeTriggerUserID,
				RuntimeID:       task.SessionAffinityRuntimeID,
				WorkspaceID:     request.WorkspaceID,
				RequirementsAll: requirements.CapabilitiesAll,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				plan.pinnedReason, err = s.diagnosePinnedReason(ctx, task, requirements)
			} else if err == nil {
				plan.candidates = []db.ListPoolRuntimeCandidatesRow{{AgentRuntime: candidate}}
			}
		default:
			continue
		}
		if err != nil {
			return result, fmt.Errorf("list Runtime candidates for Task %s: %w", util.UUIDToString(task.ID), err)
		}
		compatibleCandidates := plan.candidates[:0]
		for _, candidate := range plan.candidates {
			if runtimeSupportsPoolQuickCreate(task, candidate.AgentRuntime) {
				compatibleCandidates = append(compatibleCandidates, candidate)
			}
		}
		if task.SessionAffinityState == SessionAffinityPinned && len(plan.candidates) > 0 && len(compatibleCandidates) == 0 {
			plan.pinnedReason = "session_runtime_capability_mismatch"
		}
		plan.candidates = compatibleCandidates
		for _, candidate := range plan.candidates {
			id := util.UUIDToString(candidate.AgentRuntime.ID)
			if _, ok := seenRuntimeIDs[id]; ok {
				continue
			}
			seenRuntimeIDs[id] = struct{}{}
			runtimeIDs = append(runtimeIDs, id)
		}
		plans = append(plans, plan)
	}

	alive := map[string]bool{}
	authoritative := false
	if len(runtimeIDs) > 0 && s.liveness != nil {
		alive, authoritative = s.liveness.IsAliveBatch(ctx, runtimeIDs)
		if alive == nil {
			alive = map[string]bool{}
		}
	}
	now := s.now()
	for i := range plans {
		plans[i].candidates = filterAliveInOrder(plans[i].candidates, alive, authoritative, now)
		if plans[i].task.SessionAffinityState == SessionAffinityPinned && plans[i].pinnedReason == "" && len(plans[i].candidates) == 0 {
			plans[i].pinnedReason = "session_runtime_offline"
		}
		if plans[i].pinnedReason != "" {
			if err := s.updatePinnedReason(ctx, plans[i].task, plans[i].pinnedReason); err != nil {
				return result, err
			}
		}
	}

	for _, plan := range plans {
		if len(result.Assigned) >= limit {
			break
		}
		if plan.pinnedReason != "" {
			continue
		}
		for _, candidate := range plan.candidates {
			assigned, stop, reason, assignErr := s.assignCandidate(ctx, plan, candidate.AgentRuntime, alive, authoritative)
			if assignErr != nil {
				return result, assignErr
			}
			if reason != "" && plan.task.SessionAffinityState == SessionAffinityPinned {
				if err := s.updatePinnedReason(ctx, plan.task, reason); err != nil {
					return result, err
				}
			}
			if assigned != nil {
				result.Assigned = append(result.Assigned, *assigned)
				break
			}
			if stop {
				break
			}
		}
	}
	return result, nil
}

func (s *Scheduler) diagnosePinnedReason(ctx context.Context, task db.AgentTaskQueue, requirements Requirements) (string, error) {
	runtime, err := s.q.GetAgentRuntime(ctx, task.SessionAffinityRuntimeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "session_runtime_offline", nil
	}
	if err != nil {
		return "", err
	}
	if runtime.WorkspaceID != task.PlacementWorkspaceID {
		return "session_runtime_unauthorized", nil
	}
	_, err = s.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      task.RuntimeRequesterUserID,
		WorkspaceID: task.PlacementWorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "session_runtime_unauthorized", nil
	}
	if err != nil {
		return "", err
	}
	if !RuntimeMatchesTriggerPolicy(runtime, task.RuntimeTriggerUserID) {
		return "session_runtime_unauthorized", nil
	}
	if !ContainsAllCapabilities(runtime.Capabilities, requirements.CapabilitiesAll) {
		return "session_runtime_capability_mismatch", nil
	}
	if runtime.Status != "online" {
		return "session_runtime_offline", nil
	}
	return "session_runtime_offline", nil
}

func (s *Scheduler) updatePinnedReason(ctx context.Context, task db.AgentTaskQueue, reason string) error {
	_, err := s.q.UpdatePinnedPoolTaskWaitReasonCAS(ctx, db.UpdatePinnedPoolTaskWaitReasonCASParams{
		Reason:                           pgtype.Text{String: reason, Valid: true},
		TaskID:                           task.ID,
		ExpectedStatus:                   task.Status,
		ExpectedRuntimeID:                task.RuntimeID,
		ExpectedAgentID:                  task.AgentID,
		ExpectedChatSessionID:            task.ChatSessionID,
		ExpectedRuntimeBindingMode:       task.RuntimeBindingMode,
		ExpectedPlacementWorkspaceID:     task.PlacementWorkspaceID,
		ExpectedRuntimeRequesterUserID:   task.RuntimeRequesterUserID,
		ExpectedRuntimeTriggerUserID:     task.RuntimeTriggerUserID,
		ExpectedRuntimeRequirements:      task.RuntimeRequirements,
		ExpectedSessionAffinityState:     task.SessionAffinityState,
		ExpectedSessionAffinityRuntimeID: task.SessionAffinityRuntimeID,
		ExpectedExplicitFreshSession:     task.ExplicitFreshSession,
		ExpectedWaitReason:               task.WaitReason,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("update pinned Pool wait reason: %w", err)
	}
	return nil
}

func (s *Scheduler) assignCandidate(ctx context.Context, plan placementPlan, candidate db.AgentRuntime, alive map[string]bool, authoritative bool) (*db.AgentTaskQueue, bool, string, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return nil, true, "", fmt.Errorf("begin Runtime Pool assignment: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	member, err := qtx.LockPoolPlacementMember(ctx, db.LockPoolPlacementMemberParams{
		WorkspaceID:     plan.task.PlacementWorkspaceID,
		RequesterUserID: plan.task.RuntimeRequesterUserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, true, pinnedReason(plan.task, "session_runtime_unauthorized"), nil
	}
	if err != nil {
		return nil, true, "", fmt.Errorf("lock Pool placement Member: %w", err)
	}
	if member.WorkspaceID != plan.task.PlacementWorkspaceID || member.UserID != plan.task.RuntimeRequesterUserID {
		return nil, true, pinnedReason(plan.task, "session_runtime_unauthorized"), nil
	}

	runtime, err := qtx.LockPoolRuntimeForPlacement(ctx, candidate.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		// SKIP LOCKED is a transient placement race. In particular, a pinned
		// Task must not turn a short lock conflict into an offline diagnosis.
		return nil, plan.task.SessionAffinityState == SessionAffinityPinned, "", nil
	}
	if err != nil {
		return nil, true, "", fmt.Errorf("lock Pool Runtime: %w", err)
	}
	if runtime.WorkspaceID != plan.task.PlacementWorkspaceID || !RuntimeMatchesTriggerPolicy(runtime, plan.task.RuntimeTriggerUserID) {
		return nil, plan.task.SessionAffinityState == SessionAffinityPinned, pinnedReason(plan.task, "session_runtime_unauthorized"), nil
	}
	if !ContainsAllCapabilities(runtime.Capabilities, plan.requirements.CapabilitiesAll) {
		return nil, plan.task.SessionAffinityState == SessionAffinityPinned, pinnedReason(plan.task, "session_runtime_capability_mismatch"), nil
	}
	if runtime.Status != "online" || !runtimeIsAlive(runtime, alive, authoritative, s.now()) {
		return nil, plan.task.SessionAffinityState == SessionAffinityPinned, pinnedReason(plan.task, "session_runtime_offline"), nil
	}
	if !runtimeSupportsPoolQuickCreate(plan.task, runtime) {
		return nil, plan.task.SessionAffinityState == SessionAffinityPinned, pinnedReason(plan.task, "session_runtime_capability_mismatch"), nil
	}
	if plan.task.SessionAffinityState == SessionAffinityPinned && runtime.ID != plan.task.SessionAffinityRuntimeID {
		return nil, true, "session_runtime_offline", nil
	}

	if plan.task.ChatSessionID.Valid {
		session, lockErr := qtx.LockPoolChatSessionForPlacement(ctx, plan.task.ChatSessionID)
		if errors.Is(lockErr, pgx.ErrNoRows) {
			return nil, true, "", nil
		}
		if lockErr != nil {
			return nil, true, "", fmt.Errorf("lock Pool Chat Session: %w", lockErr)
		}
		if session.WorkspaceID != plan.task.PlacementWorkspaceID || session.AgentID != plan.task.AgentID {
			return nil, true, "", nil
		}
		head, headErr := qtx.IsPoolChatExecutionHead(ctx, db.IsPoolChatExecutionHeadParams{
			TaskID:        plan.task.ID,
			ChatSessionID: plan.task.ChatSessionID,
		})
		if headErr != nil {
			return nil, true, "", fmt.Errorf("validate Pool Chat execution head: %w", headErr)
		}
		if !head {
			return nil, true, "", nil
		}
	}

	agent, err := qtx.LockPoolAgentForPlacement(ctx, plan.task.AgentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, true, "", nil
	}
	if err != nil {
		return nil, true, "", fmt.Errorf("lock Pool Agent: %w", err)
	}
	if agent.WorkspaceID != plan.task.PlacementWorkspaceID || agent.ArchivedAt.Valid {
		return nil, true, "", nil
	}
	if plan.task.SessionAffinityState == SessionAffinityNone {
		occupied, countErr := qtx.CountRuntimeCapacityBearingTasks(ctx, runtime.ID)
		if countErr != nil {
			return nil, true, "", fmt.Errorf("count Runtime capacity-bearing Tasks: %w", countErr)
		}
		if occupied != 0 {
			return nil, false, "", nil
		}
	}

	assigned, err := qtx.AssignWaitingPoolTask(ctx, db.AssignWaitingPoolTaskParams{
		RuntimeID:            runtime.ID,
		TaskID:               plan.task.ID,
		PlacementWorkspaceID: plan.task.PlacementWorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, true, "", nil
	}
	if err != nil {
		return nil, true, "", fmt.Errorf("assign waiting Pool Task: %w", err)
	}
	if !sameRoutingSnapshot(plan.task, assigned) || assigned.Status != "queued" || assigned.RuntimeID != runtime.ID {
		return nil, true, "", nil
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, true, "", fmt.Errorf("commit Runtime Pool assignment: %w", err)
	}
	return &assigned, true, "", nil
}

func pinnedReason(task db.AgentTaskQueue, reason string) string {
	if task.SessionAffinityState != SessionAffinityPinned {
		return ""
	}
	return reason
}

func sameRoutingSnapshot(before, after db.AgentTaskQueue) bool {
	return before.ID == after.ID &&
		before.AgentID == after.AgentID &&
		before.ChatSessionID == after.ChatSessionID &&
		before.RuntimeBindingMode == after.RuntimeBindingMode &&
		before.PlacementWorkspaceID == after.PlacementWorkspaceID &&
		before.RuntimeRequesterUserID == after.RuntimeRequesterUserID &&
		before.RuntimeTriggerUserID == after.RuntimeTriggerUserID &&
		bytes.Equal(before.RuntimeRequirements, after.RuntimeRequirements) &&
		before.SessionAffinityState == after.SessionAffinityState &&
		before.SessionAffinityRuntimeID == after.SessionAffinityRuntimeID &&
		before.ExplicitFreshSession == after.ExplicitFreshSession
}

func (s *Scheduler) SweepWaiting(ctx context.Context, requestedLimit int) ([]AssignResult, error) {
	if s == nil || s.q == nil {
		return nil, errors.New("runtime pool scheduler is not configured")
	}
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()

	limit := requestedLimit
	if limit <= 0 || limit > WorkspaceSweepLimit {
		limit = WorkspaceSweepLimit
	}
	after := s.sweepCursor
	workspaces, err := s.q.ListRuntimePoolSweepWorkspaces(ctx, db.ListRuntimePoolSweepWorkspacesParams{
		AfterWorkspaceID: after,
		WorkspaceLimit:   int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list Runtime Pool sweep Workspaces: %w", err)
	}
	if len(workspaces) == 0 && after.Valid {
		workspaces, err = s.q.ListRuntimePoolSweepWorkspaces(ctx, db.ListRuntimePoolSweepWorkspacesParams{
			AfterWorkspaceID: pgtype.UUID{},
			WorkspaceLimit:   int32(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("wrap Runtime Pool sweep cursor: %w", err)
		}
		if len(workspaces) == 0 {
			s.sweepCursor = pgtype.UUID{}
		}
	}

	results := make([]AssignResult, 0, len(workspaces))
	for _, workspaceID := range workspaces {
		promoted, promoteErr := s.q.PromoteDuePoolDeferredTasksForWorkspace(ctx, db.PromoteDuePoolDeferredTasksForWorkspaceParams{
			PlacementWorkspaceID: workspaceID,
			Now:                  pgtype.Timestamptz{Time: s.now(), Valid: true},
			PromoteLimit:         DeferredPromotionLimit,
		})
		if promoteErr != nil {
			return results, fmt.Errorf("promote due Pool Tasks in Workspace %s: %w", util.UUIDToString(workspaceID), promoteErr)
		}
		result, assignErr := s.AssignWaiting(ctx, AssignRequest{
			WorkspaceID: workspaceID,
			Limit:       AssignmentBatchLimit,
		})
		var verifyErr error
		result.PromotedWaiting, verifyErr = s.persistedPromotedWaiting(ctx, workspaceID, promoted, result.Assigned)
		if verifyErr != nil {
			if len(result.Assigned) != 0 || len(result.PromotedWaiting) != 0 {
				results = append(results, result)
			}
			verifyErr = fmt.Errorf("verify promoted Pool Tasks in Workspace %s: %w", util.UUIDToString(workspaceID), verifyErr)
			return results, errors.Join(assignErr, verifyErr)
		}
		if len(result.Assigned) != 0 || len(result.PromotedWaiting) != 0 {
			results = append(results, result)
		}
		if assignErr != nil {
			return results, assignErr
		}
		s.sweepCursor = workspaceID
	}
	return results, nil
}

func (s *Scheduler) persistedPromotedWaiting(ctx context.Context, workspaceID pgtype.UUID, promoted, assigned []db.AgentTaskQueue) ([]db.AgentTaskQueue, error) {
	assignedIDs := make(map[pgtype.UUID]struct{}, len(assigned))
	for _, task := range assigned {
		assignedIDs[task.ID] = struct{}{}
	}
	fallback := make([]db.AgentTaskQueue, 0, len(promoted))
	taskIDs := make([]pgtype.UUID, 0, len(promoted))
	for _, promotedTask := range promoted {
		if _, wasAssigned := assignedIDs[promotedTask.ID]; wasAssigned {
			continue
		}
		fallback = append(fallback, promotedTask)
		taskIDs = append(taskIDs, promotedTask.ID)
	}
	if len(taskIDs) == 0 {
		return nil, nil
	}
	if s.getTasks == nil {
		return fallback, errors.New("runtime pool promoted Task reader is not configured")
	}
	currentTasks, err := s.getTasks(ctx, taskIDs)
	if err != nil {
		return fallback, err
	}
	currentByID := make(map[pgtype.UUID]db.AgentTaskQueue, len(currentTasks))
	for _, current := range currentTasks {
		currentByID[current.ID] = current
	}
	persisted := make([]db.AgentTaskQueue, 0, len(currentTasks))
	for _, promotedTask := range fallback {
		current, exists := currentByID[promotedTask.ID]
		if !exists {
			continue
		}
		if current.Status != StatusWaitingRuntime ||
			current.RuntimeBindingMode != BindingPool ||
			current.PlacementWorkspaceID != workspaceID ||
			current.RuntimeID.Valid {
			continue
		}
		persisted = append(persisted, current)
	}
	return persisted, nil
}
