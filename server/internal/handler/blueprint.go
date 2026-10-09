package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/blueprint"
	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ExportBlueprintRequest struct {
	Name     string   `json:"name"`
	SquadIDs []string `json:"squad_ids"`
	AgentIDs []string `json:"agent_ids"`
	SkillIDs []string `json:"skill_ids"`
}

type PreviewBlueprintRequest struct {
	Manifest        blueprint.Manifest         `json:"manifest"`
	RuntimeMappings []blueprint.RuntimeMapping `json:"runtime_mappings"`
	ProvidedEnv     []blueprint.ProvidedEnvVar `json:"provided_env"`
}

type ApplyBlueprintRequest struct {
	Manifest        blueprint.Manifest             `json:"manifest"`
	RuntimeMappings []blueprint.RuntimeMapping     `json:"runtime_mappings"`
	ProvidedEnv     []ApplyBlueprintProvidedEnvVar `json:"provided_env"`
}

type ApplyBlueprintProvidedEnvVar struct {
	AgentRef string `json:"agent_ref"`
	Name     string `json:"name"`
	Value    string `json:"value"`
}

type ApplyBlueprintResponse struct {
	Preview blueprint.Preview          `json:"preview"`
	Squads  []BlueprintApplyResultItem `json:"squads"`
	Agents  []BlueprintApplyResultItem `json:"agents"`
	Skills  []BlueprintApplyResultItem `json:"skills"`
}

type BlueprintApplyResultItem struct {
	Ref    string `json:"ref"`
	Name   string `json:"name"`
	Action string `json:"action"`
	ID     string `json:"id"`
}

