package handler

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/engineering"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Engineering context is optional: any failed read leaves the original project
// description intact, without blocking task claims or writing derived text back.
func (h *Handler) appendEngineeringProjectDescription(ctx context.Context, workspaceID, projectID pgtype.UUID, baseDescription string) string {
	config, err := h.Queries.LabrastroGetEngineering(ctx, db.LabrastroGetEngineeringParams{WorkspaceID: workspaceID, ProjectID: projectID})
	if err != nil || !config.Enabled {
		return baseDescription
	}
	docs, err := h.Queries.LabrastroListBaselineDocs(ctx, db.LabrastroListBaselineDocsParams{WorkspaceID: workspaceID, ProjectID: projectID})
	if err != nil {
		return baseDescription
	}
	var stages []engineeringStage
	if err := json.Unmarshal(config.Stages, &stages); err != nil {
		return baseDescription
	}
	stageLine := fmt.Sprintf("当前主攻：Stage %d", config.CurrentStage)
	for _, stage := range stages {
		if stage.Stage == int64(config.CurrentStage) {
			stageLine += " · " + engineeringContextText(stage.Title, 60)
			if goal := engineeringContextText(stage.Goal, 280); goal != "" {
				stageLine += "；目标：" + goal
			}
			break
		}
	}
	lines := []string{"### 工程管理", stageLine}
	if mode := engineeringContextText(config.ModeSummary, 280); mode != "" {
		lines = append(lines, "推进方针："+mode)
	}
	var adopted, candidates []string
	for _, chapter := range engineering.Chapters {
		doc := baselineDoc(docs, chapter)
		if doc.CandidateRevisionID.Valid {
			candidates = append(candidates, chapter)
		}
		if !doc.AdoptedRevisionID.Valid {
			continue
		}
		revision, err := h.Queries.LabrastroGetBaselineRevision(ctx, db.LabrastroGetBaselineRevisionParams{WorkspaceID: workspaceID, ProjectID: projectID, ID: doc.AdoptedRevisionID})
		if err != nil {
			return baseDescription
		}
		sections, err := engineering.Parse(chapter, revision.ContentMarkdown)
		if err != nil {
			return baseDescription
		}
		var titles []string
		for _, section := range sections {
			if section.Title != "" {
				titles = append(titles, section.Title)
			}
		}
		outline := engineeringContextText(strings.Join(titles, "；"), 240)
		if outline == "" {
			outline = "无小节标题"
		}
		adopted = append(adopted, fmt.Sprintf("- %s v%d：%s", chapter, revision.RevisionNumber, outline))
	}
	if len(adopted) == 0 {
		lines = append(lines, "当前尚未建立生效基线")
	} else {
		lines = append(lines, "已生效基线（adopted）：")
		lines = append(lines, adopted...)
	}
	if len(candidates) > 0 {
		lines = append(lines, "待审候选（candidate，尚未生效）："+strings.Join(candidates, "、"))
	}
	lines = append(lines,
		fmt.Sprintf("本项目已启用工程管理（Project ID: %s）。在规划、拆单、编码或审阅前，请运行 `multica project baseline get %s` 查阅完整项目基线与契约。", uuidToString(projectID), uuidToString(projectID)),
		"如已绑定 strixnova-* 系列工程 skill，请按对应 skill 规程调用相关能力。")
	summary := strings.Join(lines, "\n")
	if baseDescription == "" {
		return summary
	}
	return baseDescription + "\n\n" + summary
}

// Keep free-form configuration and headings on one bounded line per summary item.
func engineeringContextText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > limit {
		return string([]rune(value)[:limit-3]) + "..."
	}
	return value
}

func (h *Handler) RegisterLabrastroEngineeringRoutes(r chi.Router) {
	r.Get("/api/projects/{id}/engineering", h.LabrastroGetEngineering)
	r.With(RequireHumanActor).Patch("/api/projects/{id}/engineering", h.LabrastroPatchEngineering)
	r.Post("/api/projects/{id}/baseline/candidates", h.LabrastroProposeBaseline)
	r.Post("/api/projects/{id}/baseline/decisions", h.LabrastroDecideBaseline)
	r.Get("/api/projects/{id}/baseline/revisions", h.LabrastroBaselineHistory)
}

