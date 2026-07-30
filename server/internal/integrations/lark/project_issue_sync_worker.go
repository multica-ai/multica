package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	defaultProjectSyncPollInterval = 2 * time.Second
	defaultProjectSyncBatchSize    = int32(25)
	projectSyncLeaseTimeout        = 2 * time.Minute
	projectSyncMaxAttempts         = int32(5)
)

type ProjectIssueSyncWorker struct {
	sync         *ProjectSyncService
	store        *projectSyncStore
	workerID     string
	pollInterval time.Duration
	batchSize    int32
	logger       *slog.Logger
}

func NewProjectIssueSyncWorker(syncService *ProjectSyncService, workerID string) *ProjectIssueSyncWorker {
	if strings.TrimSpace(workerID) == "" {
		workerID = "project-issue-sync"
	}
	return &ProjectIssueSyncWorker{
		sync:         syncService,
		store:        syncService.store,
		workerID:     workerID,
		pollInterval: defaultProjectSyncPollInterval,
		batchSize:    defaultProjectSyncBatchSize,
		logger:       syncService.logger,
	}
}

func (w *ProjectIssueSyncWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		if err := w.ProcessOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			w.logger.Warn("lark project sync worker iteration failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *ProjectIssueSyncWorker) ProcessOnce(ctx context.Context) error {
	items, err := w.store.claimNotifications(
		ctx, w.workerID, time.Now().Add(-projectSyncLeaseTimeout), w.batchSize,
	)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := w.processItem(ctx, item); err != nil {
			w.handleFailure(ctx, item, err)
		}
	}
	return nil
}

type projectNotificationPayload struct {
	IssueID        string `json:"issue_id"`
	Number         int32  `json:"number"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status"`
	IssueStatus    string `json:"issue_status"`
	TaskID         string `json:"task_id"`
	AgentID        string `json:"agent_id"`
	Reason         string `json:"reason"`
	Backfill       bool   `json:"backfill"`
	OccurredAt     string `json:"occurred_at"`
}

func (w *ProjectIssueSyncWorker) processItem(ctx context.Context, item ChannelNotificationOutbox) error {
	if !item.ProjectBindingID.Valid {
		return permanentSyncError{"project_unbound"}
	}
	binding, err := w.store.getProjectBindingByID(ctx, w.store.pool, item.WorkspaceID, item.ProjectBindingID)
	if isNoRows(err) || (err == nil && binding.State != "active") {
		return permanentSyncError{"project_unbound"}
	}
	if err != nil {
		return err
	}

	issue, err := w.sync.queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID: item.IssueID, WorkspaceID: item.WorkspaceID,
	})
	if isNoRows(err) {
		return permanentSyncError{"issue_not_found"}
	}
	if err != nil {
		return err
	}
	if !issue.ProjectID.Valid || issue.ProjectID != binding.ProjectID {
		return permanentSyncError{"project_or_topic_unbound"}
	}

	var payload projectNotificationPayload
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		return permanentSyncError{"invalid_payload"}
	}

	inst, err := NewChannelStore(w.sync.queries).GetLarkInstallationInWorkspace(ctx, GetInstallationInWorkspaceParams{
		ID: binding.InstallationID, WorkspaceID: binding.WorkspaceID,
	})
	if isNoRows(err) || (err == nil && InstallationStatus(inst.Status) != InstallationActive) {
		return permanentSyncError{"bot_revoked"}
	}
	if err != nil {
		return err
	}
	if w.sync.client == nil || w.sync.credentials == nil {
		return errors.New("lark project sync transport is not configured")
	}
	creds, err := installationCredentialsFor(inst, w.sync.credentials)
	if err != nil {
		return err
	}

	topic, _, err := w.ensureIssueTopic(ctx, item, binding, issue, payload, creds)
	if err != nil {
		return err
	}
	if item.EventType == "issue_created" {
		return w.store.markNotificationSent(ctx, item.ID, w.workerID)
	}

	if topic.State != "active" || topic.ProjectBindingID != item.ProjectBindingID {
		return permanentSyncError{"project_or_topic_unbound"}
	}
	text, err := w.renderNotification(ctx, item, issue, payload)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) != "" {
		_, err = w.sync.client.SendTextMessage(ctx, SendTextParams{
			InstallationID: creds,
			ChatID:         ChatID(topic.ChannelChatID),
			Text:           text,
			ReplyTarget: ReplyTarget{
				MessageID: topic.TopicRootMessageID,
				InThread:  true,
			},
			IdempotencyKey: util.UUIDToString(item.ID),
		})
		if err != nil {
			return err
		}
	}
	return w.store.markNotificationSent(ctx, item.ID, w.workerID)
}