func (h *Handler) ExportBlueprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	var req ExportBlueprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.SquadIDs) == 0 && len(req.AgentIDs) == 0 && len(req.SkillIDs) == 0 {
		writeError(w, http.StatusBadRequest, "at least one squad_id, agent_id, or skill_id is required")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	squadIDs, ok := parseBlueprintExportIDs(w, req.SquadIDs, "squad_ids")
	if !ok {
		return
	}
	explicitAgentIDs, ok := parseBlueprintExportIDs(w, req.AgentIDs, "agent_ids")
	if !ok {
		return
	}
	explicitSkillIDs, ok := parseBlueprintExportIDs(w, req.SkillIDs, "skill_ids")
	if !ok {
		return
	}

	source := blueprint.Source{
		Name:          req.Name,
		SquadMembers:  map[string][]blueprint.SourceSquadMember{},
		AgentSkillIDs: map[string][]string{},
		SkillFiles:    map[string][]blueprint.SourceSkillFile{},
	}

	agentIDs := orderedIDSet{}
	skillIDs := orderedIDSet{}

	for _, squadID := range squadIDs {
		squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
			ID:          parseUUID(squadID),
			WorkspaceID: wsUUID,
		})
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, "squad not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to load squad")
			return
		}
		source.Squads = append(source.Squads, sourceSquadFromDB(squad))
		agentIDs.add(uuidToString(squad.LeaderID))

		members, err := h.Queries.ListSquadMembers(r.Context(), squad.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load squad members")
			return
		}
		memberSources := make([]blueprint.SourceSquadMember, 0, len(members))
		for _, member := range members {
			memberID := uuidToString(member.MemberID)
			memberSources = append(memberSources, blueprint.SourceSquadMember{
				MemberType: member.MemberType,
				MemberID:   memberID,
				Role:       member.Role,
			})
			if member.MemberType == "agent" {
				agentIDs.add(memberID)
			}
		}
		source.SquadMembers[uuidToString(squad.ID)] = memberSources
	}

	for _, agentID := range explicitAgentIDs {
		agentIDs.add(agentID)
	}
	for _, skillID := range explicitSkillIDs {
		skillIDs.add(skillID)
	}

	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	for _, agentID := range agentIDs.ids {
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          parseUUID(agentID),
			WorkspaceID: wsUUID,
		})
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, "agent not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to load agent")
			return
		}
		if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
			writeError(w, http.StatusForbidden, "you do not have access to this agent")
			return
		}

		runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
			ID:          agent.RuntimeID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load agent runtime")
			return
		}
		source.Agents = append(source.Agents, sourceAgentFromDB(agent, runtime.Provider))

		agentSkills, err := h.Queries.ListAgentSkills(r.Context(), agent.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load agent skills")
			return
		}
		for _, skill := range agentSkills {
			skillID := uuidToString(skill.ID)
			source.AgentSkillIDs[agentID] = append(source.AgentSkillIDs[agentID], skillID)
			skillIDs.add(skillID)
		}
	}

	for _, skillID := range skillIDs.ids {
		skill, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
			ID:          parseUUID(skillID),
			WorkspaceID: wsUUID,
		})
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, "skill not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to load skill")
			return
		}
		source.Skills = append(source.Skills, sourceSkillFromDB(skill))

		files, err := h.Queries.ListSkillFiles(r.Context(), skill.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load skill files")
			return
		}
		fileSources := make([]blueprint.SourceSkillFile, 0, len(files))
		for _, file := range files {
			fileSources = append(fileSources, blueprint.SourceSkillFile{
				Path:    file.Path,
				Content: file.Content,
			})
		}
		source.SkillFiles[skillID] = fileSources
	}

	manifest, err := blueprint.BuildManifest(source)
	if err != nil {
		slog.Warn("blueprint export failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to build blueprint")
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (h *Handler) PreviewBlueprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	var req PreviewBlueprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := blueprint.ValidateManifest(req.Manifest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid blueprint manifest: "+err.Error())
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	for _, mapping := range req.RuntimeMappings {
		if mapping.RuntimeID == "" {
			continue
		}
		if _, ok := parseUUIDOrBadRequest(w, mapping.RuntimeID, "runtime_id"); !ok {
			return
		}
	}

	inventory, ok := h.blueprintPreviewInventory(w, r, wsUUID, req.RuntimeMappings, req.ProvidedEnv)
	if !ok {
		return
	}
	preview, err := blueprint.PreviewManifest(req.Manifest, inventory)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid blueprint manifest: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *Handler) ApplyBlueprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	var req ApplyBlueprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := blueprint.ValidateManifest(req.Manifest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid blueprint manifest: "+err.Error())
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	for _, mapping := range req.RuntimeMappings {
		if mapping.RuntimeID == "" {
			continue
		}
		if _, ok := parseUUIDOrBadRequest(w, mapping.RuntimeID, "runtime_id"); !ok {
			return
		}
	}

	providedEnv := providedEnvVarsFromApplyValues(req.ProvidedEnv)
	inventory, ok := h.blueprintPreviewInventory(w, r, wsUUID, req.RuntimeMappings, providedEnv)
	if !ok {
		return
	}
	preview, err := blueprint.PreviewManifest(req.Manifest, inventory)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid blueprint manifest: "+err.Error())
		return
	}
	if preview.HasBlockingIssues {
		writeJSON(w, http.StatusUnprocessableEntity, ApplyBlueprintResponse{Preview: preview})
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start blueprint import")
		return
	}
	defer tx.Rollback(r.Context())

	resp, err := applyBlueprintInTx(r.Context(), h.Queries.WithTx(tx), member, wsUUID, req.Manifest, preview, req.ProvidedEnv)
	if err != nil {
		var statusErr blueprintApplyStatusError
		if errors.As(err, &statusErr) {
			writeError(w, statusErr.Status, statusErr.Message)
			return
		}
		slog.Warn("blueprint apply failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to apply blueprint")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to apply blueprint")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func parseBlueprintExportIDs(w http.ResponseWriter, ids []string, fieldName string) ([]string, bool) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		u, ok := parseUUIDOrBadRequest(w, id, fieldName)
		if !ok {
			return nil, false
		}
		out = append(out, uuidToString(u))
	}
	return out, true
}

type orderedIDSet struct {
	ids  []string
	seen map[string]struct{}
}

func (s *orderedIDSet) add(id string) {
	if id == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	if _, ok := s.seen[id]; ok {
		return
	}
	s.seen[id] = struct{}{}
	s.ids = append(s.ids, id)
}

func sourceSquadFromDB(s db.Squad) blueprint.SourceSquad {
	return blueprint.SourceSquad{
		ID:           uuidToString(s.ID),
		Name:         s.Name,
		Description:  s.Description,
		Instructions: s.Instructions,
		AvatarURL:    textToPtr(s.AvatarUrl),
		LeaderID:     uuidToString(s.LeaderID),
	}
}

