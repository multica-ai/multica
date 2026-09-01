package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	marketplaceTemplateSnapshotVersion = 1
	maxMarketplaceTemplateNameLength   = 120
	minMarketplaceTemplateDescription  = 50
	maxMarketplaceTemplateDescription  = 4000
	maxMarketplaceTemplateTags         = 8
	maxMarketplaceTemplateTagLength    = 40
)

type MarketplaceTemplateSkillFileSnapshot struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type MarketplaceTemplateSkillSnapshot struct {
	Key         string                                 `json:"key"`
	Name        string                                 `json:"name"`
	Description string                                 `json:"description"`
	Content     string                                 `json:"content"`
	Config      json.RawMessage                        `json:"config"`
	Files       []MarketplaceTemplateSkillFileSnapshot `json:"files"`
}

type MarketplaceTemplateAgentSnapshot struct {
	Key                  string                     `json:"key"`
	Name                 string                     `json:"name"`
	Description          string                     `json:"description"`
	Instructions         string                     `json:"instructions"`
	ConversationStarters []AgentConversationStarter `json:"conversation_starters"`
	MaxConcurrentTasks   int32                      `json:"max_concurrent_tasks"`
	SkillKeys            []string                   `json:"skill_keys"`
}

type MarketplaceTemplateSquadMemberSnapshot struct {
	AgentKey string `json:"agent_key"`
	Role     string `json:"role"`
}

type MarketplaceTemplateSquadSnapshot struct {
	Name         string                                   `json:"name"`
	Description  string                                   `json:"description"`
	Instructions string                                   `json:"instructions"`
	LeaderKey    string                                   `json:"leader_key"`
	Members      []MarketplaceTemplateSquadMemberSnapshot `json:"members"`
}

type MarketplaceTemplateSnapshot struct {
	Version    int                                `json:"version"`
	SourceType string                             `json:"source_type"`
	Agents     []MarketplaceTemplateAgentSnapshot `json:"agents"`
	Skills     []MarketplaceTemplateSkillSnapshot `json:"skills"`
	Squad      *MarketplaceTemplateSquadSnapshot  `json:"squad,omitempty"`
}

type MarketplaceTemplateAgentPreviewResponse struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Role        string `json:"role"`
	IsLeader    bool   `json:"is_leader"`
}

type MarketplaceTemplateResponse struct {
	ID                string                                    `json:"id"`
	SourceWorkspaceID string                                    `json:"source_workspace_id"`
	CreatedBy         string                                    `json:"created_by"`
	CreatorName       string                                    `json:"creator_name"`
	SourceType        string                                    `json:"source_type"`
	SourceID          *string                                   `json:"source_id"`
	Name              string                                    `json:"name"`
	Description       string                                    `json:"description"`
	Tags              []string                                  `json:"tags"`
	Visibility        string                                    `json:"visibility"`
	ImageURL          *string                                   `json:"image_url"`
	SnapshotVersion   int32                                     `json:"snapshot_version"`
	AppliedCount      int64                                     `json:"applied_count"`
	FeaturedAt        *string                                   `json:"featured_at"`
	CreatedAt         string                                    `json:"created_at"`
	UpdatedAt         string                                    `json:"updated_at"`
	AgentCount        int                                       `json:"agent_count"`
	SkillCount        int                                       `json:"skill_count"`
	PreviewAgents     []MarketplaceTemplateAgentPreviewResponse `json:"preview_agents"`
	Snapshot          *MarketplaceTemplateSnapshot              `json:"snapshot,omitempty"`
	CanManage         bool                                      `json:"can_manage"`
}

type marketplaceTemplateRecord struct {
	ID                pgtype.UUID
	SourceWorkspaceID pgtype.UUID
	CreatedBy         pgtype.UUID
	SourceType        string
	SourceID          pgtype.UUID
	Name              string
	Description       string
	Tags              []string
	Visibility        string
	ImageURL          pgtype.Text
	SnapshotVersion   int32
	Snapshot          []byte
	AppliedCount      int64
	FeaturedAt        pgtype.Timestamptz
	CreatedAt         pgtype.Timestamptz
	UpdatedAt         pgtype.Timestamptz
}

