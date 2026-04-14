package handler

import (
	"encoding/json"
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

// --- Response structs ---

type SkillResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Config      any    `json:"config"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
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
	var config any
	if s.Config != nil {
		json.Unmarshal(s.Config, &config)
	}
	if config == nil {
		config = map[string]any{}
	}

	return SkillResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		Name:        s.Name,
		Description: s.Description,
		Content:     s.Content,
		Config:      config,
		CreatedBy:   uuidToPtr(s.CreatedBy),
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
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
	workspaceID := resolveWorkspaceID(r)
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
	workspaceID := resolveWorkspaceID(r)

	skills, err := h.Queries.ListSkillsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}

	resp := make([]SkillResponse, len(skills))
	for i, s := range skills {
		resp[i] = skillToResponse(s)
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
	workspaceID := resolveWorkspaceID(r)

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}

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

	config, _ := json.Marshal(req.Config)
	if req.Config == nil {
		config = []byte("{}")
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	skill, err := qtx.CreateSkill(r.Context(), db.CreateSkillParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Config:      config,
		CreatedBy:   parseUUID(creatorID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a skill with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
		return
	}

	fileResps := make([]SkillFileResponse, 0, len(req.Files))
	for _, f := range req.Files {
		sf, err := qtx.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{
			SkillID: skill.ID,
			Path:    f.Path,
			Content: f.Content,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create skill file: "+err.Error())
			return
		}
		fileResps = append(fileResps, skillFileToResponse(sf))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	resp := SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
	}
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(protocol.EventSkillCreated, workspaceID, actorType, actorID, map[string]any{"skill": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, uuidToString(skill.WorkspaceID), "skill not found", "owner", "admin"); !ok {
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
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Content != nil {
		params.Content = pgtype.Text{String: *req.Content, Valid: true}
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
				Path:    f.Path,
				Content: f.Content,
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
	wsID := resolveWorkspaceID(r)
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
	if _, ok := h.requireWorkspaceRole(w, r, uuidToString(skill.WorkspaceID), "skill not found", "owner", "admin"); !ok {
		return
	}

	if err := h.Queries.DeleteSkill(r.Context(), parseUUID(id)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skill")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(skill.WorkspaceID))
	h.publish(protocol.EventSkillDeleted, uuidToString(skill.WorkspaceID), actorType, actorID, map[string]any{"skill_id": id})
	w.WriteHeader(http.StatusNoContent)
}

// --- Skill import ---

type ImportSkillRequest struct {
	URL string `json:"url"`
}

// importedSkill holds the data extracted from an external source.
type importedSkill struct {
	name        string
	description string
	content     string // SKILL.md body
	files       []importedFile
}

type importedFile struct {
	path    string
	content string
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
	DownloadURL string `json:"download_url"`
}

type githubRepoInfo struct {
	DefaultBranch string `json:"default_branch"`
}

// fetchGitHubDefaultBranch returns the default branch of a GitHub repository.
// Falls back to "main" if the API call fails.
func fetchGitHubDefaultBranch(httpClient *http.Client, owner, repo string) string {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo))
	resp, err := httpClient.Get(apiURL)
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
			slog.Warn("clawhub import: file download failed", "path", fp, "error", err)
			continue
		}
		if fp == "SKILL.md" {
			result.content = string(body)
		} else {
			result.files = append(result.files, importedFile{path: fp, content: string(body)})
		}
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
	//   {name}/SKILL.md                 (skill at repo root level)
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
		body, err := fetchRawFile(httpClient, rawPrefix+"/"+dir+"/SKILL.md")
		if err == nil {
			skillMdBody = body
			skillDir = dir
			break
		}
	}
	if skillMdBody == nil {
		return nil, fmt.Errorf("SKILL.md not found in repository %s/%s for skill %s", owner, repo, skillName)
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
	}

	// 2. List supporting files via GitHub API
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		url.PathEscape(owner), url.PathEscape(repo), skillDir, url.QueryEscape(defaultBranch))
	dirResp, err := httpClient.Get(apiURL)
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
		return result, nil
	}

	// 3. Recursively collect files (excluding SKILL.md and LICENSE)
	var allFiles []githubContentEntry
	collectGitHubFiles(httpClient, entries, &allFiles, apiURL)

	// 4. Download each file
	basePath := skillDir + "/"
	for _, entry := range allFiles {
		if entry.DownloadURL == "" {
			continue
		}
		body, err := fetchRawFile(httpClient, entry.DownloadURL)
		if err != nil {
			slog.Warn("skills.sh import: file download failed", "path", entry.Path, "error", err)
			continue
		}
		// Convert absolute GitHub path to relative path within skill
		relPath := strings.TrimPrefix(entry.Path, basePath)
		result.files = append(result.files, importedFile{path: relPath, content: string(body)})
	}

	return result, nil
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
			subURL := parentURL + "/" + url.PathEscape(entry.Name)
			subResp, err := httpClient.Get(subURL)
			if err != nil || subResp.StatusCode != http.StatusOK {
				if subResp != nil {
					subResp.Body.Close()
				}
				continue
			}
			var subEntries []githubContentEntry
			json.NewDecoder(subResp.Body).Decode(&subEntries)
			subResp.Body.Close()
			collectGitHubFiles(httpClient, subEntries, out, subURL)
		}
	}
}

// --- GitHub direct import ---

// parseGitHubURL extracts owner, repo, and the raw path remainder from a
// github.com URL. The branch and skill directory are resolved later via the
// GitHub API because branch names can contain `/` (e.g. `feat/some-feature`),
// making naive URL-segment splitting unreliable.
//
// Supported URL formats:
//
//	github.com/owner/repo/tree/branch/path/to/skill
//	github.com/owner/repo/tree/feat/branch-name/path/to/skill
//	github.com/owner/repo/blob/branch/path/to/SKILL.md
//	github.com/owner/repo (root-level skill)
func parseGitHubURL(rawURL string) (owner, repo, pathRemainder string, err error) {
	parsed, parseErr := url.Parse(rawURL)
	if parseErr != nil {
		return "", "", "", fmt.Errorf("invalid URL: %w", parseErr)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("GitHub URL must include owner and repo: %s", rawURL)
	}
	owner = parts[0]
	repo = parts[1]
	if len(parts) == 2 {
		return owner, repo, "", nil
	}
	// Skip "tree" or "blob" prefix — everything after is branch+path
	if len(parts) >= 3 && (parts[2] == "tree" || parts[2] == "blob") {
		pathRemainder = strings.Join(parts[3:], "/")
	} else {
		pathRemainder = strings.Join(parts[2:], "/")
	}
	// Strip trailing SKILL.md if the user copied a direct file link
	pathRemainder = strings.TrimSuffix(pathRemainder, "/SKILL.md")
	pathRemainder = strings.TrimSuffix(pathRemainder, "/skill.md")
	return owner, repo, pathRemainder, nil
}

// resolveGitHubBranchAndDir uses the GitHub API to split a raw path remainder
// (e.g. "feat/my-branch/skills/my-skill") into a valid branch name and the
// remaining directory path. This handles branch names with slashes correctly.
//
// Strategy: try progressively longer prefixes of the path as the branch name,
// starting with the default branch, then single-segment, then two-segment, etc.
func resolveGitHubBranchAndDir(httpClient *http.Client, owner, repo, pathRemainder string) (branch, skillDir string) {
	if pathRemainder == "" {
		return fetchGitHubDefaultBranch(httpClient, owner, repo), ""
	}

	// Try the default branch first — if pathRemainder starts after it, great.
	defaultBranch := fetchGitHubDefaultBranch(httpClient, owner, repo)
	if strings.HasPrefix(pathRemainder, defaultBranch+"/") {
		return defaultBranch, strings.TrimPrefix(pathRemainder, defaultBranch+"/")
	}
	if pathRemainder == defaultBranch {
		return defaultBranch, ""
	}

	// Try progressively longer prefixes as branch names via the GitHub API.
	segments := strings.Split(pathRemainder, "/")
	for i := 1; i <= len(segments) && i <= 10; i++ {
		candidate := strings.Join(segments[:i], "/")
		apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches/%s",
			url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(candidate))
		resp, err := httpClient.Get(apiURL)
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil && resp.StatusCode == http.StatusOK {
			dir := ""
			if i < len(segments) {
				dir = strings.Join(segments[i:], "/")
			}
			return candidate, dir
		}
	}

	// Fallback: assume single-segment branch or default
	if len(segments) > 1 {
		return segments[0], strings.Join(segments[1:], "/")
	}
	return defaultBranch, pathRemainder
}

// fetchFromGitHub imports a skill directly from a github.com URL. It supports
// any repo layout and branch names containing slashes — the branch is resolved
// via the GitHub API rather than naive URL parsing.
func fetchFromGitHub(httpClient *http.Client, rawURL string) (*importedSkill, error) {
	owner, repo, pathRemainder, err := parseGitHubURL(rawURL)
	if err != nil {
		return nil, err
	}

	branch, skillDir := resolveGitHubBranchAndDir(httpClient, owner, repo, pathRemainder)

	rawPrefix := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(branch))

	// Build SKILL.md URL
	var skillMdURL string
	if skillDir != "" {
		skillMdURL = rawPrefix + "/" + skillDir + "/SKILL.md"
	} else {
		skillMdURL = rawPrefix + "/SKILL.md"
	}

	skillMdBody, err := fetchRawFile(httpClient, skillMdURL)
	if err != nil {
		return nil, fmt.Errorf("SKILL.md not found at %s/%s branch %s path %s: %w",
			owner, repo, branch, skillDir, err)
	}

	name, description := parseSkillFrontmatter(string(skillMdBody))
	if name == "" {
		if skillDir != "" {
			parts := strings.Split(skillDir, "/")
			name = parts[len(parts)-1]
		} else {
			name = repo
		}
	}

	result := &importedSkill{
		name:        name,
		description: description,
		content:     string(skillMdBody),
	}

	// List supporting files via GitHub API
	var contentsPath string
	if skillDir != "" {
		contentsPath = skillDir
	}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		url.PathEscape(owner), url.PathEscape(repo), contentsPath, url.QueryEscape(branch))
	dirResp, err := githubGet(httpClient, apiURL)
	if err != nil || dirResp.StatusCode != http.StatusOK {
		if dirResp != nil {
			dirResp.Body.Close()
		}
		return result, nil
	}
	defer dirResp.Body.Close()

	var entries []githubContentEntry
	if err := json.NewDecoder(dirResp.Body).Decode(&entries); err != nil {
		return result, nil
	}

	// Recursively collect files with depth and count limits to prevent
	// abuse from deeply nested or file-heavy repositories.
	var allFiles []githubContentEntry
	collectGitHubFilesLimited(httpClient, entries, &allFiles, apiURL, 0)

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
			slog.Warn("github import: file download failed", "path", entry.Path, "error", err)
			continue
		}
		relPath := strings.TrimPrefix(entry.Path, basePath)
		result.files = append(result.files, importedFile{path: relPath, content: string(body)})
	}

	return result, nil
}

// Maximum recursion depth and file count for GitHub directory traversal.
const (
	maxGitHubRecurseDepth = 10
	maxGitHubFiles        = 100
)

// collectGitHubFilesLimited is a bounded version of collectGitHubFiles that
// stops recursing beyond maxGitHubRecurseDepth and stops collecting beyond
// maxGitHubFiles. This prevents excessive API calls on malicious/huge repos.
func collectGitHubFilesLimited(httpClient *http.Client, entries []githubContentEntry, out *[]githubContentEntry, parentURL string, depth int) {
	if depth >= maxGitHubRecurseDepth {
		return
	}
	for _, entry := range entries {
		if len(*out) >= maxGitHubFiles {
			return
		}
		lower := strings.ToLower(entry.Name)
		if lower == "skill.md" || lower == "license" || lower == "license.txt" || lower == "license.md" {
			continue
		}
		if entry.Type == "file" {
			*out = append(*out, entry)
		} else if entry.Type == "dir" {
			subURL := parentURL + "/" + url.PathEscape(entry.Name)
			subResp, err := githubGet(httpClient, subURL)
			if err != nil || subResp.StatusCode != http.StatusOK {
				if subResp != nil {
					subResp.Body.Close()
				}
				continue
			}
			var subEntries []githubContentEntry
			json.NewDecoder(subResp.Body).Decode(&subEntries)
			subResp.Body.Close()
			collectGitHubFilesLimited(httpClient, subEntries, out, subURL, depth+1)
		}
	}
}

// githubGet performs an HTTP GET with an optional GitHub token for higher rate
// limits. Without a token, the GitHub API allows 60 requests/hour per IP;
// with a token, 5000/hour. Set GITHUB_TOKEN env var to enable.
//
// Also detects rate-limit responses and returns a clear error.
func githubGet(httpClient *http.Client, apiURL string) (*http.Response, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == 429 {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub API rate limit exceeded — set GITHUB_TOKEN env var for higher limits (5000 req/hour)")
	}
	return resp, nil
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

// --- Shared helpers ---

// fetchRawFile downloads a URL and returns the body bytes. Limit 1MB.
func fetchRawFile(httpClient *http.Client, fileURL string) ([]byte, error) {
	resp, err := httpClient.Get(fileURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// --- Import handler ---

func (h *Handler) ImportSkill(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}

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

	// Create skill in database
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	skill, err := qtx.CreateSkill(r.Context(), db.CreateSkillParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        imported.name,
		Description: imported.description,
		Content:     imported.content,
		Config:      []byte("{}"),
		CreatedBy:   parseUUID(creatorID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a skill with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
		return
	}

	fileResps := make([]SkillFileResponse, 0, len(imported.files))
	for _, f := range imported.files {
		if !validateFilePath(f.path) {
			continue
		}
		sf, err := qtx.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{
			SkillID: skill.ID,
			Path:    f.path,
			Content: f.content,
		})
		if err != nil {
			continue
		}
		fileResps = append(fileResps, skillFileToResponse(sf))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	resp := SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
	}
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(protocol.EventSkillCreated, workspaceID, actorType, actorID, map[string]any{"skill": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// ImportLocalSkill accepts a pre-assembled skill payload from the CLI (which
// reads a local directory) and creates it in the workspace. This avoids the
// CLI needing to duplicate the full create-with-files transaction logic.
func (h *Handler) ImportLocalSkill(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Cap request body to 10MB to prevent abuse (skills with many files).
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"` // SKILL.md body
		Files       []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content (SKILL.md body) is required")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	skill, err := qtx.CreateSkill(r.Context(), db.CreateSkillParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Config:      []byte("{}"),
		CreatedBy:   parseUUID(creatorID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a skill with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
		return
	}

	fileResps := make([]SkillFileResponse, 0, len(req.Files))
	for _, f := range req.Files {
		if !validateFilePath(f.Path) {
			continue
		}
		sf, err := qtx.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{
			SkillID: skill.ID,
			Path:    f.Path,
			Content: f.Content,
		})
		if err != nil {
			slog.Warn("local import: file create failed", "path", f.Path, "error", err)
			continue
		}
		fileResps = append(fileResps, skillFileToResponse(sf))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	resp := SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
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
	if _, ok := h.requireWorkspaceRole(w, r, uuidToString(skill.WorkspaceID), "skill not found", "owner", "admin"); !ok {
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
		Path:    req.Path,
		Content: req.Content,
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
	if _, ok := h.requireWorkspaceRole(w, r, uuidToString(skill.WorkspaceID), "skill not found", "owner", "admin"); !ok {
		return
	}

	fileID := chi.URLParam(r, "fileId")
	if err := h.Queries.DeleteSkillFile(r.Context(), parseUUID(fileID)); err != nil {
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

	skills, err := h.Queries.ListAgentSkills(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent skills")
		return
	}

	resp := make([]SkillResponse, len(skills))
	for i, s := range skills {
		resp[i] = skillToResponse(s)
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

	for _, skillID := range req.SkillIDs {
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: agent.ID,
			SkillID: parseUUID(skillID),
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
	skills, err := h.Queries.ListAgentSkills(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent skills")
		return
	}

	resp := make([]SkillResponse, len(skills))
	for i, s := range skills {
		resp[i] = skillToResponse(s)
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(agent.WorkspaceID))
	h.publish(protocol.EventAgentStatus, uuidToString(agent.WorkspaceID), actorType, actorID, map[string]any{"agent_id": uuidToString(agent.ID), "skills": resp})
	writeJSON(w, http.StatusOK, resp)
}
