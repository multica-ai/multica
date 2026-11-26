package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/contribution"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Contribution dashboard — the "Contribution" tab of the workspace dashboard:
//
//	GET /api/dashboard/contribution/summary  KPIs, trends, leaderboards
//	GET /api/dashboard/contribution/events   paged activity detail
//	GET /api/dashboard/contribution/issues   paged per-issue interactions
//
// Each accepts ?days=N (exact N calendar days in the viewer's tz, cut to the
// plan's dashboard history limit) and ?project_id. Issues in private projects the viewer cannot see are left out
// entirely; agents the viewer may not see are folded onto
// restrictedAgentsRowID, as on the other per-agent rollups.

const (
	contributionDefaultLimit = 50
	contributionMaxLimit     = 100
)

type contributionRequest struct {
	workspaceID pgtype.UUID
	projectID   pgtype.UUID
	loc         *time.Location
	since       time.Time
	restricted  map[string]struct{}
}

// parseContributionRequest validates the shared parameters. On failure it has
// already written the response.
func (h *Handler) parseContributionRequest(w http.ResponseWriter, r *http.Request) (contributionRequest, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return contributionRequest{}, false
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return contributionRequest{}, false
	}
	if !h.requireProjectVisible(w, r, workspaceID, projectID, http.StatusNotFound, "project not found") {
		return contributionRequest{}, false
	}
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return contributionRequest{}, false
	}
	tz := h.resolveViewingTZ(r)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	// Exactly N calendar days, cut to the workspace's dashboard history limit
	// like the other dashboard endpoints.
	window := h.dashboardCutoff(w, r, workspaceID, tz, 1)
	return contributionRequest{
		workspaceID: parseUUID(workspaceID),
		projectID:   projectID,
		loc:         loc,
		since:       window.since.Time,
		restricted:  restricted,
	}, true
}

func (h *Handler) loadContributionDataset(ctx context.Context, w http.ResponseWriter, r *http.Request, req contributionRequest) (*contribution.Dataset, bool) {
	hidden, ok := h.requestHiddenProjects(w, r, uuidToString(req.workspaceID))
	if !ok {
		return nil, false
	}
	ds, err := h.buildContributionDataset(ctx, req, hidden)
	if err != nil {
		if writeQueryCancellation(w, r, err) {
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load contribution data")
		return nil, false
	}
	return ds, true
}

func (h *Handler) buildContributionDataset(ctx context.Context, req contributionRequest, hidden projectaccess.Hidden) (*contribution.Dataset, error) {
	since := pgtype.Timestamptz{Time: req.since, Valid: true}
	issueRows, err := h.Queries.ListContributionIssues(ctx, db.ListContributionIssuesParams{
		WorkspaceID:      req.workspaceID,
		ProjectID:        req.projectID,
		HiddenProjectIds: hidden.Param(),
	})
	if err != nil {
		return nil, err
	}
	commentRows, err := h.Queries.ListContributionComments(ctx, db.ListContributionCommentsParams{
		WorkspaceID: req.workspaceID,
		Since:       since,
	})
	if err != nil {
		return nil, err
	}
	statusRows, err := h.Queries.ListContributionStatusChanges(ctx, db.ListContributionStatusChangesParams{
		WorkspaceID: req.workspaceID,
		Since:       since,
	})
	if err != nil {
		return nil, err
	}

	issues := make([]contribution.Issue, 0, len(issueRows))
	for _, row := range issueRows {
		issues = append(issues, contribution.Issue{
			ID: uuidToString(row.ID), Number: row.Number, Title: row.Title, Status: row.Status, StatusCategory: row.StatusCategory,
			ProjectID: uuidToString(row.ProjectID), CreatorType: row.CreatorType, CreatorID: uuidToString(row.CreatorID),
			CreatedAt: row.CreatedAt.Time,
		})
	}
	comments := make([]contribution.Comment, 0, len(commentRows))
	for _, row := range commentRows {
		comments = append(comments, contribution.Comment{
			IssueID: uuidToString(row.IssueID), AuthorType: row.AuthorType, AuthorID: uuidToString(row.AuthorID), At: row.CreatedAt.Time,
		})
	}
	changes := make([]contribution.StatusChange, 0, len(statusRows))
	for _, row := range statusRows {
		changes = append(changes, contribution.StatusChange{
			IssueID: uuidToString(row.IssueID), ActorType: row.ActorType, ActorID: uuidToString(row.ActorID),
			At: row.CreatedAt.Time, ToStatus: row.ToStatus, ToCategory: row.ToCategory,
		})
	}

	var mapActor func(string, string) (string, string)
	if len(req.restricted) > 0 {
		mapActor = func(actorType, actorID string) (string, string) {
			if actorType == contribution.ActorAgent {
				if _, hide := req.restricted[actorID]; hide {
					return actorType, restrictedAgentsRowID
				}
			}
			return actorType, actorID
		}
	}
	return contribution.NewDataset(contribution.Input{
		Issues: issues, Comments: comments, StatusChanges: changes,
		Since: req.since, Now: dayWindowNow(), Loc: req.loc, MapActor: mapActor,
	}), nil
}

func parseContributionPage(r *http.Request) (limit, offset int) {
	limit = contributionDefaultLimit
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, contributionMaxLimit)
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	return limit, offset
}

