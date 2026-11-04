package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Scorecards and reviews (spec 4.6). Owners and admins read every subject. A
// squad leader reads its squad and the agents in it. The workspace's reviewer
// agent reads every subject, since its job is to review them, and it alone
// publishes reviews.

// reviewListLimit bounds one list response.
const reviewListLimit int32 = 200

// ScorecardResponse is one stored scorecard.
type ScorecardResponse struct {
	ID              string                             `json:"id"`
	SubjectType     string                             `json:"subject_type"`
	SubjectID       string                             `json:"subject_id"`
	Period          string                             `json:"period"`
	PeriodStart     string                             `json:"period_start"`
	PeriodEnd       string                             `json:"period_end"`
	Status          string                             `json:"status"`
	SampleSize      int32                              `json:"sample_size"`
	ComputedAt      time.Time                          `json:"computed_at"`
	Metrics         map[string]service.ScorecardMetric `json:"metrics"`
	FailuresByCause map[string]int64                   `json:"failures_by_cause"`
}

func scorecardResponse(s db.Scorecard) (ScorecardResponse, error) {
	doc, err := service.DecodeScorecardDocument(s.Metrics)
	if err != nil {
		return ScorecardResponse{}, err
	}
	metrics := doc.Metrics
	if metrics == nil {
		metrics = map[string]service.ScorecardMetric{}
	}
	causes := doc.FailuresByCause
	if causes == nil {
		causes = map[string]int64{}
	}
	return ScorecardResponse{
		ID:              uuidToString(s.ID),
		SubjectType:     s.SubjectType,
		SubjectID:       uuidToString(s.SubjectID),
		Period:          s.Period,
		PeriodStart:     s.PeriodStart.Time.Format("2006-01-02"),
		PeriodEnd:       s.PeriodEnd.Time.Format("2006-01-02"),
		Status:          s.Status,
		SampleSize:      s.SampleSize,
		ComputedAt:      s.ComputedAt.Time,
		Metrics:         metrics,
		FailuresByCause: causes,
	}, nil
}

// readableSubjects returns the subjects the caller may read scorecards and
// reviews of. all is true when there is no filter; otherwise ids is the list,
// which may be empty. ok is false when the caller is not a workspace member.
func (h *Handler) readableSubjects(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, workspaceID string) (ids []pgtype.UUID, all bool, ok bool) {
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		agentUUID, err := util.ParseUUID(actorID)
		if err != nil {
			writeError(w, http.StatusForbidden, "agent identity is not valid")
			return nil, false, false
		}
		seesAll, err := service.ReviewSeesAll(r.Context(), h.Queries, wsUUID, agentUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check review access")
			return nil, false, false
		}
		if seesAll {
			return nil, true, true
		}
		visible, err := service.ReviewVisibleSubjects(r.Context(), h.Queries, wsUUID, agentUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check review access")
			return nil, false, false
		}
		return visible, false, true
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return nil, false, false
	}
	if roleAllowed(member.Role, "owner", "admin") {
		return nil, true, true
	}
	return []pgtype.UUID{}, false, true
}

// ListScorecards returns the stored scorecards the caller may read, newest
// period first. Filters: subject_type, subject_id, period.
//
//	GET /api/scorecards?subject_type=&subject_id=&period=
func (h *Handler) ListScorecards(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	q := r.URL.Query()
	params := db.ListScorecardsParams{WorkspaceID: wsUUID, RowLimit: reviewListLimit}
	if st := q.Get("subject_type"); st != "" {
		switch st {
		case service.ScorecardSubjectAgent, service.ScorecardSubjectSquad, service.ScorecardSubjectCompany:
		default:
			writeError(w, http.StatusBadRequest, "subject_type must be agent, squad or company")
			return
		}
		params.SubjectType = pgtype.Text{String: st, Valid: true}
	}
	if raw := q.Get("subject_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "subject_id")
		if !ok {
			return
		}
		params.SubjectID = id
	}
	if p := q.Get("period"); p != "" {
		if p != service.ScorecardPeriodWeek && p != service.ScorecardPeriodMonth {
			writeError(w, http.StatusBadRequest, "period must be week or month")
			return
		}
		params.Period = pgtype.Text{String: p, Valid: true}
	}
	ids, all, ok := h.readableSubjects(w, r, wsUUID, workspaceID)
	if !ok {
		return
	}
	if !all {
		if len(ids) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"scorecards": []ScorecardResponse{}})
			return
		}
		params.SubjectIds = ids
	}
	rows, err := h.Queries.ListScorecards(r.Context(), params)
	if err != nil {
		slog.Warn("list scorecards failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list scorecards")
		return
	}
	out := make([]ScorecardResponse, 0, len(rows))
	for _, row := range rows {
		resp, err := scorecardResponse(row)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read a scorecard")
			return
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"scorecards": out})
}

