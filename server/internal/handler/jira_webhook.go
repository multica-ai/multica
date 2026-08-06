package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/jira"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// HandleJiraWebhook (POST /api/webhooks/jira/{connectionId}) authenticates
// and mirrors Jira issue webhooks. The connection id in the path selects the
// workspace and the decryption secret; Jira webhooks carry no native
// signature, so authentication is a constant-time compare of the minted
// per-connection secret (X-Multica-Webhook-Secret header or `secret` query
// parameter). Mirrors HandleVCSWebhook.
func (h *Handler) HandleJiraWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.isJiraConfigured() {
		writeError(w, http.StatusServiceUnavailable, "jira webhooks not configured")
		return
	}
	connUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connectionId"), "connection id")
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	conn, err := h.Queries.GetJiraConnectionByID(r.Context(), connUUID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("jira: lookup connection failed", "err", err)
		}
		writeError(w, http.StatusNotFound, "unknown connection")
		return
	}

	secret, err := h.openJiraSecret(conn.WebhookSecretEncrypted)
	if err != nil {
		slog.Error("jira: decrypt webhook secret failed", "err", err)
		writeError(w, http.StatusInternalServerError, "secret error")
		return
	}
	if !jira.VerifySecret(secret, r) {
		writeError(w, http.StatusUnauthorized, "invalid webhook secret")
		return
	}

	ev, err := jira.ParseIssueEvent(body)
	if err != nil {
		slog.Warn("jira: bad webhook payload", "err", err)
		// Acknowledge — Jira retries/disables hooks that keep failing, and a
		// malformed body will not improve on redelivery. Mirrors the VCS
		// handler's tolerance of unparseable payloads.
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch ev.Kind {
	case jira.EventIssueCreated, jira.EventIssueUpdated:
		h.syncJiraIssue(r.Context(), conn, ev)
	default:
		// Acknowledge unmodelled events so Jira doesn't flag the hook.
	}
	w.WriteHeader(http.StatusAccepted)
}

// jiraSyncOutcome reports what syncJiraIssue did with one Jira issue. The
// webhook path ignores it; the pull-based sync endpoint aggregates outcomes
// into its {created, updated} summary.
type jiraSyncOutcome int

const (
	jiraSyncSkipped jiraSyncOutcome = iota // missing data or a non-fatal failure
	jiraSyncCreated
	jiraSyncUpdated
)

// syncJiraIssue creates or syncs the Multica issue mirrored from a Jira
// issue. It is the single create-or-sync path shared by the webhook handler
// and the pull-based sync endpoint. First sighting of a Jira issue key
// creates the Multica issue and records the link; subsequent sightings
// update the mirrored title and description. Thin payloads (no summary) are
// enriched via the Jira REST client using the connection's stored API token.
func (h *Handler) syncJiraIssue(ctx context.Context, conn db.JiraConnection, ev jira.IssueEvent) jiraSyncOutcome {
	if ev.IssueKey == "" {
		slog.Warn("jira: issue event missing key")
		return jiraSyncSkipped
	}

	// Enrich a thin payload before mirroring. Some Jira webhook
	// configurations exclude the body's fields; the REST fetch restores the
	// summary/description this handler needs.
	if ev.Summary == "" {
		token, err := h.openJiraSecret(conn.ApiTokenEncrypted)
		if err != nil {
			slog.Error("jira: decrypt api token failed", "err", err)
			return jiraSyncSkipped
		}
		issue, err := h.JiraClient.GetIssue(ctx, conn.BaseUrl, conn.AccountEmail, token, ev.IssueKey)
		if err != nil {
			slog.Warn("jira: enrich issue failed", "key", ev.IssueKey, "err", err)
			return jiraSyncSkipped
		}
		ev.Summary = issue.Summary
		if ev.Description == "" {
			ev.Description = issue.Description
		}
		if ev.IssueID == "" {
			ev.IssueID = issue.ID
		}
	}
	if ev.Summary == "" {
		slog.Warn("jira: issue has no summary after enrichment", "key", ev.IssueKey)
		return jiraSyncSkipped
	}

	workspaceID := uuidToString(conn.WorkspaceID)

	link, err := h.Queries.GetJiraIssueLink(ctx, db.GetJiraIssueLinkParams{
		ConnectionID: conn.ID,
		JiraIssueKey: ev.IssueKey,
	})
	switch {
	case err == nil:
		// Existing link → sync the mirrored fields.
		issue, err := h.Queries.SyncIssueFromJira(ctx, db.SyncIssueFromJiraParams{
			ID:          link.MulticaIssueID,
			Title:       ev.Summary,
			Description: ptrToText(strPtrOrNil(ev.Description)),
			WorkspaceID: conn.WorkspaceID,
		})
		if err != nil {
			// The mirrored issue may have been deleted in Multica; record the
			// divergence on the link rather than recreating the issue (the
			// deletion was a user decision this webhook must not undo).
			slog.Warn("jira: sync mirrored issue failed", "key", ev.IssueKey, "err", err)
			h.touchJiraIssueLink(ctx, conn, ev, link.MulticaIssueID, "error")
			return jiraSyncSkipped
		}
		h.touchJiraIssueLink(ctx, conn, ev, issue.ID, "synced")
		prefix := h.getIssuePrefix(ctx, conn.WorkspaceID)
		h.publish(protocol.EventIssueUpdated, workspaceID, "system", "", map[string]any{
			"issue": issueToResponse(issue, prefix),
		})
		return jiraSyncUpdated
	case errors.Is(err, pgx.ErrNoRows):
		// First delivery for this Jira issue → create the mirrored issue.
		// The connecting admin is the creator (issue.creator_id is NOT NULL
		// and webhooks carry no Multica actor).
		if !conn.ConnectedByID.Valid {
			slog.Warn("jira: connection has no connected_by user; cannot create mirrored issue", "key", ev.IssueKey)
			return jiraSyncSkipped
		}
		res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
			WorkspaceID:    conn.WorkspaceID,
			Title:          ev.Summary,
			Description:    ptrToText(strPtrOrNil(ev.Description)),
			Status:         "todo",
			Priority:       "none", // Jira→Multica priority mapping is PR 2
			CreatorType:    "member",
			CreatorID:      conn.ConnectedByID,
			OriginType:     strToText("jira"),
			OriginID:       conn.ID,
			AllowDuplicate: true, // the Jira key, not the title, is identity
		}, service.IssueCreateOpts{Platform: "jira"})
		if err != nil {
			slog.Warn("jira: create mirrored issue failed", "key", ev.IssueKey, "err", err)
			return jiraSyncSkipped
		}
		h.touchJiraIssueLink(ctx, conn, ev, res.Issue.ID, "synced")
		return jiraSyncCreated
	default:
		slog.Warn("jira: lookup issue link failed", "key", ev.IssueKey, "err", err)
		return jiraSyncSkipped
	}
}

// touchJiraIssueLink upserts the link row's sync bookkeeping.
func (h *Handler) touchJiraIssueLink(ctx context.Context, conn db.JiraConnection, ev jira.IssueEvent, multicaIssueID pgtype.UUID, status string) {
	if _, err := h.Queries.UpsertJiraIssueLink(ctx, db.UpsertJiraIssueLinkParams{
		WorkspaceID:    conn.WorkspaceID,
		ConnectionID:   conn.ID,
		JiraIssueKey:   ev.IssueKey,
		JiraIssueID:    ev.IssueID,
		MulticaIssueID: multicaIssueID,
		SyncStatus:     status,
	}); err != nil {
		slog.Warn("jira: upsert issue link failed", "key", ev.IssueKey, "err", err)
	}
}
