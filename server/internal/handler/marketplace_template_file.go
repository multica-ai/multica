package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	marketplaceTemplateFileFormat    = "multica-template"
	marketplaceTemplateFileVersion   = 1
	marketplaceTemplateV2FileFormat  = "multica.template"
	marketplaceTemplateV2FileVersion = 2
	maxTemplateFileBytes             = 16 << 20
	maxTemplateSnapshotAgents        = 50
	maxTemplateSnapshotSkills        = 100
	maxTemplateSnapshotFiles         = 500
)

type MarketplaceTemplateFile struct {
	Format          string                      `json:"format"`
	Version         int                         `json:"version"`
	ExportedAt      string                      `json:"exported_at"`
	Name            string                      `json:"name"`
	Description     string                      `json:"description"`
	Tags            []string                    `json:"tags"`
	SourceType      string                      `json:"source_type"`
	SnapshotVersion int32                       `json:"snapshot_version"`
	Snapshot        MarketplaceTemplateSnapshot `json:"snapshot"`
}

type MarketplaceTemplateFileV2Metadata struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	UseCases    string   `json:"use_cases"`
	Tags        []string `json:"tags"`
	UsageNotes  string   `json:"usage_notes"`
}

type MarketplaceTemplateFileV2Agent struct {
	Key                  string                     `json:"key"`
	Name                 string                     `json:"name"`
	Description          string                     `json:"description"`
	Instructions         string                     `json:"instructions"`
	ConversationStarters []AgentConversationStarter `json:"conversation_starters,omitempty"`
	MaxConcurrentTasks   int32                      `json:"max_concurrent_tasks"`
	SkillRefs            []string                   `json:"skill_refs"`
}

type MarketplaceTemplateFileV2Skill struct {
	Key         string                                 `json:"key"`
	Name        string                                 `json:"name"`
	Description string                                 `json:"description"`
	Content     string                                 `json:"content"`
	SourceType  string                                 `json:"source_type"`
	Config      json.RawMessage                        `json:"config,omitempty"`
	Files       []MarketplaceTemplateSkillFileSnapshot `json:"files"`
}

type MarketplaceTemplateFileV2Member struct {
	AgentRef string `json:"agent_ref"`
	Role     string `json:"role"`
}

type MarketplaceTemplateFileV2Spec struct {
	Name         string                            `json:"name"`
	Description  string                            `json:"description"`
	Instructions string                            `json:"instructions,omitempty"`
	LeaderRef    string                            `json:"leader_ref"`
	Members      []MarketplaceTemplateFileV2Member `json:"members"`
}

type MarketplaceTemplateFileV2 struct {
	Format        string                            `json:"format"`
	SchemaVersion int                               `json:"schema_version"`
	Type          string                            `json:"type"`
	Metadata      MarketplaceTemplateFileV2Metadata `json:"metadata"`
	Resources     struct {
		Agents []MarketplaceTemplateFileV2Agent `json:"agents"`
		Skills []MarketplaceTemplateFileV2Skill `json:"skills"`
	} `json:"resources"`
	Spec *MarketplaceTemplateFileV2Spec `json:"spec,omitempty"`
}

type applyMarketplaceTemplateRequest struct {
	Name       string            `json:"name"`
	RuntimeIDs map[string]string `json:"runtime_ids"`
}

type marketplaceTemplateApplyResult struct {
	AgentIDs      map[string]string
	SquadID       *string
	ReusedSkillID []string
}

func (result marketplaceTemplateApplyResult) response(templateID pgtype.UUID) map[string]any {
	return map[string]any{
		"template_id":      uuidToString(templateID),
		"agent_ids":        result.AgentIDs,
		"squad_id":         result.SquadID,
		"reused_skill_ids": result.ReusedSkillID,
	}
}

