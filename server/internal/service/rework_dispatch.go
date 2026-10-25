package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var (
	ErrReworkDispatchConflict    = errors.New("rework dispatch conflicts with existing work")
	ErrReworkDispatchMismatch    = errors.New("rework dispatch operation key is bound to another package or agent")
	ErrReworkDispatchTarget      = errors.New("rework dispatch supports only the issue's directly assigned agent")
	ErrReworkDispatchMissingTask = errors.New("rework dispatch recorded task is unavailable")
	ErrReworkDispatchSource      = errors.New("rework dispatch requires a terminal source run for the same issue, agent and runtime")
	ErrInvalidReworkDispatch     = errors.New("invalid rework dispatch")
)

// ReworkDispatchRequest is deliberately separate from a manual rerun. Its
// package is immutable: the caller supplies the frozen instruction and its
// SHA-256, and an operation key names precisely one package on one issue.
type ReworkDispatchRequest struct {
	OperationKey    string
	Instruction     string
	PayloadSHA256   string
	ExpectedAgentID pgtype.UUID
	SourceTaskID    pgtype.UUID
	ActorUserID     pgtype.UUID
}

type ReworkDispatchResult struct {
	Task     db.AgentTaskQueue
	Replayed bool
}

func reworkPayloadSHA256(instruction string) string {
	sum := sha256.Sum256([]byte(instruction))
	return hex.EncodeToString(sum[:])
}

func validateReworkDispatchRequest(req ReworkDispatchRequest) error {
	if strings.TrimSpace(req.OperationKey) == "" || len(req.OperationKey) > 256 {
		return fmt.Errorf("%w: operation_key is required and must be at most 256 characters", ErrInvalidReworkDispatch)
	}
	for _, ch := range req.OperationKey {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("._:-", ch)) {
			return fmt.Errorf("%w: operation_key must use letters, digits, dot, underscore, colon or hyphen", ErrInvalidReworkDispatch)
		}
	}
	if req.Instruction == "" || len(req.Instruction) > 64*1024 {
		return fmt.Errorf("%w: instruction must contain 1 to 65536 UTF-8 bytes", ErrInvalidReworkDispatch)
	}
	if !req.ExpectedAgentID.Valid {
		return fmt.Errorf("%w: agent_id is required", ErrInvalidReworkDispatch)
	}
	if !req.SourceTaskID.Valid {
		return fmt.Errorf("%w: source_task_id is required", ErrInvalidReworkDispatch)
	}
	if req.PayloadSHA256 != reworkPayloadSHA256(req.Instruction) {
		return fmt.Errorf("%w: payload_sha256 does not match instruction", ErrInvalidReworkDispatch)
	}
	return nil
}