func marketplaceTemplateRecordFromDB(row db.MarketplaceTemplate) marketplaceTemplateRecord {
	return marketplaceTemplateRecord{
		ID: row.ID, SourceWorkspaceID: row.SourceWorkspaceID, CreatedBy: row.CreatedBy,
		SourceType: row.SourceType, SourceID: row.SourceID, Name: row.Name,
		Description: row.Description, Tags: row.Tags, Visibility: row.Visibility,
		ImageURL: row.ImageUrl, SnapshotVersion: row.SnapshotVersion, Snapshot: row.Snapshot,
		AppliedCount: row.AppliedCount, FeaturedAt: row.FeaturedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func marketplaceTemplateResponse(
	record marketplaceTemplateRecord,
	creatorName string,
	includeSnapshot bool,
	canManage bool,
) (MarketplaceTemplateResponse, error) {
	var snapshot MarketplaceTemplateSnapshot
	if err := json.Unmarshal(record.Snapshot, &snapshot); err != nil {
		return MarketplaceTemplateResponse{}, err
	}
	roles := map[string]string{}
	leaderKey := ""
	if snapshot.Squad != nil {
		leaderKey = snapshot.Squad.LeaderKey
		for _, member := range snapshot.Squad.Members {
			roles[member.AgentKey] = member.Role
		}
	}
	preview := make([]MarketplaceTemplateAgentPreviewResponse, 0, min(4, len(snapshot.Agents)))
	for _, agent := range snapshot.Agents {
		if len(preview) == 4 {
			break
		}
		preview = append(preview, MarketplaceTemplateAgentPreviewResponse{
			Key: agent.Key, Name: agent.Name, Description: agent.Description,
			Role: roles[agent.Key], IsLeader: agent.Key == leaderKey,
		})
	}
	resp := MarketplaceTemplateResponse{
		ID: uuidToString(record.ID), SourceWorkspaceID: uuidToString(record.SourceWorkspaceID),
		CreatedBy: uuidToString(record.CreatedBy), CreatorName: creatorName,
		SourceType: record.SourceType, SourceID: uuidToPtr(record.SourceID),
		Name: record.Name, Description: record.Description, Tags: record.Tags,
		Visibility: record.Visibility, ImageURL: textToPtr(record.ImageURL),
		SnapshotVersion: record.SnapshotVersion, AppliedCount: record.AppliedCount,
		FeaturedAt: timestampToPtr(record.FeaturedAt), CreatedAt: timestampToString(record.CreatedAt),
		UpdatedAt: timestampToString(record.UpdatedAt), AgentCount: len(snapshot.Agents),
		SkillCount: len(snapshot.Skills), PreviewAgents: preview, CanManage: canManage,
	}
	if resp.Tags == nil {
		resp.Tags = []string{}
	}
	if includeSnapshot {
		resp.Snapshot = &snapshot
	}
	return resp, nil
}

func marketplaceTemplateListResponse(
	row db.ListVisibleMarketplaceTemplatesRow,
	canManage bool,
) (MarketplaceTemplateResponse, error) {
	var previewJSON []byte
	switch value := row.PreviewAgents.(type) {
	case []byte:
		previewJSON = value
	case string:
		previewJSON = []byte(value)
	default:
		var err error
		previewJSON, err = json.Marshal(value)
		if err != nil {
			return MarketplaceTemplateResponse{}, err
		}
	}
	preview := []MarketplaceTemplateAgentPreviewResponse{}
	if len(previewJSON) > 0 {
		if err := json.Unmarshal(previewJSON, &preview); err != nil {
			return MarketplaceTemplateResponse{}, err
		}
	}
	tags := row.Tags
	if tags == nil {
		tags = []string{}
	}
	return MarketplaceTemplateResponse{
		ID: uuidToString(row.ID), SourceWorkspaceID: uuidToString(row.SourceWorkspaceID),
		CreatedBy: uuidToString(row.CreatedBy), CreatorName: row.CreatorName,
		SourceType: row.SourceType, SourceID: uuidToPtr(row.SourceID), Name: row.Name,
		Description: row.Description, Tags: tags, Visibility: row.Visibility,
		ImageURL: textToPtr(row.ImageUrl), SnapshotVersion: row.SnapshotVersion,
		AppliedCount: row.AppliedCount, FeaturedAt: timestampToPtr(row.FeaturedAt),
		CreatedAt: timestampToString(row.CreatedAt), UpdatedAt: timestampToString(row.UpdatedAt),
		AgentCount: int(row.AgentCount), SkillCount: int(row.SkillCount),
		PreviewAgents: preview, CanManage: canManage,
	}, nil
}

func normaliseMarketplaceTemplateTags(tags []string) ([]string, error) {
	result := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > maxMarketplaceTemplateTagLength {
			return nil, fmt.Errorf("tags must be %d characters or fewer", maxMarketplaceTemplateTagLength)
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, tag)
		if len(result) > maxMarketplaceTemplateTags {
			return nil, fmt.Errorf("templates may have at most %d tags", maxMarketplaceTemplateTags)
		}
	}
	return result, nil
}

func canReadMarketplaceTemplate(record marketplaceTemplateRecord, workspaceID, userID pgtype.UUID) bool {
	if record.Visibility == "public" {
		return true
	}
	if uuidToString(record.SourceWorkspaceID) != uuidToString(workspaceID) {
		return false
	}
	return record.Visibility == "workspace" || uuidToString(record.CreatedBy) == uuidToString(userID)
}

func canManageMarketplaceTemplate(record marketplaceTemplateRecord, member db.Member) bool {
	if uuidToString(record.SourceWorkspaceID) != uuidToString(member.WorkspaceID) {
		return false
	}
	return roleAllowed(member.Role, "owner", "admin") || uuidToString(record.CreatedBy) == uuidToString(member.UserID)
}

func canPublishAgent(agent db.Agent, member db.Member) bool {
	return roleAllowed(member.Role, "owner", "admin") || uuidToString(agent.OwnerID) == uuidToString(member.UserID)
}

func (h *Handler) marketplaceTemplateSnapshotForAgent(
	r *http.Request,
	w http.ResponseWriter,
	member db.Member,
	agent db.Agent,
) (MarketplaceTemplateSnapshot, bool) {
	if !canPublishAgent(agent, member) {
		writeError(w, http.StatusForbidden, "only the agent owner or a workspace admin can publish this agent")
		return MarketplaceTemplateSnapshot{}, false
	}
	agents, skills, ok := h.marketplaceTemplateSnapshotsForAgents(r, w, member, []db.Agent{agent})
	if !ok {
		return MarketplaceTemplateSnapshot{}, false
	}
	return MarketplaceTemplateSnapshot{
		Version: marketplaceTemplateSnapshotVersion, SourceType: "agent",
		Agents: agents, Skills: skills,
	}, true
}

func (h *Handler) marketplaceTemplateSnapshotForSquad(
	r *http.Request,
	w http.ResponseWriter,
	member db.Member,
	squad db.Squad,
) (MarketplaceTemplateSnapshot, bool) {
	if !canManageSquad(member, squad) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return MarketplaceTemplateSnapshot{}, false
	}
	members, err := h.Queries.ListSquadMembers(r.Context(), squad.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load squad members")
		return MarketplaceTemplateSnapshot{}, false
	}
	orderedIDs := []pgtype.UUID{squad.LeaderID}
	roles := map[string]string{uuidToString(squad.LeaderID): "leader"}
	seen := map[string]struct{}{uuidToString(squad.LeaderID): {}}
	for _, squadMember := range members {
		if squadMember.MemberType != "agent" {
			continue
		}
		id := uuidToString(squadMember.MemberID)
		roles[id] = squadMember.Role
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		orderedIDs = append(orderedIDs, squadMember.MemberID)
	}
	agentRows := make([]db.Agent, 0, len(orderedIDs))
	for _, agentID := range orderedIDs {
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID: agentID, WorkspaceID: squad.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "squad contains an unavailable agent")
			return MarketplaceTemplateSnapshot{}, false
		}
		if !canPublishAgent(agent, member) {
			writeError(w, http.StatusForbidden, "every agent in a published squad must be manageable by the publisher")
			return MarketplaceTemplateSnapshot{}, false
		}
		agentRows = append(agentRows, agent)
	}
	agents, skills, ok := h.marketplaceTemplateSnapshotsForAgents(r, w, member, agentRows)
	if !ok {
		return MarketplaceTemplateSnapshot{}, false
	}
	keyByAgentID := map[string]string{}
	for i, agent := range agentRows {
		keyByAgentID[uuidToString(agent.ID)] = agents[i].Key
	}
	squadMembers := make([]MarketplaceTemplateSquadMemberSnapshot, 0, len(agentRows))
	for _, agent := range agentRows {
		id := uuidToString(agent.ID)
		squadMembers = append(squadMembers, MarketplaceTemplateSquadMemberSnapshot{
			AgentKey: keyByAgentID[id], Role: roles[id],
		})
	}
	return MarketplaceTemplateSnapshot{
		Version: marketplaceTemplateSnapshotVersion, SourceType: "squad",
		Agents: agents, Skills: skills,
		Squad: &MarketplaceTemplateSquadSnapshot{
			Name: squad.Name, Description: squad.Description, Instructions: squad.Instructions,
			LeaderKey: keyByAgentID[uuidToString(squad.LeaderID)], Members: squadMembers,
		},
	}, true
}

