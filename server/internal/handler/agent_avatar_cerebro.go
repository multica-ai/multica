package handler

// CEREBRO-PATCH(agent-avatar-auto): bridge upstream CreateAgent to Cerebro avatar generation.

import (
	"context"
	"log/slog"
	"strings"
	"time"

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
