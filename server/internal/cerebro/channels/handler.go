package channels

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentListenModeResponse is the row shape returned by the listing endpoint
// and the upsert endpoint. The frontend renders one row per agent in the
// channel; rows missing from the response default to 'always' on the client.
type AgentListenModeResponse struct {
	AgentID    string `json:"agent_id"`
	ListenMode string `json:"listen_mode"`
}

type listSettingsResponse struct {
	Settings []AgentListenModeResponse `json:"settings"`
}

type upsertListenModeRequest struct {
	ListenMode string `json:"listen_mode"`
}

// ListSettings handles GET /api/channels/{id}/agent-settings. Requires the
// caller to be a member subscriber of the channel — non-participants cannot
// read who is listening.
func (s *Service) ListSettings(w http.ResponseWriter, r *http.Request) {
	channelID, ok := s.requireChannelMember(w, r)
	if !ok {
		return
	}

	rows, err := s.ListListenModes(r.Context(), channelID)
	if err != nil {
		slog.Error("listen-mode list failed", "channel_id", util.UUIDToString(channelID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load listen modes")
		return
	}

	out := make([]AgentListenModeResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, AgentListenModeResponse{
			AgentID:    util.UUIDToString(row.AgentID),
			ListenMode: row.ListenMode,
		})
	}
	writeJSON(w, http.StatusOK, listSettingsResponse{Settings: out})
}

// SetListenModeHandler handles PUT /api/channels/{id}/agents/{agentId}/listen-mode.
// Requires the caller to be a channel participant and the targeted agent to
// be subscribed to the channel — toggling listen-mode for a non-participant
// agent has no effect on routing and is rejected to keep state coherent.
func (s *Service) SetListenModeHandler(w http.ResponseWriter, r *http.Request) {
	channelID, ok := s.requireChannelMember(w, r)
	if !ok {
		return
	}

	agentIDParam := chi.URLParam(r, "agentId")
	agentID, err := util.ParseUUID(agentIDParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	var req upsertListenModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ListenMode != ListenModeAlways && req.ListenMode != ListenModeMentionOnly {
		writeError(w, http.StatusBadRequest, "listen_mode must be 'always' or 'mention_only'")
		return
	}

	// Confirm the agent is actually subscribed to this channel. Without this
	// check a row could be inserted for an agent that has nothing to do with
	// the channel — confusing in the UI and useless at runtime.
	subscribed, err := s.Queries.IsIssueSubscriber(r.Context(), db.IsIssueSubscriberParams{
		IssueID:  channelID,
		UserType: "agent",
		UserID:   agentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify agent participation")
		return
	}
	if !subscribed {
		writeError(w, http.StatusNotFound, "agent is not a participant of this channel")
		return
	}

	if err := s.SetListenMode(r.Context(), channelID, agentID, req.ListenMode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel or agent not found")
			return
		}
		slog.Error("listen-mode upsert failed", "channel_id", util.UUIDToString(channelID), "agent_id", util.UUIDToString(agentID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update listen mode")
		return
	}

	// WS broadcast restricted to the channel's member participants — non-members
	// must not learn an agent is in the channel just from a settings flip.
	userID := r.Header.Get("X-User-ID")
	s.Bus.Publish(events.Event{
		Type:        EventChannelListenModeChanged,
		WorkspaceID: middleware.WorkspaceIDFromContext(r.Context()),
		ActorType:   "member",
		ActorID:     userID,
		Payload: map[string]any{
			"channel_id":  util.UUIDToString(channelID),
			"agent_id":    util.UUIDToString(agentID),
			"listen_mode": req.ListenMode,
		},
		AudienceUserIDs: s.memberAudience(r.Context(), channelID),
	})

	writeJSON(w, http.StatusOK, AgentListenModeResponse{
		AgentID:    util.UUIDToString(agentID),
		ListenMode: req.ListenMode,
	})
}

// archiveChannelResponse is the body returned by POST /api/channels/{id}/archive.
type archiveChannelResponse struct {
	ArchivedAt string `json:"archived_at"`
}

// ArchiveChannelHandler handles POST /api/channels/{id}/archive. Idempotent:
// posting against an already-archived channel returns 200 with archived_at
// refreshed to NOW.
func (s *Service) ArchiveChannelHandler(w http.ResponseWriter, r *http.Request) {
	channelID, ok := s.requireChannelMember(w, r)
	if !ok {
		return
	}
	userID := r.Header.Get("X-User-ID")
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	if err := s.ArchiveChannel(r.Context(), channelID, userUUID); err != nil {
		slog.Error("channel archive failed", "channel_id", util.UUIDToString(channelID), "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to archive channel")
		return
	}

	ts, _, err := s.GetChannelArchivedAt(r.Context(), channelID, userUUID)
	if err != nil {
		slog.Error("channel archive lookup failed", "channel_id", util.UUIDToString(channelID), "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to archive channel")
		return
	}
	archivedAtStr := ts.Time.UTC().Format("2006-01-02T15:04:05.999999Z07:00")

	// Per-user state — broadcast restricted to the acting user's own sessions
	// so other channel members aren't notified about someone else's inbox flip.
	s.Bus.Publish(events.Event{
		Type:        EventChannelArchived,
		WorkspaceID: middleware.WorkspaceIDFromContext(r.Context()),
		ActorType:   "member",
		ActorID:     userID,
		Payload: map[string]any{
			"channel_id":  util.UUIDToString(channelID),
			"user_id":     userID,
			"archived_at": archivedAtStr,
		},
		AudienceUserIDs: []string{userID},
	})

	writeJSON(w, http.StatusOK, archiveChannelResponse{ArchivedAt: archivedAtStr})
}

// UnarchiveChannelHandler handles DELETE /api/channels/{id}/archive.
// Idempotent: deleting a missing archive row also returns 204.
func (s *Service) UnarchiveChannelHandler(w http.ResponseWriter, r *http.Request) {
	channelID, ok := s.requireChannelMember(w, r)
	if !ok {
		return
	}
	userID := r.Header.Get("X-User-ID")
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	if err := s.UnarchiveChannel(r.Context(), channelID, userUUID); err != nil {
		slog.Error("channel unarchive failed", "channel_id", util.UUIDToString(channelID), "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to unarchive channel")
		return
	}

	s.Bus.Publish(events.Event{
		Type:        EventChannelUnarchived,
		WorkspaceID: middleware.WorkspaceIDFromContext(r.Context()),
		ActorType:   "member",
		ActorID:     userID,
		Payload: map[string]any{
			"channel_id": util.UUIDToString(channelID),
			"user_id":    userID,
		},
		AudienceUserIDs: []string{userID},
	})

	w.WriteHeader(http.StatusNoContent)
}

// requireChannelMember resolves the {id} path param to a channel/dm issue and
// confirms the calling member is subscribed to it. Returns the channel UUID
// for downstream queries.
func (s *Service) requireChannelMember(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}

	idParam := chi.URLParam(r, "id")
	channelID, err := util.ParseUUID(idParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid channel id")
		return pgtype.UUID{}, false
	}

	issue, err := s.Queries.GetIssue(r.Context(), channelID)
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return pgtype.UUID{}, false
	}
	if issue.Kind != "channel" && issue.Kind != "dm" {
		writeError(w, http.StatusNotFound, "channel not found")
		return pgtype.UUID{}, false
	}

	subscribed, err := s.Queries.IsIssueSubscriber(r.Context(), db.IsIssueSubscriberParams{
		IssueID:  channelID,
		UserType: "member",
		UserID:   userUUID,
	})
	if err != nil || !subscribed {
		writeError(w, http.StatusForbidden, "not a participant of this channel")
		return pgtype.UUID{}, false
	}

	return channelID, true
}

// memberAudience returns the WS audience for events on a channel — the
// member subscribers only.
func (s *Service) memberAudience(ctx context.Context, channelID pgtype.UUID) []string {
	subs, err := s.Queries.ListIssueSubscribers(ctx, channelID)
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(subs))
	for _, sub := range subs {
		if sub.UserType == "member" {
			out = append(out, util.UUIDToString(sub.UserID))
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
