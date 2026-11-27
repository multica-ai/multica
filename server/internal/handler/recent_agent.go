package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const maxRecentAgents = 10

// RecentAgentsResponse is the server-owned, newest-first recent agent order.
// Returning IDs only lets clients reuse their already-authorized agent list
// without duplicating agent response parsing or exposing hidden agent details.
type RecentAgentsResponse struct {
	AgentIDs []string `json:"agent_ids"`
}

func recentAgentIsSelectable(
	ctx context.Context,
	lookup service.RuntimeLookup,
	agent db.Agent,
	userID pgtype.UUID,
) (bool, error) {
	verdict, err := service.EffectiveAgentReadiness(ctx, lookup, agent, userID)
	if err != nil {
		return false, err
	}
	return !verdict.Blocked(), nil
}

func visibleRecentAgentIDs(
	ctx context.Context,
	q *db.Queries,
	metrics *obsmetrics.BusinessMetrics,
	workspaceID pgtype.UUID,
	member db.Member,
	userID string,
	orderedIDs []pgtype.UUID,
) ([]string, error) {
	if len(orderedIDs) == 0 {
		return []string{}, nil
	}

	agents, err := q.ListAgentsByIDsForWorkspace(ctx, db.ListAgentsByIDsForWorkspaceParams{
		AgentIds:    orderedIDs,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, err
	}

	agentByID := make(map[string]db.Agent, len(agents))
	agentIDs := make([]pgtype.UUID, 0, len(agents))
	for _, agent := range agents {
		id := uuidToString(agent.ID)
		agentByID[id] = agent
		agentIDs = append(agentIDs, agent.ID)
	}

	targetsByAgent := make(map[string][]db.AgentInvocationTarget, len(agents))
	if len(agentIDs) > 0 {
		targets, err := q.ListAgentInvocationTargetsByAgentIDs(ctx, agentIDs)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			id := uuidToString(target.AgentID)
			targetsByAgent[id] = append(targetsByAgent[id], target)
		}
	}

	visible := make([]string, 0, len(orderedIDs))
	seen := make(map[string]struct{}, len(orderedIDs))
	for _, rawID := range orderedIDs {
		id := uuidToString(rawID)
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		agent, exists := agentByID[id]
		if !exists || !memberAllowedToViewAgent(agent, targetsByAgent[id], userID, member.Role) {
			continue
		}
		selectable, err := recentAgentIsSelectable(
			ctx,
			service.RuntimeLookup{Queries: q, Metrics: metrics, Source: obsmetrics.RuntimeLookupSourceIssue},
			agent,
			member.UserID,
		)
		if err != nil {
			return nil, err
		}
		if !selectable {
			continue
		}
		seen[id] = struct{}{}
		visible = append(visible, id)
		if len(visible) == maxRecentAgents {
			break
		}
	}
	return visible, nil
}

// ListRecentAgents returns only active agents that remain visible and
// selectable for the current member. Stored stale IDs stay opaque and never
// reach the response.
func (h *Handler) ListRecentAgents(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID := requestUserID(r)
	wsUUID := parseUUID(workspaceID)

	selection, err := h.Queries.GetRecentAgentSelection(r.Context(), db.GetRecentAgentSelectionParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, RecentAgentsResponse{AgentIDs: []string{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list recent agents")
		return
	}

	agentIDs, err := visibleRecentAgentIDs(r.Context(), h.Queries, h.Metrics, wsUUID, member, userID, selection.AgentIds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve recent agents")
		return
	}
	writeJSON(w, http.StatusOK, RecentAgentsResponse{AgentIDs: agentIDs})
}

type selectRecentAgentRequest struct {
	AgentID string `json:"agent_id"`
}

// SelectRecentAgent atomically moves one visible agent to the front and
// returns the canonical server order. PostgreSQL serializes concurrent upserts
// on the per-user/workspace unique index, so clients should trust this result.
func (h *Handler) SelectRecentAgent(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	_, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID := requestUserID(r)

	var req selectRecentAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	wsUUID := parseUUID(workspaceID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recent agents")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceForRecentAgentSelection(r.Context(), wsUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update recent agents")
		}
		return
	}

	// The lock closes the archive/delete race between authorization and write.
	agents, err := qtx.ListAgentsByIDsForWorkspaceForUpdate(r.Context(), db.ListAgentsByIDsForWorkspaceForUpdateParams{
		AgentIds:    []pgtype.UUID{agentID},
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}
	if len(agents) != 1 {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	// Re-assert and lock membership inside the transaction so a concurrent
	// revoke cannot prune the row and then let this FK-free write recreate it.
	if _, err := qtx.LockActiveMember(r.Context(), db.LockActiveMemberParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update recent agents")
		}
		return
	}
	lockedMember, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}
	targets, err := qtx.ListAgentInvocationTargets(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}
	if !memberAllowedToViewAgent(agents[0], targets, userID, lockedMember.Role) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	selectable, err := recentAgentIsSelectable(
		r.Context(),
		service.RuntimeLookup{Queries: qtx, Metrics: h.Metrics, Source: obsmetrics.RuntimeLookupSourceIssue},
		agents[0],
		lockedMember.UserID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}
	if !selectable {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	selection, err := qtx.UpsertRecentAgentSelection(r.Context(), db.UpsertRecentAgentSelectionParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
		AgentID:     agentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recent agents")
		return
	}
	agentIDs, err := visibleRecentAgentIDs(r.Context(), qtx, h.Metrics, wsUUID, lockedMember, userID, selection.AgentIds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve recent agents")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recent agents")
		return
	}

	writeJSON(w, http.StatusOK, RecentAgentsResponse{AgentIDs: agentIDs})
}