func sourceAgentFromDB(a db.Agent, runtimeProvider string) blueprint.SourceAgent {
	customEnv := map[string]string{}
	if len(a.CustomEnv) > 0 {
		if err := json.Unmarshal(a.CustomEnv, &customEnv); err != nil {
			slog.Warn("failed to unmarshal agent custom_env for blueprint export", "agent_id", uuidToString(a.ID), "error", err)
			customEnv = map[string]string{}
		}
	}

	customArgs := []string{}
	if len(a.CustomArgs) > 0 {
		if err := json.Unmarshal(a.CustomArgs, &customArgs); err != nil {
			slog.Warn("failed to unmarshal agent custom_args for blueprint export", "agent_id", uuidToString(a.ID), "error", err)
			customArgs = []string{}
		}
	}

	var mcpConfig json.RawMessage
	if len(a.McpConfig) > 0 {
		mcpConfig = append(json.RawMessage(nil), a.McpConfig...)
	}

	return blueprint.SourceAgent{
		ID:                 uuidToString(a.ID),
		Name:               a.Name,
		Description:        a.Description,
		Instructions:       a.Instructions,
		AvatarURL:          textToPtr(a.AvatarUrl),
		RuntimeID:          uuidToString(a.RuntimeID),
		RuntimeMode:        a.RuntimeMode,
		RuntimeProvider:    runtimeProvider,
		RuntimeConfig:      append(json.RawMessage(nil), a.RuntimeConfig...),
		Visibility:         a.Visibility,
		MaxConcurrentTasks: a.MaxConcurrentTasks,
		Model:              a.Model.String,
		ThinkingLevel:      a.ThinkingLevel.String,
		CustomEnv:          customEnv,
		CustomArgs:         customArgs,
		MCPConfig:          mcpConfig,
	}
}

func sourceSkillFromDB(s db.Skill) blueprint.SourceSkill {
	config := json.RawMessage(`{}`)
	if len(s.Config) > 0 {
		config = append(json.RawMessage(nil), s.Config...)
	}
	return blueprint.SourceSkill{
		ID:          uuidToString(s.ID),
		Name:        s.Name,
		Description: s.Description,
		Content:     s.Content,
		Config:      config,
	}
}

func (h *Handler) blueprintPreviewInventory(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, runtimeMappings []blueprint.RuntimeMapping, providedEnv []blueprint.ProvidedEnvVar) (blueprint.Inventory, bool) {
	squads, err := h.Queries.ListAllSquads(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load squads")
		return blueprint.Inventory{}, false
	}
	agents, err := h.Queries.ListAllAgents(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agents")
		return blueprint.Inventory{}, false
	}
	skills, err := h.Queries.ListSkillSummariesByWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skills")
		return blueprint.Inventory{}, false
	}
	runtimes, err := h.Queries.ListAgentRuntimes(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load runtimes")
		return blueprint.Inventory{}, false
	}

	inventory := blueprint.Inventory{
		Squads:          make([]blueprint.ExistingResource, 0, len(squads)),
		Agents:          make([]blueprint.ExistingResource, 0, len(agents)),
		Skills:          make([]blueprint.ExistingResource, 0, len(skills)),
		Runtimes:        make([]blueprint.ExistingRuntime, 0, len(runtimes)),
		RuntimeMappings: runtimeMappings,
		ProvidedEnv:     providedEnv,
	}
	for _, squad := range squads {
		inventory.Squads = append(inventory.Squads, blueprint.ExistingResource{
			ID:   uuidToString(squad.ID),
			Name: squad.Name,
		})
	}
	for _, agent := range agents {
		inventory.Agents = append(inventory.Agents, blueprint.ExistingResource{
			ID:   uuidToString(agent.ID),
			Name: agent.Name,
		})
	}
	for _, skill := range skills {
		inventory.Skills = append(inventory.Skills, blueprint.ExistingResource{
			ID:   uuidToString(skill.ID),
			Name: skill.Name,
		})
	}
	for _, runtime := range runtimes {
		inventory.Runtimes = append(inventory.Runtimes, blueprint.ExistingRuntime{
			ID:       uuidToString(runtime.ID),
			Provider: runtime.Provider,
		})
	}
	return inventory, true
}

