package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var (
	ErrClaimAttemptAcknowledged = errors.New("claim attempt already acknowledged")
	ErrClaimAttemptExpired      = errors.New("claim attempt expired")
	ErrClaimAttemptMismatch     = errors.New("claim attempt id reused with different parameters")
	ErrClaimAttemptNotFound     = errors.New("claim attempt not found")
	ErrClaimAttemptProcessing   = errors.New("claim attempt is still processing")
)

// ClaimAttemptRequest is the canonical, authorization-filtered input to one
// idempotent machine-level claim. Fingerprint includes every response-affecting
// request property; PrincipalKey binds the opaque UUID to the authenticated
// daemon or user without storing credentials.
type ClaimAttemptRequest struct {
	ID                 pgtype.UUID
	DaemonID           string
	PrincipalKey       string
	RequestFingerprint string
	RuntimeIDs         []pgtype.UUID
	MaxTasks           int
}

// ClaimAttemptResult returns the durable task set for an attempt. Replayed is
// true when the receipt already existed before this request.
type ClaimAttemptResult struct {
	Tasks    []db.AgentTaskQueue
	Replayed bool
}

// ClaimTasksForRuntimesAttempt performs the v2 batch claim and writes each
// task's attempt mapping in one transaction. A committed attempt is therefore
// always replayable, while cancellation or a process crash rolls back both the
// dispatches and the receipt. Concurrent WS/HTTP calls with the same UUID
// serialize on the receipt's primary key and observe the same task set.
func (s *TaskService) ClaimTasksForRuntimesAttempt(ctx context.Context, req ClaimAttemptRequest) (ClaimAttemptResult, error) {
	var (
		result         ClaimAttemptResult
		promoted       []db.AgentTaskQueue
		fresh          []db.AgentTaskQueue
		reclaimedCount int
	)

	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		if _, err := qtx.ExpireDaemonClaimAttempts(ctx); err != nil {
			return fmt.Errorf("expire claim attempts: %w", err)
		}
		if _, err := qtx.DeleteOldDaemonClaimAttempts(ctx); err != nil {
			return fmt.Errorf("delete old claim attempts: %w", err)
		}

		attempt, err := qtx.CreateDaemonClaimAttempt(ctx, db.CreateDaemonClaimAttemptParams{
			ID:                 req.ID,
			DaemonID:           req.DaemonID,
			PrincipalKey:       req.PrincipalKey,
			RequestFingerprint: req.RequestFingerprint,
			RuntimeIds:         req.RuntimeIDs,
			MaxTasks:           int32(req.MaxTasks),
			ExpiresInSeconds:   claimResponseRecoveryWindow.Seconds(),
		})
		created := err == nil
		if errors.Is(err, pgx.ErrNoRows) {
			attempt, err = qtx.GetDaemonClaimAttemptForUpdate(ctx, req.ID)
		}
		if err != nil {
			return fmt.Errorf("create or load claim attempt: %w", err)
		}

		if !created {
			if attempt.DaemonID != req.DaemonID || attempt.PrincipalKey != req.PrincipalKey {
				return ErrClaimAttemptNotFound
			}
			if attempt.RequestFingerprint != req.RequestFingerprint || attempt.MaxTasks != int32(req.MaxTasks) {
				return ErrClaimAttemptMismatch
			}
			switch attempt.Status {
			case "ready":
				tasks, err := qtx.ListDaemonClaimAttemptTasks(ctx, req.ID)
				if err != nil {
					return fmt.Errorf("load claim attempt tasks: %w", err)
				}
				result.Tasks = tasks
				result.Replayed = true
				return nil
			case "acknowledged":
				return ErrClaimAttemptAcknowledged
			case "expired":
				return ErrClaimAttemptExpired
			default:
				// A processing row is normally invisible because creation, task
				// dispatch, and ready transition commit together. Keep this guard
				// for manual repairs or a future staged implementation.
				return ErrClaimAttemptProcessing
			}
		}

		if req.MaxTasks <= 0 || len(req.RuntimeIDs) == 0 {
			if _, err := qtx.MarkDaemonClaimAttemptReady(ctx, req.ID); err != nil {
				return fmt.Errorf("mark empty claim attempt ready: %w", err)
			}
			result.Tasks = []db.AgentTaskQueue{}
			return nil
		}

		promoted, err = qtx.PromoteDueDeferredTasksForRuntimes(ctx, req.RuntimeIDs)
		if err != nil {
			return fmt.Errorf("promote deferred tasks: %w", err)
		}

		reclaimed, err := qtx.ReclaimStaleDispatchedTasksForAttempt(ctx, db.ReclaimStaleDispatchedTasksForAttemptParams{
			PrepareLeaseSecs:  prepareLeaseDuration.Seconds(),
			ClaimAttemptID:    req.ID,
			StartOrdinal:      0,
			RuntimeIds:        req.RuntimeIDs,
			ClaimRecoverySecs: claimResponseRecoveryWindow.Seconds(),
			MaxTasks:          int32(req.MaxTasks),
		})
		if err != nil {
			return fmt.Errorf("reclaim stale dispatched tasks: %w", err)
		}
		sort.Slice(reclaimed, func(i, j int) bool {
			return reclaimed[i].ClaimAttemptOrdinal.Int32 < reclaimed[j].ClaimAttemptOrdinal.Int32
		})
		result.Tasks = append(result.Tasks, reclaimed...)
		reclaimedCount = len(reclaimed)

		if len(result.Tasks) < req.MaxTasks {
			candidates, err := qtx.ListQueuedClaimCandidatesByRuntimes(ctx, req.RuntimeIDs)
			if err != nil {
				return fmt.Errorf("list queued claim candidates: %w", err)
			}
			triedAgents := make(map[string]struct{}, len(candidates))
			for i := range candidates {
				if len(result.Tasks) >= req.MaxTasks {
					break
				}
				agentKey := util.UUIDToString(candidates[i].AgentID)
				if _, tried := triedAgents[agentKey]; tried {
					continue
				}
				triedAgents[agentKey] = struct{}{}

				agent, err := qtx.GetAgentForClaimUpdate(ctx, candidates[i].AgentID)
				if err != nil {
					return fmt.Errorf("load agent for claim: %w", err)
				}
				running, err := qtx.CountRunningTasks(ctx, candidates[i].AgentID)
				if err != nil {
					return fmt.Errorf("count running tasks: %w", err)
				}
				if running >= int64(agent.MaxConcurrentTasks) {
					continue
				}

				task, err := qtx.ClaimAgentTaskForAttempt(ctx, db.ClaimAgentTaskForAttemptParams{
					PrepareLeaseSecs:    prepareLeaseDuration.Seconds(),
					ClaimAttemptID:      req.ID,
					ClaimAttemptOrdinal: pgtype.Int4{Int32: int32(len(result.Tasks)), Valid: true},
					AgentID:             candidates[i].AgentID,
					RuntimeIds:          req.RuntimeIDs,
				})
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				if err != nil {
					return fmt.Errorf("claim task: %w", err)
				}
				result.Tasks = append(result.Tasks, task)
				fresh = append(fresh, task)
			}
		}

		if _, err := qtx.MarkDaemonClaimAttemptReady(ctx, req.ID); err != nil {
			return fmt.Errorf("mark claim attempt ready: %w", err)
		}
		return nil
	})
	if err != nil {
		return ClaimAttemptResult{}, err
	}

	// The state transition is already durable. Reproduce the existing claim
	// path's cache, analytics, agent-status, and realtime side effects after the
	// transaction commits; none of them participate in idempotency correctness.
	for _, task := range promoted {
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
		s.NotifyTaskEnqueued(ctx, task)
	}
	for i := 0; i < reclaimedCount; i++ {
		task := result.Tasks[i]
		slog.Info("stale dispatched task reclaimed for claim attempt",
			"task_id", util.UUIDToString(task.ID),
			"claim_attempt_id", util.UUIDToString(req.ID))
	}
	affectedAgents := make(map[string]pgtype.UUID, len(fresh))
	for _, task := range fresh {
		s.captureTaskDispatched(ctx, task)
		s.broadcastTaskDispatch(ctx, task)
		affectedAgents[util.UUIDToString(task.AgentID)] = task.AgentID
	}
	for _, agentID := range affectedAgents {
		s.ReconcileAgentStatus(ctx, agentID)
	}

	return result, nil
}

// AcknowledgeClaimAttempt records that the daemon received a replayable task
// set. It is idempotent and principal-scoped; StartTask is an independent
// implicit acknowledgement for the response-delivered/ACK-lost case.
func (s *TaskService) AcknowledgeClaimAttempt(ctx context.Context, id pgtype.UUID, daemonID, principalKey string) error {
	_, err := s.Queries.AcknowledgeDaemonClaimAttempt(ctx, db.AcknowledgeDaemonClaimAttemptParams{
		ID:           id,
		DaemonID:     daemonID,
		PrincipalKey: principalKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrClaimAttemptNotFound
	}
	return err
}
