package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/connectedapps"
	"github.com/multica-ai/multica/server/internal/integrations/composio"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Remote MCP apps (MUL-9792): apps such as Mobbin whose own MCP server
// Multica connects to over OAuth. They sit in the same catalog, accounts and
// agent apps as Composio apps; only connecting and executing differ.

// AppsRemoteGateway (POST /api/apps/mcp/{app}) is the MCP server a run sees
// for one remote app. It authenticates like AppsGateway.
func (h *Handler) AppsRemoteGateway(w http.ResponseWriter, r *http.Request) {
	if h.ConnectedApps == nil || !h.composioMCPAppsEnabled(r.Context()) {
		http.Error(w, "connected apps are not configured", http.StatusServiceUnavailable)
		return
	}
	h.ConnectedApps.ServeRemoteApp(w, r, chi.URLParam(r, "app"))
}

// AppsOAuthCallback (GET /api/apps/oauth/callback) is where a remote app's
// authorization server sends the browser back. It is public: the sealed state
// names who started the handshake. Like the Composio callback, every failure
// collapses to one generic redirect.
func (h *Handler) AppsOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || h.ConnectedApps == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeFeatureDisabled(w, "apps_not_configured", "connected apps are not configured")
		return
	}
	q := r.URL.Query()
	result, err := h.ConnectedApps.CompleteRemoteConnect(r.Context(), q.Get("state"), q.Get("code"), q.Get("error"))
	redirect := composio.CallbackResult{ToolkitSlug: result.Slug, ReturnTo: result.ReturnTo}
	if result.Scope == connectedapps.ScopeTeam {
		redirect.WorkspaceID = uuidToString(result.Owner)
	} else if result.Owner.Valid {
		redirect.UserID = uuidToString(result.Owner)
	}
	if errors.Is(err, composio.ErrTeamConnectRefused) {
		http.Redirect(w, r, h.Composio.CallbackRedirectFailure(redirect, composio.CallbackFeatureLockedReason), http.StatusFound)
		return
	}
	if err != nil {
		if !errors.Is(err, connectedapps.ErrRemoteState) {
			slog.Warn("apps remote connect failed", append(logger.RequestAttrs(r), "app", result.Slug, "error", err)...)
		}
		http.Redirect(w, r, h.Composio.CallbackRedirect(redirect, false), http.StatusFound)
		return
	}
	if result.Scope == connectedapps.ScopePersonal {
		h.ConnectedApps.ResolveConnectRequests(r.Context(), result.Owner, result.Slug)
	}
	http.Redirect(w, r, h.Composio.CallbackRedirect(redirect, true), http.StatusFound)
}

// listRemoteAppTools is ListAppTools for a remote app. Its tools come from the
// app's server, so they are listed through the team account or the viewer's
// own; with neither connected the list is empty.
func (h *Handler) listRemoteAppTools(w http.ResponseWriter, r *http.Request, workspaceID, viewer pgtype.UUID, slug string) {
	tools, err := h.ConnectedApps.ListRemoteTools(r.Context(), workspaceID, viewer, slug)
	if errors.Is(err, connectedapps.ErrAccountMissing) {
		writeJSON(w, http.StatusOK, map[string]any{"tools": []AppToolResponse{}})
		return
	}
	if err != nil {
		slog.Warn("apps remote tools failed", append(logger.RequestAttrs(r), "app", slug, "error", err)...)
		writeError(w, http.StatusBadGateway, "failed to load tools")
		return
	}
	overrides := h.ConnectedApps.ClassOverrides(r.Context(), workspaceID, slug)
	out := make([]AppToolResponse, 0, len(tools))
	for _, t := range tools {
		auto := connectedapps.Classify(slug, t.Name, t.Tags)
		class, overridden := overrides[strings.ToUpper(t.Name)]
		if !overridden {
			class = auto
		}
		out = append(out, AppToolResponse{
			Slug: t.Name, Name: t.Name, Description: t.Description,
			AutoClass: auto, Class: class, Overridden: overridden,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": out})
}

func connectionFromRemote(row db.RemoteAppConnection) AppConnectionResponse {
	conn := AppConnectionResponse{
		ID:          uuidToString(row.ID),
		ToolkitSlug: row.AppSlug,
		Status:      row.Status,
		ConnectedAt: timestampToString(row.ConnectedAt),
		LastUsedAt:  timestampToPtr(row.LastUsedAt),
	}
	if row.Scope == connectedapps.ScopeTeam {
		conn.ConnectedBy = uuidToPtr(row.ConnectedBy)
	}
	return conn
}

// remoteConnections lists an owner's remote app accounts, or none when remote
// apps are off.
func (h *Handler) remoteConnections(r *http.Request, scope string, owner pgtype.UUID) ([]db.RemoteAppConnection, error) {
	if len(h.ConnectedApps.RemoteApps()) == 0 {
		return nil, nil
	}
	return h.Queries.ListRemoteAppConnectionsForOwner(r.Context(), db.ListRemoteAppConnectionsForOwnerParams{Scope: scope, OwnerID: owner})
}
