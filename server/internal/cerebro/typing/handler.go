// TECH-3422
//
// Package typing owns the cerebro-only "is typing…" relay for channels and DMs.
// The client POSTs that the user is typing in a channel; the server broadcasts a
// `cerebro:typing` WS event to the OTHER member participants of that channel so
// their clients can show a transient typing indicator.
//
// This endpoint is fire-and-forget and high-frequency: it does the minimum work
// (a cheap membership lookup + a fan-out publish) and never writes to the DB.
package typing

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EventCerebroTyping is the WS event type broadcast when a member is typing in a
// channel/dm. It mirrors the constant the router worker adds to
// server/pkg/protocol/events.go (protocol.EventCerebroTyping = "cerebro:typing");
// this package references the literal string to stay decoupled from that add.
const EventCerebroTyping = "cerebro:typing"

// Handler exposes the typing relay endpoint. It mirrors the dependency style of
// the cerebro channels Service (Queries for the membership/subscriber lookups,
// Bus for the WS fan-out) so the router worker can build it with the same
// primitives. No store of its own — typing is ephemeral and never persisted.
type Handler struct {
	Queries *db.Queries
	Bus     *events.Bus
}

// NewHandler constructs a typing Handler. Wired from the router, same shape as
// channels.New's first two dependencies (queries + bus).
func NewHandler(queries *db.Queries, bus *events.Bus) *Handler {
	return &Handler{
		Queries: queries,
		Bus:     bus,
	}
}

// RegisterRoutes mounts the typing endpoint under a chi router. The router
// worker calls this with the same subrouter it uses for /api/cerebro/channels,
// so the full path resolves to POST /api/cerebro/channels/{id}/typing.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/channels/{id}/typing", h.Typing)
}

// Typing handles POST /api/cerebro/channels/{id}/typing. It verifies the caller
// is a member participant of the channel, then broadcasts a `cerebro:typing`
// event to the channel's member audience and returns 204. Outsiders cannot
// broadcast typing into a channel they are not in (403/404).
func (h *Handler) Typing(w http.ResponseWriter, r *http.Request) {
	channelID, userUUID, ok := h.requireChannelMember(w, r)
	if !ok {
		return
	}

	userID := util.UUIDToString(userUUID)

	// Audience: every member subscriber of the channel. Agents do not receive
	// typing events. Excluding the typer is optional — the frontend already
	// ignores its own user_id — so we keep the cheaper "all members" audience.
	h.Bus.Publish(events.Event{
		Type:        EventCerebroTyping,
		WorkspaceID: middleware.WorkspaceIDFromContext(r.Context()),
		ActorType:   "member",
		ActorID:     userID,
		Payload: map[string]any{
			"channel_id": util.UUIDToString(channelID),
			"user_id":    userID,
		},
		AudienceUserIDs: h.memberAudience(r.Context(), channelID),
	})

	w.WriteHeader(http.StatusNoContent)
}

// requireChannelMember resolves the {id} path param to a channel/dm issue and
// confirms the calling member is subscribed to it. Returns the channel UUID and
// the caller UUID for downstream use. Mirrors channels.Service.requireChannelMember
// (replicated locally to avoid taking a dependency on the channels package).
func (h *Handler) requireChannelMember(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	idParam := chi.URLParam(r, "id")
	channelID, err := util.ParseUUID(idParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid channel id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	issue, err := h.Queries.GetIssue(r.Context(), channelID)
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	if issue.Kind != "channel" && issue.Kind != "dm" {
		writeError(w, http.StatusNotFound, "channel not found")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	subscribed, err := h.Queries.IsIssueSubscriber(r.Context(), db.IsIssueSubscriberParams{
		IssueID:  channelID,
		UserType: "member",
		UserID:   userUUID,
	})
	if err != nil || !subscribed {
		writeError(w, http.StatusForbidden, "not a participant of this channel")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	return channelID, userUUID, true
}

// memberAudience returns the WS audience for typing events on a channel — the
// member subscribers only. Mirrors channels.Service.memberAudience.
func (h *Handler) memberAudience(ctx context.Context, channelID pgtype.UUID) []string {
	subs, err := h.Queries.ListIssueSubscribers(ctx, channelID)
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

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