// DispatchRework atomically records a correction-package receipt and creates
// exactly one queue row. It intentionally does not call RerunIssue: ordinary
// rerun retains its historical replacement/queue-behind-active semantics.
func (s *TaskService) DispatchRework(ctx context.Context, issueID, workspaceID pgtype.UUID, req ReworkDispatchRequest, canInvoke func(db.Agent) bool) (ReworkDispatchResult, error) {
	if err := validateReworkDispatchRequest(req); err != nil {
		return ReworkDispatchResult{}, err
	}
	if s.TxStarter == nil {
		return ReworkDispatchResult{}, errors.New("rework dispatch requires a transaction starter")
	}

	// Permission checks and optional app-session builders may use the pool or
	// an external service. Prepare them before taking transactional row locks.
	preparedIssue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
	if err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("load rework issue: %w", err)
	}
	preparedAgent, err := s.Queries.GetAgent(ctx, req.ExpectedAgentID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && preparedAgent.WorkspaceID != workspaceID {
		return ReworkDispatchResult{}, ErrReworkDispatchTarget
	}
	if err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("load rework agent: %w", err)
	}
	if canInvoke != nil && !canInvoke(preparedAgent) {
		return ReworkDispatchResult{}, ErrRerunInvokeNotAllowed
	}
	preparedAttribution := s.attributionForIssueTask(ctx, preparedIssue, pgtype.UUID{}, attribution.SourceDirectHuman, req.ActorUserID)
	preparedAttribution, err = s.applyAttributionFallback(ctx, preparedAttribution, preparedAgent)
	if err != nil {
		return ReworkDispatchResult{}, err
	}
	var overlay runtimeMCPOverlayData
	if _, err := s.Queries.GetIssueReworkDispatchReceipt(ctx, db.GetIssueReworkDispatchReceiptParams{
		WorkspaceID: workspaceID, IssueID: issueID, OperationKey: req.OperationKey,
	}); errors.Is(err, pgx.ErrNoRows) {
		overlay = s.buildRuntimeMCPOverlay(ctx, preparedAttribution.UserID, preparedAgent)
	} else if err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("prepare rework receipt: %w", err)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("begin rework dispatch: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)

	// Keep migration 284's workspace -> agent -> issue order. Do not take KEY
	// SHARE on issue first: two safe dispatches would then both need an upgrade.
	if _, err := q.LockReworkDispatchWorkspace(ctx, workspaceID); err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("lock workspace: %w", err)
	}
	if _, err := q.LockReworkDispatchAgent(ctx, db.LockReworkDispatchAgentParams{AgentID: req.ExpectedAgentID, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ReworkDispatchResult{}, ErrReworkDispatchTarget
		}
		return ReworkDispatchResult{}, fmt.Errorf("lock target agent: %w", err)
	}
	// This conflicts with ordinary queue writes' KEY SHARE on issue. It must
	// precede receipt and active-work reads.
	issue, err := q.LockIssueForReworkDispatch(ctx, db.LockIssueForReworkDispatchParams{ID: issueID, WorkspaceID: workspaceID})
	if err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("lock issue: %w", err)
	}
	agent, err := q.GetAgent(ctx, req.ExpectedAgentID)
	if err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("load locked target agent: %w", err)
	}
	// The SHARE lock keeps this snapshot stable until commit. If preparation
	// raced a settings/permission change, reject instead of using stale access.
	if agent.UpdatedAt != preparedAgent.UpdatedAt {
		return ReworkDispatchResult{}, ErrReworkDispatchConflict
	}

	if receipt, err := q.GetIssueReworkDispatchReceipt(ctx, db.GetIssueReworkDispatchReceiptParams{
		WorkspaceID: workspaceID, IssueID: issueID, OperationKey: req.OperationKey,
	}); err == nil {
		if util.UUIDToString(receipt.AgentID) != util.UUIDToString(req.ExpectedAgentID) || receipt.PayloadSha256 != req.PayloadSHA256 || util.UUIDToString(receipt.SourceTaskID) != util.UUIDToString(req.SourceTaskID) {
			return ReworkDispatchResult{}, ErrReworkDispatchMismatch
		}
		task, err := q.GetAgentTask(ctx, receipt.TaskID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ReworkDispatchResult{Task: db.AgentTaskQueue{ID: receipt.TaskID}, Replayed: true}, ErrReworkDispatchMissingTask
		}
		if err != nil {
			return ReworkDispatchResult{}, fmt.Errorf("load recorded rework task: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return ReworkDispatchResult{}, fmt.Errorf("commit replay: %w", err)
		}
		return ReworkDispatchResult{Task: task, Replayed: true}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ReworkDispatchResult{}, fmt.Errorf("load rework receipt: %w", err)
	}

	if issue.TriageState.Valid {
		return ReworkDispatchResult{}, ErrIssueInTriage
	}
	if issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid || util.UUIDToString(issue.AssigneeID) != util.UUIDToString(req.ExpectedAgentID) {
		return ReworkDispatchResult{}, ErrReworkDispatchTarget
	}
	if err := guardIssueNotInTriage(ctx, q, issue.ID, OriginDerived); err != nil {
		return ReworkDispatchResult{}, err
	}
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		return ReworkDispatchResult{}, ErrReworkDispatchTarget
	}
	if _, err := q.FindActiveIssueTaskForAgent(ctx, db.FindActiveIssueTaskForAgentParams{IssueID: issueID, AgentID: agent.ID}); err == nil {
		return ReworkDispatchResult{}, ErrReworkDispatchConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ReworkDispatchResult{}, fmt.Errorf("check active rework task: %w", err)
	}
	sourceTask, err := q.GetAgentTask(ctx, req.SourceTaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReworkDispatchResult{}, ErrReworkDispatchSource
	}
	if err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("load source run: %w", err)
	}
	if sourceTask.IssueID != issue.ID || sourceTask.AgentID != agent.ID || sourceTask.RuntimeID != agent.RuntimeID || IsTriageTask(sourceTask) ||
		(sourceTask.Status != "completed" && sourceTask.Status != "failed" && sourceTask.Status != "cancelled") {
		return ReworkDispatchResult{}, ErrReworkDispatchSource
	}

	txService := &TaskService{Queries: q}
	attr := txService.attributionForIssueTask(ctx, issue, pgtype.UUID{}, attribution.SourceDirectHuman, req.ActorUserID)
	attr, err = txService.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		return ReworkDispatchResult{}, err
	}
	if attr.UserID != preparedAttribution.UserID {
		return ReworkDispatchResult{}, ErrReworkDispatchConflict
	}
	source, delegatedFrom, evidenceKind, evidenceRef := attributionCreateParams(attr)
	task, err := q.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		ID: dbid.NewV7(), AgentID: agent.ID, RuntimeID: agent.RuntimeID, IssueID: issue.ID,
		Priority: priorityToInt(issue.Priority), ForceFreshSession: pgtype.Bool{Bool: true, Valid: true},
		RerunOfTaskID: req.SourceTaskID,
		HandoffNote:   pgtype.Text{String: req.Instruction, Valid: true}, OriginatorUserID: attr.UserID,
		AccountableUserID: attr.AccountableUserID, RuleVersionID: attr.RuleVersionID,
		RuntimeMcpOverlay: overlay.Overlay, RuntimeConnectedApps: overlay.ConnectedApps,
		OriginatorSource: source, DelegatedFromTaskID: delegatedFrom,
		TriggerEvidenceKind: evidenceKind, TriggerEvidenceRefID: evidenceRef,
		HeadSha: headShaText(txService.ResolveIssueReviewSHA(ctx, issue.ID)),
	})
	if err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("create rework task: %w", err)
	}
	if _, err := q.CreateIssueReworkDispatchReceipt(ctx, db.CreateIssueReworkDispatchReceiptParams{
		WorkspaceID: workspaceID, IssueID: issueID, OperationKey: req.OperationKey,
		AgentID: agent.ID, PayloadSha256: req.PayloadSHA256, Instruction: req.Instruction,
		TaskID: task.ID, SourceTaskID: req.SourceTaskID, ActorUserID: req.ActorUserID,
	}); err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("record rework receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ReworkDispatchResult{}, fmt.Errorf("commit rework dispatch: %w", err)
	}
	s.RecordHumanDelegation(ctx, attr)
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.NotifyTaskEnqueued(ctx, task)
	return ReworkDispatchResult{Task: task}, nil
}
