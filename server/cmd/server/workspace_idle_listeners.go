package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const workspaceIdleNotificationType = "workspace_idle"

var workspaceBusyTaskEvents = []string{
	protocol.EventTaskQueued,
	protocol.EventTaskDispatch,
	protocol.EventTaskRunning,
	protocol.EventTaskWaitingLocalDirectory,
}

var workspaceTerminalTaskEvents = []string{
	protocol.EventTaskCompleted,
	protocol.EventTaskFailed,
	protocol.EventTaskCancelled,
}

// registerWorkspaceIdleListeners emits one workspace-level notification for
// each busy -> idle edge. State lives in PostgreSQL so concurrent terminal
// events handled by different server replicas cannot create duplicate rounds.
func registerWorkspaceIdleListeners(bus *events.Bus, pool *pgxpool.Pool) {
	queries := db.New(pool)
	for _, eventType := range workspaceBusyTaskEvents {
		bus.Subscribe(eventType, func(e events.Event) {
			if e.WorkspaceID == "" || taskEventIssueID(e) == "" {
				return
			}
			if err := queries.MarkWorkspaceWorkflowBusy(context.Background(), parseUUID(e.WorkspaceID)); err != nil {
				slog.Error("workspace idle: mark busy failed", "workspace_id", e.WorkspaceID, "error", err)
			}
		})
	}

	for _, eventType := range workspaceTerminalTaskEvents {
		bus.Subscribe(eventType, func(e events.Event) {
			if e.WorkspaceID == "" || taskRetryPending(e) {
				return
			}
			handleWorkspaceIdle(context.Background(), bus, pool, e.WorkspaceID, taskEventIssueID(e) != "")
		})
	}
}

func taskRetryPending(e events.Event) bool {
	if e.Type != protocol.EventTaskFailed {
		return false
	}
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return false
	}
	retryPending, _ := payload["retry_pending"].(bool)
	return retryPending
}

func handleWorkspaceIdle(ctx context.Context, bus *events.Bus, pool *pgxpool.Pool, workspaceID string, seedIfMissing bool) {
	workspaceUUID := parseUUID(workspaceID)
	tx, err := pool.Begin(ctx)
	if err != nil {
		slog.Error("workspace idle: begin transaction failed", "workspace_id", workspaceID, "error", err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck -- commit below owns the successful path

	queries := db.New(tx)
	if _, err := queries.LockWorkspaceForIdleNotification(ctx, workspaceUUID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("workspace idle: lock workspace failed", "workspace_id", workspaceID, "error", err)
		}
		return
	}

	hasActive, err := queries.HasActiveTasksInWorkspace(ctx, workspaceUUID)
	if err != nil {
		slog.Error("workspace idle: active task check failed", "workspace_id", workspaceID, "error", err)
		return
	}
	if hasActive {
		return
	}
	if seedIfMissing {
		if err := queries.EnsureWorkspaceWorkflowBusy(ctx, workspaceUUID); err != nil {
			slog.Error("workspace idle: ensure busy generation failed", "workspace_id", workspaceID, "error", err)
			return
		}
	}

	generation, err := queries.ClaimWorkspaceIdleNotification(ctx, workspaceUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		slog.Error("workspace idle: claim notification generation failed", "workspace_id", workspaceID, "error", err)
		return
	}

	inProgressCount, err := queries.CountWorkflowInProgressIssues(ctx, workspaceUUID)
	if err != nil {
		slog.Error("workspace idle: count in-progress issues failed", "workspace_id", workspaceID, "error", err)
		return
	}

	var recipients []pgtype.UUID
	if inProgressCount > 0 {
		recipients, err = queries.ListWorkflowIdleRecipientUserIDs(ctx, workspaceUUID)
	} else {
		recipients, err = queries.ListWorkspaceManagerUserIDs(ctx, workspaceUUID)
	}
	if err != nil {
		slog.Error("workspace idle: list recipients failed", "workspace_id", workspaceID, "error", err)
		return
	}

	recipientStrings := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		recipientStrings = append(recipientStrings, util.UUIDToString(recipient))
	}
	prefs := loadUserPrefs(ctx, queries, workspaceID, recipientStrings)

	title := "工作区空闲"
	body := "当前没有 in_progress 任务，所有智能体均已空闲。"
	if inProgressCount > 0 {
		title = "流程停滞"
		body = fmt.Sprintf("当前仍有 %d 个 in_progress 任务，但所有智能体均已空闲，请检查是否需要继续派发。", inProgressCount)
	}
	details, _ := json.Marshal(map[string]string{
		"busy_generation":   strconv.FormatInt(generation, 10),
		"in_progress_count": strconv.FormatInt(inProgressCount, 10),
	})

	items := make([]db.InboxItem, 0, len(recipients))
	for _, recipient := range recipients {
		recipientID := util.UUIDToString(recipient)
		if p, ok := prefs[recipientID]; ok && isNotifMuted(p, workspaceIdleNotificationType) {
			continue
		}
		item, createErr := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   workspaceUUID,
			RecipientType: "member",
			RecipientID:   recipient,
			Type:          workspaceIdleNotificationType,
			Severity:      "attention",
			Title:         title,
			Body:          util.StrToText(body),
			ActorType:     util.StrToText("system"),
			Details:       details,
		})
		if createErr != nil {
			slog.Error("workspace idle: create inbox item failed", "workspace_id", workspaceID, "recipient_id", recipientID, "error", createErr)
			return
		}
		items = append(items, item)
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("workspace idle: commit notification failed", "workspace_id", workspaceID, "error", err)
		return
	}

	for _, item := range items {
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: workspaceID,
			ActorType:   "system",
			Payload:     map[string]any{"item": inboxItemToResponse(item)},
		})
	}
}

func taskEventIssueID(e events.Event) string {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return ""
	}
	issueID, _ := payload["issue_id"].(string)
	return issueID
}