type engineeringError struct {
	status        int
	code, message string
}

func (e *engineeringError) Error() string { return e.code }
func baselineError(status int, code string) error {
	return &engineeringError{status, code, strings.ReplaceAll(code, "_", " ")}
}
func writeEngineeringError(w http.ResponseWriter, err error) {
	var e *engineeringError
	var invalid *engineering.Error
	switch {
	case errors.As(err, &e):
		writeErrorCode(w, e.status, e.code, e.message)
	case errors.As(err, &invalid):
		status := 422
		if invalid.Code == "invalid_request" {
			status = 400
		}
		writeErrorCode(w, status, invalid.Code, "invalid baseline content")
	case errors.Is(err, pgx.ErrNoRows):
		writeErrorCode(w, 404, "not_found", "project or revision not found")
	default:
		writeErrorCode(w, 500, "engineering_failed", "project engineering operation failed")
	}
}

func decodeEngineering(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil || len(raw) == 0 || raw[0] != '{' {
		writeErrorCode(w, 400, "invalid_request", "expected one JSON object")
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		writeErrorCode(w, 400, "invalid_request", "expected one JSON object")
		return false
	}
	fields := json.NewDecoder(bytes.NewReader(raw))
	fields.DisallowUnknownFields()
	if err := fields.Decode(out); err != nil {
		writeErrorCode(w, 400, "invalid_request", "invalid request body")
		return false
	}
	return true
}

type baselineActor struct {
	kind, credential string
	id, task         pgtype.UUID
}

func engineeringActor(r *http.Request) (baselineActor, error) {
	denied := baselineError(403, "baseline_credential_denied")
	identity, hasIdentity := auth.IdentityFromContext(r.Context())
	source := r.Header.Get("X-Actor-Source")
	if isMachineCredentialActor(r) {
		if source != "task_token" || !hasIdentity || identity.CredentialKind != "task" || identity.WorkspaceID != r.Header.Get("X-Workspace-ID") {
			return baselineActor{}, denied
		}
		agent, err := util.ParseUUID(identity.AgentID)
		if err != nil {
			return baselineActor{}, denied
		}
		task, err := util.ParseUUID(identity.TaskID)
		if err != nil {
			return baselineActor{}, denied
		}
		return baselineActor{"agent", "agent_task", agent, task}, nil
	}
	if source != "" {
		return baselineActor{}, denied
	}
	user, err := util.ParseUUID(r.Header.Get("X-User-ID"))
	if err != nil {
		return baselineActor{}, denied
	}
	credential := "session"
	if hasIdentity {
		if identity.CredentialKind != "pat" || identity.UserID != r.Header.Get("X-User-ID") {
			return baselineActor{}, denied
		}
		credential = "pat"
	}
	return baselineActor{"member", credential, user, pgtype.UUID{}}, nil
}

type engineeringState struct {
	tx          pgx.Tx
	q           *db.Queries
	project     db.Project
	member      db.Member
	config      db.ProjectEngineering
	issuePrefix string
}

