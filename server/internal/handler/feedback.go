package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// feedbackImageRegex is a coarse check for markdown image syntax ![alt](url).
// It exists only to set the `has_images` analytics flag — we don't need a
// full markdown parser; a false positive on a literal "![" in prose is
// acceptable for a support-triage signal.
var feedbackImageRegex = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)

const (
	feedbackMaxMessageLen   = 10000
	feedbackHourlyRateLimit = 10
	// feedbackBodyLimit caps the request body at 64 KiB. Message is capped at
	// 10k chars separately; the extra budget covers JSON overhead plus the
	// optional url/workspace_id fields without letting an authenticated client
	// POST megabytes of junk into the metadata JSONB column.
	feedbackBodyLimit = 64 * 1024
)

// rejectFeedbackInLocalMode short-circuits feedback submissions when the
// server is running in local-only product mode. Remote feedback delivery
// requires the cloud backend; the UI swap to a local diagnostics flow is
// tracked separately. Returns true to continue, false if the request was
// rejected (response already written).
func (h *Handler) rejectFeedbackInLocalMode(w http.ResponseWriter) bool {
	if h.LocalMode.Enabled() {
		writeError(w, http.StatusForbidden, "remote feedback is unavailable in local mode; local diagnostics only")
		return false
	}
	return true
}

type CreateFeedbackRequest struct {
	Message     string  `json:"message"`
	URL         string  `json:"url"`
	WorkspaceID *string `json:"workspace_id,omitempty"`
}

type FeedbackResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

func (h *Handler) CreateFeedback(w http.ResponseWriter, r *http.Request) {
	if !h.rejectFeedbackInLocalMode(w) {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, feedbackBodyLimit)
	var req CreateFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if len(message) > feedbackMaxMessageLen {
		writeError(w, http.StatusBadRequest, "message too long")
		return
	}

	// Per-user rate limit: hourly cap on feedback submissions. DB-backed so it
	// survives process restarts and works across multiple instances without a
	// shared cache — cost is one cheap indexed count per submit.
	count, err := h.Queries.CountRecentFeedbackByUser(r.Context(), parseUUID(userID))
	if err != nil {
		slog.Warn("count recent feedback failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to check rate limit")
		return
	}
	if count >= feedbackHourlyRateLimit {
		writeError(w, http.StatusTooManyRequests, "too many feedback submissions, please try again later")
		return
	}

	platform, version, clientOS := middleware.ClientMetadataFromContext(r.Context())
	metadata := map[string]any{
		"url":        req.URL,
		"platform":   platform,
		"version":    version,
		"os":         clientOS,
		"user_agent": r.UserAgent(),
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		// Impossible in practice — map[string]any with primitive values never
		// fails to marshal — but fall through with an empty object rather than
		// 500ing on a non-critical field.
		metaBytes = []byte("{}")
	}

	var workspaceID pgtype.UUID
	if req.WorkspaceID != nil && *req.WorkspaceID != "" {
		ws, ok := parseUUIDOrBadRequest(w, *req.WorkspaceID, "workspace_id")
		if !ok {
			return
		}
		workspaceID = ws
	}

	fb, err := h.Queries.CreateFeedback(r.Context(), db.CreateFeedbackParams{
		UserID:      parseUUID(userID),
		Message:     message,
		Metadata:    metaBytes,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		slog.Warn("create feedback failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to submit feedback")
		return
	}

	slog.Info("feedback submitted", append(logger.RequestAttrs(r), "feedback_id", uuidToString(fb.ID))...)

	h.Analytics.Capture(analytics.FeedbackSubmitted(
		userID,
		uuidToString(fb.WorkspaceID),
		len(message),
		feedbackImageRegex.MatchString(message),
		platform,
		version,
	))

	writeJSON(w, http.StatusCreated, FeedbackResponse{
		ID:        uuidToString(fb.ID),
		CreatedAt: timestampToString(fb.CreatedAt),
	})
}
