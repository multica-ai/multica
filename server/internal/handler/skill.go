package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// sanitizeNullBytes removes null bytes (0x00) from strings.
// PostgreSQL rejects null bytes in text columns with
// "invalid byte sequence for encoding UTF8: 0x00 (SQLSTATE 22021)".
func sanitizeNullBytes(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

// --- Response structs ---

type SkillResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Content     string  `json:"content"`
	Config      any     `json:"config"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// SkillSummaryResponse is the list-endpoint shape: everything SkillResponse
// has except `content`. SKILL.md bodies routinely run 50–200KB and shipping
// them in list payloads bloats responses past CLI timeouts on high-latency
// links (GH multica-ai/multica#2174). Detail endpoints still return the full
// SkillResponse with content.
type SkillSummaryResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Config      any     `json:"config"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// AgentSkillSummary is the still-narrower shape used for skills embedded in
// an Agent payload (`GET /api/agents`, `GET /api/agents/{id}`). The agent
// list batch query only joins enough columns to render the assignee chip in
// the UI; the standalone `/api/agents/{id}/skills` endpoint returns the full
// SkillSummaryResponse for callers that need the source/origin info.
type AgentSkillSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SkillFileResponse struct {
	ID        string `json:"id"`
	SkillID   string `json:"skill_id"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type SkillWithFilesResponse struct {
	SkillResponse
	Files []SkillFileResponse `json:"files"`
}

func skillToResponse(s db.Skill) SkillResponse {
	return SkillResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		Name:        s.Name,
		Description: s.Description,
		Content:     s.Content,
		Config:      decodeSkillConfig(s.Config),
		CreatedBy:   uuidToPtr(s.CreatedBy),
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}

// decodeSkillConfig decodes a JSONB skill.config blob, defaulting to {} when
// missing or unparseable so the API surface always returns a JSON object.
func decodeSkillConfig(raw []byte) any {
	var config any
	if raw != nil {
		_ = json.Unmarshal(raw, &config)
	}
	if config == nil {
		return map[string]any{}
	}
	return config
}

func skillSummaryToResponse(
	id, workspaceID pgtype.UUID,
	name, description string,
	config []byte,
	createdBy pgtype.UUID,
	createdAt, updatedAt pgtype.Timestamptz,
) SkillSummaryResponse {
	return SkillSummaryResponse{
		ID:          uuidToString(id),
		WorkspaceID: uuidToString(workspaceID),
		Name:        name,
		Description: description,
		Config:      decodeSkillConfig(config),
		CreatedBy:   uuidToPtr(createdBy),
		CreatedAt:   timestampToString(createdAt),
		UpdatedAt:   timestampToString(updatedAt),
	}
}

func skillFileToResponse(f db.SkillFile) SkillFileResponse {
	return SkillFileResponse{
		ID:        uuidToString(f.ID),
		SkillID:   uuidToString(f.SkillID),
		Path:      f.Path,
		Content:   f.Content,
		CreatedAt: timestampToString(f.CreatedAt),
		UpdatedAt: timestampToString(f.UpdatedAt),
	}
}

// --- Request structs ---

type CreateSkillRequest struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Content     string                   `json:"content"`
	Config      any                      `json:"config"`
	Files       []CreateSkillFileRequest `json:"files,omitempty"`
}

type CreateSkillFileRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type UpdateSkillRequest struct {
	Name        *string                  `json:"name"`
	Description *string                  `json:"description"`
	Content     *string                  `json:"content"`
	Config      any                      `json:"config"`
	Files       []CreateSkillFileRequest `json:"files,omitempty"`
}

type SetAgentSkillsRequest struct {
	SkillIDs []string `json:"skill_ids"`
}

// --- Helpers ---

// validateFilePath checks that a file path is safe (no traversal, no absolute paths).
func validateFilePath(p string) bool {
	if p == "" {
		return false
	}
	if filepath.IsAbs(p) {
		return false
	}
	cleaned := filepath.Clean(p)
	if strings.HasPrefix(cleaned, "..") {
		return false
	}
	return true
}

func (h *Handler) loadSkillForUser(w http.ResponseWriter, r *http.Request, id string) (db.Skill, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Skill{}, false
	}

	skill, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return skill, false
	}
	return skill, true
}

// --- Skill CRUD ---

func (h *Handler) ListSkills(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	skills, err := h.Queries.ListSkillSummariesByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}

	resp := make([]SkillSummaryResponse, len(skills))
	for i, s := range skills {
		resp[i] = skillSummaryToResponse(
			s.ID, s.WorkspaceID, s.Name, s.Description, s.Config,
			s.CreatedBy, s.CreatedAt, s.UpdatedAt,
		)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	files, err := h.Queries.ListSkillFiles(r.Context(), skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skill files")
		return
	}

	fileResps := make([]SkillFileResponse, len(files))
	for i, f := range files {
		fileResps[i] = skillFileToResponse(f)
	}

	writeJSON(w, http.StatusOK, SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
	})
}

func (h *Handler) CreateSkill(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	creatorUUID := parseUUID(creatorID)

	var req CreateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	for _, f := range req.Files {
		if !validateFilePath(f.Path) {
			writeError(w, http.StatusBadRequest, "invalid file path: "+f.Path)
			return
		}
	}

	resp, err := h.createSkillWithFiles(r.Context(), skillCreateInput{
		WorkspaceID: workspaceUUID,
		CreatorID:   creatorUUID,
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Config:      req.Config,
		Files:       req.Files,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a skill with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
		return
	}
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(protocol.EventSkillCreated, workspaceID, actorType, actorID, map[string]any{"skill": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// canManageSkill checks whether the current user can update or delete a skill.
// The skill creator or workspace owner/admin can manage any skill.
func (h *Handler) canManageSkill(w http.ResponseWriter, r *http.Request, skill db.Skill) bool {
	wsID := uuidToString(skill.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, wsID, "skill not found", "owner", "admin", "member")
	if !ok {
		return false
	}
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	isSkillCreator := skill.CreatedBy.Valid && uuidToString(skill.CreatedBy) == requestUserID(r)
	if !isAdmin && !isSkillCreator {
		writeError(w, http.StatusForbidden, "only the skill creator can manage this skill")
		return false
	}
	return true
}

func (h *Handler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}

	var req UpdateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, f := range req.Files {
		if !validateFilePath(f.Path) {
			writeError(w, http.StatusBadRequest, "invalid file path: "+f.Path)
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	params := db.UpdateSkillParams{
		ID: parseUUID(id),
	}
	if req.Name != nil {
		params.Name = pgtype.Text{String: sanitizeNullBytes(*req.Name), Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: sanitizeNullBytes(*req.Description), Valid: true}
	}
	if req.Content != nil {
		params.Content = pgtype.Text{String: sanitizeNullBytes(*req.Content), Valid: true}
	}
	if req.Config != nil {
		config, _ := json.Marshal(req.Config)
		params.Config = config
	}

	skill, err = qtx.UpdateSkill(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a skill with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update skill: "+err.Error())
		return
	}

	// If files are provided, replace all files.
	var fileResps []SkillFileResponse
	if req.Files != nil {
		if err := qtx.DeleteSkillFilesBySkill(r.Context(), skill.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete old skill files")
			return
		}
		fileResps = make([]SkillFileResponse, 0, len(req.Files))
		for _, f := range req.Files {
			sf, err := qtx.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{
				SkillID: skill.ID,
				Path:    sanitizeNullBytes(f.Path),
				Content: sanitizeNullBytes(f.Content),
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to upsert skill file: "+err.Error())
				return
			}
			fileResps = append(fileResps, skillFileToResponse(sf))
		}
	} else {
		files, _ := qtx.ListSkillFiles(r.Context(), skill.ID)
		fileResps = make([]SkillFileResponse, len(files))
		for i, f := range files {
			fileResps[i] = skillFileToResponse(f)
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	resp := SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
	}
	wsID := h.resolveWorkspaceID(r)
	actorType, actorID := h.resolveActor(r, requestUserID(r), wsID)
	h.publish(protocol.EventSkillUpdated, wsID, actorType, actorID, map[string]any{"skill": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}

	if err := h.Queries.DeleteSkill(r.Context(), skill.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skill")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(skill.WorkspaceID))
	h.publish(protocol.EventSkillDeleted, uuidToString(skill.WorkspaceID), actorType, actorID, map[string]any{"skill_id": uuidToString(skill.ID)})
	w.WriteHeader(http.StatusNoContent)
}

// --- Skill import ---

type ImportSkillRequest struct {
	URL string `json:"url"`
}

// Per-import bundle limits. These mirror the local-runtime importer so that
// URL imports cannot smuggle in payloads that the rest of the stack would
// reject. fetchRawFile enforces the per-file cap; importedSkill.addFile
// enforces the bundle-wide caps.
const (
	maxImportFileSize  = 1 << 20 // 1 MiB per file
	maxImportTotalSize = 8 << 20 // 8 MiB per import bundle (sum of supporting files)
	maxImportFileCount = 128     // max number of supporting files
)

// importedSkill holds the data extracted from an external source.
type importedSkill struct {
	name        string
	description string
	content     string // SKILL.md body
	files       []importedFile
	bundleSize  int            // running sum of file content bytes for cap enforcement
	origin      map[string]any // written into skill.config.origin so the UI can show provenance
}

type importedFile struct {
	path    string
	content string
}

// errImportCapExceeded marks an error caused by a per-file or per-bundle cap.
// Such errors must abort the import — silently dropping a file would otherwise
// produce an incomplete skill that looks valid to the user.
var errImportCapExceeded = errors.New("import cap exceeded")

// isCapError reports whether err is (or wraps) errImportCapExceeded.
func isCapError(err error) bool {
	return errors.Is(err, errImportCapExceeded)
}

// addFile appends a supporting file while enforcing the per-bundle caps. It
// returns an error when either the file count or aggregate byte budget would
// be exceeded so the caller fails the import instead of silently truncating.
func (s *importedSkill) addFile(path, content string) error {
	if len(s.files) >= maxImportFileCount {
		return fmt.Errorf("%w: import bundle exceeds %d file limit", errImportCapExceeded, maxImportFileCount)
	}
	if s.bundleSize+len(content) > maxImportTotalSize {
		return fmt.Errorf("%w: import bundle exceeds %d byte limit", errImportCapExceeded, maxImportTotalSize)
	}
	s.bundleSize += len(content)
	s.files = append(s.files, importedFile{path: path, content: content})
	return nil
}

// --- ClawHub types ---

type clawhubGetSkillResponse struct {
	Skill         clawhubSkill          `json:"skill"`
	LatestVersion *clawhubLatestVersion `json:"latestVersion"`
}

type clawhubSkill struct {
	Slug        string            `json:"slug"`
	DisplayName string            `json:"displayName"`
	Summary     string            `json:"summary"`
	Tags        map[string]string `json:"tags"`
}

type clawhubLatestVersion struct {
	Version string `json:"version"`
}

type clawhubVersionDetailResponse struct {
	Version clawhubVersionDetail `json:"version"`
}

type clawhubVersionDetail struct {
	Version string             `json:"version"`
	Files   []clawhubFileEntry `json:"files"`
}

type clawhubFileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// --- GitHub types (for skills.sh) ---

type githubContentEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // "file" or "dir"
	URL         string `json:"url"`
	DownloadURL string `json:"download_url"`
}

type githubRepoInfo struct {
	DefaultBranch string `json:"default_branch"`
}

type githubTreeResponse struct {
	Tree      []githubTreeEntry `json:"tree"`
	Truncated bool              `json:"truncated"`
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
}

// fetchGitHubDefaultBranch returns the default branch of a GitHub repository.
// Falls back to "main" if the API call fails.
func fetchGitHubDefaultBranch(httpClient *http.Client, owner, repo string) string {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo))
	resp, err := doGitHubAPIGet(httpClient, apiURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return "main"
	}
	defer resp.Body.Close()

	var info githubRepoInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil || info.DefaultBranch == "" {
		return "main"
	}
	return info.DefaultBranch
}

// --- URL detection ---

// importSource identifies where a URL points.
type importSource int

const (
	sourceClawHub importSource = iota
	sourceSkillsSh
	sourceGitHub
)

// detectImportSource determines the source from a URL.
// Returns the source and a normalized URL (with scheme).
func detectImportSource(raw string) (importSource, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, "", fmt.Errorf("empty URL")
	}

	normalized := raw
	if !strings.HasPrefix(normalized, "http://") && !strings.HasPrefix(normalized, "https://") {
		normalized = "https://" + normalized
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return 0, "", fmt.Errorf("invalid URL: %w", err)
	}

	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "skills.sh" || host == "www.skills.sh":
		return sourceSkillsSh, normalized, nil
	case host == "clawhub.ai" || host == "www.clawhub.ai":
		return sourceClawHub, normalized, nil
	case host == "github.com" || host == "www.github.com":
		return sourceGitHub, normalized, nil
	default:
		// If no host (bare slug), default to clawhub
		if !strings.Contains(raw, "/") || !strings.Contains(raw, ".") {
			return sourceClawHub, raw, nil
		}
		return 0, "", fmt.Errorf("unsupported source: %s (supported: clawhub.ai, skills.sh, github.com)", host)
	}
}

// --- ClawHub import ---

// parseClawHubSlug extracts the skill slug from a clawhub.ai URL.
func parseClawHubSlug(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	// /{owner}/{slug} — take the last segment as the slug
	if len(parts) == 2 {
		return parts[1], nil
	}
	if len(parts) == 1 && parts[0] != "" {
		return parts[0], nil
	}
	// Bare slug (no path)
	if raw == parsed.Host || parsed.Path == "" || parsed.Path == "/" {
		return "", fmt.Errorf("missing skill slug in URL")
	}
	return "", fmt.Errorf("could not extract skill slug from URL: %s", raw)
}

func fetchFromClawHub(httpClient *http.Client, rawURL string) (*importedSkill, error) {
	slug, err := parseClawHubSlug(rawURL)
	if err != nil {
		return nil, err
	}

	apiBase := "https://clawhub.ai/api/v1"

	// 1. Fetch skill metadata
	skillResp, err := httpClient.Get(apiBase + "/skills/" + url.PathEscape(slug))
	if err != nil {
		return nil, fmt.Errorf("failed to reach ClawHub: %w", err)
	}
	defer skillResp.Body.Close()

	if skillResp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("skill not found on ClawHub: %s", slug)
	}
	if skillResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ClawHub returned status %d", skillResp.StatusCode)
	}

	var chResp clawhubGetSkillResponse
	if err := json.NewDecoder(skillResp.Body).Decode(&chResp); err != nil {
		return nil, fmt.Errorf("failed to parse ClawHub response")
	}
	chSkill := chResp.Skill

	// 2. Determine latest version and fetch file list
	latestVersion := ""
	if v, ok := chSkill.Tags["latest"]; ok {
		latestVersion = v
	} else if chResp.LatestVersion != nil {
		latestVersion = chResp.LatestVersion.Version
	}

	var filePaths []string
	if latestVersion != "" {
		vURL := fmt.Sprintf("%s/skills/%s/versions/%s", apiBase, url.PathEscape(slug), url.PathEscape(latestVersion))
		vResp, err := httpClient.Get(vURL)
		if err == nil {
			defer vResp.Body.Close()
			if vResp.StatusCode == http.StatusOK {
				var vDetail clawhubVersionDetailResponse
				if err := json.NewDecoder(vResp.Body).Decode(&vDetail); err == nil {
					for _, f := range vDetail.Version.Files {
						filePaths = append(filePaths, f.Path)
					}
				}
			}
		}
	}

	// 3. Download each file
	result := &importedSkill{
		name:        chSkill.DisplayName,
		description: chSkill.Summary,
		origin: map[string]any{
			"type":       "clawhub",
			"source_url": rawURL,
			"slug":       slug,
		},
	}
	if result.name == "" {
		result.name = slug
	}

	for _, fp := range filePaths {
		fileURL := fmt.Sprintf("%s/skills/%s/file?path=%s", apiBase, url.PathEscape(slug), url.QueryEscape(fp))
		if latestVersion != "" {
			fileURL += "&version=" + url.QueryEscape(latestVersion)
		}
		body, err := fetchRawFile(httpClient, fileURL)
		if err != nil {
			// Cap violations must abort: silently dropping a file would
			// produce an incomplete bundle that looks valid. SKILL.md is
			// load-bearing, so any failure on it is fatal too.
			if isCapError(err) || fp == "SKILL.md" {
				return nil, fmt.Errorf("clawhub import: %s: %w", fp, err)
			}
			slog.Warn("clawhub import: file download failed", "path", fp, "error", err)
			continue
		}
		if fp == "SKILL.md" {
			result.content = string(body)
			continue
		}
		if err := result.addFile(fp, string(body)); err != nil {
			return nil, err
		}
	}

	if result.content == "" {
		return nil, fmt.Errorf("clawhub import: SKILL.md is empty or missing for %s", slug)
	}

	return result, nil
}

// --- skills.sh import ---

// parseSkillsShParts extracts owner, repo, skill-name from a skills.sh URL.
// URL format: https://skills.sh/{owner}/{repo}/{skill-name}
func parseSkillsShParts(raw string) (owner, repo, skillName string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URL: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("expected URL format: skills.sh/{owner}/{repo}/{skill-name}, got: %s", parsed.Path)
	}
	return parts[0], parts[1], parts[2], nil
}

func fetchFromSkillsSh(httpClient *http.Client, rawURL string) (*importedSkill, error) {
	owner, repo, skillName, err := parseSkillsShParts(rawURL)
	if err != nil {
		return nil, err
	}

	// Skills can be at different paths depending on the repo structure:
	//   skills/{name}/SKILL.md          (most common)
	//   .claude/skills/{name}/SKILL.md  (Claude Code native discovery)
	//   plugin/skills/{name}/SKILL.md   (e.g. microsoft repos)
	//   {name}/SKILL.md                 (e.g. anthropics/skills layout)
	//   SKILL.md                        (single-skill repo: the repo is the skill)
	defaultBranch := fetchGitHubDefaultBranch(httpClient, owner, repo)
	rawPrefix := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(defaultBranch))

	candidatePaths := []string{
		"skills/" + skillName,
		".claude/skills/" + skillName,
		"plugin/skills/" + skillName,
		skillName,
	}

	var skillMdBody []byte
	var skillDir string
	for _, dir := range candidatePaths {
		body, err := fetchRawFile(httpClient, buildRawGitHubURL(rawPrefix, dir+"/SKILL.md"))
		if err == nil {
			skillMdBody = body
			skillDir = dir
			break
		}
	}
	// Single-skill repos place SKILL.md at the repository root. Try it as a
	// fast path before the tree-listing fallback to avoid a recursive tree
	// API call for a common case. Verify the frontmatter name matches so a
	// stray root SKILL.md in a multi-skill repo can't get picked up for an
	// unrelated skill URL.
	if skillMdBody == nil {
		body, err := fetchRawFile(httpClient, buildRawGitHubURL(rawPrefix, "SKILL.md"))
		if err == nil {
			if name, _ := parseSkillFrontmatter(string(body)); name == skillName {
				skillMdBody = body
				skillDir = ""
			}
		}
	}
	if skillMdBody == nil {
		skillDir, skillMdBody, err = resolveGitHubSkillDirByName(httpClient, owner, repo, defaultBranch, rawPrefix, skillName)
		if err != nil {
			return nil, err
		}
	}

	// Parse name and description from YAML frontmatter
	name, description := parseSkillFrontmatter(string(skillMdBody))
	if name == "" {
		name = skillName
	}

	result := &importedSkill{
		name:        name,
		description: description,
		content:     string(skillMdBody),
		origin: map[string]any{
			"type":       "skills_sh",
			"source_url": rawURL,
			"owner":      owner,
			"repo":       repo,
			"skill":      skillName,
		},
	}

	// 2. List supporting files via GitHub API
	apiURL := buildGitHubContentsURL(owner, repo, skillDir, defaultBranch)
	dirResp, err := doGitHubAPIGet(httpClient, apiURL)
	if err != nil || dirResp.StatusCode != http.StatusOK {
		// Can't list files — return what we have (SKILL.md only)
		if dirResp != nil {
			dirResp.Body.Close()
		}
		return result, nil
	}
	defer dirResp.Body.Close()

	var entries []githubContentEntry
	if err := json.NewDecoder(dirResp.Body).Decode(&entries); err != nil {
		slog.Warn("github import: failed to decode top-level directory listing", "url", apiURL, "error", err)
		return result, nil
	}

	// 3. Recursively collect files (excluding SKILL.md and LICENSE)
	var allFiles []githubContentEntry
	slog.Info("github import: collecting supporting files", "skill", skillName, "top_level_entries", len(entries))
	collectGitHubFiles(httpClient, entries, &allFiles, apiURL)
	slog.Info("github import: collected supporting files", "skill", skillName, "files", len(allFiles))

	// 4. Download each file
	basePath := ""
	if skillDir != "" {
		basePath = skillDir + "/"
	}
	for _, entry := range allFiles {
		if entry.DownloadURL == "" {
			continue
		}
		body, err := fetchRawFile(httpClient, entry.DownloadURL)
		if err != nil {
			if isCapError(err) {
				return nil, fmt.Errorf("github import: %s: %w", entry.Path, err)
			}
			slog.Warn("github import: file download failed", "path", entry.Path, "error", err)
			continue
		}
		// Convert absolute GitHub path to relative path within skill
		relPath := strings.TrimPrefix(entry.Path, basePath)
		if err := result.addFile(relPath, string(body)); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func resolveGitHubSkillDirByName(httpClient *http.Client, owner, repo, defaultBranch, rawPrefix, skillName string) (string, []byte, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(defaultBranch))
	resp, err := doGitHubAPIGet(httpClient, apiURL)
	if err != nil {
		return "", nil, fmt.Errorf("failed to inspect repository %s/%s for skill %s: %w", owner, repo, skillName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("failed to inspect repository %s/%s for skill %s: HTTP %d", owner, repo, skillName, resp.StatusCode)
	}

	var tree githubTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return "", nil, fmt.Errorf("failed to inspect repository %s/%s for skill %s: %w", owner, repo, skillName, err)
	}

	skillPaths := extractSkillMdPaths(tree.Tree)
	preferred, remaining := partitionSkillMdPaths(skillName, skillPaths)
	if dir, body, ok := findMatchingSkillDirByFrontmatter(httpClient, rawPrefix, skillName, preferred); ok {
		return dir, body, nil
	}
	if !tree.Truncated {
		if dir, body, ok := findMatchingSkillDirByFrontmatter(httpClient, rawPrefix, skillName, remaining); ok {
			return dir, body, nil
		}
		return "", nil, skillMdNotFoundError(owner, repo, skillName)
	}

	slog.Warn("github import: repository tree listing truncated", "owner", owner, "repo", repo, "branch", defaultBranch)
	if dir, body, ok := findSkillDirFromConventionalPrefixes(httpClient, owner, repo, defaultBranch, rawPrefix, skillName); ok {
		return dir, body, nil
	}
	return "", nil, fmt.Errorf("repository %s/%s tree is too large to scan exhaustively for skill %s", owner, repo, skillName)
}

// collectGitHubFiles recursively collects file entries from a GitHub directory listing.
func collectGitHubFiles(httpClient *http.Client, entries []githubContentEntry, out *[]githubContentEntry, parentURL string) {
	for _, entry := range entries {
		lower := strings.ToLower(entry.Name)
		if lower == "skill.md" || lower == "license" || lower == "license.txt" || lower == "license.md" {
			continue
		}
		if entry.Type == "file" {
			*out = append(*out, entry)
		} else if entry.Type == "dir" {
			// Fetch subdirectory contents
			subURL := entry.URL
			if subURL == "" {
				parsed, err := url.Parse(parentURL)
				if err != nil {
					slog.Warn("github import: invalid parent directory url", "url", parentURL, "error", err)
					continue
				}
				parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/" + entry.Name
				subURL = parsed.String()
			}
			subResp, err := doGitHubAPIGet(httpClient, subURL)
			if err != nil || subResp.StatusCode != http.StatusOK {
				attrs := []any{"url", subURL}
				if subResp != nil {
					attrs = append(attrs, "status", subResp.StatusCode)
					subResp.Body.Close()
				}
				if err != nil {
					attrs = append(attrs, "error", err)
				}
				slog.Warn("github import: failed to list subdirectory", attrs...)
				continue
			}
			var subEntries []githubContentEntry
			if err := json.NewDecoder(subResp.Body).Decode(&subEntries); err != nil {
				subResp.Body.Close()
				slog.Warn("github import: failed to decode subdirectory listing", "url", subURL, "error", err)
				continue
			}
			subResp.Body.Close()
			collectGitHubFiles(httpClient, subEntries, out, subURL)
		}
	}
}

func findSkillDirFromConventionalPrefixes(httpClient *http.Client, owner, repo, defaultBranch, rawPrefix, skillName string) (string, []byte, bool) {
	prefixes := []string{"skills", ".claude/skills", "plugin/skills"}
	var skillPaths []string
	for _, prefix := range prefixes {
		paths, err := listGitHubSkillMdPaths(httpClient, owner, repo, prefix, defaultBranch)
		if err != nil {
			slog.Warn("github import: failed to list conventional skill prefix", "prefix", prefix, "error", err)
			continue
		}
		skillPaths = append(skillPaths, paths...)
	}

	preferred, remaining := partitionSkillMdPaths(skillName, skillPaths)
	if dir, body, ok := findMatchingSkillDirByFrontmatter(httpClient, rawPrefix, skillName, preferred); ok {
		return dir, body, true
	}
	return findMatchingSkillDirByFrontmatter(httpClient, rawPrefix, skillName, remaining)
}

func listGitHubSkillMdPaths(httpClient *http.Client, owner, repo, repoPath, ref string) ([]string, error) {
	apiURL := buildGitHubContentsURL(owner, repo, repoPath, ref)
	resp, err := doGitHubAPIGet(httpClient, apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var entries []githubContentEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}

	var paths []string
	collectGitHubSkillMdPaths(httpClient, entries, &paths, apiURL)
	return paths, nil
}

func collectGitHubSkillMdPaths(httpClient *http.Client, entries []githubContentEntry, out *[]string, parentURL string) {
	for _, entry := range entries {
		lower := strings.ToLower(entry.Name)
		if entry.Type == "file" {
			if lower == "skill.md" {
				*out = append(*out, entry.Path)
			}
			continue
		}
		if entry.Type != "dir" {
			continue
		}

		subURL := entry.URL
		if subURL == "" {
			parsed, err := url.Parse(parentURL)
			if err != nil {
				slog.Warn("github import: invalid parent directory url", "url", parentURL, "error", err)
				continue
			}
			parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/" + entry.Name
			subURL = parsed.String()
		}

		subResp, err := doGitHubAPIGet(httpClient, subURL)
		if err != nil || subResp.StatusCode != http.StatusOK {
			attrs := []any{"url", subURL}
			if subResp != nil {
				attrs = append(attrs, "status", subResp.StatusCode)
				subResp.Body.Close()
			}
			if err != nil {
				attrs = append(attrs, "error", err)
			}
			slog.Warn("github import: failed to list skill metadata subdirectory", attrs...)
			continue
		}

		var subEntries []githubContentEntry
		if err := json.NewDecoder(subResp.Body).Decode(&subEntries); err != nil {
			subResp.Body.Close()
			slog.Warn("github import: failed to decode skill metadata subdirectory", "url", subURL, "error", err)
			continue
		}
		subResp.Body.Close()
		collectGitHubSkillMdPaths(httpClient, subEntries, out, subURL)
	}
}

func extractSkillMdPaths(entries []githubTreeEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type != "blob" || (!strings.HasSuffix(entry.Path, "/SKILL.md") && entry.Path != "SKILL.md") {
			continue
		}
		paths = append(paths, entry.Path)
	}
	return paths
}

func partitionSkillMdPaths(skillName string, skillPaths []string) (preferred []string, remaining []string) {
	for _, skillPath := range skillPaths {
		if isLikelySkillPathMatch(skillName, skillPath) {
			preferred = append(preferred, skillPath)
			continue
		}
		remaining = append(remaining, skillPath)
	}
	return preferred, remaining
}

func findMatchingSkillDirByFrontmatter(httpClient *http.Client, rawPrefix, skillName string, skillPaths []string) (string, []byte, bool) {
	for _, skillPath := range skillPaths {
		body, err := fetchRawFile(httpClient, buildRawGitHubURL(rawPrefix, skillPath))
		if err != nil {
			slog.Warn("github import: fallback SKILL.md fetch failed", "path", skillPath, "error", err)
			continue
		}
		name, _ := parseSkillFrontmatter(string(body))
		if name == skillName {
			return skillDirFromSkillFilePath(skillPath), body, true
		}
	}
	return "", nil, false
}

func isLikelySkillPathMatch(skillName, skillPath string) bool {
	dir := strings.ToLower(skillDirFromSkillFilePath(skillPath))
	base := strings.ToLower(filepath.Base(dir))
	for _, hint := range skillNameHints(skillName) {
		if strings.Contains(dir, hint) || strings.Contains(base, hint) || strings.Contains(hint, base) {
			return true
		}
	}
	return false
}

func skillNameHints(skillName string) []string {
	skillName = strings.ToLower(skillName)
	parts := strings.Split(skillName, "-")
	seen := map[string]struct{}{}
	var hints []string

	addHint := func(value string) {
		value = strings.TrimSpace(value)
		if len(value) < 3 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		hints = append(hints, value)
	}

	addHint(skillName)
	for i := 1; i < len(parts); i++ {
		addHint(strings.Join(parts[i:], "-"))
	}
	for _, part := range parts {
		addHint(part)
	}
	return hints
}

// parseSkillFrontmatter extracts name and description from YAML frontmatter in SKILL.md.
func parseSkillFrontmatter(content string) (name, description string) {
	if !strings.HasPrefix(content, "---") {
		return "", ""
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return "", ""
	}
	frontmatter := content[3 : 3+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			name = strings.Trim(name, "\"'")
		} else if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			description = strings.Trim(description, "\"'")
		}
	}
	return name, description
}

// --- GitHub import ---

// errGitHubAPIBlocked signals that an api.github.com probe was rejected for
// auth/rate-limit reasons (401/403/429) rather than because the resource
// genuinely does not exist. Resolvers treat this as "indeterminate" and may
// fall back to the optimistic URL split rather than aborting the import.
var errGitHubAPIBlocked = errors.New("github API blocked (rate limit or auth)")

// doGitHubAPIGet performs a GET against an api.github.com URL, attaching the
// GITHUB_TOKEN bearer header when the env var is set. Unauthenticated GitHub
// API requests are capped at 60/hour per IP, which is trivially exhausted on
// shared self-hosted servers and surfaces to users as 403 errors during
// skill imports. Setting GITHUB_TOKEN raises the limit to 5000/hour.
func doGitHubAPIGet(httpClient *http.Client, apiURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	addGitHubAuthHeader(req)
	return httpClient.Do(req)
}

func addGitHubAuthHeader(req *http.Request) {
	if req == nil {
		return
	}
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// githubSpec captures the parsed components of a github.com URL pointing at a
// skill (or single-skill repository).
type githubSpec struct {
	owner    string
	repo     string
	ref      string // empty → caller resolves the default branch
	skillDir string // relative directory within the repo, "" for the repository root

	// refSegments holds the raw path segments after /tree/ or /blob/ that
	// jointly encode (ref, skillDir). GitHub's web URLs do not delimit the
	// boundary between branch/tag name and in-repo path, so when a ref
	// contains '/' (e.g. "release/v2") segments[0] alone is not the ref.
	// fetchFromGitHub uses resolveGitHubRefAndPath to walk these segments
	// and ask the API which prefix is a real branch/tag/commit. When this
	// slice is empty, ref/skillDir above are authoritative (root URL).
	refSegments []string
	// kind is "tree" or "blob"; "" for root URLs. blob requires the last
	// segment to be SKILL.md, which is already stripped from refSegments.
	kind string
}

// parseGitHubURL extracts the owner, repo, and the raw post-/tree|/blob
// segments from a github.com URL. Supported forms:
//
//	github.com/{owner}/{repo}                                → root, default branch
//	github.com/{owner}/{repo}/tree/{ref}/{path...}           → ref / skill dir
//	github.com/{owner}/{repo}/blob/{ref}/{path.../SKILL.md}  → ref / skill dir
//
// A simple-ref shortcut (segments[0] is the ref, the rest is the path) is
// stored in spec.ref/spec.skillDir; refSegments is also populated so that
// fetchFromGitHub can disambiguate refs containing '/' against the API.
func parseGitHubURL(raw string) (githubSpec, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return githubSpec{}, fmt.Errorf("invalid URL: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return githubSpec{}, fmt.Errorf("expected URL format: github.com/{owner}/{repo}[/tree/{ref}/{path}], got: %s", parsed.Path)
	}
	spec := githubSpec{owner: parts[0], repo: strings.TrimSuffix(parts[1], ".git")}
	if len(parts) == 2 {
		return spec, nil
	}
	kind := parts[2]
	if kind != "tree" && kind != "blob" {
		return githubSpec{}, fmt.Errorf("unsupported URL form: github.com/%s/%s/%s/... (use /tree/{ref}/... or /blob/{ref}/.../SKILL.md)", spec.owner, spec.repo, kind)
	}
	if len(parts) < 4 || parts[3] == "" {
		return githubSpec{}, fmt.Errorf("missing ref after /%s/", kind)
	}
	spec.kind = kind
	rest := parts[3:]
	if kind == "blob" {
		if !strings.EqualFold(rest[len(rest)-1], "SKILL.md") {
			return githubSpec{}, fmt.Errorf("blob URL must point to a SKILL.md file")
		}
		rest = rest[:len(rest)-1]
		if len(rest) == 0 {
			return githubSpec{}, fmt.Errorf("missing ref after /blob/")
		}
	}
	// Decode URL-escaped segments (e.g. spaces) so paths match the repo's
	// real on-disk layout. Re-escaping happens in buildRawGitHubURL.
	decoded := make([]string, len(rest))
	for i, p := range rest {
		d, err := url.PathUnescape(p)
		if err != nil {
			return githubSpec{}, fmt.Errorf("invalid path segment %q: %w", p, err)
		}
		if d == "" {
			return githubSpec{}, fmt.Errorf("empty path segment in URL")
		}
		decoded[i] = d
	}
	spec.refSegments = decoded
	// Optimistic split: assume the simple case where the ref is one segment.
	// fetchFromGitHub will re-resolve via the API and overwrite both fields
	// when the optimistic guess does not validate (e.g. release/v2 refs).
	spec.ref = decoded[0]
	if len(decoded) > 1 {
		spec.skillDir = strings.Join(decoded[1:], "/")
	}
	return spec, nil
}

// resolveGitHubRefAndPath walks the parsed refSegments and asks the GitHub
// commits API which prefix corresponds to a real branch, tag, or commit.
// This is what makes refs containing '/' (e.g. "release/v2") work correctly:
// the URL github.com/o/r/tree/release/v2/skills/foo is ambiguous between
// (ref=release, path=v2/skills/foo) and (ref=release/v2, path=skills/foo),
// so we probe /repos/{o}/{r}/commits/{candidate} from longest to shortest
// and accept the first one the server confirms exists.
//
// On success spec.ref and spec.skillDir are overwritten with the resolved
// pair. On failure (no candidate resolves) a single error is returned that
// names every candidate that was tried.
func resolveGitHubRefAndPath(httpClient *http.Client, spec *githubSpec) error {
	if len(spec.refSegments) == 0 {
		return nil
	}
	// Try longest prefix first so that release/v2 wins over release.
	tried := make([]string, 0, len(spec.refSegments))
	blocked := false
	for n := len(spec.refSegments); n >= 1; n-- {
		candidate := strings.Join(spec.refSegments[:n], "/")
		tried = append(tried, candidate)
		ok, err := githubRefExists(httpClient, spec.owner, spec.repo, candidate)
		if errors.Is(err, errGitHubAPIBlocked) {
			// 401/403/429 means we can't tell whether the ref exists. Keep
			// trying the remaining (shorter) candidates so we don't punish
			// the common single-segment-ref case for one bad probe.
			blocked = true
			continue
		}
		if err != nil {
			// Network / transport errors should not be silently treated as
			// "ref does not exist" — surface them so the caller can retry.
			return fmt.Errorf("validating ref %q: %w", candidate, err)
		}
		if ok {
			spec.ref = candidate
			if n == len(spec.refSegments) {
				spec.skillDir = ""
			} else {
				spec.skillDir = strings.Join(spec.refSegments[n:], "/")
			}
			return nil
		}
	}
	if blocked {
		// Every probe was either a confirmed 404 or rate-limited and we never
		// got a confirmation. Fall back to the optimistic single-segment
		// split that parseGitHubURL populated. If that's wrong, the
		// subsequent raw-file fetch will surface a clearer "SKILL.md not
		// found" error than failing the whole import on a 403.
		slog.Warn("github import: ref resolution blocked by GitHub API (rate limit or auth); falling back to optimistic single-segment ref. Set GITHUB_TOKEN to enable disambiguation of slash-bearing refs.",
			"owner", spec.owner, "repo", spec.repo, "tried", tried)
		return nil
	}
	return fmt.Errorf("could not resolve ref in github.com/%s/%s URL — tried: %s. Make sure the branch, tag, or commit exists and that the URL is the canonical /tree/{ref}/{path} or /blob/{ref}/{path}/SKILL.md form",
		spec.owner, spec.repo, strings.Join(tried, ", "))
}

// githubRefExists returns true when GitHub recognizes ref as a branch, tag,
// or commit SHA on owner/repo. It uses the commits endpoint because that
// single call accepts all three ref kinds (unlike /branches or /tags which
// only match one). 404 means the ref does not exist; any other non-200
// status is treated as an error so the caller can distinguish "missing"
// from "API down".
func githubRefExists(httpClient *http.Client, owner, repo, ref string) (bool, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s",
		url.PathEscape(owner), url.PathEscape(repo), escapeRefPath(ref))
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return false, err
	}
	// Per GitHub docs: Accept: application/vnd.github.v3.sha returns just
	// the SHA when the ref resolves, which is the cheapest possible probe.
	req.Header.Set("Accept", "application/vnd.github.v3.sha")
	addGitHubAuthHeader(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound, http.StatusUnprocessableEntity:
		return false, nil
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return false, errGitHubAPIBlocked
	default:
		return false, fmt.Errorf("github API returned status %d for ref %q", resp.StatusCode, ref)
	}
}

func fetchFromGitHub(httpClient *http.Client, rawURL string) (*importedSkill, error) {
	spec, err := parseGitHubURL(rawURL)
	if err != nil {
		return nil, err
	}
	if len(spec.refSegments) > 0 {
		// Disambiguate slash-bearing refs (release/v2 etc.) against the API
		// before issuing any raw or contents requests.
		if err := resolveGitHubRefAndPath(httpClient, &spec); err != nil {
			return nil, err
		}
	}
	if spec.ref == "" {
		spec.ref = fetchGitHubDefaultBranch(httpClient, spec.owner, spec.repo)
	}
	rawPrefix := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s",
		url.PathEscape(spec.owner), url.PathEscape(spec.repo), escapeRefPath(spec.ref))

	skillMdPath := "SKILL.md"
	if spec.skillDir != "" {
		skillMdPath = spec.skillDir + "/SKILL.md"
	}
	skillMdBody, err := fetchRawFile(httpClient, buildRawGitHubURL(rawPrefix, skillMdPath))
	if err != nil {
		if spec.skillDir == "" {
			return nil, fmt.Errorf("SKILL.md not found at the root of %s/%s@%s. For multi-skill repositories, point to a specific directory using github.com/%s/%s/tree/%s/<skill-dir>",
				spec.owner, spec.repo, spec.ref, spec.owner, spec.repo, spec.ref)
		}
		return nil, fmt.Errorf("SKILL.md not found at %s in %s/%s@%s: %w",
			skillMdPath, spec.owner, spec.repo, spec.ref, err)
	}

	name, description := parseSkillFrontmatter(string(skillMdBody))
	if name == "" {
		if spec.skillDir != "" {
			name = filepath.Base(spec.skillDir)
		} else {
			name = spec.repo
		}
	}

	result := &importedSkill{
		name:        name,
		description: description,
		content:     string(skillMdBody),
		origin: map[string]any{
			"type":       "github",
			"source_url": rawURL,
			"owner":      spec.owner,
			"repo":       spec.repo,
			"ref":        spec.ref,
			"path":       spec.skillDir,
		},
	}

	apiURL := buildGitHubContentsURL(spec.owner, spec.repo, spec.skillDir, spec.ref)
	dirResp, err := doGitHubAPIGet(httpClient, apiURL)
	if err != nil || dirResp.StatusCode != http.StatusOK {
		// Cannot list the directory — return what we have (SKILL.md only).
		// Keep this lenient: a private rate-limited request shouldn't fail
		// an import that has already produced a valid SKILL.md.
		if dirResp != nil {
			dirResp.Body.Close()
		}
		return result, nil
	}
	defer dirResp.Body.Close()

	var entries []githubContentEntry
	if err := json.NewDecoder(dirResp.Body).Decode(&entries); err != nil {
		slog.Warn("github import: failed to decode top-level directory listing", "url", apiURL, "error", err)
		return result, nil
	}

	var allFiles []githubContentEntry
	collectGitHubFiles(httpClient, entries, &allFiles, apiURL)

	basePath := ""
	if spec.skillDir != "" {
		basePath = spec.skillDir + "/"
	}
	for _, entry := range allFiles {
		if entry.DownloadURL == "" {
			continue
		}
		body, err := fetchRawFile(httpClient, entry.DownloadURL)
		if err != nil {
			if isCapError(err) {
				return nil, fmt.Errorf("github import: %s: %w", entry.Path, err)
			}
			slog.Warn("github import: file download failed", "path", entry.Path, "error", err)
			continue
		}
		relPath := strings.TrimPrefix(entry.Path, basePath)
		if err := result.addFile(relPath, string(body)); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// --- Shared helpers ---

// fetchRawFile downloads a URL and returns the body bytes. Returns an error
// if the response exceeds maxImportFileSize so we never silently truncate a
// half-downloaded skill file into the workspace.
func fetchRawFile(httpClient *http.Client, fileURL string) ([]byte, error) {
	resp, err := httpClient.Get(fileURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImportFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxImportFileSize {
		return nil, fmt.Errorf("%w: file exceeds %d byte limit", errImportCapExceeded, maxImportFileSize)
	}
	return body, nil
}

// escapeRefPath percent-encodes each segment of a git ref individually so
// that slash-bearing refs like "release/v2" are sent to GitHub as
// "release/v2" (path separators preserved) rather than "release%2Fv2"
// (which GitHub does not accept on the commits / raw endpoints).
func escapeRefPath(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func buildRawGitHubURL(rawPrefix, repoPath string) string {
	parts := strings.Split(strings.Trim(repoPath, "/"), "/")
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(part))
	}
	if len(escaped) == 0 {
		return rawPrefix
	}
	return rawPrefix + "/" + strings.Join(escaped, "/")
}

func buildGitHubContentsURL(owner, repo, repoPath, ref string) string {
	base := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents",
		url.PathEscape(owner), url.PathEscape(repo))
	if repoPath == "" {
		return base + "?ref=" + url.QueryEscape(ref)
	}
	return base + "/" + strings.TrimPrefix(buildRawGitHubURL("", repoPath), "/") + "?ref=" + url.QueryEscape(ref)
}

func skillDirFromSkillFilePath(path string) string {
	if path == "SKILL.md" {
		return ""
	}
	return strings.TrimSuffix(path, "/SKILL.md")
}

func skillMdNotFoundError(owner, repo, skillName string) error {
	return fmt.Errorf("SKILL.md not found in repository %s/%s for skill %s", owner, repo, skillName)
}

// --- Import handler ---

func (h *Handler) ImportSkill(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	creatorUUID := parseUUID(creatorID)

	var req ImportSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	source, normalized, err := detectImportSource(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}

	var imported *importedSkill
	switch source {
	case sourceClawHub:
		imported, err = fetchFromClawHub(httpClient, normalized)
	case sourceSkillsSh:
		imported, err = fetchFromSkillsSh(httpClient, normalized)
	case sourceGitHub:
		imported, err = fetchFromGitHub(httpClient, normalized)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	files := make([]CreateSkillFileRequest, 0, len(imported.files))
	for _, f := range imported.files {
		if !validateFilePath(f.path) {
			continue
		}
		files = append(files, CreateSkillFileRequest{
			Path:    f.path,
			Content: f.content,
		})
	}

	// Persist provenance into skill.config.origin so list/detail UI can show
	// "Imported from GitHub / ClawHub / Skills.sh" and link back to the source.
	config := map[string]any{}
	if imported.origin != nil {
		config["origin"] = imported.origin
	}

	resp, err := h.createSkillWithFiles(r.Context(), skillCreateInput{
		WorkspaceID: workspaceUUID,
		CreatorID:   creatorUUID,
		Name:        imported.name,
		Description: imported.description,
		Content:     imported.content,
		Config:      config,
		Files:       files,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a skill with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
		return
	}
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(protocol.EventSkillCreated, workspaceID, actorType, actorID, map[string]any{"skill": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// --- Skill File endpoints ---

func (h *Handler) ListSkillFiles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	files, err := h.Queries.ListSkillFiles(r.Context(), skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skill files")
		return
	}

	resp := make([]SkillFileResponse, len(files))
	for i, f := range files {
		resp[i] = skillFileToResponse(f)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpsertSkillFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}

	var req CreateSkillFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateFilePath(req.Path) {
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	}

	sf, err := h.Queries.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{
		SkillID: skill.ID,
		Path:    sanitizeNullBytes(req.Path),
		Content: sanitizeNullBytes(req.Content),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to upsert skill file: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, skillFileToResponse(sf))
}

func (h *Handler) DeleteSkillFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}

	fileID := chi.URLParam(r, "fileId")
	fileUUID, ok := parseUUIDOrBadRequest(w, fileID, "file id")
	if !ok {
		return
	}
	// Verify the file belongs to the parent skill we just authorized — guards
	// against deleting a file owned by a different skill via the URL param.
	file, err := h.Queries.GetSkillFile(r.Context(), fileUUID)
	if err != nil || uuidToString(file.SkillID) != uuidToString(skill.ID) {
		writeError(w, http.StatusNotFound, "skill file not found")
		return
	}
	if err := h.Queries.DeleteSkillFile(r.Context(), file.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skill file")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Agent-Skill junction ---

func (h *Handler) ListAgentSkills(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	skills, err := h.Queries.ListAgentSkillSummaries(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent skills")
		return
	}

	resp := make([]SkillSummaryResponse, len(skills))
	for i, s := range skills {
		resp[i] = skillSummaryToResponse(
			s.ID, s.WorkspaceID, s.Name, s.Description, s.Config,
			s.CreatedBy, s.CreatedAt, s.UpdatedAt,
		)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SetAgentSkills(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req SetAgentSkillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	skillUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.SkillIDs, "skill_ids")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	if err := qtx.RemoveAllAgentSkills(r.Context(), agent.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear agent skills")
		return
	}

	for _, skillID := range skillUUIDs {
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: agent.ID,
			SkillID: skillID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add agent skill: "+err.Error())
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	// Return the updated skills list.
	skills, err := h.Queries.ListAgentSkillSummaries(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent skills")
		return
	}

	resp := make([]SkillSummaryResponse, len(skills))
	for i, s := range skills {
		resp[i] = skillSummaryToResponse(
			s.ID, s.WorkspaceID, s.Name, s.Description, s.Config,
			s.CreatedBy, s.CreatedAt, s.UpdatedAt,
		)
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(agent.WorkspaceID))
	h.publish(protocol.EventAgentStatus, uuidToString(agent.WorkspaceID), actorType, actorID, map[string]any{"agent_id": uuidToString(agent.ID), "skills": resp})
	writeJSON(w, http.StatusOK, resp)
}