// GetDashboardContributionSummary returns the KPIs, trends and leaderboards
// for the window.
func (h *Handler) GetDashboardContributionSummary(w http.ResponseWriter, r *http.Request) {
	req, ok := h.parseContributionRequest(w, r)
	if !ok {
		return
	}
	ds, ok := h.loadContributionDataset(r.Context(), w, r, req)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, ds.Summary())
}

// ContributionEventResponse is one activity detail row.
type ContributionEventResponse struct {
	contribution.EventRow
	Identifier string `json:"identifier"`
}

// ContributionEventsResponse is a page of activity detail.
type ContributionEventsResponse struct {
	Events []ContributionEventResponse `json:"events"`
	Total  int                         `json:"total"`
}

// GetDashboardContributionEvents returns the window's events newest first.
// ?actors=member:<id>,agent:<id> narrows to those actors.
func (h *Handler) GetDashboardContributionEvents(w http.ResponseWriter, r *http.Request) {
	req, ok := h.parseContributionRequest(w, r)
	if !ok {
		return
	}
	var actors []string
	for _, a := range strings.Split(r.URL.Query().Get("actors"), ",") {
		if a = strings.TrimSpace(a); a != "" {
			actors = append(actors, a)
		}
	}
	ds, ok := h.loadContributionDataset(r.Context(), w, r, req)
	if !ok {
		return
	}
	limit, offset := parseContributionPage(r)
	rows, total := ds.Events(actors, limit, offset)
	prefix := h.getIssuePrefix(r.Context(), req.workspaceID)
	resp := ContributionEventsResponse{Events: make([]ContributionEventResponse, 0, len(rows)), Total: total}
	for _, row := range rows {
		resp.Events = append(resp.Events, ContributionEventResponse{EventRow: row, Identifier: prefix + "-" + strconv.Itoa(int(row.Number))})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ContributionIssueResponse is one issue row with its identifier.
type ContributionIssueResponse struct {
	contribution.IssueRow
	Identifier string `json:"identifier"`
}

// ContributionIssuesResponse is a page of issues with activity in the window.
type ContributionIssuesResponse struct {
	Issues []ContributionIssueResponse `json:"issues"`
	Total  int                         `json:"total"`
}

// GetDashboardContributionIssues lists the window's issues with their
// participants, interactions and time to done, ordered by
// ?sort=interactions|participants|completion|recent.
func (h *Handler) GetDashboardContributionIssues(w http.ResponseWriter, r *http.Request) {
	req, ok := h.parseContributionRequest(w, r)
	if !ok {
		return
	}
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = contribution.SortInteractions
	}
	if !contribution.IsIssueSort(sortBy) {
		writeError(w, http.StatusBadRequest, "invalid sort")
		return
	}
	ds, ok := h.loadContributionDataset(r.Context(), w, r, req)
	if !ok {
		return
	}
	limit, offset := parseContributionPage(r)
	rows, total := ds.Issues(sortBy, limit, offset)
	prefix := h.getIssuePrefix(r.Context(), req.workspaceID)
	resp := ContributionIssuesResponse{Issues: make([]ContributionIssueResponse, 0, len(rows)), Total: total}
	for _, row := range rows {
		resp.Issues = append(resp.Issues, ContributionIssueResponse{IssueRow: row, Identifier: prefix + "-" + strconv.Itoa(int(row.Number))})
	}
	writeJSON(w, http.StatusOK, resp)
}
