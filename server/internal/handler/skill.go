package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	skillpkg "github.com/multica-ai/multica/server/internal/skill"
	"github.com/multica-ai/multica/server/internal/skillbundle"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// sanitizeNullBytes makes a string safe for a PostgreSQL TEXT column.
//
// Two failure modes covered:
//   - Embedded NUL (0x00) — PG rejects with SQLSTATE 22021. Removed.
//   - Other invalid-UTF-8 byte sequences (e.g. 0x91 = Windows-1252 smart
//     quote, which crashed agent-template import of skills containing
//     Windows-encoded prose). `strings.ToValidUTF8` drops them.
//
// Name is kept for compatibility with the many call sites; the behaviour
// is a strict superset of the original.
func sanitizeNullBytes(s string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "")
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

type SkillSearchCandidateResponse struct {
	Name         string  `json:"name"`
	URL          string  `json:"url"`
	Source       string  `json:"source"`
	Repo         *string `json:"repo"`
	InstallCount *int64  `json:"install_count"`
	GitHubStars  *int64  `json:"github_stars"`
	Description  string  `json:"description"`
}

type SkillWithFilesResponse struct {
	SkillResponse
	Files []SkillFileResponse `json:"files"`
}

type ExistingSkillIdentity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func writeSkillImportDuplicateConflict(w http.ResponseWriter, existing ExistingSkillIdentity) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":          "a skill with this name already exists",
		"existing_skill": existing,
	})
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

func (h *Handler) existingSkillIdentityByName(ctx context.Context, workspaceID pgtype.UUID, name string) (ExistingSkillIdentity, bool, error) {
	skill, err := h.Queries.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{
		WorkspaceID: workspaceID,
		Name:        name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ExistingSkillIdentity{}, false, nil
		}
		return ExistingSkillIdentity{}, false, err
	}
	return ExistingSkillIdentity{ID: uuidToString(skill.ID), Name: skill.Name}, true, nil
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
	Overwrite   bool                     `json:"overwrite,omitempty"`
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

type AddAgentSkillsRequest struct {
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

	skillUUID, ok := parseUUIDOrBadRequest(w, id, "skill id")
	if !ok {
		return db.Skill{}, false
	}

	skill, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
		ID:          skillUUID,
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

func (h *Handler) SearchSkills(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	candidates, err := searchClawHubSkills(httpClient, query)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"code":  "upstream_unavailable",
			"error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, candidates)
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
			// SKILL.md is reserved for the primary skill content (skill.Content).
			if skillpkg.IsReservedContentPath(f.Path) {
				continue
			}
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

	if err := h.Queries.DeleteSkill(r.Context(), db.DeleteSkillParams{
		ID:          skill.ID,
		WorkspaceID: skill.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skill")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(skill.WorkspaceID))
	h.publish(protocol.EventSkillDeleted, uuidToString(skill.WorkspaceID), actorType, actorID, map[string]any{"skill_id": uuidToString(skill.ID)})
	w.WriteHeader(http.StatusNoContent)
}

// --- Skill import ---

type ImportSkillRequest struct {
	URL        string `json:"url"`
	GiteeToken string `json:"gitee_token,omitempty"`
	Overwrite  bool   `json:"overwrite,omitempty"`
}

type DiscoverImportSkillsRequest struct {
	URL        string `json:"url"`
	GiteeToken string `json:"gitee_token,omitempty"`
}

type DiscoveredImportSkill struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Content     string                   `json:"content"`
	Config      any                      `json:"config,omitempty"`
	Files       []CreateSkillFileRequest `json:"files,omitempty"`
	SourcePath  string                   `json:"source_path"`
	SourceURL   string                   `json:"source_url"`
}

type DiscoverImportSkillsResponse struct {
	Skills []DiscoveredImportSkill `json:"skills"`
}

// Per-import bundle limits. These mirror the local-runtime importer so that
// URL imports cannot smuggle in payloads that the rest of the stack would
// reject. fetchRawFile enforces the per-file cap; importedSkill.addFile
// enforces the bundle-wide caps.
const (
	maxImportFileSize  = skillbundle.MaxFileSize
	maxImportTotalSize = skillbundle.MaxBundleSize
	maxImportFileCount = skillbundle.MaxFileCount
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
//
// Binary files (images, fonts, archives) are silently skipped: their bytes
// can't survive a PG TEXT column (SQLSTATE 22021), and they're reference
// assets the agent never reads as text anyway. Logging the skip leaves a
// breadcrumb if a user expected one of these to import.
func (s *importedSkill) addFile(path, content string) error {
	normalized, ok := skillbundle.NormalizePath(path)
	if !ok || skillbundle.ShouldSkipFile(normalized) {
		slog.Info("skill import: skipping supporting file", "path", path, "size", len(content))
		return nil
	}
	if len(s.files) >= maxImportFileCount {
		return fmt.Errorf("%w: import bundle exceeds %d file limit", errImportCapExceeded, maxImportFileCount)
	}
	if s.bundleSize+len(content) > maxImportTotalSize {
		return fmt.Errorf("%w: import bundle exceeds %d byte limit", errImportCapExceeded, maxImportTotalSize)
	}
	s.bundleSize += len(content)
	s.files = append(s.files, importedFile{path: normalized, content: content})
	return nil
}

func (s *importedSkill) createFileRequests() []CreateSkillFileRequest {
	files := make([]CreateSkillFileRequest, 0, len(s.files))
	for _, f := range s.files {
		path, ok := skillbundle.NormalizePath(f.path)
		if !ok || skillbundle.ShouldSkipFile(path) {
			continue
		}
		files = append(files, CreateSkillFileRequest{
			Path:    path,
			Content: f.content,
		})
	}
	return files
}

func (s *importedSkill) createRequest(overwrite bool) CreateSkillRequest {
	config := map[string]any{}
	if s.origin != nil {
		config["origin"] = s.origin
	}
	return CreateSkillRequest{
		Name:        s.name,
		Description: s.description,
		Content:     s.content,
		Config:      config,
		Files:       s.createFileRequests(),
		Overwrite:   overwrite,
	}
}

func (s *importedSkill) discoveredResponse() DiscoveredImportSkill {
	req := s.createRequest(false)
	sourcePath, _ := s.origin["path"].(string)
	sourceURL, _ := s.origin["source_url"].(string)
	if sourcePath == "" {
		sourcePath = "SKILL.md"
	} else {
		sourcePath += "/SKILL.md"
	}
	return DiscoveredImportSkill{
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Config:      req.Config,
		Files:       req.Files,
		SourcePath:  sourcePath,
		SourceURL:   sourceURL,
	}
}

func (s *importedSkill) discoveredMetadata(sourceURL string) DiscoveredImportSkill {
	sourcePath, _ := s.origin["path"].(string)
	if sourcePath == "" {
		sourcePath = "SKILL.md"
	} else {
		sourcePath += "/SKILL.md"
	}
	config := map[string]any{}
	if s.origin != nil {
		config["origin"] = s.origin
	}
	return DiscoveredImportSkill{
		Name:        s.name,
		Description: s.description,
		Content:     s.content,
		Config:      config,
		SourcePath:  sourcePath,
		SourceURL:   sourceURL,
	}
}

// --- ClawHub types ---

var clawHubAPIBase = "https://clawhub.ai/api/v1"

const clawHubSearchStatsLimit = 10

type clawhubSearchResponse struct {
	Results []clawhubSearchResult `json:"results"`
}

type clawhubSearchResult struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Summary     string `json:"summary"`
	OwnerHandle string `json:"ownerHandle"`
}

type clawhubSkillStats struct {
	InstallsAllTime int64 `json:"installsAllTime"`
	InstallsCurrent int64 `json:"installsCurrent"`
}

type clawhubGetSkillResponse struct {
	Skill         clawhubSkill          `json:"skill"`
	LatestVersion *clawhubLatestVersion `json:"latestVersion"`
}

type clawhubSkill struct {
	Slug        string            `json:"slug"`
	DisplayName string            `json:"displayName"`
	Summary     string            `json:"summary"`
	Tags        map[string]string `json:"tags"`
	Stats       clawhubSkillStats `json:"stats"`
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
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Type        string  `json:"type"` // "file" or "dir"
	URL         string  `json:"url"`
	DownloadURL string  `json:"download_url"`
	Content     *string `json:"content,omitempty"`
	Encoding    string  `json:"encoding,omitempty"`
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
	sourceGitee
)

// detectImportSource determines the source from a URL.
// Returns the source and a normalized URL (with scheme).
func detectImportSource(raw string) (importSource, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, "", fmt.Errorf("empty URL")
	}

	if sshURL, ok, err := normalizeSSHGitURL(raw); ok {
		if err != nil {
			return 0, "", err
		}
		raw = sshURL
	}

	normalized := raw
	if !strings.HasPrefix(normalized, "http://") && !strings.HasPrefix(normalized, "https://") {
		normalized = "https://" + normalized
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return 0, "", supportedImportURLFormatError(raw)
	}

	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "skills.sh" || host == "www.skills.sh":
		return sourceSkillsSh, normalized, nil
	case host == "clawhub.ai" || host == "www.clawhub.ai":
		return sourceClawHub, normalized, nil
	case host == "github.com" || host == "www.github.com":
		return sourceGitHub, normalized, nil
	case host == "gitee.com" || host == "www.gitee.com":
		return sourceGitee, normalized, nil
	default:
		// If no host (bare slug), default to clawhub
		if !strings.Contains(raw, "/") || !strings.Contains(raw, ".") {
			return sourceClawHub, raw, nil
		}
		return 0, "", supportedImportURLFormatError(raw)
	}
}