func normalizeMarketplaceTemplateFileV2(manifest MarketplaceTemplateFileV2) (MarketplaceTemplateFile, error) {
	if manifest.Format != marketplaceTemplateV2FileFormat || manifest.SchemaVersion != marketplaceTemplateV2FileVersion {
		return MarketplaceTemplateFile{}, fmt.Errorf("unsupported template file version")
	}
	if manifest.Type != "agent" && manifest.Type != "squad" {
		return MarketplaceTemplateFile{}, fmt.Errorf("template file type must be agent or squad")
	}
	snapshot := MarketplaceTemplateSnapshot{
		Version: marketplaceTemplateSnapshotVersion, SourceType: manifest.Type,
		Agents: make([]MarketplaceTemplateAgentSnapshot, 0, len(manifest.Resources.Agents)),
		Skills: make([]MarketplaceTemplateSkillSnapshot, 0, len(manifest.Resources.Skills)),
	}
	for _, agent := range manifest.Resources.Agents {
		conversationStarters := agent.ConversationStarters
		if conversationStarters == nil {
			conversationStarters = []AgentConversationStarter{}
		}
		snapshot.Agents = append(snapshot.Agents, MarketplaceTemplateAgentSnapshot{
			Key: agent.Key, Name: agent.Name, Description: agent.Description,
			Instructions: agent.Instructions, ConversationStarters: conversationStarters,
			MaxConcurrentTasks: agent.MaxConcurrentTasks, SkillKeys: agent.SkillRefs,
		})
	}
	for _, skill := range manifest.Resources.Skills {
		config := skill.Config
		if len(config) == 0 {
			config = json.RawMessage(`{}`)
		}
		snapshot.Skills = append(snapshot.Skills, MarketplaceTemplateSkillSnapshot{
			Key: skill.Key, Name: skill.Name, Description: skill.Description,
			Content: skill.Content, Config: config, Files: skill.Files,
		})
	}
	if manifest.Type == "squad" {
		if manifest.Spec == nil {
			return MarketplaceTemplateFile{}, fmt.Errorf("squad template file is missing spec")
		}
		members := make([]MarketplaceTemplateSquadMemberSnapshot, 0, len(manifest.Spec.Members))
		for _, member := range manifest.Spec.Members {
			members = append(members, MarketplaceTemplateSquadMemberSnapshot{AgentKey: member.AgentRef, Role: member.Role})
		}
		snapshot.Squad = &MarketplaceTemplateSquadSnapshot{
			Name: manifest.Spec.Name, Description: manifest.Spec.Description,
			Instructions: manifest.Spec.Instructions, LeaderKey: manifest.Spec.LeaderRef,
			Members: members,
		}
	}
	normalized := MarketplaceTemplateFile{
		Format: marketplaceTemplateFileFormat, Version: marketplaceTemplateFileVersion,
		Name: manifest.Metadata.Name, Description: manifest.Metadata.Description,
		Tags: manifest.Metadata.Tags, SourceType: manifest.Type,
		SnapshotVersion: marketplaceTemplateSnapshotVersion, Snapshot: snapshot,
	}
	if err := validateMarketplaceTemplateFile(normalized); err != nil {
		return MarketplaceTemplateFile{}, err
	}
	return normalized, nil
}

func parseMarketplaceTemplateFile(raw json.RawMessage) (MarketplaceTemplateFile, error) {
	var envelope struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return MarketplaceTemplateFile{}, fmt.Errorf("invalid template file")
	}
	switch envelope.Format {
	case marketplaceTemplateV2FileFormat:
		var manifest MarketplaceTemplateFileV2
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return MarketplaceTemplateFile{}, fmt.Errorf("invalid v2 template file")
		}
		return normalizeMarketplaceTemplateFileV2(manifest)
	case marketplaceTemplateFileFormat:
		var manifest MarketplaceTemplateFile
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return MarketplaceTemplateFile{}, fmt.Errorf("invalid v1 template file")
		}
		if err := validateMarketplaceTemplateFile(manifest); err != nil {
			return MarketplaceTemplateFile{}, err
		}
		return manifest, nil
	default:
		return MarketplaceTemplateFile{}, fmt.Errorf("unsupported template file format")
	}
}

