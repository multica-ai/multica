// Package webhook delivers action-required notifications to a workspace-level
// webhook URL, per issue #1020. It mirrors the Slack outbound pattern:
// subscribes to the same bus events, filters for the conditions the
// notification system classifies as action_required, and POSTs a JSON
// payload. Delivery is best-effort — failures are logged and dropped.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Outbound subscribes to issue:updated and task:failed on the event bus,
// and POSTs a JSON payload to the workspace's configured webhook URL when
// an action-required condition is met. Only registered when the package is
// wired in router.go.
type Outbound struct {
	q      outboundQueries
	client *http.Client
	logger *slog.Logger
}

// outboundQueries is the narrow query interface this outbound needs.
// *db.Queries satisfies it.
type outboundQueries interface {
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
}

// NewOutbound builds the webhook subscriber.
func NewOutbound(q outboundQueries, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	return &Outbound{
		q:      q,
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

// Register subscribes to the bus events that carry action-required conditions.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventIssueUpdated, o.handleIssueUpdated)
	bus.Subscribe(protocol.EventTaskFailed, o.handleTaskFailed)
}

// handleIssueUpdated fires on assignments and in_review transitions.
//
// The `issue` payload comes in two dialects: user/agent mutations (the HTTP
// update paths) publish a handler.IssueResponse, while background resets
// (service/task.go broadcastIssueUpdated) publish a map that only drives the
// WS fanout. The notification listeners assert the struct form and skip the
// map on purpose — background resets don't notify — and this outbound mirrors
// that contract. The bus is in-process with no serialization, so the type
// assertion sees exactly what the publisher put in.
func (o *Outbound) handleIssueUpdated(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	issue, ok := payload["issue"].(handler.IssueResponse)
	if !ok {
		return
	}

	if changed, _ := payload["assignee_changed"].(bool); changed {
		// Mirror the notification classification (cmd/server
		// notification_listeners.go): issue_assigned is action_required only
		// when a new assignee of an inbox-owning type (member/agent) exists.
		// Unassignment notifies at info severity and squads are routing
		// objects, not recipients — neither reaches a webhook.
		if issue.AssigneeType != nil && issue.AssigneeID != nil &&
			(*issue.AssigneeType == "member" || *issue.AssigneeType == "agent") {
			o.post(e.WorkspaceID, "issue_assigned", issue.ID, issue.Title, e.ActorType, e.ActorID,
				map[string]string{"assignee_id": *issue.AssigneeID})
		}
	}

	if changed, _ := payload["status_changed"].(bool); changed && issue.Status == "in_review" {
		o.post(e.WorkspaceID, "issue_in_review", issue.ID, issue.Title, e.ActorType, e.ActorID,
			map[string]string{"status": issue.Status})
	}
}

// handleTaskFailed fires on terminal task failures. This is deliberately
// stricter than the notification listeners (which notify on every failure,
// retry-pending included): a webhook receiver would otherwise be paged once
// per retry of the same run, and #1020's goal is pulling a human into a
// decision gate, not replaying the failure history.
func (o *Outbound) handleTaskFailed(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	if pending, _ := payload["retry_pending"].(bool); pending {
		return
	}

	agentID, _ := payload["agent_id"].(string)
	issueID, _ := payload["issue_id"].(string)
	if issueID == "" {
		return
	}

	title := ""
	ctx := context.Background()
	if id, err := util.ParseUUID(issueID); err == nil && id.Valid {
		if issue, err := o.q.GetIssue(ctx, id); err == nil {
			title = issue.Title
		}
	}

	details := map[string]string{"agent_id": agentID}
	if reason, _ := payload["failure_reason"].(string); reason != "" {
		details["failure_reason"] = reason
	}

	o.post(e.WorkspaceID, "task_failed", issueID, title, "agent", agentID, details)
}

// webhookPayload is the JSON body POSTed to the workspace webhook URL.
// Fields match issue #1020: event type, issue title/ID, actor, workspace ID.
type webhookPayload struct {
	Event       string            `json:"event"`
	IssueID     string            `json:"issue_id"`
	IssueTitle  string            `json:"issue_title"`
	ActorType   string            `json:"actor_type"`
	ActorID     string            `json:"actor_id"`
	WorkspaceID string            `json:"workspace_id"`
	Details     map[string]string `json:"details,omitempty"`
}

// webhookConfig reads webhook_url from workspace.settings JSONB.
type webhookConfig struct {
	URL string `json:"webhook_url"`
}

// post sends the payload if the workspace has a webhook URL configured.
// Best-effort: logs failures and returns — never blocks or panics.
func (o *Outbound) post(workspaceID, event, issueID, title, actorType, actorID string, details map[string]string) {
	defer func() {
		if r := recover(); r != nil {
			o.logger.Error("webhook outbound: panic recovered", "event", event, "recovered", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsID, err := util.ParseUUID(workspaceID)
	if err != nil || !wsID.Valid {
		return
	}
	ws, err := o.q.GetWorkspace(ctx, wsID)
	if err != nil || len(ws.Settings) == 0 {
		return
	}
	var cfg webhookConfig
	if err := json.Unmarshal(ws.Settings, &cfg); err != nil || cfg.URL == "" {
		return
	}

	body, err := json.Marshal(webhookPayload{
		Event:       event,
		IssueID:     issueID,
		IssueTitle:  title,
		ActorType:   actorType,
		ActorID:     actorID,
		WorkspaceID: workspaceID,
		Details:     details,
	})
	if err != nil {
		o.logger.Warn("webhook outbound: marshal failed", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		o.logger.Warn("webhook outbound: build request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Multica-Event", event)

	resp, err := o.client.Do(req)
	if err != nil {
		o.logger.Warn("webhook outbound: delivery failed (non-blocking)",
			"event", event, "workspace", workspaceID, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		o.logger.Warn("webhook outbound: non-2xx response",
			"event", event, "workspace", workspaceID, "status", resp.StatusCode)
		return
	}

	o.logger.Info("webhook outbound: delivered",
		"event", event, "workspace", workspaceID, "status", resp.StatusCode)
}