// Hold the workspace lifetime before the project lock, matching teardown. The
// project lock also protects the missing-config/missing-doc first-write case.
func (h *Handler) engineeringState(w http.ResponseWriter, r *http.Request, write bool) (*engineeringState, bool) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return nil, false
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(project.WorkspaceID), "project not found")
	if !ok {
		return nil, false
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeEngineeringError(w, err)
		return nil, false
	}
	s := &engineeringState{tx: tx, q: h.Queries.WithTx(tx), project: project, member: member}
	fail := func(err error) (*engineeringState, bool) {
		tx.Rollback(r.Context())
		writeEngineeringError(w, err)
		return nil, false
	}
	if _, err = s.q.LockWorkspaceForChatSessionCreate(r.Context(), project.WorkspaceID); err != nil {
		return fail(err)
	}
	workspace, err := s.q.GetWorkspace(r.Context(), project.WorkspaceID)
	if err != nil {
		return fail(err)
	}
	s.issuePrefix = workspace.IssuePrefix
	if write {
		s.project, err = s.q.LabrastroLockEngineeringProject(r.Context(), db.LabrastroLockEngineeringProjectParams{WorkspaceID: project.WorkspaceID, ID: project.ID})
	} else {
		s.project, err = s.q.LabrastroReadEngineeringProject(r.Context(), db.LabrastroReadEngineeringProjectParams{WorkspaceID: project.WorkspaceID, ID: project.ID})
	}
	if err != nil {
		return fail(err)
	}
	s.member, err = s.q.LabrastroLockEngineeringMember(r.Context(), db.LabrastroLockEngineeringMemberParams{WorkspaceID: project.WorkspaceID, UserID: member.UserID})
	if err != nil {
		return fail(err)
	}
	s.config, err = s.q.LabrastroGetEngineering(r.Context(), db.LabrastroGetEngineeringParams{WorkspaceID: project.WorkspaceID, ProjectID: project.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		s.config = db.ProjectEngineering{WorkspaceID: project.WorkspaceID, ProjectID: project.ID, CurrentStage: 1, Stages: []byte("[]"), DeciderMemberIds: []pgtype.UUID{}}
		err = nil
	}
	if err != nil {
		return fail(err)
	}
	return s, true
}

func (s *engineeringState) isDecider() bool {
	if roleAllowed(s.member.Role, "owner", "admin") {
		return true
	}
	if len(s.config.DeciderMemberIds) > 0 {
		return slices.Contains(s.config.DeciderMemberIds, s.member.UserID)
	}
	return s.project.LeadType.String == "member" && s.project.LeadID == s.member.UserID
}
func (s *engineeringState) revision(ctx context.Context, id pgtype.UUID) (db.ProjectBaselineRevision, error) {
	if !id.Valid {
		return db.ProjectBaselineRevision{}, nil
	}
	return s.q.LabrastroGetBaselineRevision(ctx, db.LabrastroGetBaselineRevisionParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID, ID: id})
}
func (s *engineeringState) docs(ctx context.Context, lock bool) ([]db.ProjectBaselineDoc, error) {
	if lock {
		return s.q.LabrastroLockBaselineDocs(ctx, db.LabrastroLockBaselineDocsParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID})
	}
	return s.q.LabrastroListBaselineDocs(ctx, db.LabrastroListBaselineDocsParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID})
}
func baselineDoc(docs []db.ProjectBaselineDoc, chapter string) db.ProjectBaselineDoc {
	for _, d := range docs {
		if d.Chapter == chapter {
			return d
		}
	}
	return db.ProjectBaselineDoc{Chapter: chapter}
}

type baselineRevisionResponse struct {
	db.ProjectBaselineRevision
	AnchorsDiff json.RawMessage `json:"anchors_diff"`
}

func baselineRevision(r db.ProjectBaselineRevision) *baselineRevisionResponse {
	if !r.ID.Valid {
		return nil
	}
	return &baselineRevisionResponse{r, json.RawMessage(r.AnchorsDiff)}
}

type baselineChapterResponse struct {
	Chapter   string                    `json:"chapter"`
	Adopted   *baselineRevisionResponse `json:"adopted"`
	Candidate *baselineRevisionResponse `json:"candidate"`
}

func (s *engineeringState) chapters(ctx context.Context) ([]baselineChapterResponse, error) {
	docs, err := s.docs(ctx, false)
	if err != nil {
		return nil, err
	}
	chapters := make([]baselineChapterResponse, 0, 4)
	for _, chapter := range engineering.Chapters {
		d := baselineDoc(docs, chapter)
		adopted, err := s.revision(ctx, d.AdoptedRevisionID)
		if err != nil {
			return nil, err
		}
		candidate, err := s.revision(ctx, d.CandidateRevisionID)
		if err != nil {
			return nil, err
		}
		chapters = append(chapters, baselineChapterResponse{chapter, baselineRevision(adopted), baselineRevision(candidate)})
	}
	return chapters, nil
}
func (h *Handler) LabrastroGetEngineering(w http.ResponseWriter, r *http.Request) {
	s, ok := h.engineeringState(w, r, false)
	if !ok {
		return
	}
	defer s.tx.Rollback(r.Context())
	chapters, err := s.chapters(r.Context())
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"engineering": engineeringConfigResponse{s.config, json.RawMessage(s.config.Stages)}, "chapters": chapters})
}