type blueprintApplyStatusError struct {
	Status  int
	Message string
}

func (e blueprintApplyStatusError) Error() string {
	return e.Message
}

func applyBlueprintInTx(
	ctx context.Context,
	qtx *db.Queries,
	member db.Member,
	workspaceID pgtype.UUID,
	manifest blueprint.Manifest,
	preview blueprint.Preview,
	providedEnv []ApplyBlueprintProvidedEnvVar,
) (ApplyBlueprintResponse, error) {
	resp := ApplyBlueprintResponse{
		Preview: preview,
		Squads:  make([]BlueprintApplyResultItem, 0, len(manifest.Squads)),
		Agents:  make([]BlueprintApplyResultItem, 0, len(manifest.Agents)),
		Skills:  make([]BlueprintApplyResultItem, 0, len(manifest.Skills)),
	}
	skillPlans := blueprintResourcePlansByRef(preview.Skills)
	agentPlans := blueprintAgentPlansByRef(preview.Agents)
	squadPlans := blueprintResourcePlansByRef(preview.Squads)
	envByAgentRef := applyEnvValuesByAgentRef(providedEnv)

	skillIDs := make(map[string]string, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		plan, ok := skillPlans[skill.Ref]
		if !ok {
			return ApplyBlueprintResponse{}, blueprintApplyStatusError{Status: http.StatusBadRequest, Message: fmt.Sprintf("missing preview plan for skill %q", skill.Ref)}
		}
		if plan.Action == blueprint.PreviewActionReuse {
			skillIDs[skill.Ref] = plan.ExistingID
			resp.Skills = append(resp.Skills, applyResultFromPlan(plan, plan.ExistingID))
			continue
		}

		files := make([]CreateSkillFileRequest, 0, len(skill.Files))
		for _, file := range skill.Files {
			files = append(files, CreateSkillFileRequest{Path: file.Path, Content: file.Content})
		}
		config := any(skill.Config)
		if len(skill.Config) == 0 {
			config = json.RawMessage(`{}`)
		}
		created, err := createSkillWithFilesInTx(ctx, qtx, skillCreateInput{
			WorkspaceID: workspaceID,
			CreatorID:   member.UserID,
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
			Config:      config,
			Files:       files,
		})
		if err != nil {
			return ApplyBlueprintResponse{}, err
		}
		skillIDs[skill.Ref] = created.ID
		resp.Skills = append(resp.Skills, applyResultFromPlan(plan, created.ID))
	}

	agentIDs := make(map[string]string, len(manifest.Agents))
	for _, bpAgent := range manifest.Agents {
		plan, ok := agentPlans[bpAgent.Ref]
		if !ok {
			return ApplyBlueprintResponse{}, blueprintApplyStatusError{Status: http.StatusBadRequest, Message: fmt.Sprintf("missing preview plan for agent %q", bpAgent.Ref)}
		}
		if plan.Action == blueprint.PreviewActionReuse {
			agentIDs[bpAgent.Ref] = plan.ExistingID
			resp.Agents = append(resp.Agents, applyResultFromPlan(plan.ResourcePlan, plan.ExistingID))
			continue
		}
		if plan.Runtime.RuntimeID == "" {
			return ApplyBlueprintResponse{}, blueprintApplyStatusError{Status: http.StatusUnprocessableEntity, Message: fmt.Sprintf("missing runtime for agent %q", bpAgent.Ref)}
		}

		runtimeID := parseUUID(plan.Runtime.RuntimeID)
		runtime, err := qtx.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
			ID:          runtimeID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return ApplyBlueprintResponse{}, blueprintApplyStatusError{Status: http.StatusUnprocessableEntity, Message: fmt.Sprintf("runtime for agent %q is no longer available", bpAgent.Ref)}
		}
		if !canUseRuntimeForAgent(member, runtime) {
			return ApplyBlueprintResponse{}, blueprintApplyStatusError{Status: http.StatusForbidden, Message: "this runtime is private; only its owner or a workspace admin can create agents on it"}
		}
		if !agentpkg.IsKnownThinkingValue(runtime.Provider, bpAgent.Runtime.ThinkingLevel) {
			return ApplyBlueprintResponse{}, blueprintApplyStatusError{
				Status:  http.StatusBadRequest,
				Message: fmt.Sprintf("thinking_level %q is not a recognised value for runtime %q", bpAgent.Runtime.ThinkingLevel, runtime.Provider),
			}
		}

		customEnvRaw, err := json.Marshal(applyCustomEnvForAgent(bpAgent, envByAgentRef[bpAgent.Ref]))
		if err != nil {
			return ApplyBlueprintResponse{}, err
		}
		customArgsRaw, err := json.Marshal(bpAgent.CustomArgs)
		if err != nil {
			return ApplyBlueprintResponse{}, err
		}
		if bpAgent.CustomArgs == nil {
			customArgsRaw = []byte("[]")
		}

		created, err := qtx.CreateAgent(ctx, db.CreateAgentParams{
			WorkspaceID:        workspaceID,
			Name:               sanitizeNullBytes(bpAgent.Name),
			Description:        sanitizeNullBytes(bpAgent.Description),
			AvatarUrl:          ptrToText(bpAgent.AvatarURL),
			RuntimeMode:        runtime.RuntimeMode,
			RuntimeConfig:      []byte("{}"),
			RuntimeID:          runtime.ID,
			Visibility:         blueprintAgentVisibility(bpAgent.Visibility),
			MaxConcurrentTasks: blueprintAgentMaxConcurrentTasks(bpAgent.MaxConcurrentTasks),
			OwnerID:            member.UserID,
			Instructions:       sanitizeNullBytes(bpAgent.Instructions),
			CustomEnv:          customEnvRaw,
			CustomArgs:         customArgsRaw,
			McpConfig:          nil,
			Model:              pgtype.Text{String: bpAgent.Runtime.Model, Valid: bpAgent.Runtime.Model != ""},
			ThinkingLevel:      pgtype.Text{String: bpAgent.Runtime.ThinkingLevel, Valid: bpAgent.Runtime.ThinkingLevel != ""},
		})
		if err != nil {
			return ApplyBlueprintResponse{}, err
		}
		createdID := uuidToString(created.ID)
		agentIDs[bpAgent.Ref] = createdID
		resp.Agents = append(resp.Agents, applyResultFromPlan(plan.ResourcePlan, createdID))

		for _, skillRef := range bpAgent.SkillRefs {
			skillID := skillIDs[skillRef]
			if skillID == "" {
				return ApplyBlueprintResponse{}, blueprintApplyStatusError{Status: http.StatusBadRequest, Message: fmt.Sprintf("agent %q references missing skill %q", bpAgent.Ref, skillRef)}
			}
			if err := qtx.AddAgentSkill(ctx, db.AddAgentSkillParams{
				AgentID: created.ID,
				SkillID: parseUUID(skillID),
			}); err != nil {
				return ApplyBlueprintResponse{}, err
			}
		}
	}

	for _, squad := range manifest.Squads {
		plan, ok := squadPlans[squad.Ref]
		if !ok {
			return ApplyBlueprintResponse{}, blueprintApplyStatusError{Status: http.StatusBadRequest, Message: fmt.Sprintf("missing preview plan for squad %q", squad.Ref)}
		}
		if plan.Action == blueprint.PreviewActionReuse {
			resp.Squads = append(resp.Squads, applyResultFromPlan(plan, plan.ExistingID))
			continue
		}

		leaderID := agentIDs[squad.LeaderRef]
		if leaderID == "" {
			return ApplyBlueprintResponse{}, blueprintApplyStatusError{Status: http.StatusBadRequest, Message: fmt.Sprintf("squad %q references missing leader %q", squad.Ref, squad.LeaderRef)}
		}
		created, err := qtx.CreateSquad(ctx, db.CreateSquadParams{
			WorkspaceID: workspaceID,
			Name:        sanitizeNullBytes(squad.Name),
			Description: sanitizeNullBytes(squad.Description),
			LeaderID:    parseUUID(leaderID),
			CreatorID:   member.UserID,
			AvatarUrl:   ptrToText(squad.AvatarURL),
		})
		if err != nil {
			return ApplyBlueprintResponse{}, err
		}
		if squad.Instructions != "" {
			created, err = qtx.UpdateSquad(ctx, db.UpdateSquadParams{
				ID:           created.ID,
				Name:         strToText(sanitizeNullBytes(squad.Name)),
				Description:  strToText(sanitizeNullBytes(squad.Description)),
				LeaderID:     parseUUID(leaderID),
				AvatarUrl:    ptrToText(squad.AvatarURL),
				Instructions: strToText(sanitizeNullBytes(squad.Instructions)),
			})
			if err != nil {
				return ApplyBlueprintResponse{}, err
			}
		}

		if err := addBlueprintSquadMembers(ctx, qtx, created.ID, squad, agentIDs); err != nil {
			return ApplyBlueprintResponse{}, err
		}
		resp.Squads = append(resp.Squads, applyResultFromPlan(plan, uuidToString(created.ID)))
	}

	return resp, nil
}