func marketplaceTemplateFileV2FromSnapshot(
	name, description string,
	tags []string,
	snapshot MarketplaceTemplateSnapshot,
) MarketplaceTemplateFileV2 {
	manifest := MarketplaceTemplateFileV2{
		Format: marketplaceTemplateV2FileFormat, SchemaVersion: marketplaceTemplateV2FileVersion,
		Type: snapshot.SourceType,
		Metadata: MarketplaceTemplateFileV2Metadata{
			Name: name, Description: description, Tags: tags,
		},
	}
	manifest.Resources.Agents = make([]MarketplaceTemplateFileV2Agent, 0, len(snapshot.Agents))
	for _, agent := range snapshot.Agents {
		manifest.Resources.Agents = append(manifest.Resources.Agents, MarketplaceTemplateFileV2Agent{
			Key: agent.Key, Name: agent.Name, Description: agent.Description,
			Instructions: agent.Instructions, ConversationStarters: agent.ConversationStarters,
			MaxConcurrentTasks: agent.MaxConcurrentTasks, SkillRefs: agent.SkillKeys,
		})
	}
	manifest.Resources.Skills = make([]MarketplaceTemplateFileV2Skill, 0, len(snapshot.Skills))
	for _, skill := range snapshot.Skills {
		manifest.Resources.Skills = append(manifest.Resources.Skills, MarketplaceTemplateFileV2Skill{
			Key: skill.Key, Name: skill.Name, Description: skill.Description,
			Content: skill.Content, SourceType: "file", Config: skill.Config, Files: skill.Files,
		})
	}
	if snapshot.SourceType == "squad" && snapshot.Squad != nil {
		members := make([]MarketplaceTemplateFileV2Member, 0, len(snapshot.Squad.Members))
		for _, member := range snapshot.Squad.Members {
			members = append(members, MarketplaceTemplateFileV2Member{AgentRef: member.AgentKey, Role: member.Role})
		}
		manifest.Spec = &MarketplaceTemplateFileV2Spec{
			Name: snapshot.Squad.Name, Description: snapshot.Squad.Description,
			Instructions: snapshot.Squad.Instructions, LeaderRef: snapshot.Squad.LeaderKey,
			Members: members,
		}
	}
	return manifest
}

func validateMarketplaceTemplateFile(manifest MarketplaceTemplateFile) error {
	if manifest.Format != marketplaceTemplateFileFormat {
		return fmt.Errorf("file format must be %q", marketplaceTemplateFileFormat)
	}
	if manifest.Version != marketplaceTemplateFileVersion {
		return fmt.Errorf("unsupported template file version")
	}
	if manifest.SourceType != manifest.Snapshot.SourceType {
		return fmt.Errorf("template file source type does not match its snapshot")
	}
	if err := validateMarketplaceSnapshot(manifest.Snapshot); err != nil {
		return err
	}
	return validateMarketplaceSnapshotFiles(manifest.Snapshot)
}

func validateMarketplaceSnapshotFiles(snapshot MarketplaceTemplateSnapshot) error {
	if len(snapshot.Agents) > maxTemplateSnapshotAgents {
		return fmt.Errorf("template contains too many agents")
	}
	if len(snapshot.Skills) > maxTemplateSnapshotSkills {
		return fmt.Errorf("template contains too many skills")
	}
	skillKeys := make(map[string]struct{}, len(snapshot.Skills))
	fileCount := 0
	for _, skill := range snapshot.Skills {
		if skill.Key == "" || skill.Name == "" {
			return fmt.Errorf("template contains an invalid skill")
		}
		if _, exists := skillKeys[skill.Key]; exists {
			return fmt.Errorf("template contains duplicate skill keys")
		}
		skillKeys[skill.Key] = struct{}{}
		for _, file := range skill.Files {
			fileCount++
			if fileCount > maxTemplateSnapshotFiles {
				return fmt.Errorf("template contains too many skill files")
			}
			clean := path.Clean(strings.ReplaceAll(file.Path, "\\", "/"))
			if file.Path == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.ContainsRune(file.Path, 0) {
				return fmt.Errorf("template contains an unsafe skill file path")
			}
		}
	}
	for _, agent := range snapshot.Agents {
		for _, skillKey := range agent.SkillKeys {
			if _, exists := skillKeys[skillKey]; !exists {
				return fmt.Errorf("template agent references an unknown skill")
			}
		}
	}
	return nil
}

func (h *Handler) ExportSquadTemplateFile(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageSquad(member, squad) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	snapshot, ok := h.marketplaceTemplateSnapshotForSquad(r, w, member, squad)
	if !ok {
		return
	}
	manifest := marketplaceTemplateFileV2FromSnapshot(squad.Name, squad.Description, []string{}, snapshot)
	filename := "squad.multica-template.json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(manifest)
}

