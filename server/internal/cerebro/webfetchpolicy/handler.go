// handler.go exposes the per-workspace web_fetch policy over HTTP (TECH-3522).
//
// Routes (mounted under /api/workspaces/{id}/web-fetch-policy in router.go):
//
//	GET  /web-fetch-policy  — read the effective policy (any workspace member)
//	PUT  /web-fetch-policy  — replace the policy (owner/admin only, gated by router)
//
// The GET response always returns a usable policy: when a workspace has never
// configured one it returns the seeded default with configured=false so the UI
// can show "using defaults".
package webfetchpolicy

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// maxRules caps how many host rules a workspace may store, to bound the list
// the gateway scans on every fetch and the size of the agent-context hint.
const maxRules = 200

// Handler exposes the web_fetch policy store over HTTP.
type Handler struct {
	cerebro *cerebrodb.Queries
}

// NewHandler constructs a Handler over the given cerebro queries.
func NewHandler(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{cerebro: cerebro}
}

type policyResponse struct {
	Mode       string   `json:"mode"`
	Rules      []string `json:"rules"`
	Configured bool     `json:"configured"`
}

type putRequest struct {
	Mode  string   `json:"mode"`
	Rules []string `json:"rules"`
}

// Get — GET /api/workspaces/{id}/web-fetch-policy
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	configured := true
	row, err := h.cerebro.GetCerebroWebFetchPolicy(r.Context(), wsID)
	var p Policy
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		configured = false
		p = Default()
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to load web_fetch policy")
		return
	default:
		p = fromRow(row)
	}
	writeJSON(w, http.StatusOK, policyResponse{Mode: p.Mode, Rules: p.Rules, Configured: configured})
}

// Set — PUT /api/workspaces/{id}/web-fetch-policy
func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())

	var req putRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Mode != ModeAllow && req.Mode != ModeDisallow {
		writeError(w, http.StatusBadRequest, "mode must be 'allow' or 'disallow'")
		return
	}
	if len(req.Rules) > maxRules {
		writeError(w, http.StatusBadRequest, "too many rules")
		return
	}

	p := Normalize(req.Mode, req.Rules)
	rulesJSON, err := json.Marshal(p.Rules)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode rules")
		return
	}
	row, err := h.cerebro.UpsertCerebroWebFetchPolicy(r.Context(), cerebrodb.UpsertCerebroWebFetchPolicyParams{
		WorkspaceID: wsID,
		Mode:        p.Mode,
		HostRules:   rulesJSON,
		UpdatedBy:   member.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save web_fetch policy")
		return
	}
	saved := fromRow(row)
	writeJSON(w, http.StatusOK, policyResponse{Mode: saved.Mode, Rules: saved.Rules, Configured: true})
}

func (h *Handler) workspaceID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	if _, ok := middleware.MemberFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
