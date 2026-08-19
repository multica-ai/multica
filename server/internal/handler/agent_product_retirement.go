package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/middleware"
)

const agentAdvancedManagementRetiredMessage = "Agent advanced management is not available in VIBES Tag"

var retiredAgentAdvancedWriteFields = map[string]struct{}{
	"runtime_config":             {},
	"custom_env":                 {},
	"custom_args":                {},
	"mcp_config":                 {},
	"composio_toolkit_allowlist": {},
	"template":                   {},
}

func hasRetiredAgentAdvancedWrite(rawFields map[string]json.RawMessage) bool {
	for field := range retiredAgentAdvancedWriteFields {
		if _, present := rawFields[field]; present {
			return true
		}
	}
	return false
}

func (h *Handler) isVIBESMirroredUser(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, nil
	}
	var mirrored bool
	err := h.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM vibes_user_mirror WHERE multica_user_id = $1
		)
	`, userID).Scan(&mirrored)
	return mirrored, err
}

func (h *Handler) isRetiredVIBESAgentProductRequest(ctx context.Context, userID string) (bool, error) {
	platform, _, _ := middleware.ClientMetadataFromContext(ctx)
	if platform != "vibes-tag-host" {
		return false, nil
	}
	return h.isVIBESMirroredUser(ctx, userID)
}

// rejectRetiredVIBESAgentSurface fences browser-era Agent advanced contracts
// for identities mirrored from VIBES while leaving their stored values and the
// independent CLI/daemon execution contracts intact for non-Tag callers.
func (h *Handler) rejectRetiredVIBESAgentSurface(w http.ResponseWriter, r *http.Request) bool {
	mirrored, err := h.isRetiredVIBESAgentProductRequest(r.Context(), requestUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve VIBES identity")
		return true
	}
	if !mirrored {
		return false
	}
	writeErrorCode(w, http.StatusGone, "agent_advanced_management_retired", agentAdvancedManagementRetiredMessage)
	return true
}

func (h *Handler) redactRetiredAgentAdvancedFieldsForVIBES(r *http.Request, response *AgentResponse) error {
	mirrored, err := h.isRetiredVIBESAgentProductRequest(r.Context(), requestUserID(r))
	if err != nil || !mirrored {
		return err
	}
	redactRetiredAgentAdvancedFields(response)
	return nil
}

func redactRetiredAgentAdvancedFields(response *AgentResponse) {
	response.RuntimeConfig = map[string]any{}
	response.CustomArgs = []string{}
	response.McpConfig = nil
	response.McpConfigRedacted = true
	response.HasCustomEnv = false
	response.CustomEnvKeyCount = 0
	response.ComposioToolkitAllowlist = nil
	response.ComposioToolkitAllowlistRedacted = true
}
