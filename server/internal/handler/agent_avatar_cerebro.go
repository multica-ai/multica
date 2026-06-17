package handler

// CEREBRO-PATCH(agent-avatar-auto): bridge upstream CreateAgent to Cerebro avatar generation.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	cerebroagentavatar "github.com/multica-ai/multica/server/internal/cerebro/agent_avatar"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) cerebroGenerateAgentAvatarAsync(workspaceID string, created db.Agent) {
	if h == nil || h.Storage == nil || h.Queries == nil {
		return
	}
	if created.AvatarUrl.Valid && strings.TrimSpace(created.AvatarUrl.String) != "" {
		return
	}
	avatarHandler := cerebroagentavatar.New(h.Storage, h.Queries)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := avatarHandler.GenerateForAgent(ctx, workspaceID, created); err != nil {
			slog.Warn("auto agent avatar generation failed", "agent_id", uuidToString(created.ID), "workspace_id", workspaceID, "error", err)
			return
		}
		updated, err := h.Queries.GetAgent(ctx, created.ID)
		if err != nil {
			slog.Warn("load auto-avatar agent failed", "agent_id", uuidToString(created.ID), "workspace_id", workspaceID, "error", err)
			return
		}
		h.publish(protocol.EventAgentStatus, workspaceID, "system", "", map[string]any{"agent": agentToResponse(updated)})
	}()
}

// CEREBRO-PATCH(agent-avatar-async): TECH-3760 background per-agent avatar regen.
// CerebroGenerateAgentAvatar handles POST /api/agents/{id}/generate-avatar.
// It regenerates the avatar for an existing agent in the BACKGROUND: the image
// model call takes ~50s and previously blocked the request, so the inspector
// "Generate AI" button either spun for a minute or surfaced an upstream-gateway
// timeout as a 500. This endpoint returns 202 immediately and runs generation
// off the request path; the new avatar reaches the UI via the EventAgentStatus
// websocket event (the agents query invalidates on any agent:* event), so the
// user never waits on the screen.
func (h *Handler) CerebroGenerateAgentAvatar(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.canManageAgent(w, r, agent); !ok {
		return
	}

	var req struct {
		CustomPrompt string `json:"custom_prompt"`
	}
	if r.Body != nil {
		// Body is optional — a blank/absent body means the default auto-prompt.
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	h.cerebroRegenerateAgentAvatarAsync(uuidToString(agent.WorkspaceID), agent, strings.TrimSpace(req.CustomPrompt))
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "generating"})
}

// cerebroRegenerateAgentAvatarAsync regenerates an existing agent's avatar off
// the request path (fresh background context, not the request's, which is
// cancelled once the 202 is written) and publishes the updated agent so every
// client refreshes the avatar live.
func (h *Handler) cerebroRegenerateAgentAvatarAsync(workspaceID string, agent db.Agent, customPrompt string) {
	if h == nil || h.Storage == nil || h.Queries == nil {
		return
	}
	avatarHandler := cerebroagentavatar.New(h.Storage, h.Queries)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := avatarHandler.RegenerateForAgentCustom(ctx, workspaceID, agent, customPrompt); err != nil {
			slog.Warn("background agent avatar generation failed", "agent_id", uuidToString(agent.ID), "workspace_id", workspaceID, "error", err)
			return
		}
		updated, err := h.Queries.GetAgent(ctx, agent.ID)
		if err != nil {
			slog.Warn("load regenerated-avatar agent failed", "agent_id", uuidToString(agent.ID), "workspace_id", workspaceID, "error", err)
			return
		}
		h.publish(protocol.EventAgentStatus, workspaceID, "system", "", map[string]any{"agent": agentToResponse(updated)})
	}()
}