type engineeringConfigResponse struct {
	db.ProjectEngineering
	Stages json.RawMessage `json:"stages"`
}

type engineeringStage struct {
	Stage int64  `json:"stage"`
	Title string `json:"title"`
	Goal  string `json:"goal"`
}

type patchEngineeringRequest struct {
	Stages *[]struct {
		Stage json.RawMessage `json:"stage"`
		Title json.RawMessage `json:"title"`
		Goal  json.RawMessage `json:"goal"`
	} `json:"stages"`
	Enabled          *bool     `json:"enabled"`
	CurrentStage     *int32    `json:"current_stage"`
	ModeSummary      *string   `json:"mode_summary"`
	DeciderMemberIDs *[]string `json:"decider_member_ids"`
}

func (h *Handler) LabrastroPatchEngineering(w http.ResponseWriter, r *http.Request) {
	actor, err := engineeringActor(r)
	if err != nil || actor.kind != "member" {
		writeError(w, 403, "this endpoint is only available to human actors")
		return
	}
	var req patchEngineeringRequest
	if !decodeEngineering(w, r, &req) {
		return
	}
	if req.CurrentStage != nil && *req.CurrentStage < 1 || req.ModeSummary != nil && utf8.RuneCountInString(*req.ModeSummary) > 280 {
		writeErrorCode(w, 422, "invalid_request", "invalid engineering configuration")
		return
	}
	var stages []byte
	if req.Stages != nil {
		normalized := make([]engineeringStage, 0, len(*req.Stages))
		seen := map[int64]bool{}
		if len(*req.Stages) > 50 {
			writeErrorCode(w, 422, "invalid_request", "stages must contain at most 50 entries")
			return
		}
		for _, item := range *req.Stages {
			var stage int64
			var title, goal string
			if json.Unmarshal(item.Stage, &stage) != nil || json.Unmarshal(item.Title, &title) != nil ||
				len(item.Goal) > 0 && (string(item.Goal) == "null" || json.Unmarshal(item.Goal, &goal) != nil) ||
				strings.ContainsAny(title+goal, "\r\n\x00") {
				writeErrorCode(w, 422, "invalid_request", "invalid stage number, title or goal")
				return
			}
			title, goal = strings.TrimSpace(title), strings.TrimSpace(goal)
			if stage < 1 || seen[stage] ||
				utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > 60 || utf8.RuneCountInString(goal) > 280 {
				writeErrorCode(w, 422, "invalid_request", "invalid stage number, title or goal")
				return
			}
			seen[stage] = true
			normalized = append(normalized, engineeringStage{stage, title, goal})
		}
		slices.SortFunc(normalized, func(a, b engineeringStage) int { return cmp.Compare(a.Stage, b.Stage) })
		stages, _ = json.Marshal(normalized)
	}
	s, ok := h.engineeringState(w, r, true)
	if !ok {
		return
	}
	defer s.tx.Rollback(r.Context())
	admin := roleAllowed(s.member.Role, "owner", "admin")
	if !s.isDecider() || !admin && (req.Enabled != nil || req.DeciderMemberIDs != nil) {
		writeEngineeringError(w, baselineError(403, "baseline_not_decider"))
		return
	}
	config := s.config
	if stages != nil {
		config.Stages = stages
	}
	if req.Enabled != nil {
		config.Enabled = *req.Enabled
	}
	if req.CurrentStage != nil {
		config.CurrentStage = *req.CurrentStage
	}
	if req.ModeSummary != nil {
		config.ModeSummary = *req.ModeSummary
	}
	if req.DeciderMemberIDs != nil {
		config.DeciderMemberIds = []pgtype.UUID{}
		for _, id := range *req.DeciderMemberIDs {
			uid, ok := parseUUIDOrBadRequest(w, id, "decider_member_ids")
			if !ok {
				return
			}
			if _, err := s.q.LabrastroLockEngineeringMember(r.Context(), db.LabrastroLockEngineeringMemberParams{WorkspaceID: s.project.WorkspaceID, UserID: uid}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeErrorCode(w, 422, "invalid_request", "deciders must be current workspace members")
				} else {
					writeEngineeringError(w, err)
				}
				return
			}
			if !slices.Contains(config.DeciderMemberIds, uid) {
				config.DeciderMemberIds = append(config.DeciderMemberIds, uid)
			}
		}
	}
	s.config, err = s.q.LabrastroPutEngineering(r.Context(), db.LabrastroPutEngineeringParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID, Enabled: config.Enabled, CurrentStage: config.CurrentStage, ModeSummary: config.ModeSummary, DeciderMemberIds: config.DeciderMemberIds, UpdatedByType: actor.kind, UpdatedByID: actor.id, Stages: config.Stages})
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	if err = s.tx.Commit(r.Context()); err != nil {
		writeEngineeringError(w, err)
		return
	}
	h.publish(protocol.EventProjectEngineeringUpdated, uuidToString(s.project.WorkspaceID), actor.kind, uuidToString(actor.id), map[string]any{"project_id": uuidToString(s.project.ID)})
	writeJSON(w, 200, map[string]any{"engineering": engineeringConfigResponse{s.config, json.RawMessage(s.config.Stages)}})
}