func (w *ProjectIssueSyncWorker) ensureIssueTopic(ctx context.Context, item ChannelNotificationOutbox, projectBinding ChannelProjectBinding, issue db.Issue, payload projectNotificationPayload, creds InstallationCredentials) (ChannelIssueTopicBinding, bool, error) {
	active, err := w.store.getActiveIssueTopicByIssue(ctx, w.store.pool, item.WorkspaceID, item.IssueID)
	if err == nil {
		if active.ProjectBindingID != projectBinding.ID {
			return ChannelIssueTopicBinding{}, false, permanentSyncError{"project_or_topic_unbound"}
		}
		return active, false, nil
	}
	if !isNoRows(err) {
		return ChannelIssueTopicBinding{}, false, err
	}

	latest, latestErr := w.store.getLatestIssueTopicByIssue(ctx, w.store.pool, item.WorkspaceID, item.IssueID)
	if latestErr == nil && latest.State == "manual_unbound" {
		return ChannelIssueTopicBinding{}, false, permanentSyncError{"manual_unbound"}
	}
	if latestErr != nil && !isNoRows(latestErr) {
		return ChannelIssueTopicBinding{}, false, latestErr
	}

	rootText := w.renderIssueCreated(ctx, issue)
	rootID, err := w.sync.client.SendTextMessage(ctx, SendTextParams{
		InstallationID: creds,
		ChatID:         ChatID(projectBinding.ChannelChatID.String),
		Text:           rootText,
		IdempotencyKey: util.UUIDToString(item.ID),
	})
	if err != nil {
		return ChannelIssueTopicBinding{}, false, err
	}

	source := "issue_created_by_multica"
	if payload.Backfill {
		source = "project_backfill"
	}
	topic, err := w.store.createIssueTopic(
		ctx, w.store.pool, item.WorkspaceID, projectBinding.ID,
		projectBinding.ProjectID, issue.ID, projectBinding.ChannelChatID.String,
		rootID, "", source, pgtype.UUID{},
	)
	if err != nil {
		if current, getErr := w.store.getActiveIssueTopicByIssue(ctx, w.store.pool, item.WorkspaceID, issue.ID); getErr == nil {
			return current, false, nil
		}
		return ChannelIssueTopicBinding{}, false, translateProjectSyncConstraint(err)
	}
	return topic, true, nil
}

func (w *ProjectIssueSyncWorker) renderIssueCreated(ctx context.Context, issue db.Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🆕 %s %s\n\n", w.sync.issueIdentifier(ctx, issue), issue.Title)
	if issue.ProjectID.Valid {
		if project, err := w.sync.queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
			ID: issue.ProjectID, WorkspaceID: issue.WorkspaceID,
		}); err == nil {
			fmt.Fprintf(&b, "Project: %s\n", project.Title)
		}
	}
	fmt.Fprintf(&b, "Status: %s", issue.Status)
	if w.sync.appURL != "" {
		fmt.Fprintf(&b, "\nView: %s/issues/%s", w.sync.appURL, util.UUIDToString(issue.ID))
	}
	return b.String()
}

func (w *ProjectIssueSyncWorker) renderNotification(ctx context.Context, item ChannelNotificationOutbox, issue db.Issue, payload projectNotificationPayload) (string, error) {
	identifier := w.sync.issueIdentifier(ctx, issue)
	switch item.EventType {
	case "issue_created":
		return w.renderIssueCreated(ctx, issue), nil
	case "issue_status_changed":
		return fmt.Sprintf("🔄 %s status updated\n%s → %s", identifier, payload.PreviousStatus, payload.Status), nil
	case "task_failed":
		text := fmt.Sprintf("🔴 %s execution failed\nTask: %s\nReason: %s", identifier, safeTaskID(payload.TaskID, item.TaskID), safeFailureReason(payload.Reason))
		if w.sync.appURL != "" {
			text += "\nView: " + w.sync.appURL + "/issues/" + util.UUIDToString(issue.ID)
		}
		return text, nil
	case "task_cancelled":
		return fmt.Sprintf("⏹ %s execution stopped\nTask: %s\nCurrent Issue status: %s", identifier, safeTaskID(payload.TaskID, item.TaskID), issue.Status), nil
	default:
		return "", permanentSyncError{"unsupported_event_type"}
	}
}

func (w *ProjectIssueSyncWorker) handleFailure(ctx context.Context, item ChannelNotificationOutbox, processErr error) {
	var permanent permanentSyncError
	if errors.As(processErr, &permanent) {
		if err := w.store.deadNotification(ctx, item, permanent.reason); err != nil {
			w.logger.Warn("lark project sync: mark notification dead failed", "error", err)
		}
		return
	}
	nextAttempt := item.Attempts + 1
	safeErr := sanitizeSyncError(processErr)
	if nextAttempt >= projectSyncMaxAttempts {
		if err := w.store.deadNotification(ctx, item, safeErr); err != nil {
			w.logger.Warn("lark project sync: mark exhausted notification dead failed", "error", err)
		}
		return
	}
	next := time.Now().Add(projectSyncRetryDelay(nextAttempt))
	if err := w.store.retryNotification(ctx, item, w.workerID, safeErr, next); err != nil {
		w.logger.Warn("lark project sync: schedule notification retry failed", "error", err)
	}
}

type permanentSyncError struct {
	reason string
}

func (e permanentSyncError) Error() string { return e.reason }

func projectSyncRetryDelay(attempt int32) time.Duration {
	switch attempt {
	case 1:
		return 5 * time.Second
	case 2:
		return 30 * time.Second
	case 3:
		return 2 * time.Minute
	case 4:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}

func sanitizeSyncError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ReplaceAll(err.Error(), "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	if len(text) > 500 {
		text = text[:500]
	}
	return text
}

func safeFailureReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 160 {
		return "Task execution failed"
	}
	return reason
}

func safeTaskID(payloadID string, taskID pgtype.UUID) string {
	if strings.TrimSpace(payloadID) != "" {
		return payloadID
	}
	return util.UUIDToString(taskID)
}
