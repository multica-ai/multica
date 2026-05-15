package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/agenttmpl"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// agentTemplates is the in-memory catalog loaded once at package init. We
// fail-fast here rather than at the first request: a malformed template ships
// in source, so it's a deploy-time defect, not a runtime one.
var agentTemplates *agenttmpl.Registry

func init() {
	reg, err := agenttmpl.Load()
	if err != nil {
		panic("agenttmpl: failed to load templates at startup: " + err.Error())
	}
	agentTemplates = reg
}

// --- Response shapes ---

// AgentTemplateSkillResponse is the per-skill payload returned in the picker
// list and detail. CachedName/CachedDescription let the UI render without an
// HTTP round-trip to upstream — they reflect the template author's snapshot.
type AgentTemplateSkillResponse struct {
	SourceURL         string `json:"source_url"`
	CachedName        string `json:"cached_name"`
	CachedDescription string `json:"cached_description"`
}

// AgentTemplateSummaryResponse is what `GET /api/agent-templates` returns
// per entry. Omits Instructions to keep the list payload small; the detail
// endpoint (or the create flow) loads the full template.
type AgentTemplateSummaryResponse struct {
	Slug        string                       `json:"slug"`
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Category    string                       `json:"category,omitempty"`
	Icon        string                       `json:"icon,omitempty"`
	Accent      string                       `json:"accent,omitempty"`
	Skills      []AgentTemplateSkillResponse `json:"skills"`
}

// AgentTemplateResponse is the detail variant — same as the summary plus the
// full Instructions block.
type AgentTemplateResponse struct {
	AgentTemplateSummaryResponse
	Instructions string `json:"instructions"`
}

func templateToSummary(t agenttmpl.Template) AgentTemplateSummaryResponse {
	skills := make([]AgentTemplateSkillResponse, 0, len(t.Skills))
	for _, s := range t.Skills {
		skills = append(skills, AgentTemplateSkillResponse{
			SourceURL:         s.SourceURL,
			CachedName:        s.CachedName,
			CachedDescription: s.CachedDescription,
		})
	}
	return AgentTemplateSummaryResponse{
		Slug:        t.Slug,
		Name:        t.Name,
		Description: t.Description,
		Category:    t.Category,
		Icon:        t.Icon,
		Accent:      t.Accent,
		Skills:      skills,
	}
}

func templateToDetail(t agenttmpl.Template) AgentTemplateResponse {
	return AgentTemplateResponse{
		AgentTemplateSummaryResponse: templateToSummary(t),
		Instructions:                 t.Instructions,
	}
}

// --- List + Get handlers ---

