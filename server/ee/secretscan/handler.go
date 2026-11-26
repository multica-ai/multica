package secretscan

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/ee/secretscan/scandb"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	listLimit       = 200
	maxActionTarget = 200
)

type SettingsResponse struct {
	Mode     string           `json:"mode"`
	Backfill BackfillResponse `json:"backfill"`
}

type BackfillResponse struct {
	Status     string  `json:"status"`
	Scanned    int64   `json:"scanned"`
	StartedAt  *string `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
}

type FindingResponse struct {
	ID              string  `json:"id"`
	IssueID         string  `json:"issue_id"`
	IssueIdentifier string  `json:"issue_identifier,omitempty"`
	IssueTitle      string  `json:"issue_title,omitempty"`
	CommentID       string  `json:"comment_id"`
	AuthorType      string  `json:"author_type"`
	AuthorID        string  `json:"author_id"`
	Rule            string  `json:"rule"`
	Masked          string  `json:"masked"`
	Fingerprint     string  `json:"fingerprint"`
	Status          string  `json:"status"`
	ResolvedByType  *string `json:"resolved_by_type"`
	ResolvedByID    *string `json:"resolved_by_id"`
	ResolvedAt      *string `json:"resolved_at"`
	CreatedAt       string  `json:"created_at"`
}

type FindingCounts struct {
	Open      int64 `json:"open"`
	Redacted  int64 `json:"redacted"`
	Dismissed int64 `json:"dismissed"`
}

type ListFindingsResponse struct {
	Findings []FindingResponse `json:"findings"`
	Counts   FindingCounts     `json:"counts"`
}

func settingsResponse(s scandb.SecretScanSetting) SettingsResponse {
	return SettingsResponse{
		Mode: s.Mode,
		Backfill: BackfillResponse{
			Status:     s.BackfillStatus,
			Scanned:    s.BackfillScanned,
			StartedAt:  util.TimestampToPtr(s.BackfillStartedAt),
			FinishedAt: util.TimestampToPtr(s.BackfillFinishedAt),
		},
	}
}

func findingResponse(f scandb.SecretFinding) FindingResponse {
	return FindingResponse{
		ID:             util.UUIDToString(f.ID),
		IssueID:        util.UUIDToString(f.IssueID),
		CommentID:      util.UUIDToString(f.CommentID),
		AuthorType:     f.AuthorType,
		AuthorID:       util.UUIDToString(f.AuthorID),
		Rule:           f.Rule,
		Masked:         f.Masked,
		Fingerprint:    f.Fingerprint,
		Status:         f.Status,
		ResolvedByType: util.TextToPtr(f.ResolvedByType),
		ResolvedByID:   util.UUIDToPtr(f.ResolvedByID),
		ResolvedAt:     util.TimestampToPtr(f.ResolvedAt),
		CreatedAt:      util.TimestampToString(f.CreatedAt),
	}
}

type caller struct {
	workspaceID string
	workspace   pgtype.UUID
	userID      string
	admin       bool
}

func (s *Service) caller(w http.ResponseWriter, r *http.Request) (caller, bool) {
	userID, ok := handler.RequireUserID(w, r)
	if !ok {
		return caller{}, false
	}
	workspaceID := s.h.WorkspaceID(r)
	role, ok := s.h.WorkspaceRole(w, r, workspaceID)
	if !ok {
		return caller{}, false
	}
	ws, err := util.ParseUUID(workspaceID)
	if err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid workspace id")
		return caller{}, false
	}
	return caller{workspaceID: workspaceID, workspace: ws, userID: userID, admin: handler.IsWorkspaceAdmin(role)}, true
}

func (s *Service) admin(w http.ResponseWriter, r *http.Request) (caller, bool) {
	c, ok := s.caller(w, r)
	if ok && !c.admin {
		handler.WriteError(w, http.StatusForbidden, "only workspace admins can manage secret scanning")
		return caller{}, false
	}
	return c, ok
}

// mayAct is who may redact or dismiss a finding: the people who may edit
// the comment it is in.
func (c caller) mayAct(authorType string, authorID pgtype.UUID) bool {
	return c.admin || (authorType == "member" && util.UUIDToString(authorID) == c.userID)
}

func (s *Service) GetSettings(w http.ResponseWriter, r *http.Request) {
	c, ok := s.admin(w, r)
	if !ok {
		return
	}
	setting, err := s.q.GetSetting(r.Context(), c.workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		handler.WriteJSON(w, http.StatusOK, SettingsResponse{Mode: ModeNotify, Backfill: BackfillResponse{Status: "idle"}})
		return
	}
	if err != nil {
		slog.Warn("secret scan: get setting", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	handler.WriteJSON(w, http.StatusOK, settingsResponse(setting))
}

func (s *Service) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	c, ok := s.admin(w, r)
	if !ok {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Mode != ModeNotify && req.Mode != ModeRedact {
		handler.WriteError(w, http.StatusBadRequest, "mode must be notify or redact")
		return
	}
	setting, err := s.q.SetMode(r.Context(), scandb.SetModeParams{WorkspaceID: c.workspace, Mode: req.Mode})
	if err != nil {
		slog.Warn("secret scan: set mode", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}
	actorType, actorID := s.h.Actor(r, c.userID, c.workspaceID)
	s.h.Publish(EventChanged, c.workspaceID, actorType, actorID, map[string]any{})
	handler.WriteJSON(w, http.StatusOK, settingsResponse(setting))
}

func (s *Service) ListFindings(w http.ResponseWriter, r *http.Request) {
	c, ok := s.admin(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = StatusOpen
	}
	if status != StatusOpen && status != StatusRedacted && status != StatusDismissed {
		handler.WriteError(w, http.StatusBadRequest, "status must be open, redacted or dismissed")
		return
	}
	hidden, ok := s.h.HiddenProjects(w, r, c.workspaceID)
	if !ok {
		return
	}
	rows, err := s.q.ListFindings(r.Context(), scandb.ListFindingsParams{
		WorkspaceID: c.workspace, Status: status, HiddenProjectIds: hidden.Param(), RowLimit: listLimit,
	})
	if err != nil {
		slog.Warn("secret scan: list findings", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to list findings")
		return
	}
	counts, err := s.q.CountFindings(r.Context(), scandb.CountFindingsParams{WorkspaceID: c.workspace, HiddenProjectIds: hidden.Param()})
	if err != nil {
		slog.Warn("secret scan: count findings", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to list findings")
		return
	}
	prefix := ""
	if ws, err := s.h.Queries.GetWorkspace(r.Context(), c.workspace); err == nil {
		prefix = ws.IssuePrefix
	}
	resp := ListFindingsResponse{Findings: make([]FindingResponse, 0, len(rows))}
	for _, row := range rows {
		f := findingResponse(row.SecretFinding)
		f.IssueIdentifier = fmt.Sprintf("%s-%d", prefix, row.IssueNumber)
		f.IssueTitle = row.IssueTitle
		resp.Findings = append(resp.Findings, f)
	}
	for _, n := range counts {
		switch n.Status {
		case StatusOpen:
			resp.Counts.Open += n.Total
		case StatusRedacted:
			resp.Counts.Redacted += n.Total
		case StatusDismissed:
			resp.Counts.Dismissed += n.Total
		}
	}
	handler.WriteJSON(w, http.StatusOK, resp)
}

// ListIssueFindings lists the open findings in an issue's comments that the
// caller may act on.
func (s *Service) ListIssueFindings(w http.ResponseWriter, r *http.Request) {
	c, ok := s.caller(w, r)
	if !ok {
		return
	}
	issueID, ok := handler.ParseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue id")
	if !ok {
		return
	}
	if _, err := s.h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: c.workspace}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			handler.WriteError(w, http.StatusNotFound, "issue not found")
		} else {
			handler.WriteError(w, http.StatusInternalServerError, "failed to load issue")
		}
		return
	}
	if !s.h.RequireIssueVisible(w, r, c.workspaceID, issueID) {
		return
	}
	rows, err := s.q.ListOpenIssueFindings(r.Context(), scandb.ListOpenIssueFindingsParams{WorkspaceID: c.workspace, IssueID: issueID})
	if err != nil {
		slog.Warn("secret scan: list issue findings", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to list findings")
		return
	}
	out := make([]FindingResponse, 0, len(rows))
	for _, f := range rows {
		if c.mayAct(f.AuthorType, f.AuthorID) {
			out = append(out, findingResponse(f))
		}
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"findings": out})
}

// targets loads the open findings a redact or dismiss request names,
// answering 404 unless the caller may act on every one of them.
func (s *Service) targets(w http.ResponseWriter, r *http.Request, c caller) ([]scandb.GetFindingsRow, bool) {
	var req struct {
		FindingIDs []string `json:"finding_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	if len(req.FindingIDs) == 0 || len(req.FindingIDs) > maxActionTarget {
		handler.WriteError(w, http.StatusBadRequest, fmt.Sprintf("finding_ids must name 1 to %d findings", maxActionTarget))
		return nil, false
	}
	ids := make([]pgtype.UUID, 0, len(req.FindingIDs))
	seen := make(map[string]bool, len(req.FindingIDs))
	for _, raw := range req.FindingIDs {
		id, ok := handler.ParseUUIDOrBadRequest(w, raw, "finding id")
		if !ok {
			return nil, false
		}
		if key := util.UUIDToString(id); !seen[key] {
			seen[key] = true
			ids = append(ids, id)
		}
	}
	rows, err := s.q.GetFindings(r.Context(), scandb.GetFindingsParams{WorkspaceID: c.workspace, Ids: ids})
	if err != nil {
		slog.Warn("secret scan: get findings", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to load findings")
		return nil, false
	}
	hidden, ok := s.h.HiddenProjects(w, r, c.workspaceID)
	if !ok {
		return nil, false
	}
	if len(rows) != len(ids) {
		handler.WriteError(w, http.StatusNotFound, "finding not found")
		return nil, false
	}
	open := rows[:0]
	for _, f := range rows {
		visible := hidden.Empty() || !f.ProjectID.Valid || hidden.CanSeeProject(f.ProjectID)
		if !visible || !c.mayAct(f.AuthorType, f.AuthorID) {
			handler.WriteError(w, http.StatusNotFound, "finding not found")
			return nil, false
		}
		if f.Status == StatusOpen {
			open = append(open, f)
		}
	}
	return open, true
}