func normalizeSSHGitURL(raw string) (string, bool, error) {
	if !strings.HasPrefix(raw, "git@") {
		return "", false, nil
	}
	afterUser := strings.TrimPrefix(raw, "git@")
	host, repoPath, ok := strings.Cut(afterUser, ":")
	if !ok || host == "" || repoPath == "" {
		return "", true, supportedImportURLFormatError(raw)
	}
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "github.com", "gitee.com":
	default:
		return "", true, supportedImportURLFormatError(raw)
	}
	repoPath = strings.Trim(strings.TrimSpace(repoPath), "/")
	repoPath = strings.TrimSuffix(repoPath, ".git")
	parts := strings.Split(repoPath, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", true, supportedImportURLFormatError(raw)
	}
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return "https://" + host + "/" + strings.Join(parts, "/"), true, nil
}

func supportedImportURLFormatError(raw string) error {
	return fmt.Errorf("invalid URL format %q. Supported formats include https://github.com/owner/repo, https://gitee.com/owner/repo, git@github.com:owner/repo.git, and git@gitee.com:owner/repo.git", raw)
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

func searchClawHubSkills(httpClient *http.Client, query string) ([]SkillSearchCandidateResponse, error) {
	searchURL := clawHubAPIBase + "/search?q=" + url.QueryEscape(query)
	resp, err := httpClient.Get(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to reach ClawHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ClawHub search returned status %d", resp.StatusCode)
	}

	var searchResp clawhubSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to parse ClawHub search response")
	}

	candidates := make([]SkillSearchCandidateResponse, 0, len(searchResp.Results))
	for i, result := range searchResp.Results {
		if result.Slug == "" {
			continue
		}
		candidate := SkillSearchCandidateResponse{
			Name:        result.DisplayName,
			URL:         buildClawHubSkillURL(result.OwnerHandle, result.Slug),
			Source:      "clawhub.ai",
			Description: result.Summary,
		}
		if candidate.Name == "" {
			candidate.Name = result.Slug
		}
		if i < clawHubSearchStatsLimit {
			if count, ok := fetchClawHubInstallCount(httpClient, result.Slug); ok {
				candidate.InstallCount = &count
			}
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func buildClawHubSkillURL(ownerHandle, slug string) string {
	if ownerHandle == "" {
		return "https://clawhub.ai/" + url.PathEscape(slug)
	}
	return "https://clawhub.ai/" + url.PathEscape(ownerHandle) + "/" + url.PathEscape(slug)
}

func fetchClawHubInstallCount(httpClient *http.Client, slug string) (int64, bool) {
	detailURL := clawHubAPIBase + "/skills/" + url.PathEscape(slug)
	resp, err := httpClient.Get(detailURL)
	if err != nil {
		slog.Warn("clawhub search: failed to fetch skill details", "slug", slug, "error", err)
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("clawhub search: skill details returned non-200", "slug", slug, "status", resp.StatusCode)
		return 0, false
	}
	var detail clawhubGetSkillResponse
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		slog.Warn("clawhub search: failed to parse skill details", "slug", slug, "error", err)
		return 0, false
	}
	if detail.Skill.Stats.InstallsAllTime > 0 {
		return detail.Skill.Stats.InstallsAllTime, true
	}
	return detail.Skill.Stats.InstallsCurrent, true
}

func fetchFromClawHub(httpClient *http.Client, rawURL string) (*importedSkill, error) {
	slug, err := parseClawHubSlug(rawURL)
	if err != nil {
		return nil, err
	}

	apiBase := clawHubAPIBase

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
			if name, _ := skillpkg.ParseSkillFrontmatter(string(body)); name == skillName {
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
	name, description := skillpkg.ParseSkillFrontmatter(string(skillMdBody))
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
		if entry.Type == "file" {
			if skillbundle.ShouldSkipFile(entry.Name) {
				continue
			}
			*out = append(*out, entry)
		} else if entry.Type == "dir" {
			if skillbundle.ShouldSkipDir(entry.Name) {
				continue
			}
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

func shouldSkipSkillDiscoveryDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ".agents", ".claude", ".codex", ".opencode", ".openclaw", ".pi":
		return false
	default:
		return skillbundle.ShouldSkipDir(name)
	}
}

func importedSkillsFromGitHubPaths(httpClient *http.Client, rawURL string, spec githubSpec, rawPrefix string, skillPaths []string) ([]*importedSkill, error) {
	sort.Strings(skillPaths)
	result := make([]*importedSkill, 0, len(skillPaths))
	for _, skillPath := range skillPaths {
		body, err := fetchRawFile(httpClient, buildRawGitHubURL(rawPrefix, skillPath))
		if err != nil {
			return nil, fmt.Errorf("SKILL.md not found at %s in %s/%s@%s: %w",
				skillPath, spec.owner, spec.repo, spec.ref, err)
		}
		spec.skillDir = skillDirFromSkillFilePath(skillPath)
		imported, err := buildGitHubImportedSkill(httpClient, rawURL, spec, body)
		if err != nil {
			return nil, err
		}
		result = append(result, imported)
	}
	return result, nil
}

func fetchGitHubSkillList(httpClient *http.Client, rawURL string) ([]*importedSkill, error) {
	spec, err := parseGitHubURL(rawURL)
	if err != nil {
		return nil, err
	}
	if len(spec.refSegments) > 0 {
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
	if err == nil {
		imported, err := buildGitHubImportedSkill(httpClient, rawURL, spec, skillMdBody)
		if err != nil {
			return nil, err
		}
		return []*importedSkill{imported}, nil
	}

	skillPaths, listErr := listGitHubSkillMdPaths(httpClient, spec.owner, spec.repo, spec.skillDir, spec.ref)
	if listErr != nil {
		return nil, fmt.Errorf("failed to inspect %s/%s@%s for skill directories: %w", spec.owner, spec.repo, spec.ref, listErr)
	}
	if len(skillPaths) == 0 {
		if spec.skillDir == "" {
			return nil, fmt.Errorf("SKILL.md not found at the root of %s/%s@%s. For multi-skill repositories, point to a specific directory using github.com/%s/%s/tree/%s/<skill-dir>",
				spec.owner, spec.repo, spec.ref, spec.owner, spec.repo, spec.ref)
		}
		return nil, fmt.Errorf("SKILL.md not found at %s in %s/%s@%s: %w",
			skillMdPath, spec.owner, spec.repo, spec.ref, err)
	}
	return importedSkillsFromGitHubPaths(httpClient, rawURL, spec, rawPrefix, skillPaths)
}

func discoverGitHubSkillMetadata(httpClient *http.Client, rawURL string) ([]DiscoveredImportSkill, error) {
	spec, err := parseGitHubURL(rawURL)
	if err != nil {
		return nil, err
	}
	if len(spec.refSegments) > 0 {
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
	if body, err := fetchRawFile(httpClient, buildRawGitHubURL(rawPrefix, skillMdPath)); err == nil {
		return []DiscoveredImportSkill{buildGitHubDiscoveredSkill(rawURL, spec, body)}, nil
	}

	skillPaths, err := listGitHubSkillMdPaths(httpClient, spec.owner, spec.repo, spec.skillDir, spec.ref)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect %s/%s@%s for skill directories: %w", spec.owner, spec.repo, spec.ref, err)
	}
	if len(skillPaths) == 0 {
		if spec.skillDir == "" {
			return nil, fmt.Errorf("SKILL.md not found at the root of %s/%s@%s. For multi-skill repositories, point to a specific directory using github.com/%s/%s/tree/%s/<skill-dir>",
				spec.owner, spec.repo, spec.ref, spec.owner, spec.repo, spec.ref)
		}
		return nil, fmt.Errorf("SKILL.md not found at %s in %s/%s@%s",
			skillMdPath, spec.owner, spec.repo, spec.ref)
	}

	sort.Strings(skillPaths)
	discovered := make([]DiscoveredImportSkill, 0, len(skillPaths))
	for _, skillPath := range skillPaths {
		body, err := fetchRawFile(httpClient, buildRawGitHubURL(rawPrefix, skillPath))
		if err != nil {
			return nil, fmt.Errorf("SKILL.md not found at %s in %s/%s@%s: %w",
				skillPath, spec.owner, spec.repo, spec.ref, err)
		}
		candidate := spec
		candidate.skillDir = skillDirFromSkillFilePath(skillPath)
		discovered = append(discovered, buildGitHubDiscoveredSkill(rawURL, candidate, body))
	}
	return discovered, nil
}

func buildGitHubDiscoveredSkill(rawURL string, spec githubSpec, skillMdBody []byte) DiscoveredImportSkill {
	name, description := skillpkg.ParseSkillFrontmatter(string(skillMdBody))
	if name == "" {
		if spec.skillDir != "" {
			name = filepath.Base(spec.skillDir)
		} else {
			name = spec.repo
		}
	}
	sourceURL := buildGitWebTreeURL("github.com", spec.owner, spec.repo, spec.ref, spec.skillDir)
	imported := &importedSkill{
		name:        name,
		description: description,
		content:     string(skillMdBody),
		origin: map[string]any{
			"type":       "github",
			"source_url": sourceURL,
			"requested":  rawURL,
			"owner":      spec.owner,
			"repo":       spec.repo,
			"ref":        spec.ref,
			"path":       spec.skillDir,
		},
	}
	return imported.discoveredMetadata(sourceURL)
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
		if shouldSkipSkillDiscoveryDir(entry.Name) {
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
		name, _ := skillpkg.ParseSkillFrontmatter(string(body))
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
	skills, err := fetchGitHubSkillList(httpClient, rawURL)
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("SKILL.md not found")
	}
	if len(skills) > 1 {
		return nil, fmt.Errorf("multiple skills found in repository URL; use the discovery flow to choose which ones to import")
	}
	return skills[0], nil
}

func buildGitHubImportedSkill(httpClient *http.Client, rawURL string, spec githubSpec, skillMdBody []byte) (*importedSkill, error) {
	name, description := skillpkg.ParseSkillFrontmatter(string(skillMdBody))
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

// --- Gitee import ---
//
// Gitee's web URL structure mirrors GitHub's (/tree/{ref}/{path},
// /blob/{ref}/{path}/SKILL.md), and its V5 API exposes analogous endpoints
// under https://gitee.com/api/v5/repos/.... The raw file URL is served from
// the main domain: https://gitee.com/{owner}/{repo}/raw/{ref}/{path}.
//
// parseGitHubURL is reused since the path layout is identical; only the API
// host, raw URL scheme, and auth differ.

// doGiteeAPIGet performs a GET against a gitee.com/api/v5 URL, attaching the
// per-import token when supplied, otherwise falling back to GITEE_TOKEN.
func doGiteeAPIGet(httpClient *http.Client, apiURL, giteeToken string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	addGiteeAuthHeader(req, giteeToken)
	return httpClient.Do(req)
}

func addGiteeAuthHeader(req *http.Request, giteeToken string) {
	if req == nil {
		return
	}
	if token := effectiveGiteeToken(giteeToken); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
}

func effectiveGiteeToken(giteeToken string) string {
	if token := strings.TrimSpace(giteeToken); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GITEE_TOKEN"))
}

var errGiteeAuthRequired = errors.New("该仓库为私有仓库，请提供 Gitee 个人访问令牌后重试")

func giteeRepoAccessError(owner, repo string, statusCode int, giteeToken string) error {
	repoLabel := fmt.Sprintf("gitee.com/%s/%s", owner, repo)
	if effectiveGiteeToken(giteeToken) == "" {
		switch statusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return fmt.Errorf("%w: %s", errGiteeAuthRequired, repoLabel)
		}
	}
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("Gitee token does not have access to repository %s", repoLabel)
	case http.StatusNotFound:
		return fmt.Errorf("Gitee repository not found: %s", repoLabel)
	default:
		return fmt.Errorf("Gitee returned status %d for repository %s", statusCode, repoLabel)
	}
}

// fetchGiteeDefaultBranch returns the default branch of a Gitee repository.
// Falls back to "master" — Gitee's historical default — if the API call fails.
func fetchGiteeDefaultBranch(httpClient *http.Client, owner, repo, giteeToken string) (string, error) {
	apiURL := fmt.Sprintf("https://gitee.com/api/v5/repos/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo))
	resp, err := doGiteeAPIGet(httpClient, apiURL, giteeToken)
	if err != nil {
		return "master", nil
	}
	if resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return "", giteeRepoAccessError(owner, repo, resp.StatusCode, giteeToken)
		default:
			return "master", nil
		}
	}
	defer resp.Body.Close()

	var info githubRepoInfo // same JSON shape
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil || info.DefaultBranch == "" {
		return "master", nil
	}
	return info.DefaultBranch, nil
}

// giteeRefExists probes whether ref is a valid branch, tag, or commit in the
// Gitee repository. Uses the commits endpoint analogous to githubRefExists.
func giteeRefExists(httpClient *http.Client, owner, repo, ref, giteeToken string) (bool, error) {
	apiURL := fmt.Sprintf("https://gitee.com/api/v5/repos/%s/%s/commits/%s",
		url.PathEscape(owner), url.PathEscape(repo), escapeRefPath(ref))
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return false, err
	}
	addGiteeAuthHeader(req, giteeToken)
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
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, giteeRepoAccessError(owner, repo, resp.StatusCode, giteeToken)
	case http.StatusTooManyRequests:
		return false, errGitHubAPIBlocked
	default:
		return false, fmt.Errorf("gitee API returned status %d for ref %q", resp.StatusCode, ref)
	}
}