func providedEnvVarsFromApplyValues(values []ApplyBlueprintProvidedEnvVar) []blueprint.ProvidedEnvVar {
	out := make([]blueprint.ProvidedEnvVar, 0, len(values))
	for _, value := range values {
		out = append(out, blueprint.ProvidedEnvVar{
			AgentRef: value.AgentRef,
			Name:     value.Name,
		})
	}
	return out
}

func applyEnvValuesByAgentRef(values []ApplyBlueprintProvidedEnvVar) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, value := range values {
		if out[value.AgentRef] == nil {
			out[value.AgentRef] = map[string]string{}
		}
		out[value.AgentRef][value.Name] = value.Value
	}
	return out
}

func applyCustomEnvForAgent(agent blueprint.Agent, provided map[string]string) map[string]string {
	out := map[string]string{}
	for _, env := range agent.CustomEnvSchema {
		if value, ok := provided[env.Name]; ok {
			out[env.Name] = value
		}
	}
	return out
}

func blueprintResourcePlansByRef(plans []blueprint.ResourcePlan) map[string]blueprint.ResourcePlan {
	out := make(map[string]blueprint.ResourcePlan, len(plans))
	for _, plan := range plans {
		out[plan.Ref] = plan
	}
	return out
}

func blueprintAgentPlansByRef(plans []blueprint.AgentPlan) map[string]blueprint.AgentPlan {
	out := make(map[string]blueprint.AgentPlan, len(plans))
	for _, plan := range plans {
		out[plan.Ref] = plan
	}
	return out
}