func (s *engineeringState) updateDoc(ctx context.Context, chapter string, adopted, candidate pgtype.UUID) error {
	return s.q.LabrastroUpdateBaselineDoc(ctx, db.LabrastroUpdateBaselineDocParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID, Chapter: chapter, AdoptedRevisionID: adopted, CandidateRevisionID: candidate})
}
func (s *engineeringState) createRevision(ctx context.Context, chapter, content, summary string, base, replaces, source pgtype.UUID, actor baselineActor, diff engineering.Diff) (db.ProjectBaselineRevision, error) {
	raw, err := json.Marshal(diff)
	if err != nil {
		return db.ProjectBaselineRevision{}, err
	}
	return s.q.LabrastroCreateBaselineRevision(ctx, db.LabrastroCreateBaselineRevisionParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID, Chapter: chapter, ContentMarkdown: content, ChangeSummary: summary, BaseRevisionID: base, ReplacesRevisionID: replaces, SourceIssueID: source, CreatedByType: actor.kind, CreatedByID: actor.id, CreatedByTaskID: actor.task, AnchorsDiff: raw})
}
func (s *engineeringState) decide(ctx context.Context, id pgtype.UUID, status string, a baselineActor) (db.ProjectBaselineRevision, error) {
	return s.q.LabrastroDecideBaselineRevision(ctx, db.LabrastroDecideBaselineRevisionParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID, ID: id, Status: status, DecidedByType: pgtype.Text{String: a.kind, Valid: true}, DecidedByID: a.id, DecidedByTaskID: a.task, DecisionCredential: pgtype.Text{String: a.credential, Valid: true}})
}
func (s *engineeringState) retire(ctx context.Context, id pgtype.UUID, status string) error {
	if !id.Valid {
		return nil
	}
	return s.q.LabrastroRetireBaselineRevision(ctx, db.LabrastroRetireBaselineRevisionParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID, ID: id, Status: status})
}
func (h *Handler) baselineChanged(s *engineeringState, a baselineActor, chapter string) {
	h.publish(protocol.EventProjectBaselineChanged, uuidToString(s.project.WorkspaceID), a.kind, uuidToString(a.id), map[string]any{"project_id": uuidToString(s.project.ID), "chapter": chapter})
}

type proposeBaselineRequest struct {
	Chapter  string  `json:"chapter"`
	Content  string  `json:"content_markdown"`
	Summary  string  `json:"change_summary"`
	Base     *string `json:"base_revision_id"`
	Replaces *string `json:"replaces_candidate_id"`
	Source   *string `json:"source_issue_id"`
}