// resolveGiteeRefAndPath mirrors resolveGitHubRefAndPath for Gitee repos.
func resolveGiteeRefAndPath(httpClient *http.Client, spec *githubSpec, giteeToken string) error {
	if len(spec.refSegments) == 0 {
		return nil
	}
	tried := make([]string, 0, len(spec.refSegments))
	blocked := false
	for n := len(spec.refSegments); n >= 1; n-- {
		candidate := strings.Join(spec.refSegments[:n], "/")
		tried = append(tried, candidate)
		ok, err := giteeRefExists(httpClient, spec.owner, spec.repo, candidate, giteeToken)
		if errors.Is(err, errGitHubAPIBlocked) {
			blocked = true
			continue
		}
		if err != nil {
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
		slog.Warn("gitee import: ref resolution blocked by Gitee API; falling back to optimistic single-segment ref. Set GITEE_TOKEN to enable disambiguation of slash-bearing refs.",
			"owner", spec.owner, "repo", spec.repo, "tried", tried)
		return nil
	}
	if _, err := fetchGiteeDefaultBranch(httpClient, spec.owner, spec.repo, giteeToken); err != nil {
		return err
	}
	return fmt.Errorf("could not resolve ref in gitee.com/%s/%s URL — tried: %s. Make sure the branch, tag, or commit exists",
		spec.owner, spec.repo, strings.Join(tried, ", "))
}

func buildRawGiteeURL(owner, repo, ref, repoPath string) string {
	base := fmt.Sprintf("https://gitee.com/%s/%s/raw/%s",
		url.PathEscape(owner), url.PathEscape(repo), escapeRefPath(ref))
	parts := strings.Split(strings.Trim(repoPath, "/"), "/")
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(part))
	}
	if len(escaped) == 0 {
		return base
	}
	return base + "/" + strings.Join(escaped, "/")
}

