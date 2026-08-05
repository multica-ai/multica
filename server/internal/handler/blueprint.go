package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/blueprint"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ExportBlueprintRequest struct {
	Name     string   `json:"name"`
	SquadIDs []string `json:"squad_ids"`
	AgentIDs []string `json:"agent_ids"`
	SkillIDs []string `json:"skill_ids"`
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