func (h *Handler) ListAgentTemplates(w http.ResponseWriter, r *http.Request) {
	tmpls := agentTemplates.List()
	resp := make([]AgentTemplateSummaryResponse, 0, len(tmpls))
	for _, t := range tmpls {
		resp = append(resp, templateToSummary(t))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetAgentTemplate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	t, ok := agentTemplates.Get(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	writeJSON(w, http.StatusOK, templateToDetail(t))
}

// --- Create-from-template handler ---

type CreateAgentFromTemplateRequest struct {
	TemplateSlug       string `json:"template_slug"`
	Name               string `json:"name"`
	RuntimeID          string `json:"runtime_id"`
	Model              string `json:"model,omitempty"`
	Visibility         string `json:"visibility,omitempty"`
	MaxConcurrentTasks int32  `json:"max_concurrent_tasks,omitempty"`
	// Optional overrides — let the picker UI customise the template before
	// creation without forcing a second round-trip to the detail page.
	// When nil/empty, the template's own values are used.
	Description     *string  `json:"description,omitempty"`
	Instructions    *string  `json:"instructions,omitempty"`
	AvatarURL       *string  `json:"avatar_url,omitempty"`
	// Workspace skill IDs to attach **in addition to** the template's
	// skills. The merge dedupes against template skills automatically
	// (agent_skill INSERT uses ON CONFLICT DO NOTHING).
	ExtraSkillIDs []string `json:"extra_skill_ids,omitempty"`
}

type CreateAgentFromTemplateResponse struct {
	Agent            AgentResponse `json:"agent"`
	ImportedSkillIDs []string      `json:"imported_skill_ids"`
	ReusedSkillIDs   []string      `json:"reused_skill_ids"`
}

type fetchFailureResponse struct {
	Error      string   `json:"error"`
	FailedURLs []string `json:"failed_urls"`
}

func (h *Handler) CreateAgentFromTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	ownerID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateAgentFromTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	if req.MaxConcurrentTasks == 0 {
		req.MaxConcurrentTasks = 6
	}

	tmpl, found := agentTemplates.Get(req.TemplateSlug)
	if !found {
		writeError(w, http.StatusBadRequest, "template not found: "+req.TemplateSlug)
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}

	// Runtime validation reproduces the gating done by CreateAgent
	// (handler/agent.go) — keep the two paths in sync. Done before fetch so
	// we don't waste GitHub API calls for a request that's going to 403.
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can create agents on it")
		return
	}

	slog.Info("agent-template create: request received",
		append(logger.RequestAttrs(r),
			"template_slug", tmpl.Slug,
			"workspace_id", workspaceID,
			"skill_url_count", len(tmpl.Skills),
		)...)

	// Pre-flight dedupe: each skill that already exists in the workspace
	// by `cached_name` can be reused WITHOUT fetching. This is the big win:
	// on the second create-from-the-same-template, fetch_count drops to 0
	// and the whole operation completes in <100ms instead of 20+ seconds.
	//
	// `cached_name` MUST match the upstream SKILL.md frontmatter `name`
	// (see template authoring docs in agenttmpl/types.go). When it doesn't,
	// the pre-flight misses and we fall back to the in-TX find-or-create
	// below — slower (one wasted fetch) but still correct.
	preReused := make(map[int]db.Skill, len(tmpl.Skills))
	toFetchRefs := make([]agenttmpl.TemplateSkillRef, 0, len(tmpl.Skills))
	toFetchOrigIdx := make([]int, 0, len(tmpl.Skills))
	for i, ref := range tmpl.Skills {
		if ref.CachedName == "" {
			toFetchRefs = append(toFetchRefs, ref)
			toFetchOrigIdx = append(toFetchOrigIdx, i)
			continue
		}
		existing, err := h.Queries.GetSkillByWorkspaceAndName(r.Context(), db.GetSkillByWorkspaceAndNameParams{
			WorkspaceID: wsUUID,
			Name:        ref.CachedName,
		})
		if err == nil {
			preReused[i] = existing
			slog.Info("agent-template create: pre-reuse hit (skipped fetch)",
				append(logger.RequestAttrs(r),
					"index", i,
					"cached_name", ref.CachedName,
					"existing_skill_id", uuidToString(existing.ID),
				)...)
			continue
		}
		toFetchRefs = append(toFetchRefs, ref)
		toFetchOrigIdx = append(toFetchOrigIdx, i)
	}

	// Fetch only the skills that aren't already in the workspace. fetched[j]
	// corresponds to toFetchRefs[j], whose original index is toFetchOrigIdx[j].
	httpClient := &http.Client{Timeout: 30 * time.Second}
	fetchStart := time.Now()
	var fetched []*importedSkill
	var failedURLs []string
	if len(toFetchRefs) > 0 {
		fetched, failedURLs = fetchTemplateSkillsParallel(httpClient, toFetchRefs)
	}
	slog.Info("agent-template create: fetch phase done",
		append(logger.RequestAttrs(r),
			"template_slug", tmpl.Slug,
			"fetch_duration_ms", time.Since(fetchStart).Milliseconds(),
			"pre_reused_count", len(preReused),
			"fetched_count", len(toFetchRefs)-len(failedURLs),
			"fail_count", len(failedURLs),
			"failed_urls", failedURLs,
		)...)
	if len(failedURLs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, fetchFailureResponse{
			Error:      "one or more skill sources are unavailable",
			FailedURLs: failedURLs,
		})
		return
	}

	// Build a per-original-index lookup so we can iterate tmpl.Skills in
	// order below without fiddling with toFetch* slices.
	fetchedByOrigIdx := make(map[int]*importedSkill, len(fetched))
	for j, imp := range fetched {
		fetchedByOrigIdx[toFetchOrigIdx[j]] = imp
	}

	creatorUUID := parseUUID(ownerID)
	isFirstAgent := false
	if existing, listErr := h.Queries.ListAgents(r.Context(), wsUUID); listErr == nil {
		isFirstAgent = len(existing) == 0
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin tx: "+err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	importedIDs := make([]string, 0, len(tmpl.Skills))
	reusedIDs := make([]string, 0, len(tmpl.Skills))
	allSkillIDs := make([]pgtype.UUID, 0, len(tmpl.Skills))

	for i, ref := range tmpl.Skills {
		// Pre-flight hit: reuse the workspace's existing skill ID without
		// any further fetch or DB work.
		if existing, ok := preReused[i]; ok {
			allSkillIDs = append(allSkillIDs, existing.ID)
			reusedIDs = append(reusedIDs, uuidToString(existing.ID))
			continue
		}

		imp := fetchedByOrigIdx[i]
		if imp == nil {
			// Defensive — shouldn't happen since we already checked failedURLs.
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("internal: missing fetch result for skill index %d", i))
			return
		}

		// Second-chance dedupe by ACTUAL frontmatter name. Catches the case
		// where the template author's cached_name drifted from the upstream
		// frontmatter `name` — pre-flight misses but the workspace still has
		// the skill under its real name, and we want to reuse not duplicate.
		existing, err := qtx.GetSkillByWorkspaceAndName(r.Context(), db.GetSkillByWorkspaceAndNameParams{
			WorkspaceID: wsUUID,
			Name:        imp.name,
		})
		if err == nil {
			slog.Info("agent-template create: reusing existing skill (frontmatter-name match, cached_name drifted)",
				append(logger.RequestAttrs(r),
					"index", i,
					"frontmatter_name", imp.name,
					"cached_name", ref.CachedName,
					"existing_skill_id", uuidToString(existing.ID),
				)...)
			allSkillIDs = append(allSkillIDs, existing.ID)
			reusedIDs = append(reusedIDs, uuidToString(existing.ID))
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("agent-template create: lookup existing skill failed",
				append(logger.RequestAttrs(r),
					"index", i,
					"name", imp.name,
					"error", err,
				)...)
			writeError(w, http.StatusInternalServerError, "lookup existing skill failed: "+err.Error())
			return
		}

		slog.Info("agent-template create: inserting new skill",
			append(logger.RequestAttrs(r),
				"index", i,
				"name", imp.name,
				"file_count", len(imp.files),
			)...)

		files := make([]CreateSkillFileRequest, 0, len(imp.files))
		for _, f := range imp.files {
			if !validateFilePath(f.path) {
				continue
			}
			files = append(files, CreateSkillFileRequest{Path: f.path, Content: f.content})
		}

		// Record provenance: which template seeded this skill, plus the
		// upstream URL. Mirrors handler/skill.go:ImportSkill's origin block
		// so the skill detail page renders a consistent "imported from …"
		// chip regardless of entry point.
		origin := map[string]any{
			"type":          "agent_template",
			"template_slug": tmpl.Slug,
			"source_url":    ref.SourceURL,
		}
		// Preserve the upstream-specific origin fields (owner/repo/...) when
		// the fetcher returned them — useful for a "Open on GitHub" link.
		if imp.origin != nil {
			for k, v := range imp.origin {
				if _, exists := origin[k]; !exists {
					origin[k] = v
				}
			}
		}

		created, err := createSkillWithFilesInTx(r.Context(), qtx, skillCreateInput{
			WorkspaceID: wsUUID,
			CreatorID:   creatorUUID,
			Name:        imp.name,
			Description: imp.description,
			Content:     imp.content,
			Config:      map[string]any{"origin": origin},
			Files:       files,
		})
		if err != nil {
			// Full PG error in the log so we can tell unique-constraint from
			// other failures without guessing.
			slog.Error("agent-template create: failed to create skill",
				append(logger.RequestAttrs(r),
					"index", i,
					"name", imp.name,
					"workspace_id", workspaceID,
					"error", err,
					"is_unique_violation", isUniqueViolation(err),
				)...)
			writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
			return
		}
		allSkillIDs = append(allSkillIDs, parseUUID(created.ID))
		importedIDs = append(importedIDs, created.ID)
	}

	rc, _ := json.Marshal(map[string]any{})
	ce, _ := json.Marshal(map[string]string{})
	ca, _ := json.Marshal([]string{})

	// Apply optional overrides — nil means "use template default".
	description := tmpl.Description
	if req.Description != nil {
		description = *req.Description
	}
	instructions := tmpl.Instructions
	if req.Instructions != nil {
		instructions = *req.Instructions
	}
	avatarURL := pgtype.Text{}
	if req.AvatarURL != nil && *req.AvatarURL != "" {
		avatarURL = pgtype.Text{String: *req.AvatarURL, Valid: true}
	}

	agent, err := qtx.CreateAgent(r.Context(), db.CreateAgentParams{
		WorkspaceID:        wsUUID,
		Name:               req.Name,
		Description:        description,
		Instructions:       instructions,
		AvatarUrl:          avatarURL,
		RuntimeMode:        runtime.RuntimeMode,
		RuntimeConfig:      rc,
		RuntimeID:          runtime.ID,
		Visibility:         req.Visibility,
		MaxConcurrentTasks: req.MaxConcurrentTasks,
		OwnerID:            creatorUUID,
		CustomEnv:          ce,
		CustomArgs:         ca,
		McpConfig:          nil,
		Model:              pgtype.Text{String: req.Model, Valid: req.Model != ""},
	})
	if err != nil {
		// Mirror handler/agent.go:CreateAgent: when the duplicate is the
		// agent name UNIQUE in this workspace, return 409 with a clear
		// message instead of leaking the raw PG error as 500. Frontend
		// already knows how to render 409 from the manual create path.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "agent_workspace_name_unique" {
			slog.Info("agent-template create: agent name conflict",
				append(logger.RequestAttrs(r),
					"agent_name", req.Name,
					"workspace_id", workspaceID,
				)...)
			writeError(w, http.StatusConflict, fmt.Sprintf("an agent named %q already exists in this workspace", req.Name))
			return
		}
		slog.Error("agent-template create: failed to create agent",
			append(logger.RequestAttrs(r),
				"agent_name", req.Name,
				"workspace_id", workspaceID,
				"error", err,
				"is_unique_violation", isUniqueViolation(err),
			)...)
		writeError(w, http.StatusInternalServerError, "failed to create agent: "+err.Error())
		return
	}

	for idx, skillID := range allSkillIDs {
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: agent.ID,
			SkillID: skillID,
		}); err != nil {
			slog.Error("agent-template create: failed to attach skill",
				append(logger.RequestAttrs(r),
					"agent_id", uuidToString(agent.ID),
					"skill_id", uuidToString(skillID),
					"skill_index", idx,
					"error", err,
				)...)
			writeError(w, http.StatusInternalServerError, "failed to attach skill: "+err.Error())
			return
		}
	}

	// Attach user-supplied extra skills (selected in the create dialog
	// alongside the template). AddAgentSkill uses ON CONFLICT DO NOTHING,
	// so duplicates with template-imported skills are harmless.
	for _, raw := range req.ExtraSkillIDs {
		extraUUID, perr := util.ParseUUID(raw)
		if perr != nil {
			// Skip malformed IDs but don't fail the whole create — the agent
			// is otherwise valid. Logged so the bad ID can be traced.
			slog.Warn("agent-template create: skipping malformed extra_skill_id",
				append(logger.RequestAttrs(r), "raw", raw, "error", perr)...)
			continue
		}
		// Verify the skill belongs to this workspace before attaching;
		// otherwise a malicious client could attach a skill from another
		// workspace by guessing UUIDs.
		owned, qerr := qtx.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
			ID: extraUUID, WorkspaceID: wsUUID,
		})
		if qerr != nil {
			slog.Warn("agent-template create: skipping cross-workspace extra_skill_id",
				append(logger.RequestAttrs(r), "skill_id", raw, "error", qerr)...)
			continue
		}
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: agent.ID,
			SkillID: owned.ID,
		}); err != nil {
			slog.Error("agent-template create: failed to attach extra skill",
				append(logger.RequestAttrs(r), "skill_id", raw, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to attach skill: "+err.Error())
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("agent-template create: commit failed",
			append(logger.RequestAttrs(r),
				"agent_id", uuidToString(agent.ID),
				"error", err,
			)...)
		writeError(w, http.StatusInternalServerError, "commit failed: "+err.Error())
		return
	}

	if runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(r.Context(), agent.ID)
		agent, _ = h.Queries.GetAgent(r.Context(), agent.ID)
	}

	resp := agentToResponse(agent)
	actorType, actorID := h.resolveActor(r, ownerID, workspaceID)
	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": resp})

	h.Analytics.Capture(analytics.AgentCreated(
		ownerID,
		workspaceID,
		uuidToString(agent.ID),
		runtime.Provider,
		runtime.RuntimeMode,
		tmpl.Slug, // template slug doubles as the analytics template field
		isFirstAgent,
	))

	slog.Info("agent created from template",
		append(logger.RequestAttrs(r),
			"agent_id", uuidToString(agent.ID),
			"template_slug", tmpl.Slug,
			"imported_skill_count", len(importedIDs),
			"reused_skill_count", len(reusedIDs),
		)...)

	writeJSON(w, http.StatusCreated, CreateAgentFromTemplateResponse{
		Agent:            resp,
		ImportedSkillIDs: importedIDs,
		ReusedSkillIDs:   reusedIDs,
	})
}

