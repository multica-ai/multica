package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ReceiveGitlabWebhook accepts an unauthenticated HTTP POST from GitLab,
// validates the X-Gitlab-Token header against a workspace's stored
// webhook_secret, and persists the event into gitlab_webhook_event for the
// background worker to apply.
//
// Must respond <10s — GitLab cancels deliveries that take longer.
func (h *Handler) ReceiveGitlabWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}

	suppliedToken := r.Header.Get("X-Gitlab-Token")
	if suppliedToken == "" {
		writeError(w, http.StatusUnauthorized, "missing X-Gitlab-Token")
		return
	}
	eventHeader := r.Header.Get("X-Gitlab-Event")
	if eventHeader == "" {
		writeError(w, http.StatusBadRequest, "missing X-Gitlab-Event")
		return
	}

	conn, err := h.Queries.GetWorkspaceGitlabConnectionByWebhookSecret(r.Context(), pgtype.Text{String: suppliedToken, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "unknown webhook token")
			return
		}
		slog.Error("webhook lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// Constant-time compare (defense-in-depth: the equality query above
	// isn't constant-time at the SQL/index layer).
	stored := ""
	if conn.WebhookSecret.Valid {
		stored = conn.WebhookSecret.String
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(suppliedToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "secret mismatch")
		return
	}

	// Cap body at 1MiB to defend against abusive payloads. Real GitLab
	// payloads are well under that.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		slog.Error("read webhook body", "error", err)
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	eventType, objectID, gitlabUpdatedAt, ok := parseWebhookKey(eventHeader, body)
	if !ok {
		// Unknown event type — ACK 200 so GitLab doesn't retry, but log so
		// we can see if a new event type starts arriving.
		slog.Info("ignoring unknown gitlab webhook event", "event", eventHeader)
		w.WriteHeader(http.StatusOK)
		return
	}

	hash := sha256.Sum256(body)
	_, err = h.Queries.InsertGitlabWebhookEvent(r.Context(), db.InsertGitlabWebhookEventParams{
		WorkspaceID:     conn.WorkspaceID,
		EventType:       eventType,
		ObjectID:        objectID,
		GitlabUpdatedAt: gitlabUpdatedAt,
		PayloadHash:     hash[:],
		Payload:         body,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// ErrNoRows means ON CONFLICT DO NOTHING fired — duplicate delivery,
		// silently deduplicated. Any other error is a real problem.
		slog.Error("insert webhook event", "error", err)
		writeError(w, http.StatusInternalServerError, "persist failed")
		return
	}

	if err := h.Queries.TouchWorkspaceGitlabLastWebhookReceived(r.Context(), conn.WorkspaceID); err != nil {
		slog.Warn("touch last_webhook_received_at", "error", err)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}

// parseWebhookKey extracts (event_type, object_id, gitlab_updated_at) from
// the webhook header + body. event_type is the short form we store
// ("issue", "note", "emoji", "label"). object_id is the integer that, with
// event_type, identifies the object the event is about. gitlab_updated_at
// is best-effort — used by the worker to skip stale events.
func parseWebhookKey(eventHeader string, body []byte) (string, int64, pgtype.Timestamptz, bool) {
	switch eventHeader {
	case "Issue Hook", "Confidential Issue Hook":
		var p struct {
			ObjectAttributes struct {
				IID       int64  `json:"iid"`
				UpdatedAt string `json:"updated_at"`
			} `json:"object_attributes"`
		}
		if err := json.Unmarshal(body, &p); err != nil || p.ObjectAttributes.IID == 0 {
			return "", 0, pgtype.Timestamptz{}, false
		}
		return "issue", p.ObjectAttributes.IID, parseTSGitlab(p.ObjectAttributes.UpdatedAt), true
	case "Note Hook", "Confidential Note Hook":
		var p struct {
			ObjectAttributes struct {
				ID        int64  `json:"id"`
				UpdatedAt string `json:"updated_at"`
			} `json:"object_attributes"`
		}
		if err := json.Unmarshal(body, &p); err != nil || p.ObjectAttributes.ID == 0 {
			return "", 0, pgtype.Timestamptz{}, false
		}
		return "note", p.ObjectAttributes.ID, parseTSGitlab(p.ObjectAttributes.UpdatedAt), true
	case "Emoji Hook":
		var p struct {
			ObjectAttributes struct {
				ID        int64  `json:"id"`
				UpdatedAt string `json:"updated_at"`
			} `json:"object_attributes"`
		}
		if err := json.Unmarshal(body, &p); err != nil || p.ObjectAttributes.ID == 0 {
			return "", 0, pgtype.Timestamptz{}, false
		}
		return "emoji", p.ObjectAttributes.ID, parseTSGitlab(p.ObjectAttributes.UpdatedAt), true
	case "Label Hook":
		var p struct {
			ObjectAttributes struct {
				ID        int64  `json:"id"`
				UpdatedAt string `json:"updated_at"`
			} `json:"object_attributes"`
		}
		if err := json.Unmarshal(body, &p); err != nil || p.ObjectAttributes.ID == 0 {
			return "", 0, pgtype.Timestamptz{}, false
		}
		return "label", p.ObjectAttributes.ID, parseTSGitlab(p.ObjectAttributes.UpdatedAt), true
	}
	return "", 0, pgtype.Timestamptz{}, false
}

// parseTSGitlab is local to this file. RFC3339 → Timestamptz; zero value on
// missing/malformed input.
func parseTSGitlab(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