func optionalBaselineUUID(w http.ResponseWriter, s *string, field string) (pgtype.UUID, bool) {
	if s == nil {
		return pgtype.UUID{}, true
	}
	return parseUUIDOrBadRequest(w, *s, field)
}
func (h *Handler) LabrastroProposeBaseline(w http.ResponseWriter, r *http.Request) {
	actor, err := engineeringActor(r)
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	var req proposeBaselineRequest
	if !decodeEngineering(w, r, &req) {
		return
	}
	if _, err := engineering.Parse(req.Chapter, req.Content); err != nil {
		writeEngineeringError(w, err)
		return
	}
	if utf8.RuneCountInString(req.Summary) > 500 {
		writeErrorCode(w, 422, "invalid_request", "change summary exceeds 500 characters")
		return
	}
	base, ok := optionalBaselineUUID(w, req.Base, "base_revision_id")
	if !ok {
		return
	}
	replaces, ok := optionalBaselineUUID(w, req.Replaces, "replaces_candidate_id")
	if !ok {
		return
	}
	source, ok := optionalBaselineUUID(w, req.Source, "source_issue_id")
	if !ok {
		return
	}
	s, ok := h.engineeringState(w, r, true)
	if !ok {
		return
	}
	defer s.tx.Rollback(r.Context())
	if !s.config.Enabled {
		writeEngineeringError(w, baselineError(409, "engineering_disabled"))
		return
	}
	if source.Valid {
		issue, err := s.q.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: source, WorkspaceID: s.project.WorkspaceID})
		if err != nil || issue.WorkspaceID != s.project.WorkspaceID || issue.ProjectID != s.project.ID {
			writeErrorCode(w, 422, "invalid_request", "source issue must belong to this project")
			return
		}
	}
	if err = s.q.LabrastroEnsureBaselineDoc(r.Context(), db.LabrastroEnsureBaselineDocParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID, Chapter: req.Chapter}); err != nil {
		writeEngineeringError(w, err)
		return
	}
	docs, err := s.docs(r.Context(), true)
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	d := baselineDoc(docs, req.Chapter)
	if d.AdoptedRevisionID != base {
		writeEngineeringError(w, baselineError(409, "baseline_base_stale"))
		return
	}
	if d.CandidateRevisionID != replaces {
		writeEngineeringError(w, baselineError(409, "baseline_candidate_open"))
		return
	}
	old, err := s.revision(r.Context(), base)
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	diff, err := engineering.Compute(req.Chapter, old.ContentMarkdown, req.Content, old.RevisionNumber)
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	if err = s.retire(r.Context(), replaces, "withdrawn"); err != nil {
		writeEngineeringError(w, err)
		return
	}
	candidate, err := s.createRevision(r.Context(), req.Chapter, req.Content, req.Summary, base, replaces, source, actor, diff)
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	if err = s.updateDoc(r.Context(), req.Chapter, base, candidate.ID); err != nil {
		writeEngineeringError(w, err)
		return
	}
	if err = s.tx.Commit(r.Context()); err != nil {
		writeEngineeringError(w, err)
		return
	}
	h.baselineChanged(s, actor, req.Chapter)
	writeJSON(w, 201, baselineRevision(candidate))
}

type decideBaselineRequest struct {
	Action    string `json:"action"`
	Revisions []struct {
		ID       string  `json:"revision_id"`
		Expected *string `json:"expected_adopted_revision_id"`
	} `json:"revisions"`
}

