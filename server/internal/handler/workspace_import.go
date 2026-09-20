package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/agentconfig"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Workspace import copies portable agent and squad configuration from another
// workspace the caller belongs to. Secrets and machine-local fields never
// travel: custom_env, mcp_config, and runtime_config stay empty on the copy.
// Skills attach only when a skill of the same name already exists in the
// target workspace.

const (
	importConflictSkip   = "skip"
	importConflictRename = "rename"
)

type workspaceImportPreviewAgent struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Model       string   `json:"model"`
	SkillNames  []string `json:"skill_names"`
}

type workspaceImportPreviewSquad struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	LeaderID       string   `json:"leader_id"`
	LeaderName     string   `json:"leader_name"`
	MemberCount    int      `json:"member_count"`
	AgentMemberIDs []string `json:"agent_member_ids"`
}

type WorkspaceImportPreviewResponse struct {
	SourceWorkspaceID   string                        `json:"source_workspace_id"`
	SourceWorkspaceName string                        `json:"source_workspace_name"`
	Agents              []workspaceImportPreviewAgent `json:"agents"`
	Squads              []workspaceImportPreviewSquad `json:"squads"`
}

type workspaceImportRequest struct {
	SourceWorkspaceID string   `json:"source_workspace_id"`
	RuntimeID         string   `json:"runtime_id"`
	AgentIDs          []string `json:"agent_ids"`
	SquadIDs          []string `json:"squad_ids"`
	ImportAll         bool     `json:"import_all"`
	OnConflict        string   `json:"on_conflict"`
}

