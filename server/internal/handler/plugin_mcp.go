package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// Admin approval for an mcp-transport hook.
//
// Discovery is read-only and adopts nothing. Approval is the grant, and it pins
// the tools by schema digest — because an MCP server, unlike an http endpoint,
// decides its own tool list at runtime and can change it after the
// administrator read the manifest.

type pluginMCPToolResponse struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	SchemaDigest string `json:"schema_digest"`
	Approved     bool   `json:"approved"`
	// Drifted marks a tool that IS approved but whose schema no longer matches.
	// Surfaced rather than silently re-approved: the administrator approved a
	// specific shape, and a changed one is a new decision.
	Drifted bool `json:"drifted,omitempty"`
}

// ListPluginMCPTools — GET /api/workspaces/{id}/plugins/{installationId}/mcp/{hookKey}/tools
func (h *Handler) ListPluginMCPTools(w http.ResponseWriter, r *http.Request) {
	installation, ok := h.pluginInstallationFromURL(w, r)
	if !ok {
		return
	}
	hookKey := chi.URLParam(r, "hookKey")

	discovered, err := h.PluginService.DiscoverMCPHookTools(r.Context(), installation, hookKey)
	if err != nil {
		writePluginError(w, err, "failed to reach the Plugin's MCP server")
		return
	}
	approved := h.PluginService.ApprovedMCPTools(installation, hookKey)

	items := make([]pluginMCPToolResponse, 0, len(discovered))
	for _, tool := range discovered {
		pinned, isApproved := approved[tool.Name]
		items = append(items, pluginMCPToolResponse{
			Name:         tool.Name,
			Description:  tool.Description,
			SchemaDigest: tool.SchemaDigest,
			Approved:     isApproved,
			Drifted:      isApproved && pinned.SchemaDigest != tool.SchemaDigest,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": items})
}

type approvePluginMCPToolsRequest struct {
	// Tools is the complete approved set, not a delta. An administrator
	// removing one is the same request shape as adding one, so there is no way
	// to think you revoked something and have it stay.
	Tools []string `json:"tools"`
}

// ApprovePluginMCPTools — PUT /api/workspaces/{id}/plugins/{installationId}/mcp/{hookKey}/tools
func (h *Handler) ApprovePluginMCPTools(w http.ResponseWriter, r *http.Request) {
	installation, ok := h.pluginInstallationFromURL(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	parsedUserID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	var req approvePluginMCPToolsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.PluginService.ApproveMCPHookTools(
		r.Context(), installation, chi.URLParam(r, "hookKey"), req.Tools, parsedUserID)
	if err != nil {
		writePluginError(w, err, "failed to approve the MCP tools")
		return
	}
	payload, err := h.pluginInstallationPayload(r.Context(), updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the Plugin")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// splitPluginContributionID reads back the
// "plugin:<installation uuid>:<hook key>" id AgentMCPConnections built.
//
// The prefix is required, not tolerated: it is what distinguishes a Plugin
// contribution from a workspace's own Remote MCP connection, and those resolve
// their credentials from entirely different places. Both halves after it must
// be present too — a bare installation id would otherwise resolve to an empty
// hook key, and an empty installation id to whatever the workspace lookup makes
// of "", so a daemon holding one contribution's id could name another by
// trimming the string.
func splitPluginContributionID(contribution string) (string, string, bool) {
	trimmed, hasPrefix := strings.CutPrefix(contribution, remotemcp.PluginContributionPrefix)
	if !hasPrefix {
		return "", "", false
	}
	installationID, hookKey, found := strings.Cut(trimmed, ":")
	if !found || installationID == "" || hookKey == "" {
		return "", "", false
	}
	return installationID, hookKey, true
}

// ResolvePluginMCPCredential — GET /api/daemon/tasks/{id}/plugin-mcp/{contributionId}/credential
//
// The daemon's broker asks for the credential at connection time rather than
// receiving it in the claim payload, so a secret never sits in a task record.
// Authenticated as the daemon and scoped to a claimed task, like every other
// /api/daemon route.
func (h *Handler) ResolvePluginMCPCredential(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	_, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	installationID, hookKey, ok := splitPluginContributionID(chi.URLParam(r, "contributionId"))
	if !ok {
		writeError(w, http.StatusBadRequest, "malformed contribution id")
		return
	}
	installation, err := h.PluginService.InstallationForWorkspace(r.Context(), parseUUID(workspaceID), installationID)
	if err != nil {
		writePluginError(w, err, "failed to load the Plugin")
		return
	}

	header, credential, err := h.PluginService.MCPHookCredential(r.Context(), installation, hookKey)
	if err != nil {
		writePluginError(w, err, "failed to resolve the credential")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"credential_header": header,
		"credential":        credential,
	})
}
