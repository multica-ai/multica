// TECH-3422
package presence

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// EventCerebroPresence is the WS event type broadcast on a presence transition.
// It mirrors the constant protocol.EventCerebroPresence that the router worker
// adds to server/pkg/protocol/events.go — referenced here as a literal so this
// package does not depend on a constant that does not exist yet.
const EventCerebroPresence = "cerebro:presence"

// snapshotResponse is the body returned by both Ping and Snapshot — the list of
// user IDs currently online in the caller's workspace.
type snapshotResponse struct {
	OnlineUserIDs []string `json:"online_user_ids"`
}

// Handler serves the presence REST routes and broadcasts presence transitions
// over the websocket via the event Bus. It is constructed by the router worker
// with the same Bus the channels handler receives.
type Handler struct {
	Tracker *Tracker
	Bus     *events.Bus
}

// NewHandler builds a presence Handler. A nil tracker is replaced with a
// fresh DefaultTTL tracker so callers can wire it with just the Bus if they
// don't need to share the tracker.
func NewHandler(tracker *Tracker, bus *events.Bus) *Handler {
	if tracker == nil {
		tracker = NewTracker(DefaultTTL)
	}
	return &Handler{Tracker: tracker, Bus: bus}
}

// RegisterRoutes mounts the presence routes on r. The router worker mounts this
// under /api/cerebro/presence, yielding POST /api/cerebro/presence/ping and
// GET /api/cerebro/presence.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/ping", h.Ping)
	r.Get("/", h.Snapshot)
}

// Ping marks the authenticated user online in their workspace and returns the
// current online roster. On an offline->online transition it broadcasts a
// cerebro:presence {user_id, online:true} event to the whole workspace.
func (h *Handler) Ping(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.identity(w, r)
	if !ok {
		return
	}

	if becameOnline := h.Tracker.Touch(workspaceID, userID); becameOnline {
		h.broadcast(workspaceID, userID, true)
	}

	writeJSON(w, http.StatusOK, snapshotResponse{
		OnlineUserIDs: h.Tracker.OnlineUserIDs(workspaceID),
	})
}

// Snapshot returns the online roster for the caller's workspace without
// touching the caller's own presence.
func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.identity(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, snapshotResponse{
		OnlineUserIDs: h.Tracker.OnlineUserIDs(workspaceID),
	})
}

// broadcast publishes a presence transition to the whole workspace. A nil
// AudienceUserIDs means "fan out to the entire workspace room" — every member's
// UI should learn that someone came online or went offline.
func (h *Handler) broadcast(workspaceID, userID string, online bool) {
	if h.Bus == nil {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        EventCerebroPresence,
		WorkspaceID: workspaceID,
		ActorType:   "member",
		ActorID:     userID,
		Payload: map[string]any{
			"user_id": userID,
			"online":  online,
		},
	})
}

// identity resolves the authenticated member and their workspace from the
// request. The user id comes from the trusted X-User-ID header (set by auth
// middleware) and is validated as a UUID; the workspace id comes from the
// X-Workspace-ID routing context. Writes the appropriate error and returns
// ok=false on failure.
func (h *Handler) identity(w http.ResponseWriter, r *http.Request) (userID, workspaceID string, ok bool) {
	rawUser := r.Header.Get("X-User-ID")
	if rawUser == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", "", false
	}
	if _, err := util.ParseUUID(rawUser); err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", "", false
	}

	workspaceID = middleware.WorkspaceIDFromContext(r.Context())
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace not resolved")
		return "", "", false
	}
	return rawUser, workspaceID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
