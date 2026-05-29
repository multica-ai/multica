package sandboxprofile

// handler.go exposes the selectable sandbox isolation profiles over HTTP
// (FIR-2230 phase 6 — sandbox as an interface, not raw JSON). The profile a
// given runtime is currently on is NOT served here — it is derived from that
// runtime's stored sandbox_policy and returned on the runtime response itself
// (AgentRuntimeResponse.SandboxProfile), so the screen always reflects the real
// enforced state. This endpoint only answers "which profiles exist and what do
// they mean", giving the picker a single server-owned source of truth for
// labels and descriptions instead of hardcoding them in the client.
//
// Route (mounted under /api/workspaces/{id} in cmd/server/router.go):
//
//	GET /sandbox-profiles   list selectable isolation profiles. Any member.
//
// Writes go through the existing PATCH /api/runtimes/{runtimeId}/sandbox-policy
// with a {"profile": "..."} body, which expands the preset server-side so the
// security semantics can never drift between client and server.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler serves the sandbox profile catalog. It is stateless: the catalog is
// static code, and per-runtime state lives on the runtime record.
type Handler struct{}

// NewHandler constructs a Handler.
func NewHandler() *Handler { return &Handler{} }

type listResponse struct {
	Profiles []Definition `json:"profiles"`
}

// List — GET /api/workspaces/{id}/sandbox-profiles
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.MemberFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	if _, err := util.ParseUUID(chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Profiles: Definitions()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