func buildGiteeContentsURL(owner, repo, repoPath, ref string) string {
	base := fmt.Sprintf("https://gitee.com/api/v5/repos/%s/%s/contents",
		url.PathEscape(owner), url.PathEscape(repo))
	if repoPath == "" {
		return base + "?ref=" + url.QueryEscape(ref)
	}
	pathParts := strings.Split(strings.Trim(repoPath, "/"), "/")
	escapedParts := make([]string, 0, len(pathParts))
	for _, p := range pathParts {
		if p == "" {
			continue
		}
		escapedParts = append(escapedParts, url.PathEscape(p))
	}
	return base + "/" + strings.Join(escapedParts, "/") + "?ref=" + url.QueryEscape(ref)
}

func buildGitWebTreeURL(host, owner, repo, ref, repoPath string) string {
	base := fmt.Sprintf("https://%s/%s/%s/tree/%s",
		host, url.PathEscape(owner), url.PathEscape(repo), escapeRefPath(ref))
	if repoPath == "" {
		return base
	}
	return base + "/" + strings.TrimPrefix(buildRawGitHubURL("", repoPath), "/")
}

func fetchFromGitee(httpClient *http.Client, rawURL, giteeToken string) (*importedSkill, error) {
	skills, err := fetchGiteeSkillList(httpClient, rawURL, giteeToken)
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("SKILL.md not found")
	}
	if len(skills) > 1 {
		return nil, fmt.Errorf("multiple skills found in repository URL; use the discovery flow to choose which ones to import")
	}
	return skills[0], nil
}

