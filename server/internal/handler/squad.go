package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response types ──────────────────────────────────────────────────────────

type SquadResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Instructions string  `json:"instructions"`
	AvatarURL    *string `json:"avatar_url"`
	LeaderID     string  `json:"leader_id"`
	CreatorID    string  `json:"creator_id"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	ArchivedAt   *string `json:"archived_at"`
	ArchivedBy   *string `json:"archived_by"`
}

type SquadMemberResponse struct {
	ID         string `json:"id"`
	SquadID    string `json:"squad_id"`
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
	Role       string `json:"role"`
	CreatedAt  string `json:"created_at"`
}

// ── Converters ──────────────────────────────────────────────────────────────

func squadToResponse(s db.Squad) SquadResponse {
	return SquadResponse{
		ID:           uuidToString(s.ID),
		WorkspaceID:  uuidToString(s.WorkspaceID),
		Name:         s.Name,
		Description:  s.Description,
		Instructions: s.Instructions,
		AvatarURL:    textToPtr(s.AvatarUrl),
		LeaderID:     uuidToString(s.LeaderID),
		CreatorID:    uuidToString(s.CreatorID),
		CreatedAt:    timestampToString(s.CreatedAt),
		UpdatedAt:    timestampToString(s.UpdatedAt),
		ArchivedAt:   timestampToPtr(s.ArchivedAt),
		ArchivedBy:   uuidToPtr(s.ArchivedBy),
	}
}

func squadMemberToResponse(m db.SquadMember) SquadMemberResponse {
	return SquadMemberResponse{
		ID:         uuidToString(m.ID),
		SquadID:    uuidToString(m.SquadID),
		MemberType: m.MemberType,
		MemberID:   uuidToString(m.MemberID),
		Role:       m.Role,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// loadSquadInWorkspace loads a squad scoped to the current workspace.
func (h *Handler) loadSquadInWorkspace(w http.ResponseWriter, r *http.Request) (db.Squad, string, bool) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	squadID := chi.URLParam(r, "id")
	squadUUID, ok := parseUUIDOrBadRequest(w, squadID, "squad id")
	if !ok {
		return db.Squad{}, "", false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return db.Squad{}, "", false
	}
	squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
		ID:          squadUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "squad not found")
		return db.Squad{}, "", false
	}
	return squad, workspaceID, true
}

// ── Handlers ────────────────────────────────────────────────────────────────

func (h *Handler) ListSquads(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	squads, err := h.Queries.ListSquads(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list squads")
		return
	}
	resp := make([]SquadResponse, len(squads))
	for i, s := range squads {
		resp[i] = squadToResponse(s)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateSquad(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		LeaderID    string `json:"leader_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.LeaderID == "" {
		writeError(w, http.StatusBadRequest, "leader_id is required")
		return
	}

	leaderUUID, ok := parseUUIDOrBadRequest(w, req.LeaderID, "leader_id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	// Validate leader is an agent in this workspace.
	_, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          leaderUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "leader must be a valid agent in this workspace")
		return
	}

	squad, err := h.Queries.CreateSquad(r.Context(), db.CreateSquadParams{
		WorkspaceID: wsUUID,
		Name:        req.Name,
		Description: req.Description,
		LeaderID:    leaderUUID,
		CreatorID:   member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create squad")
		return
	}

	// Auto-add leader as a member with role "leader".
	h.Queries.AddSquadMember(r.Context(), db.AddSquadMemberParams{
		SquadID:    squad.ID,
		MemberType: "agent",
		MemberID:   leaderUUID,
		Role:       "leader",
	})

	resp := squadToResponse(squad)
	h.publish(protocol.EventSquadCreated, workspaceID, "member", uuidToString(member.UserID), map[string]any{"squad": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) GetSquad(w http.ResponseWriter, r *http.Request) {
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, squadToResponse(squad))
}

func (h *Handler) UpdateSquad(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		Instructions *string `json:"instructions"`
		LeaderID     *string `json:"leader_id"`
		AvatarURL    *string `json:"avatar_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateSquadParams{ID: squad.ID}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Instructions != nil {
		params.Instructions = pgtype.Text{String: *req.Instructions, Valid: true}
	}
	if req.AvatarURL != nil {
		params.AvatarUrl = pgtype.Text{String: *req.AvatarURL, Valid: true}
	}
	if req.LeaderID != nil {
		lid, ok := parseUUIDOrBadRequest(w, *req.LeaderID, "leader_id")
		if !ok {
			return
		}
		// Validate new leader is an agent in workspace.
		if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID: lid, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "leader must be a valid agent in this workspace")
			return
		}
		// Ensure new leader is a squad member; auto-add if not.
		isMember, _ := h.Queries.IsSquadMember(r.Context(), db.IsSquadMemberParams{
			SquadID: squad.ID, MemberType: "agent", MemberID: lid,
		})
		if !isMember {
			h.Queries.AddSquadMember(r.Context(), db.AddSquadMemberParams{
				SquadID: squad.ID, MemberType: "agent", MemberID: lid, Role: "leader",
			})
		}
		params.LeaderID = lid
	}

	updated, err := h.Queries.UpdateSquad(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update squad")
		return
	}

	resp := squadToResponse(updated)
	h.publish(protocol.EventSquadUpdated, workspaceID, "member", requestUserID(r), map[string]any{"squad": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteSquad(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}

	if squad.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "squad is already archived")
		return
	}

	// Transfer issues assigned to this squad to the leader agent.
	if err := h.Queries.TransferSquadAssignees(r.Context(), db.TransferSquadAssigneesParams{
		AssigneeID:   squad.ID,
		AssigneeID_2: squad.LeaderID,
	}); err != nil {
		slog.Warn("transfer squad assignees failed", "squad_id", uuidToString(squad.ID), "error", err)
	}

	userID := requestUserID(r)
	userUUID, _ := parseUUIDOrBadRequest(w, userID, "user_id")

	if _, err := h.Queries.ArchiveSquad(r.Context(), db.ArchiveSquadParams{
		ID:         squad.ID,
		ArchivedBy: userUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive squad")
		return
	}

	h.publish(protocol.EventSquadDeleted, workspaceID, "member", userID, map[string]any{
		"squad_id":  uuidToString(squad.ID),
		"leader_id": uuidToString(squad.LeaderID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── Squad Members ───────────────────────────────────────────────────────────

func (h *Handler) ListSquadMembers(w http.ResponseWriter, r *http.Request) {
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	members, err := h.Queries.ListSquadMembers(r.Context(), squad.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list squad members")
		return
	}
	resp := make([]SquadMemberResponse, len(members))
	for i, m := range members {
		resp[i] = squadMemberToResponse(m)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) AddSquadMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req struct {
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
		Role       string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MemberType != "agent" && req.MemberType != "member" {
		writeError(w, http.StatusBadRequest, "member_type must be 'agent' or 'member'")
		return
	}
	if req.MemberID == "" {
		writeError(w, http.StatusBadRequest, "member_id is required")
		return
	}

	memberUUID, ok := parseUUIDOrBadRequest(w, req.MemberID, "member_id")
	if !ok {
		return
	}

	// Validate the member belongs to this workspace.
	if req.MemberType == "agent" {
		if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID: memberUUID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "agent not found in this workspace")
			return
		}
	} else {
		if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID: memberUUID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "member not found in this workspace")
			return
		}
	}

	sm, err := h.Queries.AddSquadMember(r.Context(), db.AddSquadMemberParams{
		SquadID:    squad.ID,
		MemberType: req.MemberType,
		MemberID:   memberUUID,
		Role:       req.Role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "member already in squad")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to add squad member")
		return
	}

	writeJSON(w, http.StatusCreated, squadMemberToResponse(sm))
	h.publish(protocol.EventSquadUpdated, workspaceID, "member", requestUserID(r), map[string]any{
		"squad_id": uuidToString(squad.ID),
	})
}

func (h *Handler) RemoveSquadMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}

	var req struct {
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	memberUUID, ok := parseUUIDOrBadRequest(w, req.MemberID, "member_id")
	if !ok {
		return
	}

	// Prevent removing the leader.
	if req.MemberType == "agent" && uuidToString(squad.LeaderID) == req.MemberID {
		writeError(w, http.StatusBadRequest, "cannot remove the squad leader; change leader first")
		return
	}

	rows, err := h.Queries.RemoveSquadMember(r.Context(), db.RemoveSquadMemberParams{
		SquadID:    squad.ID,
		MemberType: req.MemberType,
		MemberID:   memberUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove squad member")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "squad member not found")
		return
	}

	h.publish(protocol.EventSquadUpdated, workspaceID, "member", requestUserID(r), map[string]any{
		"squad_id": uuidToString(squad.ID),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateSquadMemberRole(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}

	var req struct {
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
		Role       string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	memberUUID, ok := parseUUIDOrBadRequest(w, req.MemberID, "member_id")
	if !ok {
		return
	}

	sm, err := h.Queries.UpdateSquadMemberRole(r.Context(), db.UpdateSquadMemberRoleParams{
		SquadID:    squad.ID,
		MemberType: req.MemberType,
		MemberID:   memberUUID,
		Role:       req.Role,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "squad member not found")
		return
	}

	h.publish(protocol.EventSquadUpdated, workspaceID, "member", requestUserID(r), map[string]any{
		"squad_id": uuidToString(squad.ID),
	})
	writeJSON(w, http.StatusOK, squadMemberToResponse(sm))
}

// ── Squad Leader Evaluation ──────────────────────────────────────────────────

// RecordSquadLeaderEvaluation records a squad leader's evaluation decision
// into the unified activity_log. Called by the leader agent via CLI after
// each trigger to record whether it took action, stayed silent, or failed.
func (h *Handler) RecordSquadLeaderEvaluation(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var req struct {
		Outcome string `json:"outcome"` // action | no_action | failed
		Reason  string `json:"reason"`  // short explanation from leader
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Outcome != "action" && req.Outcome != "no_action" && req.Outcome != "failed" {
		writeError(w, http.StatusBadRequest, "outcome must be 'action', 'no_action', or 'failed'")
		return
	}

	// The issue must be assigned to a squad.
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "squad" || !issue.AssigneeID.Valid {
		writeError(w, http.StatusBadRequest, "issue is not assigned to a squad")
		return
	}

	squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "squad not found")
		return
	}

	// Security: only the squad leader agent can record evaluations.
	workspaceID := uuidToString(issue.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "agent" || actorID != uuidToString(squad.LeaderID) {
		writeError(w, http.StatusForbidden, "only the squad leader agent can record evaluations")
		return
	}

	details, _ := json.Marshal(map[string]string{
		"squad_id": uuidToString(squad.ID),
		"outcome":  req.Outcome,
		"reason":   req.Reason,
	})

	activity, err := h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		ActorType:   pgtype.Text{String: "agent", Valid: true},
		ActorID:     squad.LeaderID,
		Action:      "squad_leader_evaluated",
		Details:     details,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record evaluation")
		return
	}

	h.publish(protocol.EventActivityCreated, uuidToString(issue.WorkspaceID), "agent", actorID, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"entry": map[string]any{
			"type":       "activity",
			"id":         uuidToString(activity.ID),
			"actor_type": "agent",
			"actor_id":   actorID,
			"action":     activity.Action,
			"details":    json.RawMessage(details),
			"created_at": timestampToString(activity.CreatedAt),
		},
	})

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":         uuidToString(activity.ID),
		"action":     activity.Action,
		"created_at": timestampToString(activity.CreatedAt),
	})
}

// ── Squad Trigger Logic ─────────────────────────────────────────────────────

// shouldEnqueueSquadLeaderOnComment returns true if the issue is assigned to a
// squad and the comment author is NOT a member of that squad (anti-loop).
// commentContent is the new comment's markdown body; when a member explicitly
// @mentions anyone (agent, member, squad, or @all) in that body, the leader
// is skipped — the @ marks deliberate routing and the leader would otherwise
// just observe and record no_action. Issue cross-reference mentions
// (mention://issue/...) are NOT a routing signal and do not suppress the
// leader. Agent-authored comments always go through the leader (subject to
// the leader self-trigger guard) so agent updates still drive coordination.
func (h *Handler) shouldEnqueueSquadLeaderOnComment(ctx context.Context, issue db.Issue, commentContent, authorType, authorID string) bool {
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "squad" || !issue.AssigneeID.Valid {
		return false
	}

	// Load the squad.
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return false
	}

	// Skip if the comment author is the squad leader itself (prevent self-trigger).
	// Other squad members ARE allowed to trigger the leader — the leader uses
	// silent/no-op turns when no action is needed.
	if authorType == "agent" && authorID == uuidToString(squad.LeaderID) {
		return false
	}

	// Member explicitly @mentioned someone → that someone owns the next step,
	// skip the leader. Covers @agent / @member / @squad / @all; issue
	// cross-references do NOT count as routing. Agent-authored comments are
	// intentionally exempt: when an agent posts a result that @mentions
	// another agent, the leader still needs to coordinate the thread.
	if authorType == "member" && commentMentionsAnyone(commentContent) {
		return false
	}

	// Verify leader agent is ready (has runtime, not archived).
	agent, err := h.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return false
	}

	return true
}

// commentMentionsAnyone returns true when the comment body contains at least
// one routing-style mention — [@Name](mention://agent|member|squad|all/<id>).
// Issue cross-references (mention://issue/...) are ignored because they are
// not directed at a participant. Only the current comment is inspected —
// parent (thread root) mentions are NOT inherited here.
func commentMentionsAnyone(content string) bool {
	for _, m := range util.ParseMentions(content) {
		switch m.Type {
		case "agent", "member", "squad", "all":
			return true
		}
	}
	return false
}

// shouldEnqueueSquadLeaderOnAssign returns true when assigning an issue to a
// squad (or creating an issue pre-assigned to a squad) should immediately
// trigger the squad leader. Mirrors shouldEnqueueAgentTask: backlog issues
// are skipped (parking lot), and the leader agent must have a runtime and
// not be archived.
func (h *Handler) shouldEnqueueSquadLeaderOnAssign(ctx context.Context, issue db.Issue) bool {
	if issue.Status == "backlog" {
		return false
	}
	return h.isSquadLeaderReady(ctx, issue)
}

// isSquadLeaderReady returns true when the issue is assigned to a squad whose
// leader agent is ready (has a runtime, not archived).
func (h *Handler) isSquadLeaderReady(ctx context.Context, issue db.Issue) bool {
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "squad" || !issue.AssigneeID.Valid {
		return false
	}
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return false
	}
	agent, err := h.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return false
	}
	return true
}

// enqueueSquadLeaderTask triggers the squad leader agent for an issue assigned to a squad.
func (h *Handler) enqueueSquadLeaderTask(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, authorType, authorID string) {
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return
	}

	// Dedup: skip if leader already has a pending task for this issue.
	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: squad.LeaderID,
	})
	if err != nil || hasPending {
		return
	}

	if _, err := h.TaskService.EnqueueTaskForMention(ctx, issue, squad.LeaderID, triggerCommentID); err != nil {
		slog.Warn("enqueue squad leader task failed",
			"issue_id", uuidToString(issue.ID),
			"squad_id", uuidToString(squad.ID),
			"leader_id", uuidToString(squad.LeaderID),
			"error", err)
	}
}