func (h *Handler) ApplyMarketplaceTemplateFile(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTemplateFileBytes)
	var req struct {
		Manifest   json.RawMessage   `json:"manifest"`
		Name       string            `json:"name"`
		RuntimeIDs map[string]string `json:"runtime_ids"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid template file request")
		return
	}
	manifest, err := parseMarketplaceTemplateFile(req.Manifest)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, ok := h.applyMarketplaceTemplateSnapshot(w, r, workspaceID, member, pgtype.UUID{}, false, manifest.Snapshot, applyMarketplaceTemplateRequest{
		Name: req.Name, RuntimeIDs: req.RuntimeIDs,
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, result.response(pgtype.UUID{}))
}

func (h *Handler) applyMarketplaceTemplateSnapshot(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID string,
	member db.Member,
	templateID pgtype.UUID,
	countUsage bool,
	snapshot MarketplaceTemplateSnapshot,
	req applyMarketplaceTemplateRequest,
) (marketplaceTemplateApplyResult, bool) {
	if err := validateMarketplaceSnapshot(snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return marketplaceTemplateApplyResult{}, false
	}
	if err := validateMarketplaceSnapshotFiles(snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return marketplaceTemplateApplyResult{}, false
	}
	runtimes := make(map[string]db.AgentRuntime, len(snapshot.Agents))
	for _, agentSnapshot := range snapshot.Agents {
		runtimeID, exists := req.RuntimeIDs[agentSnapshot.Key]
		if !exists || strings.TrimSpace(runtimeID) == "" {
			writeError(w, http.StatusBadRequest, "runtime_ids must assign every template agent")
			return marketplaceTemplateApplyResult{}, false
		}
		runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
		if !ok {
			return marketplaceTemplateApplyResult{}, false
		}
		runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{ID: runtimeUUID, WorkspaceID: member.WorkspaceID})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid runtime_id")
			return marketplaceTemplateApplyResult{}, false
		}
		if !canUseRuntimeForAgent(member, runtime) {
			writeError(w, http.StatusForbidden, "this runtime is private; only its owner can create agents on it")
			return marketplaceTemplateApplyResult{}, false
		}
		runtimes[agentSnapshot.Key] = runtime
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start template import transaction")
		return marketplaceTemplateApplyResult{}, false
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	skillIDs := map[string]pgtype.UUID{}
	reusedSkillIDs := []string{}
	for _, skillSnapshot := range snapshot.Skills {
		skill, err := qtx.GetSkillByWorkspaceAndName(r.Context(), db.GetSkillByWorkspaceAndNameParams{WorkspaceID: member.WorkspaceID, Name: skillSnapshot.Name})
		if err == nil {
			skillIDs[skillSnapshot.Key] = skill.ID
			reusedSkillIDs = append(reusedSkillIDs, uuidToString(skill.ID))
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to check imported skills")
			return marketplaceTemplateApplyResult{}, false
		}
		config := []byte(skillSnapshot.Config)
		if len(config) == 0 {
			config = []byte(`{}`)
		}
		skill, err = qtx.CreateSkill(r.Context(), db.CreateSkillParams{
			WorkspaceID: member.WorkspaceID, Name: skillSnapshot.Name,
			Description: skillSnapshot.Description, Content: skillSnapshot.Content,
			Config: config, CreatedBy: member.UserID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create imported skill")
			return marketplaceTemplateApplyResult{}, false
		}
		for _, file := range skillSnapshot.Files {
			if _, err := qtx.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{SkillID: skill.ID, Path: file.Path, Content: file.Content}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create imported skill file")
				return marketplaceTemplateApplyResult{}, false
			}
		}
		skillIDs[skillSnapshot.Key] = skill.ID
	}

	existingAgents, err := qtx.ListAllAgents(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workspace agents")
		return marketplaceTemplateApplyResult{}, false
	}
	usedNames := make(map[string]struct{}, len(existingAgents)+len(snapshot.Agents))
	for _, agent := range existingAgents {
		usedNames[strings.ToLower(agent.Name)] = struct{}{}
	}
	createdByKey := map[string]db.Agent{}
	createdIDs := map[string]string{}
	for _, agentSnapshot := range snapshot.Agents {
		runtime := runtimes[agentSnapshot.Key]
		starters, _ := json.Marshal(agentSnapshot.ConversationStarters)
		maxConcurrent := agentSnapshot.MaxConcurrentTasks
		if maxConcurrent < 1 {
			maxConcurrent = 1
		}
		created, err := qtx.CreateAgent(r.Context(), db.CreateAgentParams{
			WorkspaceID: member.WorkspaceID, Name: nextMarketplaceAgentName(agentSnapshot.Name, usedNames),
			Description: agentSnapshot.Description, Instructions: agentSnapshot.Instructions,
			RuntimeMode: runtime.RuntimeMode, RuntimeConfig: []byte(`{}`), RuntimeID: runtime.ID,
			Visibility: "private", PermissionMode: "private", MaxConcurrentTasks: maxConcurrent,
			OwnerID: member.UserID, CustomEnv: []byte(`{}`), CustomArgs: []byte(`[]`),
			ConversationStarters: starters,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create imported agent")
			return marketplaceTemplateApplyResult{}, false
		}
		for _, skillKey := range agentSnapshot.SkillKeys {
			skillID, exists := skillIDs[skillKey]
			if !exists {
				writeError(w, http.StatusBadRequest, "template agent references an unknown skill")
				return marketplaceTemplateApplyResult{}, false
			}
			if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{AgentID: created.ID, SkillID: skillID}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to attach imported skill")
				return marketplaceTemplateApplyResult{}, false
			}
		}
		createdByKey[agentSnapshot.Key] = created
		createdIDs[agentSnapshot.Key] = uuidToString(created.ID)
	}

	var createdSquad *db.Squad
	if snapshot.SourceType == "squad" && snapshot.Squad != nil {
		leader, exists := createdByKey[snapshot.Squad.LeaderKey]
		if !exists {
			writeError(w, http.StatusBadRequest, "template squad leader is missing")
			return marketplaceTemplateApplyResult{}, false
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = snapshot.Squad.Name
		}
		squad, err := qtx.CreateSquad(r.Context(), db.CreateSquadParams{
			WorkspaceID: member.WorkspaceID, Name: name, Description: snapshot.Squad.Description,
			LeaderID: leader.ID, CreatorID: member.UserID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create imported squad")
			return marketplaceTemplateApplyResult{}, false
		}
		if snapshot.Squad.Instructions != "" {
			updated, err := qtx.UpdateSquad(r.Context(), db.UpdateSquadParams{
				ID: squad.ID, Instructions: pgtype.Text{String: snapshot.Squad.Instructions, Valid: true},
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to set imported squad instructions")
				return marketplaceTemplateApplyResult{}, false
			}
			squad = updated
		}
		for _, squadMember := range snapshot.Squad.Members {
			agent, exists := createdByKey[squadMember.AgentKey]
			if !exists {
				writeError(w, http.StatusBadRequest, "template squad member is missing")
				return marketplaceTemplateApplyResult{}, false
			}
			role := squadMember.Role
			if squadMember.AgentKey == snapshot.Squad.LeaderKey && role == "" {
				role = "leader"
			}
			if _, err := qtx.AddSquadMember(r.Context(), db.AddSquadMemberParams{
				SquadID: squad.ID, MemberType: "agent", MemberID: agent.ID, Role: role,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to add imported squad member")
				return marketplaceTemplateApplyResult{}, false
			}
		}
		createdSquad = &squad
	}
	if countUsage {
		if _, err := qtx.IncrementMarketplaceTemplateAppliedCount(r.Context(), templateID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record template usage")
			return marketplaceTemplateApplyResult{}, false
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit template import")
		return marketplaceTemplateApplyResult{}, false
	}

	actorID := uuidToString(member.UserID)
	for key, created := range createdByKey {
		if runtimes[key].Status == "online" {
			h.TaskService.ReconcileAgentStatus(r.Context(), created.ID)
			created, _ = h.Queries.GetAgent(r.Context(), created.ID)
		}
		resp := h.agentToResponse(created)
		_ = h.attachAgentSkills(r.Context(), &resp, created.ID)
		_ = h.enrichAgentResponseWithTargets(r.Context(), &resp, created.ID)
		h.publish(protocol.EventAgentCreated, workspaceID, "member", actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	}
	squadID := (*string)(nil)
	if createdSquad != nil {
		idString := uuidToString(createdSquad.ID)
		squadID = &idString
		if resp, err := h.squadToResponseWithPreview(r.Context(), *createdSquad); err == nil {
			h.publish(protocol.EventSquadCreated, workspaceID, "member", actorID, map[string]any{"squad": resp})
		}
	}
	return marketplaceTemplateApplyResult{
		AgentIDs: createdIDs, SquadID: squadID, ReusedSkillID: reusedSkillIDs,
	}, true
}