func fetchGiteeSkillList(httpClient *http.Client, rawURL, giteeToken string) ([]*importedSkill, error) {
	// parseGitHubURL works because Gitee uses the identical path layout.
	spec, err := parseGitHubURL(rawURL)
	if err != nil {
		return nil, err
	}
	if len(spec.refSegments) > 0 {
		if err := resolveGiteeRefAndPath(httpClient, &spec, giteeToken); err != nil {
			return nil, err
		}
	}
	if spec.ref == "" {
		ref, err := fetchGiteeDefaultBranch(httpClient, spec.owner, spec.repo, giteeToken)
		if err != nil {
			return nil, err
		}
		spec.ref = ref
	}

	skillMdPath := "SKILL.md"
	if spec.skillDir != "" {
		skillMdPath = spec.skillDir + "/SKILL.md"
	}
	skillMdBody, err := fetchGiteeFile(httpClient, spec.owner, spec.repo, spec.ref, skillMdPath, giteeToken)
	if err == nil {
		imported, err := buildGiteeImportedSkill(httpClient, rawURL, spec, skillMdBody, giteeToken)
		if err != nil {
			return nil, err
		}
		return []*importedSkill{imported}, nil
	}

	skillPaths, listErr := listGiteeSkillMdPaths(httpClient, spec.owner, spec.repo, spec.skillDir, spec.ref, giteeToken)
	if listErr != nil {
		return nil, fmt.Errorf("failed to inspect %s/%s@%s for skill directories: %w", spec.owner, spec.repo, spec.ref, listErr)
	}
	if len(skillPaths) == 0 {
		if spec.skillDir == "" {
			return nil, fmt.Errorf("SKILL.md not found at the root of %s/%s@%s. For multi-skill repositories, point to a specific directory using gitee.com/%s/%s/tree/%s/<skill-dir>",
				spec.owner, spec.repo, spec.ref, spec.owner, spec.repo, spec.ref)
		}
		return nil, fmt.Errorf("SKILL.md not found at %s in %s/%s@%s: %w",
			skillMdPath, spec.owner, spec.repo, spec.ref, err)
	}
	return importedSkillsFromGiteePaths(httpClient, rawURL, spec, skillPaths, giteeToken)
}

func importedSkillsFromGiteePaths(httpClient *http.Client, rawURL string, spec githubSpec, skillPaths []string, giteeToken string) ([]*importedSkill, error) {
	sort.Strings(skillPaths)
	result := make([]*importedSkill, 0, len(skillPaths))
	for _, skillPath := range skillPaths {
		body, err := fetchGiteeFile(httpClient, spec.owner, spec.repo, spec.ref, skillPath, giteeToken)
		if err != nil {
			return nil, fmt.Errorf("SKILL.md not found at %s in %s/%s@%s: %w",
				skillPath, spec.owner, spec.repo, spec.ref, err)
		}
		spec.skillDir = skillDirFromSkillFilePath(skillPath)
		imported, err := buildGiteeImportedSkill(httpClient, rawURL, spec, body, giteeToken)
		if err != nil {
			return nil, err
		}
		result = append(result, imported)
	}
	return result, nil
}