func applyResultFromPlan(plan blueprint.ResourcePlan, id string) BlueprintApplyResultItem {
	return BlueprintApplyResultItem{
		Ref:    plan.Ref,
		Name:   plan.Name,
		Action: plan.Action,
		ID:     id,
	}
}

func blueprintAgentVisibility(visibility string) string {
	if visibility == "" {
		return "private"
	}
	return visibility
}

func blueprintAgentMaxConcurrentTasks(maxConcurrentTasks int32) int32 {
	if maxConcurrentTasks == 0 {
		return 6
	}
	return maxConcurrentTasks
}

func addBlueprintSquadMembers(ctx context.Context, qtx *db.Queries, squadID pgtype.UUID, squad blueprint.Squad, agentIDs map[string]string) error {
	seen := map[string]struct{}{}
	addMember := func(ref, role string) error {
		if ref == "" {
			return nil
		}
		if _, ok := seen[ref]; ok {
			return nil
		}
		seen[ref] = struct{}{}
		agentID := agentIDs[ref]
		if agentID == "" {
			return blueprintApplyStatusError{Status: http.StatusBadRequest, Message: fmt.Sprintf("squad %q references missing member %q", squad.Ref, ref)}
		}
		if role == "" && ref == squad.LeaderRef {
			role = "leader"
		}
		_, err := qtx.AddSquadMember(ctx, db.AddSquadMemberParams{
			SquadID:    squadID,
			MemberType: "agent",
			MemberID:   parseUUID(agentID),
			Role:       role,
		})
		return err
	}

	for _, member := range squad.Members {
		if err := addMember(member.Ref, member.Role); err != nil {
			return err
		}
	}
	return addMember(squad.LeaderRef, "leader")
}