// ReviewResponse is one review as the API serves it.
type ReviewResponse struct {
	ID              string                         `json:"id"`
	SubjectType     string                         `json:"subject_type"`
	SubjectID       string                         `json:"subject_id"`
	PeriodStart     string                         `json:"period_start"`
	PeriodEnd       string                         `json:"period_end"`
	ScorecardID     string                         `json:"scorecard_id"`
	Rating          string                         `json:"rating"`
	Narrative       string                         `json:"narrative"`
	Recommendations []service.ReviewRecommendation `json:"recommendations"`
	ReviewerAgentID string                         `json:"reviewer_agent_id"`
	CreatedAt       time.Time                      `json:"created_at"`
}

func reviewResponse(rv db.Review) (ReviewResponse, error) {
	recs := []service.ReviewRecommendation{}
	if len(rv.Recommendations) > 0 {
		if err := json.Unmarshal(rv.Recommendations, &recs); err != nil {
			return ReviewResponse{}, err
		}
	}
	return ReviewResponse{
		ID:              uuidToString(rv.ID),
		SubjectType:     rv.SubjectType,
		SubjectID:       uuidToString(rv.SubjectID),
		PeriodStart:     rv.PeriodStart.Time.Format("2006-01-02"),
		PeriodEnd:       rv.PeriodEnd.Time.Format("2006-01-02"),
		ScorecardID:     uuidToString(rv.ScorecardID),
		Rating:          rv.Rating,
		Narrative:       rv.Narrative,
		Recommendations: recs,
		ReviewerAgentID: uuidToString(rv.ReviewerAgentID),
		CreatedAt:       rv.CreatedAt.Time,
	}, nil
}

// ListReviews returns the reviews the caller may read, newest first. Filters:
// subject_type, subject_id.
//
//	GET /api/reviews?subject_type=&subject_id=
func (h *Handler) ListReviews(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	q := r.URL.Query()
	params := db.ListReviewsParams{WorkspaceID: wsUUID, RowLimit: reviewListLimit}
	if st := q.Get("subject_type"); st != "" {
		switch st {
		case service.ScorecardSubjectAgent, service.ScorecardSubjectSquad, service.ScorecardSubjectCompany:
		default:
			writeError(w, http.StatusBadRequest, "subject_type must be agent, squad or company")
			return
		}
		params.SubjectType = pgtype.Text{String: st, Valid: true}
	}
	if raw := q.Get("subject_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "subject_id")
		if !ok {
			return
		}
		params.SubjectID = id
	}
	ids, all, ok := h.readableSubjects(w, r, wsUUID, workspaceID)
	if !ok {
		return
	}
	if !all {
		if len(ids) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"reviews": []ReviewResponse{}})
			return
		}
		params.SubjectIds = ids
	}
	rows, err := h.Queries.ListReviews(r.Context(), params)
	if err != nil {
		slog.Warn("list reviews failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list reviews")
		return
	}
	out := make([]ReviewResponse, 0, len(rows))
	for _, rv := range rows {
		resp, err := reviewResponse(rv)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read a review")
			return
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": out})
}

// GetReview returns one review the caller may read. A review the caller may not
// read is reported as not found, so its existence does not leak.
//
//	GET /api/reviews/{id}
func (h *Handler) GetReview(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "review id")
	if !ok {
		return
	}
	rv, err := h.Queries.GetReviewInWorkspace(r.Context(), db.GetReviewInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "review not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load review")
		return
	}
	ids, all, ok := h.readableSubjects(w, r, wsUUID, workspaceID)
	if !ok {
		return
	}
	if !all && !slicesContainsUUID(ids, rv.SubjectID) {
		writeError(w, http.StatusNotFound, "review not found")
		return
	}
	resp, err := reviewResponse(rv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read review")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"review": resp})
}