// --- Parallel skill fetch ---

type templateFetchResult struct {
	index    int
	imported *importedSkill
	url      string
	err      error
}

// fetchTemplateSkillsParallel resolves every template skill ref into an
// importedSkill, in parallel. Returns the imports in input order; failed_urls
// is non-nil iff any fetch failed. Logs per-URL timing so we can spot which
// upstream is the long pole in a slow request.
func fetchTemplateSkillsParallel(client *http.Client, refs []agenttmpl.TemplateSkillRef) ([]*importedSkill, []string) {
	results := make(chan templateFetchResult, len(refs))
	var wg sync.WaitGroup
	for i, ref := range refs {
		wg.Add(1)
		go func(i int, ref agenttmpl.TemplateSkillRef) {
			defer wg.Done()
			start := time.Now()
			slog.Info("agent-template fetch: start", "index", i, "source_url", ref.SourceURL)
			imp, err := fetchSkillFromURL(client, ref.SourceURL)
			elapsedMs := time.Since(start).Milliseconds()
			if err != nil {
				slog.Warn("agent-template fetch: failed",
					"index", i,
					"source_url", ref.SourceURL,
					"duration_ms", elapsedMs,
					"error", err,
				)
			} else {
				resolvedName := ""
				fileCount := 0
				if imp != nil {
					resolvedName = imp.name
					fileCount = len(imp.files)
				}
				slog.Info("agent-template fetch: done",
					"index", i,
					"source_url", ref.SourceURL,
					"duration_ms", elapsedMs,
					"resolved_name", resolvedName,
					"file_count", fileCount,
				)
			}
			results <- templateFetchResult{index: i, imported: imp, url: ref.SourceURL, err: err}
		}(i, ref)
	}
	wg.Wait()
	close(results)

	imports := make([]*importedSkill, len(refs))
	var failed []string
	for r := range results {
		if r.err != nil {
			failed = append(failed, r.url)
			continue
		}
		imports[r.index] = r.imported
	}
	return imports, failed
}

// fetchSkillFromURL dispatches to the right upstream fetcher based on URL.
// Mirrors the switch inside ImportSkill (skill.go:1566) so both entry points
// stay in sync.
func fetchSkillFromURL(client *http.Client, rawURL string) (*importedSkill, error) {
	source, normalized, err := detectImportSource(rawURL)
	if err != nil {
		return nil, err
	}
	switch source {
	case sourceClawHub:
		return fetchFromClawHub(client, normalized)
	case sourceSkillsSh:
		return fetchFromSkillsSh(client, normalized)
	case sourceGitHub:
		return fetchFromGitHub(client, normalized)
	}
	return nil, fmt.Errorf("unknown import source for %s", rawURL)
}