func (h *Handler) LabrastroDecideBaseline(w http.ResponseWriter, r *http.Request) {
	actor, err := engineeringActor(r)
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	if actor.kind == "agent" {
		writeEngineeringError(w, baselineError(403, "baseline_human_required"))
		return
	}
	var req decideBaselineRequest
	if !decodeEngineering(w, r, &req) {
		return
	}
	if req.Action != "adopt" && req.Action != "reject" || len(req.Revisions) < 1 || len(req.Revisions) > 4 {
		writeErrorCode(w, 400, "invalid_request", "specify adopt or reject and one to four revisions")
		return
	}
	ids, expected := make([]pgtype.UUID, len(req.Revisions)), make([]pgtype.UUID, len(req.Revisions))
	for i, item := range req.Revisions {
		var ok bool
		ids[i], ok = parseUUIDOrBadRequest(w, item.ID, "revision_id")
		if !ok {
			return
		}
		expected[i], ok = optionalBaselineUUID(w, item.Expected, "expected_adopted_revision_id")
		if !ok {
			return
		}
	}
	s, ok := h.engineeringState(w, r, true)
	if !ok {
		return
	}
	defer s.tx.Rollback(r.Context())
	if !s.isDecider() {
		writeEngineeringError(w, baselineError(403, "baseline_not_decider"))
		return
	}
	if !s.config.Enabled {
		writeEngineeringError(w, baselineError(409, "engineering_disabled"))
		return
	}
	docs, err := s.docs(r.Context(), true)
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	revisions := make([]db.ProjectBaselineRevision, len(ids))
	seen := map[string]bool{}
	for i, id := range ids {
		candidate, err := s.revision(r.Context(), id)
		if err != nil {
			writeEngineeringError(w, err)
			return
		}
		if seen[candidate.Chapter] {
			writeErrorCode(w, 400, "invalid_request", "each chapter may be decided once")
			return
		}
		seen[candidate.Chapter] = true
		d := baselineDoc(docs, candidate.Chapter)
		if candidate.Status != "candidate" || d.CandidateRevisionID != id {
			writeEngineeringError(w, baselineError(409, "baseline_candidate_closed"))
			return
		}
		if d.AdoptedRevisionID != expected[i] || candidate.BaseRevisionID != d.AdoptedRevisionID {
			writeEngineeringError(w, baselineError(409, "baseline_base_stale"))
			return
		}
		base, err := s.revision(r.Context(), d.AdoptedRevisionID)
		if err != nil {
			writeEngineeringError(w, err)
			return
		}
		diff, err := engineering.Compute(candidate.Chapter, base.ContentMarkdown, candidate.ContentMarkdown, base.RevisionNumber)
		if err != nil {
			writeEngineeringError(w, err)
			return
		}
		var stored engineering.Diff
		if err = json.Unmarshal(candidate.AnchorsDiff, &stored); err != nil {
			writeEngineeringError(w, err)
			return
		}
		if !reflect.DeepEqual(stored, diff) {
			writeEngineeringError(w, baselineError(409, "baseline_base_stale"))
			return
		}
		revisions[i] = candidate
	}
	for _, candidate := range revisions {
		d := baselineDoc(docs, candidate.Chapter)
		status := "rejected"
		adopted := d.AdoptedRevisionID
		if req.Action == "adopt" {
			status = "adopted"
			adopted = candidate.ID
			if err = s.retire(r.Context(), d.AdoptedRevisionID, "superseded"); err != nil {
				writeEngineeringError(w, err)
				return
			}
		}
		if _, err = s.decide(r.Context(), candidate.ID, status, actor); err != nil {
			writeEngineeringError(w, err)
			return
		}
		if err = s.updateDoc(r.Context(), candidate.Chapter, adopted, pgtype.UUID{}); err != nil {
			writeEngineeringError(w, err)
			return
		}
	}
	chapters, err := s.chapters(r.Context())
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	open, err := s.openIssues(r.Context())
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	if err = s.tx.Commit(r.Context()); err != nil {
		writeEngineeringError(w, err)
		return
	}
	for _, candidate := range revisions {
		h.baselineChanged(s, actor, candidate.Chapter)
	}
	writeJSON(w, 200, map[string]any{"chapters": chapters, "open_issues": open})
}

type baselineOpenIssue struct {
	ID           string  `json:"id"`
	Identifier   string  `json:"identifier"`
	Title        string  `json:"title"`
	Status       string  `json:"status"`
	Category     string  `json:"status_category"`
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
}
type baselineOpenIssues struct {
	InFlight  []baselineOpenIssue `json:"in_flight"`
	Queued    []baselineOpenIssue `json:"queued"`
	Truncated bool                `json:"truncated"`
}

