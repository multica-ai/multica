package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	slackpkg "github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// validTriggerStatuses is the set of issue status values accepted in
// trigger_statuses. Values outside this set are rejected to prevent
// silent mismatches from typos.
var validTriggerStatuses = map[string]bool{
	"backlog":     true,
	"todo":        true,
	"in_progress": true,
	"in_review":   true,
	"done":        true,
	"blocked":     true,
	"cancelled":   true,
}

const slackWebhookURLPrefix = "https://hooks.slack.com/services/"

// ── Response shape ────────────────────────────────────────────────────────────

type SlackIntegrationResponse struct {
	WorkspaceID    string   `json:"workspace_id"`
	Enabled        bool     `json:"enabled"`
	WebhookURLMask string   `json:"webhook_url_mask"`
	Label          string   `json:"label"`
	TriggerStatuses []string `json:"trigger_statuses"`
	LastSentAt     *string  `json:"last_sent_at"`
	LastError      *string  `json:"last_error"`
}

func slackIntegrationToResponse(row db.WorkspaceSlackIntegration) SlackIntegrationResponse {
	mask := maskWebhookURL(row.WebhookUrl)

	var triggers []string
	if len(row.TriggerStatuses) > 0 {
		_ = json.Unmarshal(row.TriggerStatuses, &triggers)
	}
	if triggers == nil {
		triggers = []string{}
	}

	return SlackIntegrationResponse{
		WorkspaceID:     uuidToString(row.WorkspaceID),
		Enabled:         row.Enabled,
		WebhookURLMask:  mask,
		Label:           row.Label,
		TriggerStatuses: triggers,
		LastSentAt:      timestampToPtr(row.LastSentAt),
		LastError:       textToPtr(row.LastError),
	}
}

// maskWebhookURL returns a redacted webhook URL exposing only the last 6 chars
// of the path. This lets admins confirm which webhook is configured without
// leaking the full secret.
func maskWebhookURL(url string) string {
	if len(url) <= 6 {
		return "••••••"
	}
	return "https://hooks.slack.com/services/••••••••••••" + url[len(url)-6:]
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// GetSlackIntegration returns the current Slack integration config for a
// workspace. The webhook URL is masked in the response.
func (h *Handler) GetSlackIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	row, err := h.Queries.GetSlackIntegrationForWorkspace(r.Context(), wsUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"configured": false})
			return
		}
		slog.Warn("GetSlackIntegration failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get Slack integration")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"configured":   true,
		"integration":  slackIntegrationToResponse(row),
	})
}

type putSlackIntegrationRequest struct {
	WebhookURL      string   `json:"webhook_url"`
	Label           string   `json:"label"`
	TriggerStatuses []string `json:"trigger_statuses"`
	Enabled         *bool    `json:"enabled"`
}

// PutSlackIntegration creates or updates the Slack integration for a workspace.
// Requires admin/owner role (enforced by router middleware).
func (h *Handler) PutSlackIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	creatorUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	var req putSlackIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !strings.HasPrefix(req.WebhookURL, slackWebhookURLPrefix) {
		writeError(w, http.StatusBadRequest, "webhook_url must start with "+slackWebhookURLPrefix)
		return
	}

	for _, s := range req.TriggerStatuses {
		if !validTriggerStatuses[s] {
			writeError(w, http.StatusBadRequest, "invalid trigger status: "+s)
			return
		}
	}

	triggersJSON, err := json.Marshal(req.TriggerStatuses)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal trigger_statuses")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	row, err := h.Queries.UpsertSlackIntegration(r.Context(), db.UpsertSlackIntegrationParams{
		WorkspaceID:     wsUUID,
		Enabled:         enabled,
		WebhookUrl:      req.WebhookURL,
		Label:           req.Label,
		TriggerStatuses: triggersJSON,
		CreatedBy:       creatorUUID,
	})
	if err != nil {
		slog.Warn("UpsertSlackIntegration failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save Slack integration")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"configured":  true,
		"integration": slackIntegrationToResponse(row),
	})
}

// DeleteSlackIntegration removes the Slack integration for a workspace.
// Requires admin/owner role.
func (h *Handler) DeleteSlackIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	if err := h.Queries.DeleteSlackIntegration(r.Context(), wsUUID); err != nil {
		slog.Warn("DeleteSlackIntegration failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete Slack integration")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TestSlackIntegration fires a synthetic Slack message synchronously to verify
// the webhook URL is valid and reachable. Returns 200 on success, 502 on Slack error.
func (h *Handler) TestSlackIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	row, err := h.Queries.GetSlackIntegrationForWorkspace(r.Context(), wsUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no Slack integration configured")
			return
		}
		slog.Warn("TestSlackIntegration: get failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get Slack integration")
		return
	}

	body := slackpkg.BuildMessage(slackpkg.MessageParams{
		IssueKey:   "TEST-1",
		IssueTitle: "Test notification from Forge",
		PrevStatus: "todo",
		NewStatus:  "in_progress",
		ActorName:  "Forge",
		IssueURL:   "https://forge.asymbl.app",
	})

	testCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := slackpkg.PostWebhook(testCtx, row.WebhookUrl, body); err != nil {
		slog.Warn("TestSlackIntegration: post failed", append(logger.RequestAttrs(r), "error", err)...)
		_ = h.Queries.UpdateSlackIntegrationStatus(r.Context(), db.UpdateSlackIntegrationStatusParams{
			WorkspaceID: wsUUID,
			LastError:   pgtype.Text{String: err.Error(), Valid: true},
		})
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	_ = h.Queries.UpdateSlackIntegrationStatus(r.Context(), db.UpdateSlackIntegrationStatusParams{
		WorkspaceID: wsUUID,
		LastSentAt:  pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
