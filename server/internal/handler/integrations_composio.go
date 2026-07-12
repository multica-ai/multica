package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/featureflags"
	composio "github.com/multica-ai/multica/server/internal/integrations/composio"
)

// Composio integration handlers (MUL-3720, Stage 2 MVP). A Composio connection
// belongs to a user, not a workspace, so these handlers live outside the
// workspace-membership group. The four management endpoints (connect/init,
// toolkits, connections, delete) are user-scoped (requireUserID) and sit under
// the Auth middleware. ComposioCallback is the exception: it is a public route
// (outside the Auth group, see router.go / MUL-3843) because the browser often
// arrives without a session cookie — its identity comes from the signed state,
// not requireUserID. The whole block returns 503 when h.Composio is nil
// (COMPOSIO_API_KEY unset), matching the Lark/GitHub "integration not
// configured" convention.

// ComposioConnectInitRequest is the POST /connect/init body.
type ComposioConnectInitRequest struct {
	ToolkitSlug string `json:"toolkit_slug"`
}

// ComposioConnectInitResponse carries the hosted Composio Connect Link the
// frontend redirects the user to.
type ComposioConnectInitResponse struct {
	RedirectURL string `json:"redirect_url"`
}

// ComposioConnectionResponse is the wire shape for one connection row.
type ComposioConnectionResponse struct {
	ID          string  `json:"id"`
	ToolkitSlug string  `json:"toolkit_slug"`
	Status      string  `json:"status"`
	ConnectedAt string  `json:"connected_at"`
	LastUsedAt  *string `json:"last_used_at"`
}

// ComposioToolkitResponse is the wire shape for one toolkit in the catalog.
// Since MUL-4009 the catalog only contains connectable toolkits, so
// `connectable` is always true. The field is retained for backward
// compatibility with older desktop clients that branch on it (dropping it would
// make them treat every entry as non-connectable and hide the Connect button).
type ComposioToolkitResponse struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Logo        string `json:"logo,omitempty"`
	Category    string `json:"category,omitempty"`
	Connectable bool   `json:"connectable"`
}

func (h *Handler) composioMCPAppsEnabled(ctx context.Context) bool {
	return featureflags.ComposioMCPAppsEnabled(ctx, h.FeatureFlags)
}

// ComposioConnectInit (POST /api/integrations/composio/connect/init) starts a
// hosted Composio auth flow for the requested toolkit and returns the redirect
// URL. An unsupported toolkit slug is a 400 (the MVP only wires Notion).
func (h *Handler) ComposioConnectInit(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	var req ComposioConnectInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.ToolkitSlug) == "" {
		writeError(w, http.StatusBadRequest, "toolkit_slug is required")
		return
	}

	redirectURL, err := h.Composio.BeginConnect(r.Context(), userUUID, req.ToolkitSlug)
	if err != nil {
		if errors.Is(err, composio.ErrToolkitNotSupported) {
			writeError(w, http.StatusBadRequest, "toolkit not supported")
			return
		}
		writeError(w, http.StatusBadGateway, "failed to start composio connect")
		return
	}
	writeJSON(w, http.StatusOK, ComposioConnectInitResponse{RedirectURL: redirectURL})
}

// ComposioCallback (GET /api/integrations/composio/callback) is the browser
// redirect target Composio sends the user back to after the hosted flow. It is
// registered as a PUBLIC route (outside the Auth middleware group — see
// router.go / MUL-3843), because the browser frequently lands here without a
// session cookie (expired session, SameSite/ITP stripping, private window,
// self-hosted callback subdomain). Identity therefore comes solely from the
// HMAC-signed `state` query param, which CompleteCallback verifies before
// doing anything. On success the row is upserted and the browser is redirected
// to the settings page; any failure redirects to the same page with a stable
// error code so the user is never left on a blank API response.
func (h *Handler) ComposioCallback(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	q := r.URL.Query()
	state := q.Get("state")
	status := q.Get("status")
	connectedAccountID := q.Get("connected_account_id")

	slug, err := h.Composio.CompleteCallback(r.Context(), state, status, connectedAccountID)
	if err != nil {
		// Every failure (tampered/expired state, non-success status, write
		// error) collapses to the generic failure redirect — we never tell the
		// browser which check failed.
		http.Redirect(w, r, h.Composio.CallbackRedirect(slug, false), http.StatusFound)
		return
	}
	http.Redirect(w, r, h.Composio.CallbackRedirect(slug, true), http.StatusFound)
}

// ListComposioConnections (GET /api/integrations/composio/connections) returns
// the caller's active connections.
func (h *Handler) ListComposioConnections(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	conns, err := h.Composio.ListConnections(r.Context(), userUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list composio connections")
		return
	}
	out := make([]ComposioConnectionResponse, 0, len(conns))
	for _, c := range conns {
		out = append(out, ComposioConnectionResponse{
			ID:          c.ID,
			ToolkitSlug: c.ToolkitSlug,
			Status:      c.Status,
			ConnectedAt: c.ConnectedAt,
			LastUsedAt:  c.LastUsedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// ListComposioToolkits (GET /api/integrations/composio/toolkits) returns the
// connectable Composio toolkits for the Settings UI to render. Since MUL-4009
// the service filters out toolkits with no enabled auth config in the project,
// so every entry here is connectable; the `connectable` flag is kept on the
// wire for backward compatibility. The catalog itself is project-global (not
// per-user), but the route is user-scoped (requireUser) like the rest of the
// block. A resolver/upstream failure is a 502, letting the UI show its
// load-failed state rather than a misleading empty catalog.
func (h *Handler) ListComposioToolkits(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	toolkits, err := h.Composio.ListToolkits(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to list composio toolkits")
		return
	}
	out := make([]ComposioToolkitResponse, 0, len(toolkits))
	for _, tk := range toolkits {
		out = append(out, ComposioToolkitResponse{
			Slug:        tk.Slug,
			Name:        tk.Name,
			Logo:        tk.LogoURL,
			Category:    tk.Category,
			Connectable: tk.Connectable,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// DeleteComposioConnection (DELETE /api/integrations/composio/connections/{id})
// disconnects a connection the caller owns. Idempotent at the service layer;
// a connection that does not belong to the caller is a 404.
func (h *Handler) DeleteComposioConnection(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	connUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "connection id")
	if !ok {
		return
	}
	if err := h.Composio.Disconnect(r.Context(), userUUID, connUUID); err != nil {
		if errors.Is(err, composio.ErrConnectionNotFound) {
			writeError(w, http.StatusNotFound, "composio connection not found")
			return
		}
		writeError(w, http.StatusBadGateway, "failed to disconnect composio connection")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