func (s *Service) Dismiss(w http.ResponseWriter, r *http.Request) {
	c, ok := s.caller(w, r)
	if !ok {
		return
	}
	targets, ok := s.targets(w, r, c)
	if !ok {
		return
	}
	actorType, actorID := s.h.Actor(r, c.userID, c.workspaceID)
	ids := make([]pgtype.UUID, len(targets))
	for i, f := range targets {
		ids[i] = f.ID
	}
	settled, err := s.settle(r, c, ids, StatusDismissed, actorType, actorID)
	if err != nil {
		slog.Warn("secret scan: dismiss", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to dismiss findings")
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"settled": settled})
}

// Redact replaces the named secrets in their comments with placeholders, and
// in the notification copies of those comments. The secrets stay valid
// wherever else they were used, so the response does not end the matter for
// the people who own them.
func (s *Service) Redact(w http.ResponseWriter, r *http.Request) {
	c, ok := s.caller(w, r)
	if !ok {
		return
	}
	targets, ok := s.targets(w, r, c)
	if !ok {
		return
	}
	if len(targets) == 0 {
		handler.WriteJSON(w, http.StatusOK, map[string]any{"settled": 0})
		return
	}
	setting, err := s.q.EnsureSetting(r.Context(), c.workspace)
	if err != nil {
		slog.Warn("secret scan: load setting", append(logger.RequestAttrs(r), "error", err)...)
		handler.WriteError(w, http.StatusInternalServerError, "failed to redact")
		return
	}
	actorType, actorID := s.h.Actor(r, c.userID, c.workspaceID)

	type group struct {
		issueID pgtype.UUID
		ids     []pgtype.UUID
		fps     map[string]bool
	}
	var order []pgtype.UUID
	groups := map[pgtype.UUID]*group{}
	for _, f := range targets {
		g := groups[f.CommentID]
		if g == nil {
			g = &group{issueID: f.IssueID, fps: map[string]bool{}}
			groups[f.CommentID] = g
			order = append(order, f.CommentID)
		}
		g.ids = append(g.ids, f.ID)
		g.fps[f.Fingerprint] = true
	}
	strip := func(fps map[string]bool) func(string) string {
		return func(text string) string {
			var hits []redact.Match
			for _, m := range redact.Find(text) {
				if fps[fingerprint(setting.Salt, m)] {
					hits = append(hits, m)
				}
			}
			return redact.Replace(text, hits)
		}
	}

	settled := 0
	for _, commentID := range order {
		g := groups[commentID]
		rewrite := strip(g.fps)
		_, changed, err := s.h.RewriteComment(r, c.workspace, commentID, rewrite, actorType, actorID)
		if errors.Is(err, pgx.ErrNoRows) {
			handler.WriteError(w, http.StatusConflict, "the comment changed; reload and try again")
			return
		}
		if err != nil {
			slog.Warn("secret scan: rewrite comment", append(logger.RequestAttrs(r), "comment_id", util.UUIDToString(commentID), "error", err)...)
			handler.WriteError(w, http.StatusInternalServerError, "failed to redact")
			return
		}
		s.stripInbox(r, g.issueID, commentID, rewrite)
		status := StatusRedacted
		if !changed {
			status = StatusResolved
		}
		n, err := s.settle(r, c, g.ids, status, actorType, actorID)
		if err != nil {
			slog.Warn("secret scan: settle findings", append(logger.RequestAttrs(r), "error", err)...)
			handler.WriteError(w, http.StatusInternalServerError, "failed to redact")
			return
		}
		settled += n
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"settled": settled})
}