func slicesContainsUUID(ids []pgtype.UUID, id pgtype.UUID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// PublishReviewResponse is the reviewer's publish answer: the stored review.
type PublishReviewResponse struct {
	Review ReviewResponse `json:"review"`
}

// CreateReview publishes a review. Only the workspace's reviewer agent, from a
// running task of its own, may publish. Each change recommendation is filed as
// an org_change proposal from the reviewer's squad.
//
//	POST /api/reviews
func (h *Handler) CreateReview(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "agent" {
		writeError(w, http.StatusForbidden, "only the workspace's reviewer agent may publish a review")
		return
	}
	agentUUID, err := util.ParseUUID(actorID)
	if err != nil {
		writeError(w, http.StatusForbidden, "only the workspace's reviewer agent may publish a review")
		return
	}
	reviewer, err := h.Queries.GetReviewerAgent(r.Context(), wsUUID)
	if err != nil || reviewer.ID != agentUUID {
		writeError(w, http.StatusForbidden, "only the workspace's reviewer agent may publish a review")
		return
	}
	task, ok := h.taskFromRequestHeader(r)
	if !ok || task.AgentID != agentUUID || isTerminalTaskStatus(task.Status) {
		writeError(w, http.StatusForbidden, "a review can only be published from inside a running task")
		return
	}
	squad, err := h.Queries.GetReviewerSquad(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusConflict, "the workspace has no squad for the reviewer to file from")
		return
	}

	var req service.ReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	svc := &service.ReviewService{Queries: h.Queries, Proposals: h.proposalService(), OrgChanges: h.orgChanges(), Issues: h.IssueService}
	res, err := svc.Publish(r.Context(), wsUUID, reviewer, squad.ID, req)
	var inputErr *service.ReviewInputError
	var payloadErr *service.ProposalPayloadError
	switch {
	case errors.As(err, &inputErr):
		writeError(w, http.StatusBadRequest, inputErr.Message)
		return
	case errors.As(err, &payloadErr):
		writeErrorCode(w, http.StatusBadRequest, "invalid_payload", payloadErr.Message)
		return
	case errors.Is(err, service.ErrReviewSubjectNotFound):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, service.ErrReviewNoScorecard):
		writeErrorCode(w, http.StatusUnprocessableEntity, "no_scorecard", err.Error())
		return
	case errors.Is(err, service.ErrReviewInsufficient):
		writeErrorCode(w, http.StatusUnprocessableEntity, "insufficient_scorecard", err.Error())
		return
	case errors.Is(err, service.ErrReviewExists):
		writeErrorCode(w, http.StatusConflict, "review_exists", err.Error())
		return
	case err != nil:
		slog.Warn("publish review failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to publish review")
		return
	}
	stored, err := h.Queries.GetReviewInWorkspace(r.Context(), db.GetReviewInWorkspaceParams{ID: util.MustParseUUID(res.ReviewID), WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the published review")
		return
	}
	resp, err := reviewResponse(stored)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the published review")
		return
	}
	writeJSON(w, http.StatusCreated, PublishReviewResponse{Review: resp})
}

// The scheduler jobs call these (spec 4.6). Each runs through the same services
// the API uses.

// RunNightlyScorecards computes the scorecards of every workspace.
func (h *Handler) RunNightlyScorecards(ctx context.Context) (int, error) {
	return service.NewScorecardService(h.Queries).RunNightly(ctx)
}

// CheckDueProbations judges the probation windows that have ended.
func (h *Handler) CheckDueProbations(ctx context.Context) (int, error) {
	svc := &service.ProbationService{
		Queries:    h.Queries,
		Changes:    h.orgChanges(),
		Scorecards: service.NewScorecardService(h.Queries),
		Proposals:  h.proposalService(),
	}
	return svc.CheckDue(ctx)
}

// FileMonthlyReviews files the monthly review routine's issues.
func (h *Handler) FileMonthlyReviews(ctx context.Context) (int, error) {
	svc := &service.ReviewService{Queries: h.Queries, Proposals: h.proposalService(), OrgChanges: h.orgChanges(), Issues: h.IssueService}
	return svc.FileMonthlyReviews(ctx)
}
