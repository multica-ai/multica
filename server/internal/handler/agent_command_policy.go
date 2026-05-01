package handler

import (
	"context"
	"net/http"

	"github.com/multica-ai/multica/server/internal/agentpolicy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	agentCommandIssueCreate         = agentpolicy.CommandIssueCreate
	agentCommandIssueUpdateStatus   = agentpolicy.CommandIssueUpdateStatus
	agentCommandIssueStatus         = agentpolicy.CommandIssueStatus
	agentCommandIssueUpdateAssignee = agentpolicy.CommandIssueUpdateAssignee
	agentCommandIssueAssign         = agentpolicy.CommandIssueAssign
)

func (h *Handler) agentPolicyForActor(ctx context.Context, actorType, actorID, workspaceID string) (agentpolicy.Policy, error) {
	if actorType != "agent" {
		return agentpolicy.Policy{}, nil
	}
	agentUUID, err := util.ParseUUID(actorID)
	if err != nil {
		return agentpolicy.Policy{}, nil
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return agentpolicy.Policy{}, err
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		return agentpolicy.Policy{}, err
	}
	return agentpolicy.FromRuntimeConfig(agent.RuntimeConfig), nil
}

func (h *Handler) denyAgentCommandsIfNeeded(w http.ResponseWriter, r *http.Request, workspaceID, actorType, actorID string, commands ...string) bool {
	policy, err := h.agentPolicyForActor(r.Context(), actorType, actorID, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to evaluate agent policy")
		return true
	}
	if policy.DeniesAnyCommand(commands...) {
		writeError(w, http.StatusForbidden, "agent policy forbids this command")
		return true
	}
	return false
}

func (h *Handler) denyAgentMentionIfNeeded(w http.ResponseWriter, r *http.Request, workspaceID, actorType, actorID, content string) bool {
	policy, err := h.agentPolicyForActor(r.Context(), actorType, actorID, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to evaluate agent policy")
		return true
	}
	if policy.DeniesAgentMentionsByDefault() && containsAgentMention(content) {
		writeError(w, http.StatusForbidden, "agent policy forbids mentioning agents")
		return true
	}
	return false
}

func containsAgentMention(content string) bool {
	for _, mention := range util.ParseMentions(content) {
		if mention.Type == "agent" {
			return true
		}
	}
	return false
}