func (h *Handler) marketplaceTemplateSnapshotsForAgents(
	r *http.Request,
	w http.ResponseWriter,
	member db.Member,
	agentRows []db.Agent,
) ([]MarketplaceTemplateAgentSnapshot, []MarketplaceTemplateSkillSnapshot, bool) {
	agentSnapshots := make([]MarketplaceTemplateAgentSnapshot, 0, len(agentRows))
	skillSnapshots := []MarketplaceTemplateSkillSnapshot{}
	skillKeyByID := map[string]string{}
	for agentIndex, agent := range agentRows {
		if !canPublishAgent(agent, member) {
			writeError(w, http.StatusForbidden, "only manageable agents can be published")
			return nil, nil, false
		}
		skills, err := h.Queries.ListAgentSkills(r.Context(), agent.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load agent skills")
			return nil, nil, false
		}
		skillKeys := make([]string, 0, len(skills))
		for _, skill := range skills {
			skillID := uuidToString(skill.ID)
			key, exists := skillKeyByID[skillID]
			if !exists {
				key = fmt.Sprintf("skill_%d", len(skillSnapshots)+1)
				skillKeyByID[skillID] = key
				files, err := h.Queries.ListSkillFiles(r.Context(), skill.ID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "failed to load skill files")
					return nil, nil, false
				}
				fileSnapshots := make([]MarketplaceTemplateSkillFileSnapshot, 0, len(files))
				for _, file := range files {
					fileSnapshots = append(fileSnapshots, MarketplaceTemplateSkillFileSnapshot{Path: file.Path, Content: file.Content})
				}
				config := json.RawMessage(skill.Config)
				if len(config) == 0 {
					config = json.RawMessage(`{}`)
				}
				skillSnapshots = append(skillSnapshots, MarketplaceTemplateSkillSnapshot{
					Key: key, Name: skill.Name, Description: skill.Description,
					Content: skill.Content, Config: config, Files: fileSnapshots,
				})
			}
			skillKeys = append(skillKeys, key)
		}
		starters := []AgentConversationStarter{}
		if len(agent.ConversationStarters) > 0 {
			_ = json.Unmarshal(agent.ConversationStarters, &starters)
		}
		agentSnapshots = append(agentSnapshots, MarketplaceTemplateAgentSnapshot{
			Key: fmt.Sprintf("agent_%d", agentIndex+1), Name: agent.Name,
			Description: agent.Description, Instructions: agent.Instructions,
			ConversationStarters: starters, MaxConcurrentTasks: agent.MaxConcurrentTasks,
			SkillKeys: skillKeys,
		})
	}
	return agentSnapshots, skillSnapshots, true
}