func discoverGiteeSkillMetadata(httpClient *http.Client, rawURL, giteeToken string) ([]DiscoveredImportSkill, error) {
	spec, err := parseGitHubURL(rawURL)
	if err != nil {
		return nil, err
	}
	if len(spec.refSegments) > 0 {
		if err := resolveGiteeRefAndPath(httpClient, &spec, giteeToken); err != nil {
			return nil, err
		}
	}
	if spec.ref == "" {
		ref, err := fetchGiteeDefaultBranch(httpClient, spec.owner, spec.repo, giteeToken)
		if err != nil {
			return nil, err
		}
		spec.ref = ref
	}

	skillMdPath := "SKILL.md"
	if spec.skillDir != "" {
		skillMdPath = spec.skillDir + "/SKILL.md"
	}
	if body, err := fetchGiteeFile(httpClient, spec.owner, spec.repo, spec.ref, skillMdPath, giteeToken); err == nil {
		return []DiscoveredImportSkill{buildGiteeDiscoveredSkill(rawURL, spec, body)}, nil
	}

	skillPaths, err := listGiteeSkillMdPaths(httpClient, spec.owner, spec.repo, spec.skillDir, spec.ref, giteeToken)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect %s/%s@%s for skill directories: %w", spec.owner, spec.repo, spec.ref, err)
	}
	if len(skillPaths) == 0 {
		if spec.skillDir == "" {
			return nil, fmt.Errorf("SKILL.md not found at the root of %s/%s@%s. For multi-skill repositories, point to a specific directory using gitee.com/%s/%s/tree/%s/<skill-dir>",
				spec.owner, spec.repo, spec.ref, spec.owner, spec.repo, spec.ref)
		}
		return nil, fmt.Errorf("SKILL.md not found at %s in %s/%s@%s",
			skillMdPath, spec.owner, spec.repo, spec.ref)
	}

	sort.Strings(skillPaths)
	discovered := make([]DiscoveredImportSkill, 0, len(skillPaths))
	for _, skillPath := range skillPaths {
		body, err := fetchGiteeFile(httpClient, spec.owner, spec.repo, spec.ref, skillPath, giteeToken)
		if err != nil {
			return nil, fmt.Errorf("SKILL.md not found at %s in %s/%s@%s: %w",
				skillPath, spec.owner, spec.repo, spec.ref, err)
		}
		candidate := spec
		candidate.skillDir = skillDirFromSkillFilePath(skillPath)
		discovered = append(discovered, buildGiteeDiscoveredSkill(rawURL, candidate, body))
	}
	return discovered, nil
}

func buildGiteeDiscoveredSkill(rawURL string, spec githubSpec, skillMdBody []byte) DiscoveredImportSkill {
	name, description := skillpkg.ParseSkillFrontmatter(string(skillMdBody))
	if name == "" {
		if spec.skillDir != "" {
			name = filepath.Base(spec.skillDir)
		} else {
			name = spec.repo
		}
	}
	sourceURL := buildGitWebTreeURL("gitee.com", spec.owner, spec.repo, spec.ref, spec.skillDir)
	imported := &importedSkill{
		name:        name,
		description: description,
		content:     string(skillMdBody),
		origin: map[string]any{
			"type":       "gitee",
			"source_url": sourceURL,
			"requested":  rawURL,
			"owner":      spec.owner,
			"repo":       spec.repo,
			"ref":        spec.ref,
			"path":       spec.skillDir,
		},
	}
	return imported.discoveredMetadata(sourceURL)
}