func (s *engineeringState) openIssues(ctx context.Context) (baselineOpenIssues, error) {
	out := baselineOpenIssues{InFlight: []baselineOpenIssue{}, Queued: []baselineOpenIssue{}}
	keys, err := issuestatus.ExpandCategories(ctx, s.q, s.project.WorkspaceID, []string{issuestatus.CategoryStarted, issuestatus.CategoryUnstarted})
	if err != nil {
		return out, err
	}
	issues, err := s.q.LabrastroBaselineOpenIssues(ctx, db.LabrastroBaselineOpenIssuesParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID, StatusKeys: keys})
	if err != nil {
		return out, err
	}
	out.Truncated = len(issues) > 200
	if out.Truncated {
		issues = issues[:200]
	}
	resolver := issuestatus.NewResolver(s.project.WorkspaceID)
	for _, i := range issues {
		category := resolver.Category(ctx, s.q, i.Status)
		if err := resolver.Err(); err != nil {
			return out, err
		}
		if category != issuestatus.CategoryStarted && category != issuestatus.CategoryUnstarted {
			continue
		}
		item := baselineOpenIssue{uuidToString(i.ID), s.issuePrefix + "-" + strconv.FormatInt(int64(i.Number), 10), i.Title, i.Status, category, textToPtr(i.AssigneeType), uuidToPtr(i.AssigneeID)}
		if category == issuestatus.CategoryStarted {
			out.InFlight = append(out.InFlight, item)
		} else {
			out.Queued = append(out.Queued, item)
		}
	}
	return out, nil
}

func (h *Handler) LabrastroBaselineHistory(w http.ResponseWriter, r *http.Request) {
	chapter := r.URL.Query().Get("chapter")
	if !engineering.ValidChapter(chapter) {
		writeEngineeringError(w, baselineError(400, "invalid_request"))
		return
	}
	number := func(key string, defaultValue int) (int32, bool) {
		value := r.URL.Query().Get(key)
		if value == "" {
			return int32(defaultValue), true
		}
		n, err := strconv.ParseInt(value, 10, 32)
		if err != nil || n < 1 {
			writeErrorCode(w, 400, "invalid_request", "invalid pagination or revision number")
			return 0, false
		}
		return int32(n), true
	}
	from, ok := number("diff_from", 0)
	if !ok {
		return
	}
	to, ok := number("diff_to", 0)
	if !ok {
		return
	}
	if (from == 0) != (to == 0) {
		writeErrorCode(w, 400, "invalid_request", "diff_from and diff_to are required together")
		return
	}
	limit, ok := number("limit", 50)
	if !ok {
		return
	}
	if limit > 200 {
		limit = 200
	}
	before, ok := number("before", 0)
	if !ok {
		return
	}
	s, ok := h.engineeringState(w, r, false)
	if !ok {
		return
	}
	defer s.tx.Rollback(r.Context())
	if from > 0 {
		read := func(n int32) (db.ProjectBaselineRevision, error) {
			return s.q.LabrastroGetBaselineRevisionNumber(r.Context(), db.LabrastroGetBaselineRevisionNumberParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID, Chapter: chapter, RevisionNumber: n})
		}
		old, err := read(from)
		if err != nil {
			writeEngineeringError(w, err)
			return
		}
		next, err := read(to)
		if err != nil {
			writeEngineeringError(w, err)
			return
		}
		diff, err := engineering.Compute(chapter, old.ContentMarkdown, next.ContentMarkdown, from)
		if err != nil {
			writeEngineeringError(w, err)
			return
		}
		writeJSON(w, 200, diff)
		return
	}
	rows, err := s.q.LabrastroListBaselineRevisions(r.Context(), db.LabrastroListBaselineRevisionsParams{WorkspaceID: s.project.WorkspaceID, ProjectID: s.project.ID, Chapter: chapter, BeforeNumber: pgtype.Int4{Int32: before, Valid: before > 0}, PageLimit: limit + 1})
	if err != nil {
		writeEngineeringError(w, err)
		return
	}
	more := len(rows) > int(limit)
	if more {
		rows = rows[:limit]
	}
	versions := make([]*baselineRevisionResponse, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, baselineRevision(row))
	}
	var nextBefore *int32
	if more {
		n := rows[len(rows)-1].RevisionNumber
		nextBefore = &n
	}
	writeJSON(w, 200, map[string]any{"revisions": versions, "has_more": more, "next_before": nextBefore})
}

func deleteWorkspaceEngineeringAndProjects(ctx context.Context, q *db.Queries, workspace pgtype.UUID) error {
	if err := q.LabrastroDeleteWorkspaceEngineering(ctx, workspace); err != nil {
		return err
	}
	return q.DeleteWorkspaceRuntimesAndProjects(ctx, workspace)
}