type workspaceImportItemResult struct {
	SourceID string `json:"source_id"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Status   string `json:"status"` // created | skipped | reused
}

type WorkspaceImportResponse struct {
	Agents []workspaceImportItemResult `json:"agents"`
	Squads []workspaceImportItemResult `json:"squads"`
}

type importNameEntry struct {
	id       string
	archived bool
}

func (h *Handler) PreviewWorkspaceImport(w http.ResponseWriter, r *http.Request) {
	targetID := workspaceIDFromURL(r, "id")
	if _, ok := h.workspaceMember(w, r, targetID); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if actorType, _ := h.resolveActor(r, userID, targetID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot import workspace configuration")
		return
	}

	sourceID := strings.TrimSpace(r.URL.Query().Get("source_workspace_id"))
	if sourceID == "" {
		writeError(w, http.StatusBadRequest, "source_workspace_id is required")
		return
	}
	if sourceID == targetID {
		writeError(w, http.StatusBadRequest, "source workspace must be different from the current workspace")
		return
	}

	sourceMember, sourceWs, ok := h.loadImportSource(w, r, userID, sourceID)
	if !ok {
		return
	}

	agents, squads, ok := h.loadImportableSource(w, r, sourceID, sourceMember, userID)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, WorkspaceImportPreviewResponse{
		SourceWorkspaceID:   sourceID,
		SourceWorkspaceName: sourceWs.Name,
		Agents:              agents,
		Squads:              squads,
	})
}

func (h *Handler) ImportFromWorkspace(w http.ResponseWriter, r *http.Request) {
	targetID := workspaceIDFromURL(r, "id")
	targetMember, ok := h.workspaceMember(w, r, targetID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if actorType, _ := h.resolveActor(r, userID, targetID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot import workspace configuration")
		return
	}

	var req workspaceImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.SourceWorkspaceID = strings.TrimSpace(req.SourceWorkspaceID)
	req.RuntimeID = strings.TrimSpace(req.RuntimeID)
	req.OnConflict = strings.TrimSpace(req.OnConflict)
	if req.OnConflict == "" {
		req.OnConflict = importConflictSkip
	}
	if req.OnConflict != importConflictSkip && req.OnConflict != importConflictRename {
		writeError(w, http.StatusBadRequest, "on_conflict must be skip or rename")
		return
	}
	if req.SourceWorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "source_workspace_id is required")
		return
	}
	if req.SourceWorkspaceID == targetID {
		writeError(w, http.StatusBadRequest, "source workspace must be different from the current workspace")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	sourceMember, _, ok := h.loadImportSource(w, r, userID, req.SourceWorkspaceID)
	if !ok {
		return
	}

	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, targetID, "workspace_id")
	if !ok {
		return
	}
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: targetUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	if !canUseRuntimeForAgent(targetMember, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner can create agents on it")
		return
	}

	previewAgents, previewSquads, ok := h.loadImportableSource(w, r, req.SourceWorkspaceID, sourceMember, userID)
	if !ok {
		return
	}
	agentIDs, squadIDs, ok := selectImportIDs(w, req, previewAgents, previewSquads)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start import")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	result, err := h.runWorkspaceImport(r.Context(), qtx, workspaceImportPlan{
		targetID:     targetID,
		targetUUID:   targetUUID,
		sourceID:     req.SourceWorkspaceID,
		userID:       userID,
		targetMember: targetMember,
		runtime:      runtime,
		onConflict:   req.OnConflict,
		agentIDs:     agentIDs,
		squadIDs:     squadIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit import")
		return
	}

	for _, item := range result.Agents {
		if item.Status != "created" {
			continue
		}
		if created, err := h.Queries.GetAgent(r.Context(), parseUUID(item.ID)); err == nil {
			resp := h.agentToResponse(created)
			h.publish(protocol.EventAgentCreated, targetID, "member", userID, map[string]any{"agent": resp})
		}
	}
	for _, item := range result.Squads {
		if item.Status != "created" {
			continue
		}
		if created, err := h.Queries.GetSquad(r.Context(), parseUUID(item.ID)); err == nil {
			resp := h.squadToResponse(created)
			h.publish(protocol.EventSquadCreated, targetID, "member", userID, map[string]any{"squad": resp})
		}
	}

	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) loadImportSource(w http.ResponseWriter, r *http.Request, userID, sourceID string) (db.Member, db.Workspace, bool) {
	sourceUUID, ok := parseUUIDOrBadRequest(w, sourceID, "source_workspace_id")
	if !ok {
		return db.Member{}, db.Workspace{}, false
	}
	sourceWs, err := h.Queries.GetWorkspace(r.Context(), sourceUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "source workspace not found")
		return db.Member{}, db.Workspace{}, false
	}
	member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: sourceUUID,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "you are not a member of the source workspace")
		return db.Member{}, db.Workspace{}, false
	}
	return member, sourceWs, true
}

func (h *Handler) loadImportableSource(w http.ResponseWriter, r *http.Request, sourceID string, sourceMember db.Member, userID string) ([]workspaceImportPreviewAgent, []workspaceImportPreviewSquad, bool) {
	sourceUUID := parseUUID(sourceID)
	agents, err := h.Queries.ListAgents(r.Context(), sourceUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list source agents")
		return nil, nil, false
	}
	targetsByAgent, ok := h.loadInvocationTargetsByAgent(r.Context(), agents)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to load agent invocation targets")
		return nil, nil, false
	}
	skillRows, err := h.Queries.ListAgentSkillsByWorkspace(r.Context(), sourceUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load source agent skills")
		return nil, nil, false
	}
	skillsByAgent := map[string][]string{}
	for _, row := range skillRows {
		if !row.Enabled {
			continue
		}
		agentID := uuidToString(row.AgentID)
		skillsByAgent[agentID] = append(skillsByAgent[agentID], row.Name)
	}

	visible := make([]workspaceImportPreviewAgent, 0, len(agents))
	visibleIDs := map[string]db.Agent{}
	for _, a := range agents {
		if a.SystemKey.Valid && a.SystemKey.String != "" {
			continue
		}
		if !memberAllowedToViewAgent(a, targetsByAgent[uuidToString(a.ID)], userID, sourceMember.Role) {
			continue
		}
		id := uuidToString(a.ID)
		visibleIDs[id] = a
		names := skillsByAgent[id]
		if names == nil {
			names = []string{}
		}
		visible = append(visible, workspaceImportPreviewAgent{
			ID:          id,
			Name:        a.Name,
			Description: a.Description,
			Model:       a.Model.String,
			SkillNames:  names,
		})
	}

	squads, err := h.Queries.ListSquads(r.Context(), sourceUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list source squads")
		return nil, nil, false
	}
	preview := make([]workspaceImportPreviewSquad, 0, len(squads))
	for _, s := range squads {
		members, err := h.Queries.ListSquadMembers(r.Context(), s.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list source squad members")
			return nil, nil, false
		}
		agentMemberIDs := make([]string, 0, len(members))
		for _, m := range members {
			if m.MemberType != "agent" {
				continue
			}
			mid := uuidToString(m.MemberID)
			if _, ok := visibleIDs[mid]; ok {
				agentMemberIDs = append(agentMemberIDs, mid)
			}
		}
		leaderID := uuidToString(s.LeaderID)
		leaderName := ""
		if leader, ok := visibleIDs[leaderID]; ok {
			leaderName = leader.Name
		} else {
			continue
		}
		preview = append(preview, workspaceImportPreviewSquad{
			ID:             uuidToString(s.ID),
			Name:           s.Name,
			Description:    s.Description,
			LeaderID:       leaderID,
			LeaderName:     leaderName,
			MemberCount:    len(members),
			AgentMemberIDs: agentMemberIDs,
		})
	}
	return visible, preview, true
}

func selectImportIDs(w http.ResponseWriter, req workspaceImportRequest, agents []workspaceImportPreviewAgent, squads []workspaceImportPreviewSquad) ([]string, []string, bool) {
	visibleAgents := map[string]struct{}{}
	for _, a := range agents {
		visibleAgents[a.ID] = struct{}{}
	}
	visibleSquads := map[string]workspaceImportPreviewSquad{}
	for _, s := range squads {
		visibleSquads[s.ID] = s
	}

	if req.ImportAll || (len(req.AgentIDs) == 0 && len(req.SquadIDs) == 0) {
		agentIDs := make([]string, 0, len(agents))
		for _, a := range agents {
			agentIDs = append(agentIDs, a.ID)
		}
		squadIDs := make([]string, 0, len(squads))
		for _, s := range squads {
			squadIDs = append(squadIDs, s.ID)
		}
		if len(agentIDs) == 0 && len(squadIDs) == 0 {
			writeError(w, http.StatusBadRequest, "source workspace has no agents or squads to import")
			return nil, nil, false
		}
		return agentIDs, squadIDs, true
	}

	agentSet := map[string]struct{}{}
	agentIDs := make([]string, 0, len(req.AgentIDs))
	for _, id := range req.AgentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := visibleAgents[id]; !ok {
			writeError(w, http.StatusBadRequest, "agent is not available to import: "+id)
			return nil, nil, false
		}
		if _, dup := agentSet[id]; dup {
			continue
		}
		agentSet[id] = struct{}{}
		agentIDs = append(agentIDs, id)
	}
	squadIDs := make([]string, 0, len(req.SquadIDs))
	for _, id := range req.SquadIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		squad, ok := visibleSquads[id]
		if !ok {
			writeError(w, http.StatusBadRequest, "squad is not available to import: "+id)
			return nil, nil, false
		}
		squadIDs = append(squadIDs, id)
		for _, mid := range squad.AgentMemberIDs {
			if _, dup := agentSet[mid]; dup {
				continue
			}
			agentSet[mid] = struct{}{}
			agentIDs = append(agentIDs, mid)
		}
	}
	if len(agentIDs) == 0 && len(squadIDs) == 0 {
		writeError(w, http.StatusBadRequest, "select at least one agent or squad")
		return nil, nil, false
	}
	return agentIDs, squadIDs, true
}

type workspaceImportPlan struct {
	targetID     string
	targetUUID   pgtype.UUID
	sourceID     string
	userID       string
	targetMember db.Member
	runtime      db.AgentRuntime
	onConflict   string
	agentIDs     []string
	squadIDs     []string
}

func (h *Handler) runWorkspaceImport(ctx context.Context, q *db.Queries, plan workspaceImportPlan) (WorkspaceImportResponse, error) {
	existingAgents, err := q.ListAllAgents(ctx, plan.targetUUID)
	if err != nil {
		return WorkspaceImportResponse{}, fmt.Errorf("failed to list target agents")
	}
	agentNames := map[string]importNameEntry{}
	for _, a := range existingAgents {
		agentNames[a.Name] = importNameEntry{id: uuidToString(a.ID), archived: a.ArchivedAt.Valid}
	}
	existingSquads, err := q.ListAllSquads(ctx, plan.targetUUID)
	if err != nil {
		return WorkspaceImportResponse{}, fmt.Errorf("failed to list target squads")
	}
	squadNames := map[string]importNameEntry{}
	for _, s := range existingSquads {
		squadNames[s.Name] = importNameEntry{id: uuidToString(s.ID), archived: s.ArchivedAt.Valid}
	}

	idMap := map[string]string{}
	out := WorkspaceImportResponse{
		Agents: make([]workspaceImportItemResult, 0, len(plan.agentIDs)),
		Squads: make([]workspaceImportItemResult, 0, len(plan.squadIDs)),
	}

	for _, sourceID := range plan.agentIDs {
		src, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          parseUUID(sourceID),
			WorkspaceID: parseUUID(plan.sourceID),
		})
		if err != nil {
			return WorkspaceImportResponse{}, fmt.Errorf("failed to load source agent")
		}
		item, err := h.copyImportedAgent(ctx, q, plan, src, agentNames, idMap)
		if err != nil {
			return WorkspaceImportResponse{}, err
		}
		out.Agents = append(out.Agents, item)
	}
	if err := remapCreatedAgentMentions(ctx, q, out.Agents, idMap); err != nil {
		return WorkspaceImportResponse{}, err
	}

	for _, sourceID := range plan.squadIDs {
		src, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          parseUUID(sourceID),
			WorkspaceID: parseUUID(plan.sourceID),
		})
		if err != nil {
			return WorkspaceImportResponse{}, fmt.Errorf("failed to load source squad")
		}
		item, err := h.copyImportedSquad(ctx, q, plan, src, squadNames, idMap)
		if err != nil {
			return WorkspaceImportResponse{}, err
		}
		if item.Status != "" {
			out.Squads = append(out.Squads, item)
		}
	}
	return out, nil
}

func (h *Handler) copyImportedAgent(ctx context.Context, q *db.Queries, plan workspaceImportPlan, src db.Agent, names map[string]importNameEntry, idMap map[string]string) (workspaceImportItemResult, error) {
	sourceID := uuidToString(src.ID)
	name, reusedID, skip := allocateImportedName(names, src.Name, plan.onConflict)
	if skip {
		// Name collision: do not create a second agent. Only reuse the
		// existing row for squad wiring when the importer could attach it
		// through CreateSquad / AddSquadMember (MUL-4223). Putting an
		// unwitable private agent in idMap would let a member smuggle it
		// in as squad leader by matching its name.
		if h.canWireImportedAgent(ctx, q, plan, reusedID) {
			idMap[sourceID] = reusedID
		}
		return workspaceImportItemResult{SourceID: sourceID, ID: reusedID, Name: src.Name, Status: "skipped"}, nil
	}

	maxTasks := src.MaxConcurrentTasks
	if !agentconfig.IsValidMaxConcurrentTasks(maxTasks) {
		maxTasks = agentconfig.DefaultMaxConcurrentTasks
	}

	customArgs := src.CustomArgs
	if len(customArgs) == 0 {
		customArgs = []byte("[]")
	}
	starters := src.ConversationStarters
	if len(starters) == 0 {
		starters = []byte("[]")
	}

	model := src.Model
	thinking := src.ThinkingLevel
	serviceTier := src.ServiceTier
	if thinking.Valid && !agent.IsKnownThinkingValue(plan.runtime.Provider, thinking.String) {
		thinking = pgtype.Text{}
	}
	if thinking.Valid && agent.ThinkingLevelRejectedWithoutModel(plan.runtime.Provider) && (!model.Valid || model.String == "") {
		thinking = pgtype.Text{}
	}
	if serviceTier.Valid && !agent.IsKnownServiceTier(plan.runtime.Provider, serviceTier.String) {
		serviceTier = pgtype.Text{}
	}

	permMode, targets := importedAgentPermission(ctx, q, src, plan.targetUUID)
	visibility := "private"
	if permMode == permissionModePublicTo {
		for _, t := range targets {
			if t.targetType == invocationTargetWorkspace {
				visibility = "workspace"
				break
			}
		}
	}

	created, err := q.CreateAgent(ctx, db.CreateAgentParams{
		WorkspaceID:          plan.targetUUID,
		Name:                 name,
		Description:          src.Description,
		Instructions:         src.Instructions,
		AvatarUrl:            src.AvatarUrl,
		RuntimeMode:          plan.runtime.RuntimeMode,
		RuntimeConfig:        []byte("{}"),
		RuntimeID:            plan.runtime.ID,
		Visibility:           visibility,
		PermissionMode:       permMode,
		MaxConcurrentTasks:   maxTasks,
		OwnerID:              parseUUID(plan.userID),
		CustomEnv:            []byte("{}"),
		CustomArgs:           customArgs,
		McpConfig:            nil,
		Model:                model,
		ThinkingLevel:        thinking,
		ServiceTier:          serviceTier,
		ConversationStarters: starters,
	})
	if err != nil {
		return workspaceImportItemResult{}, fmt.Errorf("failed to create agent %q", src.Name)
	}
	if err := replaceInvocationTargetsWithQueries(ctx, q, created.ID, parseUUID(plan.userID), targets); err != nil {
		return workspaceImportItemResult{}, fmt.Errorf("failed to copy agent access")
	}

	skills, err := q.ListAgentSkills(ctx, src.ID)
	if err != nil {
		return workspaceImportItemResult{}, fmt.Errorf("failed to load source agent skills")
	}
	for _, skill := range skills {
		matched, err := q.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{
			WorkspaceID: plan.targetUUID,
			Name:        skill.Name,
		})
		if err != nil {
			continue
		}
		if err := q.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: created.ID, SkillID: matched.ID}); err != nil {
			return workspaceImportItemResult{}, fmt.Errorf("failed to attach skill %q", skill.Name)
		}
	}

	newID := uuidToString(created.ID)
	idMap[sourceID] = newID
	names[name] = importNameEntry{id: newID}
	return workspaceImportItemResult{SourceID: sourceID, ID: newID, Name: name, Status: "created"}, nil
}

func (h *Handler) copyImportedSquad(ctx context.Context, q *db.Queries, plan workspaceImportPlan, src db.Squad, names map[string]importNameEntry, idMap map[string]string) (workspaceImportItemResult, error) {
	sourceID := uuidToString(src.ID)
	leaderID, ok := idMap[uuidToString(src.LeaderID)]
	if !ok || !h.canWireImportedAgent(ctx, q, plan, leaderID) {
		return workspaceImportItemResult{SourceID: sourceID, Name: src.Name, Status: "skipped"}, nil
	}
	name, reusedID, skip := allocateImportedName(names, src.Name, plan.onConflict)
	if skip {
		return workspaceImportItemResult{SourceID: sourceID, ID: reusedID, Name: src.Name, Status: "skipped"}, nil
	}

	created, err := q.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: plan.targetUUID,
		Name:        name,
		Description: src.Description,
		LeaderID:    parseUUID(leaderID),
		CreatorID:   parseUUID(plan.userID),
		AvatarUrl:   src.AvatarUrl,
	})
	if err != nil {
		return workspaceImportItemResult{}, fmt.Errorf("failed to create squad %q", src.Name)
	}
	if _, err := q.AddSquadMember(ctx, db.AddSquadMemberParams{
		SquadID: created.ID, MemberType: "agent", MemberID: parseUUID(leaderID), Role: "leader",
	}); err != nil {
		return workspaceImportItemResult{}, fmt.Errorf("failed to add squad leader")
	}

	members, err := q.ListSquadMembers(ctx, src.ID)
	if err != nil {
		return workspaceImportItemResult{}, fmt.Errorf("failed to list source squad members")
	}
	for _, m := range members {
		switch m.MemberType {
		case "agent":
			mapped, ok := idMap[uuidToString(m.MemberID)]
			if !ok || mapped == leaderID || !h.canWireImportedAgent(ctx, q, plan, mapped) {
				continue
			}
			if _, err := q.AddSquadMember(ctx, db.AddSquadMemberParams{
				SquadID: created.ID, MemberType: "agent", MemberID: parseUUID(mapped), Role: m.Role,
			}); err != nil {
				return workspaceImportItemResult{}, fmt.Errorf("failed to add squad agent member")
			}
		case "member":
			if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
				UserID:      m.MemberID,
				WorkspaceID: plan.targetUUID,
			}); err != nil {
				continue
			}
			if _, err := q.AddSquadMember(ctx, db.AddSquadMemberParams{
				SquadID: created.ID, MemberType: "member", MemberID: m.MemberID, Role: m.Role,
			}); err != nil {
				return workspaceImportItemResult{}, fmt.Errorf("failed to add squad member")
			}
		}
	}

	instructions := rewriteImportedMentions(src.Instructions, idMap)
	if instructions != "" {
		if _, err := q.UpdateSquad(ctx, db.UpdateSquadParams{
			ID:           created.ID,
			Instructions: pgtype.Text{String: instructions, Valid: true},
		}); err != nil {
			return workspaceImportItemResult{}, fmt.Errorf("failed to copy squad instructions")
		}
	}

	newID := uuidToString(created.ID)
	names[name] = importNameEntry{id: newID}
	return workspaceImportItemResult{SourceID: sourceID, ID: newID, Name: name, Status: "created"}, nil
}

func allocateImportedName(existing map[string]importNameEntry, name, onConflict string) (string, string, bool) {
	entry, taken := existing[name]
	if !taken {
		return name, "", false
	}
	if onConflict != importConflictRename && !entry.archived {
		return name, entry.id, true
	}
	for i := 1; ; i++ {
		candidate := name + " (imported)"
		if i > 1 {
			candidate = fmt.Sprintf("%s (imported %d)", name, i)
		}
		if _, ok := existing[candidate]; !ok {
			return candidate, "", false
		}
	}
}

func (h *Handler) canWireImportedAgent(ctx context.Context, q *db.Queries, plan workspaceImportPlan, agentID string) bool {
	if agentID == "" {
		return false
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          parseUUID(agentID),
		WorkspaceID: plan.targetUUID,
	})
	if err != nil {
		return false
	}
	return h.memberCanWireAgent(ctx, plan.targetMember, agent, plan.targetID)
}

func remapCreatedAgentMentions(ctx context.Context, q *db.Queries, items []workspaceImportItemResult, idMap map[string]string) error {
	if len(idMap) == 0 {
		return nil
	}
	for _, item := range items {
		if item.Status != "created" || item.ID == "" {
			continue
		}
		created, err := q.GetAgent(ctx, parseUUID(item.ID))
		if err != nil {
			return fmt.Errorf("failed to load imported agent for mention remap")
		}
		instructions := rewriteImportedMentions(created.Instructions, idMap)
		starters := created.ConversationStarters
		if len(starters) > 0 {
			rewritten := rewriteImportedMentions(string(starters), idMap)
			if rewritten != string(starters) {
				starters = []byte(rewritten)
			}
		}
		if instructions == created.Instructions && string(starters) == string(created.ConversationStarters) {
			continue
		}
		params := db.UpdateAgentParams{ID: created.ID}
		if instructions != created.Instructions {
			params.Instructions = pgtype.Text{String: instructions, Valid: true}
		}
		if string(starters) != string(created.ConversationStarters) {
			params.ConversationStarters = starters
		}
		if _, err := q.UpdateAgent(ctx, params); err != nil {
			return fmt.Errorf("failed to remap agent mentions")
		}
	}
	return nil
}

func rewriteImportedMentions(text string, idMap map[string]string) string {
	if text == "" || len(idMap) == 0 {
		return text
	}
	out := text
	for oldID, newID := range idMap {
		out = strings.ReplaceAll(out, oldID, newID)
	}
	return out
}

func importedAgentPermission(ctx context.Context, q *db.Queries, src db.Agent, targetUUID pgtype.UUID) (string, []targetSpec) {
	if src.PermissionMode != permissionModePublicTo {
		return permissionModePrivate, nil
	}
	targets, err := q.ListAgentInvocationTargets(ctx, src.ID)
	if err != nil {
		return permissionModePrivate, nil
	}
	copied := make([]targetSpec, 0, len(targets))
	for _, t := range targets {
		switch t.TargetType {
		case invocationTargetWorkspace:
			copied = append(copied, targetSpec{targetType: invocationTargetWorkspace, targetID: targetUUID})
		case invocationTargetMember:
			if !t.TargetID.Valid {
				continue
			}
			if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
				UserID:      t.TargetID,
				WorkspaceID: targetUUID,
			}); err != nil {
				continue
			}
			copied = append(copied, targetSpec{targetType: invocationTargetMember, targetID: t.TargetID})
		}
	}
	if len(copied) == 0 {
		return permissionModePrivate, nil
	}
	return permissionModePublicTo, copied
}