// stripInbox redacts the copies of a comment kept in notifications. A copy
// left behind is logged, not fatal: the comment itself is already clean.
func (s *Service) stripInbox(r *http.Request, issueID, commentID pgtype.UUID, rewrite func(string) string) {
	items, err := s.q.ListCommentInboxBodies(r.Context(), scandb.ListCommentInboxBodiesParams{IssueID: issueID, CommentID: util.UUIDToString(commentID)})
	if err != nil {
		slog.Warn("secret scan: list inbox copies", "comment_id", util.UUIDToString(commentID), "error", err)
		return
	}
	for _, item := range items {
		body := rewrite(item.Body.String)
		if body == item.Body.String {
			continue
		}
		if err := s.q.SetInboxBody(r.Context(), scandb.SetInboxBodyParams{ID: item.ID, Body: util.StrToText(body)}); err != nil {
			slog.Warn("secret scan: redact inbox copy", "inbox_item_id", util.UUIDToString(item.ID), "error", err)
		}
	}
}

func (s *Service) settle(r *http.Request, c caller, ids []pgtype.UUID, status, actorType, actorID string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	rows, err := s.q.SettleFindings(r.Context(), scandb.SettleFindingsParams{
		Status:         status,
		ResolvedByType: util.StrToText(actorType),
		ResolvedByID:   parseOptionalUUID(actorID),
		WorkspaceID:    c.workspace,
		Ids:            ids,
	})
	if err != nil {
		return 0, err
	}
	issues := map[pgtype.UUID]bool{}
	for _, row := range rows {
		if !issues[row.IssueID] {
			issues[row.IssueID] = true
			s.publishIssue(c.workspaceID, row.IssueID, actorType, actorID)
		}
	}
	return len(rows), nil
}

func parseOptionalUUID(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}