func buildGiteeImportedSkill(httpClient *http.Client, rawURL string, spec githubSpec, skillMdBody []byte, giteeToken string) (*importedSkill, error) {
	name, description := skillpkg.ParseSkillFrontmatter(string(skillMdBody))
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
			"type":       "gitee",
			"source_url": rawURL,
			"owner":      spec.owner,
			"repo":       spec.repo,
			"ref":        spec.ref,
			"path":       spec.skillDir,
		},
	}

	// Fetch directory listing for supporting files.
	apiURL := buildGiteeContentsURL(spec.owner, spec.repo, spec.skillDir, spec.ref)
	dirResp, err := doGiteeAPIGet(httpClient, apiURL, giteeToken)
	if err != nil || dirResp.StatusCode != http.StatusOK {
		if dirResp != nil {
			dirResp.Body.Close()
		}
		return result, nil
	}
	defer dirResp.Body.Close()

	var entries []githubContentEntry // Gitee contents API returns the same shape
	if err := json.NewDecoder(dirResp.Body).Decode(&entries); err != nil {
		slog.Warn("gitee import: failed to decode top-level directory listing", "url", apiURL, "error", err)
		return result, nil
	}

	var allFiles []githubContentEntry
	collectGiteeFiles(httpClient, entries, &allFiles, spec.owner, spec.repo, spec.ref, giteeToken)

	basePath := ""
	if spec.skillDir != "" {
		basePath = spec.skillDir + "/"
	}
	for _, entry := range allFiles {
		body, err := fetchGiteeFile(httpClient, spec.owner, spec.repo, spec.ref, entry.Path, giteeToken)
		if err != nil {
			if isCapError(err) {
				return nil, fmt.Errorf("gitee import: %s: %w", entry.Path, err)
			}
			slog.Warn("gitee import: file download failed", "path", entry.Path, "error", err)
			continue
		}
		relPath := strings.TrimPrefix(entry.Path, basePath)
		if err := result.addFile(relPath, string(body)); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func listGiteeSkillMdPaths(httpClient *http.Client, owner, repo, repoPath, ref, giteeToken string) ([]string, error) {
	apiURL := buildGiteeContentsURL(owner, repo, repoPath, ref)
	resp, err := doGiteeAPIGet(httpClient, apiURL, giteeToken)
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
	collectGiteeSkillMdPaths(httpClient, entries, &paths, owner, repo, ref, giteeToken)
	return paths, nil
}

func collectGiteeSkillMdPaths(httpClient *http.Client, entries []githubContentEntry, out *[]string, owner, repo, ref, giteeToken string) {
	for _, entry := range entries {
		lower := strings.ToLower(entry.Name)
		switch entry.Type {
		case "file":
			if lower == "skill.md" {
				*out = append(*out, entry.Path)
			}
		case "dir":
			if shouldSkipSkillDiscoveryDir(entry.Name) {
				continue
			}
			subURL := buildGiteeContentsURL(owner, repo, entry.Path, ref)
			subResp, err := doGiteeAPIGet(httpClient, subURL, giteeToken)
			if err != nil || subResp.StatusCode != http.StatusOK {
				if subResp != nil {
					subResp.Body.Close()
				}
				slog.Warn("gitee import: failed to list skill metadata subdirectory", "path", entry.Path, "error", err)
				continue
			}
			var subEntries []githubContentEntry
			if err := json.NewDecoder(subResp.Body).Decode(&subEntries); err != nil {
				subResp.Body.Close()
				slog.Warn("gitee import: failed to decode skill metadata subdirectory", "path", entry.Path, "error", err)
				continue
			}
			subResp.Body.Close()
			collectGiteeSkillMdPaths(httpClient, subEntries, out, owner, repo, ref, giteeToken)
		}
	}
}

// collectGiteeFiles recursively collects file entries from a Gitee directory
// listing. Gitee's contents API has the same shape as GitHub's, but
// download_url may not always be present for private repos, so we build raw
// URLs ourselves instead.
func collectGiteeFiles(httpClient *http.Client, entries []githubContentEntry, out *[]githubContentEntry, owner, repo, ref, giteeToken string) {
	for _, entry := range entries {
		switch entry.Type {
		case "file":
			if skillbundle.ShouldSkipFile(entry.Name) {
				continue
			}
			*out = append(*out, entry)
		case "dir":
			if skillbundle.ShouldSkipDir(entry.Name) {
				continue
			}
			subURL := buildGiteeContentsURL(owner, repo, entry.Path, ref)
			subResp, err := doGiteeAPIGet(httpClient, subURL, giteeToken)
			if err != nil || subResp.StatusCode != http.StatusOK {
				if subResp != nil {
					subResp.Body.Close()
				}
				slog.Warn("gitee import: failed to list subdirectory", "path", entry.Path, "error", err)
				continue
			}
			var subEntries []githubContentEntry
			if err := json.NewDecoder(subResp.Body).Decode(&subEntries); err != nil {
				subResp.Body.Close()
				slog.Warn("gitee import: failed to decode subdirectory listing", "path", entry.Path, "error", err)
				continue
			}
			subResp.Body.Close()
			collectGiteeFiles(httpClient, subEntries, out, owner, repo, ref, giteeToken)
		}
	}
}

func fetchGiteeFile(httpClient *http.Client, owner, repo, ref, repoPath, giteeToken string) ([]byte, error) {
	body, err := fetchGiteeFileFromContentsAPI(httpClient, owner, repo, ref, repoPath, giteeToken)
	if err == nil {
		return body, nil
	}
	if isCapError(err) || errors.Is(err, errGiteeAuthRequired) {
		return nil, err
	}
	rawBody, rawErr := fetchRawGiteeFile(httpClient, buildRawGiteeURL(owner, repo, ref, repoPath), giteeToken)
	if rawErr == nil {
		return rawBody, nil
	}
	return nil, rawErr
}

func fetchGiteeFileFromContentsAPI(httpClient *http.Client, owner, repo, ref, repoPath, giteeToken string) ([]byte, error) {
	apiURL := buildGiteeContentsURL(owner, repo, repoPath, ref)
	resp, err := doGiteeAPIGet(httpClient, apiURL, giteeToken)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, giteeRepoAccessError(owner, repo, resp.StatusCode, giteeToken)
	default:
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var entry githubContentEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, err
	}
	if entry.Content == nil {
		return nil, fmt.Errorf("gitee contents response for %s has no content", repoPath)
	}

	content := strings.NewReplacer("\n", "", "\r", "").Replace(*entry.Content)
	if content == "" {
		return []byte{}, nil
	}
	if entry.Encoding == "" || strings.EqualFold(entry.Encoding, "base64") {
		body, err := base64.StdEncoding.DecodeString(content)
		if err == nil {
			if len(body) > maxImportFileSize {
				return nil, fmt.Errorf("%w: file exceeds %d byte limit", errImportCapExceeded, maxImportFileSize)
			}
			return body, nil
		}
		if !strings.EqualFold(entry.Encoding, "base64") {
			if len(content) > maxImportFileSize {
				return nil, fmt.Errorf("%w: file exceeds %d byte limit", errImportCapExceeded, maxImportFileSize)
			}
			return []byte(content), nil
		}
		return nil, err
	}
	if len(content) > maxImportFileSize {
		return nil, fmt.Errorf("%w: file exceeds %d byte limit", errImportCapExceeded, maxImportFileSize)
	}
	return []byte(content), nil
}

// --- Shared helpers ---

// fetchRawFile downloads a URL and returns the body bytes. Returns an error
// if the response exceeds maxImportFileSize so we never silently truncate a
// half-downloaded skill file into the workspace.
func fetchRawFile(httpClient *http.Client, fileURL string) ([]byte, error) {
	return fetchRawFileWithHeaders(httpClient, fileURL, nil)
}

func fetchRawGiteeFile(httpClient *http.Client, fileURL, giteeToken string) ([]byte, error) {
	return fetchRawFileWithHeaders(httpClient, fileURL, func(req *http.Request) {
		addGiteeAuthHeader(req, giteeToken)
	})
}

