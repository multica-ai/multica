package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Reviews (spec 4.6). The reviewer agent of the People & Performance squad
// publishes a review of one subject for one period. A review cites the
// scorecard it relies on; the server copies the cited figures from that
// scorecard into the stored narrative, so a figure in a review is the stored
// figure and not the reviewer's transcription. Each recommendation that changes
// something is filed as an org_change proposal, and the review records its id.

// Review ratings.
const (
	ReviewRatingExceeds = "exceeds"
	ReviewRatingMeets   = "meets"
	ReviewRatingBelow   = "below"
	ReviewRatingAtRisk  = "at_risk"
)

// ReviewRecommendObserve is a recommendation that changes nothing and so files
// no proposal: the review notes it for the record.
const ReviewRecommendObserve = "observe"

// ReviewNarrativeMax bounds the narrative as the reviewer writes it, in characters.
// The stored narrative also carries the cited figures, so it is checked again.
const ReviewNarrativeMax = 8000

// ReviewRecommendationsMax bounds the recommendations of one review.
const ReviewRecommendationsMax = 12

// Review subject types reuse the scorecard subject types.
var reviewRatings = []string{ReviewRatingExceeds, ReviewRatingMeets, ReviewRatingBelow, ReviewRatingAtRisk}

// ReviewInputError is a publish the reviewer must fix: a field is missing or
// malformed, or a recommendation's payload does not fit its change.
type ReviewInputError struct{ Message string }

func (e *ReviewInputError) Error() string { return e.Message }

// Errors the review service returns.
var (
	// ErrReviewNoScorecard: the subject has no scorecard for the period.
	ErrReviewNoScorecard = errors.New("no scorecard exists for this subject and period")
	// ErrReviewInsufficient: the scorecard has too few runs; it is never reviewed.
	ErrReviewInsufficient = errors.New("the scorecard has too few runs to review; it is marked insufficient")
	// ErrReviewExists: the subject already has a review for the period.
	ErrReviewExists = errors.New("this subject already has a review for this period")
	// ErrReviewSubjectNotFound: the subject is not in the workspace.
	ErrReviewSubjectNotFound = errors.New("review subject not found in this workspace")
)