func (h *Handler) ListMarketplaceTemplates(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	page := positiveIntQuery(r, "page", 1, 1, 100000)
	pageSize := positiveIntQuery(r, "page_size", 12, 1, 100)
	sourceType := strings.TrimSpace(r.URL.Query().Get("type"))
	if sourceType != "" && sourceType != "agent" && sourceType != "squad" {
		writeError(w, http.StatusBadRequest, "type must be agent or squad")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope != "" && scope != "public" && scope != "workspace" && scope != "private" {
		writeError(w, http.StatusBadRequest, "scope must be public, workspace, or private")
		return
	}
	sortOrder := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sortOrder != "" && sortOrder != "popular" && sortOrder != "recent" {
		writeError(w, http.StatusBadRequest, "sort must be popular or recent")
		return
	}
	params := db.ListVisibleMarketplaceTemplatesParams{
		WorkspaceID: member.WorkspaceID, UserID: member.UserID, SourceType: sourceType,
		Scope: scope, Query: strings.TrimSpace(r.URL.Query().Get("q")), Sort: sortOrder,
		PageSize: int32(pageSize), PageOffset: int32((page - 1) * pageSize),
	}
	rows, err := h.Queries.ListVisibleMarketplaceTemplates(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list templates")
		return
	}
	total, err := h.Queries.CountVisibleMarketplaceTemplates(r.Context(), db.CountVisibleMarketplaceTemplatesParams{
		WorkspaceID: params.WorkspaceID, UserID: params.UserID, SourceType: params.SourceType,
		Scope: params.Scope, Query: params.Query,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count templates")
		return
	}
	responses := make([]MarketplaceTemplateResponse, 0, len(rows))
	for _, row := range rows {
		record := marketplaceTemplateRecord{
			ID: row.ID, SourceWorkspaceID: row.SourceWorkspaceID, CreatedBy: row.CreatedBy,
			SourceType: row.SourceType, SourceID: row.SourceID, Name: row.Name,
			Description: row.Description, Tags: row.Tags, Visibility: row.Visibility,
			ImageURL: row.ImageUrl, SnapshotVersion: row.SnapshotVersion,
			AppliedCount: row.AppliedCount, FeaturedAt: row.FeaturedAt,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		resp, err := marketplaceTemplateListResponse(row, canManageMarketplaceTemplate(record, member))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode template")
			return
		}
		responses = append(responses, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"templates": responses, "total": total, "page": page, "page_size": pageSize,
	})
}

func positiveIntQuery(r *http.Request, key string, fallback, minValue, maxValue int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue {
		return fallback
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func (h *Handler) CreateMarketplaceTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	var req struct {
		SourceType  string   `json:"source_type"`
		SourceID    string   `json:"source_id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
		Visibility  string   `json:"visibility"`
		ImageURL    *string  `json:"image_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.SourceType != "agent" && req.SourceType != "squad" {
		writeError(w, http.StatusBadRequest, "source_type must be agent or squad")
		return
	}
	if req.Name == "" || utf8.RuneCountInString(req.Name) > maxMarketplaceTemplateNameLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("name is required and must be %d characters or fewer", maxMarketplaceTemplateNameLength))
		return
	}
	descriptionLength := utf8.RuneCountInString(req.Description)
	if descriptionLength < minMarketplaceTemplateDescription || descriptionLength > maxMarketplaceTemplateDescription {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("description must be between %d and %d characters", minMarketplaceTemplateDescription, maxMarketplaceTemplateDescription))
		return
	}
	if req.Visibility != "private" && req.Visibility != "workspace" && req.Visibility != "public" {
		writeError(w, http.StatusBadRequest, "visibility must be private, workspace, or public")
		return
	}
	tags, err := normaliseMarketplaceTemplateTags(req.Tags)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sourceID, ok := parseUUIDOrBadRequest(w, req.SourceID, "source_id")
	if !ok {
		return
	}
	imageURL := pgtype.Text{}
	if req.ImageURL != nil && strings.TrimSpace(*req.ImageURL) != "" {
		accepted, ok := h.acceptAvatarURL(w, r, *req.ImageURL, "")
		if !ok {
			return
		}
		imageURL = pgtype.Text{String: accepted, Valid: true}
	}

	var snapshot MarketplaceTemplateSnapshot
	if req.SourceType == "agent" {
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: sourceID, WorkspaceID: member.WorkspaceID})
		if err != nil {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		snapshot, ok = h.marketplaceTemplateSnapshotForAgent(r, w, member, agent)
	} else {
		squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{ID: sourceID, WorkspaceID: member.WorkspaceID})
		if err != nil {
			writeError(w, http.StatusNotFound, "squad not found")
			return
		}
		snapshot, ok = h.marketplaceTemplateSnapshotForSquad(r, w, member, squad)
	}
	if !ok {
		return
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode template snapshot")
		return
	}
	created, err := h.Queries.CreateMarketplaceTemplate(r.Context(), db.CreateMarketplaceTemplateParams{
		SourceWorkspaceID: member.WorkspaceID, CreatedBy: member.UserID,
		SourceType: req.SourceType, SourceID: sourceID, Name: req.Name,
		Description: req.Description, Tags: tags, Visibility: req.Visibility,
		ImageUrl: imageURL, SnapshotVersion: marketplaceTemplateSnapshotVersion, Snapshot: snapshotJSON,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create template")
		return
	}
	user, _ := h.Queries.GetUser(r.Context(), member.UserID)
	record := marketplaceTemplateRecordFromDB(created)
	resp, err := marketplaceTemplateResponse(record, user.Name, true, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode created template")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) GetMarketplaceTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "template id")
	if !ok {
		return
	}
	row, err := h.Queries.GetMarketplaceTemplate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	record := marketplaceTemplateRecordFromDB(row)
	if !canReadMarketplaceTemplate(record, member.WorkspaceID, member.UserID) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	creatorName := "Multica"
	if row.CreatedBy.Valid {
		if user, err := h.Queries.GetUser(r.Context(), row.CreatedBy); err == nil {
			creatorName = user.Name
		}
	}
	resp, err := marketplaceTemplateResponse(record, creatorName, true, canManageMarketplaceTemplate(record, member))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode template")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteMarketplaceTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "template id")
	if !ok {
		return
	}
	row, err := h.Queries.GetMarketplaceTemplate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if !canManageMarketplaceTemplate(marketplaceTemplateRecordFromDB(row), member) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	deleted, err := h.Queries.DeleteMarketplaceTemplate(r.Context(), id)
	if err != nil || deleted == 0 {
		writeError(w, http.StatusInternalServerError, "failed to delete template")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func nextMarketplaceAgentName(base string, used map[string]struct{}) string {
	if _, exists := used[strings.ToLower(base)]; !exists {
		used[strings.ToLower(base)] = struct{}{}
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", base, i)
		if _, exists := used[strings.ToLower(candidate)]; exists {
			continue
		}
		used[strings.ToLower(candidate)] = struct{}{}
		return candidate
	}
}

func validateMarketplaceSnapshot(snapshot MarketplaceTemplateSnapshot) error {
	if snapshot.Version != marketplaceTemplateSnapshotVersion {
		return fmt.Errorf("unsupported template snapshot version")
	}
	if snapshot.SourceType != "agent" && snapshot.SourceType != "squad" {
		return fmt.Errorf("invalid template source type")
	}
	if len(snapshot.Agents) == 0 {
		return fmt.Errorf("template contains no agents")
	}
	agentKeys := map[string]struct{}{}
	for _, agent := range snapshot.Agents {
		if agent.Key == "" || agent.Name == "" {
			return fmt.Errorf("template contains an invalid agent")
		}
		if _, exists := agentKeys[agent.Key]; exists {
			return fmt.Errorf("template contains duplicate agent keys")
		}
		agentKeys[agent.Key] = struct{}{}
	}
	if snapshot.SourceType == "squad" {
		if snapshot.Squad == nil {
			return fmt.Errorf("squad template is missing squad configuration")
		}
		if _, exists := agentKeys[snapshot.Squad.LeaderKey]; !exists {
			return fmt.Errorf("squad leader is not present in the template")
		}
	}
	return nil
}

func (h *Handler) ApplyMarketplaceTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "template id")
	if !ok {
		return
	}
	templateRow, err := h.Queries.GetMarketplaceTemplate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	record := marketplaceTemplateRecordFromDB(templateRow)
	if !canReadMarketplaceTemplate(record, member.WorkspaceID, member.UserID) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	var req struct {
		Name       string            `json:"name"`
		RuntimeIDs map[string]string `json:"runtime_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var snapshot MarketplaceTemplateSnapshot
	if err := json.Unmarshal(templateRow.Snapshot, &snapshot); err != nil {
		writeError(w, http.StatusInternalServerError, "template snapshot is invalid")
		return
	}
	if err := validateMarketplaceSnapshot(snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	runtimes := make(map[string]db.AgentRuntime, len(snapshot.Agents))
	for _, agentSnapshot := range snapshot.Agents {
		runtimeID, exists := req.RuntimeIDs[agentSnapshot.Key]
		if !exists || strings.TrimSpace(runtimeID) == "" {
			writeError(w, http.StatusBadRequest, "runtime_ids must assign every template agent")
			return
		}
		runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
		if !ok {
			return
		}
		runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{ID: runtimeUUID, WorkspaceID: member.WorkspaceID})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid runtime_id")
			return
		}
		if !canUseRuntimeForAgent(member, runtime) {
			writeError(w, http.StatusForbidden, "this runtime is private; only its owner can create agents on it")
			return
		}
		runtimes[agentSnapshot.Key] = runtime
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start template import transaction")
		return
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
			return
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
			return
		}
		for _, file := range skillSnapshot.Files {
			if _, err := qtx.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{SkillID: skill.ID, Path: file.Path, Content: file.Content}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create imported skill file")
				return
			}
		}
		skillIDs[skillSnapshot.Key] = skill.ID
	}

	existingAgents, err := qtx.ListAllAgents(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workspace agents")
		return
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
			return
		}
		for _, skillKey := range agentSnapshot.SkillKeys {
			skillID, exists := skillIDs[skillKey]
			if !exists {
				writeError(w, http.StatusBadRequest, "template agent references an unknown skill")
				return
			}
			if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{AgentID: created.ID, SkillID: skillID}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to attach imported skill")
				return
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
			return
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
			return
		}
		if snapshot.Squad.Instructions != "" {
			updated, err := qtx.UpdateSquad(r.Context(), db.UpdateSquadParams{
				ID: squad.ID, Instructions: pgtype.Text{String: snapshot.Squad.Instructions, Valid: true},
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to set imported squad instructions")
				return
			}
			squad = updated
		}
		for _, squadMember := range snapshot.Squad.Members {
			agent, exists := createdByKey[squadMember.AgentKey]
			if !exists {
				writeError(w, http.StatusBadRequest, "template squad member is missing")
				return
			}
			role := squadMember.Role
			if squadMember.AgentKey == snapshot.Squad.LeaderKey && role == "" {
				role = "leader"
			}
			if _, err := qtx.AddSquadMember(r.Context(), db.AddSquadMemberParams{
				SquadID: squad.ID, MemberType: "agent", MemberID: agent.ID, Role: role,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to add imported squad member")
				return
			}
		}
		createdSquad = &squad
	}
	if _, err := qtx.IncrementMarketplaceTemplateAppliedCount(r.Context(), templateRow.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record template usage")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit template import")
		return
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
	writeJSON(w, http.StatusCreated, map[string]any{
		"template_id": uuidToString(templateRow.ID), "agent_ids": createdIDs,
		"squad_id": squadID, "reused_skill_ids": reusedSkillIDs,
	})
}
