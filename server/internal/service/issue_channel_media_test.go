package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type afterCommitTxStarter struct {
	pool        *pgxpool.Pool
	afterCommit func()
}

func (s *afterCommitTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &afterCommitTx{Tx: tx, afterCommit: s.afterCommit}, nil
}

type afterCommitTx struct {
	pgx.Tx
	afterCommit func()
}

func (t *afterCommitTx) Commit(ctx context.Context) error {
	if err := t.Tx.Commit(ctx); err != nil {
		return err
	}
	if t.afterCommit != nil {
		t.afterCommit()
		t.afterCommit = nil
	}
	return nil
}

func TestPublishAttachmentsChangedCarriesIssueScope(t *testing.T) {
	bus := events.New()
	svc := &IssueService{Bus: bus}
	workspaceID := util.MustParseUUID("11111111-1111-4111-8111-111111111111")
	issueID := util.MustParseUUID("22222222-2222-4222-8222-222222222222")
	actorID := util.MustParseUUID("33333333-3333-4333-8333-333333333333")
	var got events.Event
	bus.Subscribe(protocol.EventIssueAttachmentsChanged, func(e events.Event) { got = e })

	svc.PublishAttachmentsChanged(db.Issue{ID: issueID, WorkspaceID: workspaceID}, actorID)

	if got.Type != protocol.EventIssueAttachmentsChanged || got.WorkspaceID != util.UUIDToString(workspaceID) || got.ActorType != "member" || got.ActorID != util.UUIDToString(actorID) {
		t.Fatalf("event envelope = %+v", got)
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok || payload["issue_id"] != util.UUIDToString(issueID) {
		t.Fatalf("event payload = %#v", got.Payload)
	}
}

func TestCreateMediaGatedIssueCommitsDeferredTaskAtomicallyBeforeCreatedEvent(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	agentUUID := util.MustParseUUID(agentID)

	bus := events.New()
	createdOverlay := json.RawMessage(`{"mcpServers":{"creator":{"url":"https://creator.example"}}}`)
	taskService := &TaskService{
		Queries:      q,
		TxStarter:    pool,
		Bus:          bus,
		Composio:     &stubOverlayBuilder{resp: createdOverlay},
		FeatureFlags: composioMCPAppsTestFlags(true),
	}
	var competingTaskID pgtype.UUID
	var competingErr error
	txStarter := &afterCommitTxStarter{pool: pool, afterCommit: func() {
		// This callback runs after the issue transaction is visible but before
		// IssueService resumes. The old implementation committed only the issue,
		// so this ordinary queued insert won the unique slot. The fixed path has
		// already committed the deferred task in the same transaction.
		var issueID, runtimeID pgtype.UUID
		if err := pool.QueryRow(ctx, `
			SELECT i.id, a.runtime_id
			FROM issue i
			JOIN agent a ON a.id = i.assignee_id
			WHERE i.workspace_id = $1 AND i.title = 'Media-gated issue'`, workspaceUUID).
			Scan(&issueID, &runtimeID); err != nil {
			competingErr = fmt.Errorf("discover committed issue: %w", err)
			return
		}
		competingErr = pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
			VALUES ($1, $2, $3, 'queued', 1)
			RETURNING id`, agentUUID, runtimeID, issueID).Scan(&competingTaskID)
	}}
	issueService := NewIssueService(q, txStarter, bus, nil, taskService)

	var callbackErr error
	var mergedCommentID pgtype.UUID
	bus.Subscribe(protocol.EventIssueCreated, func(event events.Event) {
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			callbackErr = fmt.Errorf("issue:created payload = %#v", event.Payload)
			return
		}
		issueID, ok := payload["issue_id"].(string)
		if !ok {
			callbackErr = fmt.Errorf("issue:created issue_id = %#v", payload["issue_id"])
			return
		}
		issueUUID := util.MustParseUUID(issueID)

		var status string
		var mediaPending bool
		var runtimeOverlay []byte
		if err := pool.QueryRow(ctx, `
			SELECT status, context->>'channel_issue_media_pending' = 'true', runtime_mcp_overlay
			FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2`, issueUUID, agentUUID).Scan(&status, &mediaPending, &runtimeOverlay); err != nil {
			callbackErr = fmt.Errorf("load task during issue:created: %w", err)
			return
		}
		if status != "deferred" || !mediaPending {
			callbackErr = fmt.Errorf("task during issue:created = status %q media_pending %v", status, mediaPending)
			return
		}
		var runtimeOverlayValue, createdOverlayValue any
		if err := json.Unmarshal(runtimeOverlay, &runtimeOverlayValue); err != nil {
			callbackErr = fmt.Errorf("decode task overlay during issue:created: %w", err)
			return
		}
		if err := json.Unmarshal(createdOverlay, &createdOverlayValue); err != nil || !reflect.DeepEqual(runtimeOverlayValue, createdOverlayValue) {
			callbackErr = fmt.Errorf("task overlay during issue:created = %s, want %s", runtimeOverlay, createdOverlay)
			return
		}

		if err := pool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
			VALUES ($1, $2, 'member', $3, 'Immediate follow-up')
			RETURNING id`, issueUUID, workspaceUUID, userUUID).Scan(&mergedCommentID); err != nil {
			callbackErr = fmt.Errorf("insert immediate comment: %w", err)
			return
		}
		if _, err := q.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
			IssueID:                 issueUUID,
			AgentID:                 agentUUID,
			NewTriggerCommentID:     mergedCommentID,
			NewOriginatorUserID:     userUUID,
			NewAccountableUserID:    userUUID,
			NewOriginatorSource:     pgtype.Text{String: "direct_human", Valid: true},
			NewTriggerEvidenceKind:  pgtype.Text{String: "comment", Valid: true},
			NewTriggerEvidenceRefID: mergedCommentID,
		}); err != nil {
			callbackErr = fmt.Errorf("merge immediate comment into deferred task: %w", err)
		}
	})

	result, err := issueService.Create(ctx, IssueCreateParams{
		WorkspaceID:  workspaceUUID,
		Title:        "Media-gated issue",
		Status:       "todo",
		Priority:     "medium",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   agentUUID,
		CreatorType:  "member",
		CreatorID:    userUUID,
	}, IssueCreateOpts{AssignedAgentRunFireAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if !result.AssignedTaskID.Valid {
		t.Fatal("media-gated issue did not return its deferred task")
	}
	if !isDuplicatePendingTaskErr(competingErr) {
		t.Fatalf("post-commit competing queued insert error = %v, want duplicate pending task", competingErr)
	}
	if competingTaskID.Valid {
		t.Fatalf("post-commit competing task unexpectedly won: %s", util.UUIDToString(competingTaskID))
	}

	var taskID, triggerCommentID pgtype.UUID
	var taskCount int
	if err := pool.QueryRow(ctx, `
		SELECT id, trigger_comment_id, count(*) OVER ()
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
		  AND status IN ('queued', 'dispatched', 'deferred')
		ORDER BY created_at
		LIMIT 1`, result.Issue.ID, agentUUID).
		Scan(&taskID, &triggerCommentID, &taskCount); err != nil {
		t.Fatalf("load final pending task: %v", err)
	}
	if taskCount != 1 || taskID != result.AssignedTaskID || triggerCommentID != mergedCommentID {
		t.Fatalf("pending task = count %d id %v trigger %v, want one task %v with trigger %v", taskCount, taskID, triggerCommentID, result.AssignedTaskID, mergedCommentID)
	}
}

func TestHydrateDeferredChannelIssueTaskOverlayDoesNotOverwriteMergedCommentPlan(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	userUUID := util.MustParseUUID(userID)

	plainService := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := plainService.EnqueueDeferredChannelIssueTask(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   util.MustParseUUID(agentID),
		CreatorType:  "member",
		CreatorID:    userUUID,
		Priority:     "medium",
	}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("EnqueueDeferredChannelIssueTask: %v", err)
	}

	var commentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'New effective trigger')
		RETURNING id`, task.IssueID, workspaceID, userID).Scan(&commentID); err != nil {
		t.Fatalf("seed merged comment: %v", err)
	}
	mergedOverlay := json.RawMessage(`{"mcpServers":{"merged":{"url":"https://merged.example"}}}`)
	if _, err := q.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:                 task.IssueID,
		AgentID:                 task.AgentID,
		NewTriggerCommentID:     commentID,
		NewOriginatorUserID:     userUUID,
		NewAccountableUserID:    userUUID,
		NewRuntimeMcpOverlay:    mergedOverlay,
		NewOriginatorSource:     pgtype.Text{String: "direct_human", Valid: true},
		NewTriggerEvidenceKind:  pgtype.Text{String: "comment", Valid: true},
		NewTriggerEvidenceRefID: commentID,
	}); err != nil {
		t.Fatalf("MergeCommentIntoPendingTask: %v", err)
	}

	hydrationBuilder := &stubOverlayBuilder{
		resp: json.RawMessage(`{"mcpServers":{"stale":{"url":"https://stale.example"}}}`),
	}
	hydratingService := &TaskService{
		Queries:      q,
		Composio:     hydrationBuilder,
		FeatureFlags: composioMCPAppsTestFlags(true),
	}
	if err := hydratingService.hydrateDeferredChannelIssueTaskOverlay(ctx, task); err != nil {
		t.Fatalf("hydrateDeferredChannelIssueTaskOverlay: %v", err)
	}

	var triggerID pgtype.UUID
	var storedOverlay []byte
	if err := pool.QueryRow(ctx, `
		SELECT trigger_comment_id, runtime_mcp_overlay
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&triggerID, &storedOverlay); err != nil {
		t.Fatalf("load hydrated task: %v", err)
	}
	if triggerID != commentID {
		t.Fatalf("trigger_comment_id = %s, want %s", util.UUIDToString(triggerID), util.UUIDToString(commentID))
	}
	var storedValue, mergedValue any
	if err := json.Unmarshal(storedOverlay, &storedValue); err != nil {
		t.Fatalf("decode stored runtime_mcp_overlay: %v", err)
	}
	if err := json.Unmarshal(mergedOverlay, &mergedValue); err != nil {
		t.Fatalf("decode expected runtime_mcp_overlay: %v", err)
	}
	if !reflect.DeepEqual(storedValue, mergedValue) {
		t.Fatalf("runtime_mcp_overlay = %s, want merged overlay %s", storedOverlay, mergedOverlay)
	}
}