// ReviewRecommendationInput is one recommendation the reviewer publishes. For a
// change, Payload holds that change's fields; the type is set from Type.
type ReviewRecommendationInput struct {
	Type      string          `json:"type"`
	Rationale string          `json:"rationale"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// ReviewRequest is a review as the reviewer publishes it.
type ReviewRequest struct {
	SubjectType     string                      `json:"subject_type"`
	SubjectID       string                      `json:"subject_id"`
	Period          string                      `json:"period"`
	PeriodStart     string                      `json:"period_start"`
	Rating          string                      `json:"rating"`
	Narrative       string                      `json:"narrative"`
	CitedMetrics    []string                    `json:"cited_metrics"`
	Recommendations []ReviewRecommendationInput `json:"recommendations"`
}

// ReviewRecommendation is one recommendation as stored. ProposalID is empty for
// an observation and for a change the attention limits refused; Refused says why.
type ReviewRecommendation struct {
	Type       string `json:"type"`
	Rationale  string `json:"rationale"`
	ProposalID string `json:"proposal_id,omitempty"`
	// ProposalIssueID is the issue a proposal is (a proposal is an issue), so the
	// Reviews tab can link to it. Empty for an observation or a refused change.
	ProposalIssueID string `json:"proposal_issue_id,omitempty"`
	Refused         string `json:"refused,omitempty"`
}

// ReviewService publishes and reads reviews.
type ReviewService struct {
	Queries    *db.Queries
	Proposals  *ProposalService
	OrgChanges *OrgChangeService
	// Issues files the monthly review routine's issue.
	Issues *IssueService
	Now    func() time.Time
}

// ReviewPublished is the outcome of a publish: the stored review and the
// recommendations as they were filed.
type ReviewPublished struct {
	ReviewID        string
	Recommendations []ReviewRecommendation
}

// reviewChangeType reports whether a recommendation type is an organisation change.
func reviewChangeType(t string) bool { return slices.Contains(OrgChangeTypes, t) }

// ValidateReviewRequest checks the fields a publish carries, without reading the
// database. It returns the period bounds the review covers.
func ValidateReviewRequest(req ReviewRequest) (start, end time.Time, err error) {
	switch req.SubjectType {
	case ScorecardSubjectAgent, ScorecardSubjectSquad, ScorecardSubjectCompany:
	default:
		return start, end, errors.New("subject_type must be agent, squad or company")
	}
	if _, err := util.ParseUUID(req.SubjectID); err != nil {
		return start, end, errors.New("subject_id must be an id")
	}
	if req.Period != ScorecardPeriodWeek && req.Period != ScorecardPeriodMonth {
		return start, end, errors.New("period must be week or month")
	}
	start, err = time.Parse("2006-01-02", req.PeriodStart)
	if err != nil {
		return start, end, errors.New("period_start must be a date, YYYY-MM-DD")
	}
	if req.Period == ScorecardPeriodWeek {
		end = start.AddDate(0, 0, 7)
	} else {
		end = start.AddDate(0, 1, 0)
	}
	if !slices.Contains(reviewRatings, req.Rating) {
		return start, end, errors.New("rating must be exceeds, meets, below or at_risk")
	}
	if strings.TrimSpace(req.Narrative) == "" {
		return start, end, errors.New("narrative is required")
	}
	if len([]rune(req.Narrative)) > ReviewNarrativeMax {
		return start, end, fmt.Errorf("narrative must be %d characters or fewer", ReviewNarrativeMax)
	}
	if len(req.CitedMetrics) == 0 {
		return start, end, errors.New("cite at least one scorecard metric the review relies on")
	}
	for _, m := range req.CitedMetrics {
		if _, ok := scorecardMetricKeys[m]; !ok {
			return start, end, fmt.Errorf("cited metric %q is not a scorecard metric", m)
		}
	}
	if len(req.Recommendations) > ReviewRecommendationsMax {
		return start, end, fmt.Errorf("a review takes at most %d recommendations", ReviewRecommendationsMax)
	}
	for _, r := range req.Recommendations {
		if r.Type != ReviewRecommendObserve && !reviewChangeType(r.Type) {
			return start, end, fmt.Errorf("recommendation type %q must be observe or one of the organisation change types", r.Type)
		}
		if strings.TrimSpace(r.Rationale) == "" {
			return start, end, errors.New("every recommendation needs a rationale")
		}
		if r.Type != ReviewRecommendObserve && len(r.Payload) == 0 {
			return start, end, fmt.Errorf("recommendation %s needs a payload with the change", r.Type)
		}
	}
	return start, end, nil
}

// scorecardMetricKeys is the set of metric keys a review may cite.
var scorecardMetricKeys = map[string]struct{}{
	MetricRuns: {}, MetricRunsUncharged: {}, MetricSuccessRate: {}, MetricIssuesDone: {},
	MetricFirstPassRate: {}, MetricCycleTimeDays: {}, MetricCostPerDoneUSD: {}, MetricUnpricedTokens: {},
	MetricHumanInterventionRate: {}, MetricProposalAcceptance: {}, MetricRoutineCompliance: {},
	MetricKnowledgePublished: {}, MetricKnowledgeReviews: {}, MetricBriefUsefulness: {},
}

// ReviewFigures renders the cited figures of a scorecard, one line each, in the
// order the review cites them. A metric the scorecard lacks is an error: a
// review cannot cite a figure that is not there.
func ReviewFigures(doc ScorecardDocument, cited []string) ([]string, error) {
	lines := make([]string, 0, len(cited))
	for _, key := range cited {
		m, ok := doc.Metrics[key]
		if !ok {
			return nil, fmt.Errorf("the scorecard has no figure %q", key)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (sample %d)", key, figureText(key, m), m.Sample))
	}
	return lines, nil
}

func figureText(key string, m ScorecardMetric) string {
	if m.Value == nil {
		return "no data"
	}
	switch key {
	case MetricSuccessRate, MetricFirstPassRate, MetricHumanInterventionRate, MetricProposalAcceptance,
		MetricRoutineCompliance, MetricBriefUsefulness:
		return fmt.Sprintf("%.0f%%", *m.Value*100)
	case MetricCostPerDoneUSD:
		return fmt.Sprintf("$%.4f", *m.Value)
	case MetricCycleTimeDays:
		return fmt.Sprintf("%.2f days", *m.Value)
	}
	return fmt.Sprintf("%g", *m.Value)
}

func (r *ReviewService) now() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

// Publish stores a review and files its change recommendations. The reviewer
// must be the workspace's reviewer agent, filing from its own squad. Every
// change payload is validated before any proposal is filed, so a bad payload
// files nothing. A recommendation the attention limits refuse is kept on the
// review with its reason, and the review is still published.
func (r *ReviewService) Publish(ctx context.Context, ws pgtype.UUID, reviewer db.Agent, reviewerSquad pgtype.UUID, req ReviewRequest) (ReviewPublished, error) {
	start, end, err := ValidateReviewRequest(req)
	if err != nil {
		return ReviewPublished{}, &ReviewInputError{Message: err.Error()}
	}
	subjectID, _ := util.ParseUUID(req.SubjectID)
	if err := r.checkSubject(ctx, ws, req.SubjectType, subjectID); err != nil {
		return ReviewPublished{}, err
	}
	periodStart := pgtype.Date{Time: start, Valid: true}
	if _, err := r.Queries.GetReviewForSubjectPeriod(ctx, db.GetReviewForSubjectPeriodParams{
		WorkspaceID: ws, SubjectType: req.SubjectType, SubjectID: subjectID, PeriodStart: periodStart,
	}); err == nil {
		return ReviewPublished{}, ErrReviewExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ReviewPublished{}, err
	}
	sc, err := r.Queries.GetScorecardForPeriod(ctx, db.GetScorecardForPeriodParams{
		WorkspaceID: ws, SubjectType: req.SubjectType, SubjectID: subjectID, Period: req.Period, PeriodStart: periodStart,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewPublished{}, ErrReviewNoScorecard
	}
	if err != nil {
		return ReviewPublished{}, err
	}
	if sc.Status != ScorecardStatusOK {
		return ReviewPublished{}, ErrReviewInsufficient
	}
	doc, err := DecodeScorecardDocument(sc.Metrics)
	if err != nil {
		return ReviewPublished{}, err
	}
	figures, err := ReviewFigures(doc, req.CitedMetrics)
	if err != nil {
		return ReviewPublished{}, err
	}
	narrative := strings.TrimSpace(req.Narrative) + "\n\n## Scorecard figures\n\n" + strings.Join(figures, "\n") +
		"\n\nScorecard period " + start.Format("2006-01-02") + " to " + end.Format("2006-01-02") + "."
	if len([]rune(narrative)) > 8000 {
		return ReviewPublished{}, fmt.Errorf("the narrative with its figures must be 8000 characters or fewer")
	}

	// Every change payload is checked before the first proposal is filed.
	payloads := make([][]byte, len(req.Recommendations))
	for i, rec := range req.Recommendations {
		if rec.Type == ReviewRecommendObserve {
			continue
		}
		raw, err := mergeChangeType(rec.Type, rec.Payload)
		if err != nil {
			return ReviewPublished{}, &ReviewInputError{Message: fmt.Sprintf("recommendation %d: %s", i+1, err)}
		}
		canonical, _, err := r.OrgChanges.ValidateOrgChange(ctx, r.Queries, ws, raw)
		if err != nil {
			return ReviewPublished{}, &ProposalPayloadError{Message: fmt.Sprintf("recommendation %d (%s): %s", i+1, rec.Type, err)}
		}
		payloads[i] = canonical
	}

	recs := make([]ReviewRecommendation, 0, len(req.Recommendations))
	for i, rec := range req.Recommendations {
		out := ReviewRecommendation{Type: rec.Type, Rationale: strings.TrimSpace(rec.Rationale)}
		if rec.Type != ReviewRecommendObserve {
			res, err := r.Proposals.Create(ctx, ProposalCreateParams{
				WorkspaceID: ws,
				SquadID:     reviewerSquad,
				Content: ProposalContent{
					Title:          "Review: " + proposalkinds.OrgChangeTypeLabel(rec.Type),
					Summary:        out.Rationale,
					Evidence:       figures,
					Recommendation: out.Rationale,
					Options:        []string{"Approve the change", "Reject it"},
					CostRisk:       "Organisation change: it is the boss's decision and never approves itself.",
					Ask:            "Approve this organisation change?",
				},
				CreatorType: "agent",
				CreatorID:   reviewer.ID,
				Kind:        proposalkinds.KeyOrgChange,
				Payload:     payloads[i],
			})
			switch {
			case err == nil:
				out.ProposalID = util.UUIDToString(res.Proposal.ID)
				out.ProposalIssueID = util.UUIDToString(res.Proposal.IssueID)
			case isAttentionLimit(err):
				out.Refused = err.Error()
			default:
				return ReviewPublished{}, fmt.Errorf("recommendation %d (%s): %w", i+1, rec.Type, err)
			}
		}
		recs = append(recs, out)
	}

	recRaw, err := json.Marshal(recs)
	if err != nil {
		return ReviewPublished{}, err
	}
	id := dbid.NewV7()
	row, err := r.Queries.CreateReview(ctx, db.CreateReviewParams{
		ID:              id,
		WorkspaceID:     ws,
		SubjectType:     req.SubjectType,
		SubjectID:       subjectID,
		PeriodStart:     periodStart,
		PeriodEnd:       pgtype.Date{Time: end, Valid: true},
		ScorecardID:     sc.ID,
		Rating:          req.Rating,
		Narrative:       narrative,
		Recommendations: recRaw,
		ReviewerAgentID: reviewer.ID,
	})
	if err != nil {
		return ReviewPublished{}, fmt.Errorf("record review: %w", err)
	}
	proposalIDs := []string{}
	for _, rec := range recs {
		if rec.ProposalID != "" {
			proposalIDs = append(proposalIDs, rec.ProposalID)
		}
	}
	if err := NewGovernanceLog().Record(ctx, r.Queries, GovernanceEvent{
		WorkspaceID: ws,
		Kind:        GovernanceKindReviewPublished,
		ActorType:   GovernanceActorAgent,
		ActorID:     reviewer.ID,
		SubjectType: req.SubjectType,
		SubjectID:   subjectID,
		Summary:     fmt.Sprintf("Published a %s review of %s for %s: %s.", req.Period, req.SubjectType, start.Format("2006-01-02"), req.Rating),
		Details: map[string]any{
			"review_id": util.UUIDToString(row.ID), "rating": req.Rating, "scorecard_id": util.UUIDToString(sc.ID),
			"proposal_ids": proposalIDs, "recommendation_count": len(recs),
		},
	}); err != nil {
		slog.Warn("review: governance event not recorded", "review_id", util.UUIDToString(row.ID), "error", err)
	}
	return ReviewPublished{ReviewID: util.UUIDToString(row.ID), Recommendations: recs}, nil
}

// isAttentionLimit reports whether a filing was refused by an attention limit.
func isAttentionLimit(err error) bool {
	var lim *ProposalLimitError
	return errors.As(err, &lim)
}

// mergeChangeType sets payload.type to the recommendation's type. The payload
// must be an object, and it may not name a different type.
func mergeChangeType(t string, payload json.RawMessage) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err != nil || obj == nil {
		return nil, errors.New("payload must be a JSON object with the change's fields")
	}
	if raw, ok := obj["type"]; ok {
		var given string
		if json.Unmarshal(raw, &given) != nil || given != t {
			return nil, fmt.Errorf("payload type %s does not match recommendation type %s", string(raw), t)
		}
	}
	typeRaw, _ := json.Marshal(t)
	obj["type"] = typeRaw
	return json.Marshal(obj)
}

// checkSubject confirms the subject exists in the workspace. A company subject
// is the workspace itself.
func (r *ReviewService) checkSubject(ctx context.Context, ws pgtype.UUID, subjectType string, subjectID pgtype.UUID) error {
	switch subjectType {
	case ScorecardSubjectAgent:
		if _, err := r.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: subjectID, WorkspaceID: ws}); err != nil {
			return ErrReviewSubjectNotFound
		}
	case ScorecardSubjectSquad:
		if _, err := r.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: subjectID, WorkspaceID: ws}); err != nil {
			return ErrReviewSubjectNotFound
		}
	case ScorecardSubjectCompany:
		if subjectID != ws {
			return ErrReviewSubjectNotFound
		}
	}
	return nil
}

// ReviewVisibleSubjects returns the subjects a caller may read reviews and
// scorecards of, when it is neither an owner nor an admin: the squads it leads
// and the agents in them. A nil result means every subject (the caller has no
// filter to apply); an empty one means none.
func ReviewVisibleSubjects(ctx context.Context, q *db.Queries, ws, agentID pgtype.UUID) ([]pgtype.UUID, error) {
	led, err := q.ListSquadsLedByAgent(ctx, db.ListSquadsLedByAgentParams{WorkspaceID: ws, AgentID: agentID})
	if err != nil {
		return nil, err
	}
	out := []pgtype.UUID{}
	if len(led) == 0 {
		return out, nil
	}
	members, err := q.ListAgentsInSquads(ctx, db.ListAgentsInSquadsParams{WorkspaceID: ws, SquadIds: led})
	if err != nil {
		return nil, err
	}
	out = append(out, led...)
	return append(out, members...), nil
}

// ReviewSeesAll reports whether an agent may read every review and scorecard:
// the workspace's reviewer does, since its job is to read them.
func ReviewSeesAll(ctx context.Context, q *db.Queries, ws, agentID pgtype.UUID) (bool, error) {
	reviewer, err := q.GetReviewerAgent(ctx, ws)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return reviewer.ID == agentID, nil
}

// FileMonthlyReviews is the monthly routine (spec 4.6): once a month, in each
// workspace with a reviewer, an issue assigned to the reviewer asks it to review
// the squads and agents of the last completed month. It waits until the second
// of the month, so the nightly scorecards for that month exist. One issue per
// month, found by its title, so a rerun files nothing new.
func (r *ReviewService) FileMonthlyReviews(ctx context.Context) (int, error) {
	now := r.now()
	if now.UTC().Day() < 2 {
		return 0, nil
	}
	start, _, err := ScorecardPeriodBounds(ScorecardPeriodMonth, now)
	if err != nil {
		return 0, err
	}
	title := "Monthly review " + start.Format("2006-01")
	workspaces, err := r.Queries.ListAllWorkspaceIDs(ctx)
	if err != nil {
		return 0, err
	}
	filed := 0
	for _, ws := range workspaces {
		reviewer, err := r.Queries.GetReviewerAgent(ctx, ws)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			slog.Warn("monthly review: reviewer lookup failed", "workspace_id", util.UUIDToString(ws), "error", err)
			continue
		}
		existing, err := r.Queries.CountIssuesWithTitle(ctx, db.CountIssuesWithTitleParams{WorkspaceID: ws, Title: title})
		if err != nil || existing > 0 {
			continue
		}
		if err := r.fileRoutineIssue(ctx, ws, reviewer, title, start); err != nil {
			slog.Warn("monthly review: could not file the issue", "workspace_id", util.UUIDToString(ws), "error", err)
			continue
		}
		filed++
	}
	return filed, nil
}

func (r *ReviewService) fileRoutineIssue(ctx context.Context, ws pgtype.UUID, reviewer db.Agent, title string, start time.Time) error {
	owner, err := r.Queries.GetOldestWorkspaceOwnerMember(ctx, ws)
	if err != nil {
		return fmt.Errorf("workspace has no owner to file the review issue: %w", err)
	}
	desc := fmt.Sprintf(`Review the month of %s.

1. Read the scorecards of the month: %s
2. Review each squad and agent whose scorecard is "ok". A scorecard marked "insufficient" is never reviewed.
3. Review an agent whose success rate is below 60%% or whose first_pass_rate is below 60%%, and sample a few others.
4. For each review, write a JSON file with the review fields and publish it with %s.
5. Each recommendation that changes something carries a payload with that change's fields. Leave the recommendation as "observe" when nothing should change.

Cite the scorecard figures the review relies on. The server copies them from the stored scorecard.`,
		start.Format("2006-01"),
		"`multica review scorecards --period month --json`",
		"`multica review publish --file <path>`")
	if r.Issues == nil {
		return errors.New("review routine has no issue service")
	}
	_, err = r.Issues.Create(ctx, IssueCreateParams{
		WorkspaceID:    ws,
		Title:          title,
		Description:    pgtype.Text{String: desc, Valid: true},
		Status:         "todo",
		Priority:       "none",
		AssigneeType:   pgtype.Text{String: "agent", Valid: true},
		AssigneeID:     reviewer.ID,
		CreatorType:    "member",
		CreatorID:      owner.UserID,
		AllowDuplicate: true,
	}, IssueCreateOpts{})
	return err
}

// The starter company's reviewer: the People & Performance card lists it with
// this role key, and the company template files it as the squad's reviewer
// member, the role the reviewer lookups read.
const (
	CompanyReviewerRoleKey    = "people_reviewer"
	CompanyReviewerMemberRole = "reviewer"
)