func fetchRawFileWithHeaders(httpClient *http.Client, fileURL string, addHeaders func(*http.Request)) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	if addHeaders != nil {
		addHeaders(req)
	}
	resp, err := httpClient.Do(req)
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
	case sourceGitee:
		imported, err = fetchFromGitee(httpClient, normalized, req.GiteeToken)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Persist provenance into skill.config.origin so list/detail UI can show
	// "Imported from GitHub / ClawHub / Skills.sh" and link back to the source.
	createReq := imported.createRequest(false)

	input := skillCreateInput{
		WorkspaceID: workspaceUUID,
		CreatorID:   creatorUUID,
		Name:        createReq.Name,
		Description: createReq.Description,
		Content:     createReq.Content,
		Config:      createReq.Config,
		Files:       createReq.Files,
	}
	overwroteExisting := false
	resp, err := h.createSkillWithFiles(r.Context(), input)
	if err != nil && req.Overwrite && isUniqueViolation(err) {
		resp, err = h.overwriteSkillWithFiles(r.Context(), input)
		overwroteExisting = err == nil
	}
	if err != nil {
		if isUniqueViolation(err) {
			if existing, found, findErr := h.existingSkillIdentityByName(r.Context(), workspaceUUID, imported.name); findErr == nil && found {
				writeSkillImportDuplicateConflict(w, existing)
			} else {
				writeError(w, http.StatusConflict, "a skill with this name already exists")
			}
			return
		}
		if errors.Is(err, errSkillOverwriteForbidden) {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
		return
	}
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(skillImportEvent(overwroteExisting), workspaceID, actorType, actorID, map[string]any{"skill": resp})
	status := http.StatusCreated
	if overwroteExisting {
		status = http.StatusOK
	}
	writeJSON(w, status, resp)
}

func (h *Handler) DiscoverImportSkills(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	var req DiscoverImportSkillsRequest
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
	resp := DiscoverImportSkillsResponse{}
	switch source {
	case sourceClawHub:
		skill, err := fetchFromClawHub(httpClient, normalized)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		resp.Skills = []DiscoveredImportSkill{skill.discoveredResponse()}
	case sourceSkillsSh:
		skill, err := fetchFromSkillsSh(httpClient, normalized)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		resp.Skills = []DiscoveredImportSkill{skill.discoveredResponse()}
	case sourceGitHub:
		skills, err := discoverGitHubSkillMetadata(httpClient, normalized)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		resp.Skills = skills
	case sourceGitee:
		skills, err := discoverGiteeSkillMetadata(httpClient, normalized, req.GiteeToken)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		resp.Skills = skills
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Batch import ---

type BatchImportSkillsRequest struct {
	Skills []CreateSkillRequest `json:"skills"`
}

type BatchImportSkillsResponse struct {
	Created []SkillWithFilesResponse `json:"created"`
	Skipped []string                 `json:"skipped"`
}

func (h *Handler) BatchImportSkills(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req BatchImportSkillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Skills) == 0 {
		writeError(w, http.StatusBadRequest, "no skills provided")
		return
	}

	if len(req.Skills) > 100 {
		writeError(w, http.StatusBadRequest, "maximum 100 skills per batch")
		return
	}

	created := make([]SkillWithFilesResponse, 0)
	skipped := make([]string, 0)
	workspaceUUID := parseUUID(workspaceID)
	creatorUUID := parseUUID(creatorID)
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)

	for _, s := range req.Skills {
		if s.Name == "" {
			skipped = append(skipped, "(unnamed)")
			continue
		}

		files := make([]CreateSkillFileRequest, 0, len(s.Files))
		for _, f := range s.Files {
			path, ok := skillbundle.NormalizePath(f.Path)
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid file path: "+f.Path)
				return
			}
			if skillbundle.ShouldSkipFile(path) {
				continue
			}
			f.Path = path
			files = append(files, f)
		}

		input := skillCreateInput{
			WorkspaceID: workspaceUUID,
			CreatorID:   creatorUUID,
			Name:        s.Name,
			Description: s.Description,
			Content:     s.Content,
			Config:      s.Config,
			Files:       files,
		}
		overwroteExisting := false
		resp, err := h.createSkillWithFiles(r.Context(), input)
		if err != nil && isUniqueViolation(err) {
			if s.Overwrite {
				resp, err = h.overwriteSkillWithFiles(r.Context(), input)
				overwroteExisting = err == nil
			} else {
				skipped = append(skipped, s.Name)
				continue
			}
		}
		if err != nil {
			if isUniqueViolation(err) {
				skipped = append(skipped, s.Name)
				continue
			}
			if errors.Is(err, errSkillOverwriteForbidden) {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to import skill "+s.Name+": "+err.Error())
			return
		}

		created = append(created, resp)
		h.publish(skillImportEvent(overwroteExisting), workspaceID, actorType, actorID, map[string]any{"skill": resp})
	}

	writeJSON(w, http.StatusCreated, BatchImportSkillsResponse{
		Created: created,
		Skipped: skipped,
	})
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
	if skillpkg.IsReservedContentPath(req.Path) {
		writeError(w, http.StatusBadRequest, "SKILL.md is reserved for the primary skill content")
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
	if !h.validateAgentSkillIDsInWorkspace(w, r, agent, skillUUIDs) {
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

	h.writeUpdatedAgentSkills(w, r, agent)
}

func (h *Handler) AddAgentSkills(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req AddAgentSkillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	skillUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.SkillIDs, "skill_ids")
	if !ok {
		return
	}
	if !h.validateAgentSkillIDsInWorkspace(w, r, agent, skillUUIDs) {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
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

	h.writeUpdatedAgentSkills(w, r, agent)
}

func (h *Handler) validateAgentSkillIDsInWorkspace(w http.ResponseWriter, r *http.Request, agent db.Agent, skillUUIDs []pgtype.UUID) bool {
	seen := map[string]struct{}{}
	for _, skillID := range skillUUIDs {
		key := uuidToString(skillID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
			ID:          skillID,
			WorkspaceID: agent.WorkspaceID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "skill not found")
			return false
		}
	}
	return true
}

func (h *Handler) writeUpdatedAgentSkills(w http.ResponseWriter, r *http.Request, agent db.Agent) {
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
