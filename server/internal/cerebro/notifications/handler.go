// CEREBRO: cerebro-only inbox handlers kept in dedicated file so upstream
// merges don't conflict on the upstream inbox handler.
//
// Most inbox handlers (ListInbox/MarkInboxRead/etc) are upstream methods that
// cerebro modifies inline (folders, archived view, route/project_id fields)
// and therefore live in server/internal/handler/inbox.go marked with
// CEREBRO-PATCH(cerebro-inbox-*) markers. Only the strictly-additive cerebro
// handlers — those upstream knows nothing about — live here.
package notifications

import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler exposes the cerebro-only inbox HTTP endpoints. Keeps its own
// dependencies so the package can be wired into the router without touching
// the main Handler struct (which is owned by upstream).
type Handler struct {
	Queries *db.Queries
}

// New constructs a notifications.Handler. The caller wires it via dependency
// injection from the main router.
func New(queries *db.Queries) *Handler {
	return &Handler{Queries: queries}
}

// ListActiveIssueTasks returns the set of issue IDs in the current workspace
// that have at least one in-flight task. Drives the "agent is working"
// indicator on inbox list rows so users can see which conversations have an
// agent currently executing without opening each one.
func (h *Handler) ListActiveIssueTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())

	ids, err := h.Queries.ListActiveIssueIDsInWorkspace(r.Context(), util.ParseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list active issue tasks")
		return
	}

	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, util.UUIDToString(id))
	}
	writeJSON(w, http.StatusOK, map[string]any{"issue_ids": out})
}

// requireUserID mirrors handler.requireUserID — kept private here so the
// package compiles without importing the main handler package (which would
// pull in its full dependency surface and bloat the cerebro module graph).
func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
