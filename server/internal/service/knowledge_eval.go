package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Retrieval health (spec 3.3). An eval case is a question and what a good answer
// looks like: a document of some kind, or one specific document. The Librarian
// runs every case through the search and records the hit rate. A case hits when
// one of the top EvalTopK results matches its expectation.

const (
	// EvalTopK is how many leading results a case is judged on.
	EvalTopK = 5
	// EvalDropThreshold is how far the hit rate may fall between two runs
	// before the digest raises a risk line. It is a fraction: 0.10 is ten points.
	EvalDropThreshold = 0.10
	// evalCaseLimit bounds the cases one run executes; ListKnowledgeEvalCases
	// returns at most this many.
	evalCaseLimit   = 200
	evalQuestionMax = 500
	evalDocIDMax    = 200
)

var (
	// ErrKnowledgeEvalCaseNotFound: no eval case with this id in the workspace.
	ErrKnowledgeEvalCaseNotFound = errors.New("knowledge eval case not found")
)

// EvalCaseInput is a new eval case. Exactly one of ExpectedKind and
// ExpectedDocumentID is set.
type EvalCaseInput struct {
	Question           string
	ExpectedKind       string
	ExpectedDocumentID string
	OwnerSquadID       pgtype.UUID
}

// EvalCaseView is one eval case as the API reports it.
type EvalCaseView struct {
	ID                 string    `json:"id"`
	Question           string    `json:"question"`
	ExpectedKind       string    `json:"expected_kind"`
	ExpectedDocumentID string    `json:"expected_document_id"`
	OwnerSquadID       *string   `json:"owner_squad_id"`
	CreatedByType      string    `json:"created_by_type"`
	CreatedAt          time.Time `json:"created_at"`
}

// EvalResult is one case's outcome in a run. Rank is the position of the first
// matching result, one-based; zero when no top result matched.
type EvalResult struct {
	CaseID             string `json:"case_id"`
	Question           string `json:"question"`
	ExpectedKind       string `json:"expected_kind"`
	ExpectedDocumentID string `json:"expected_document_id"`
	Hit                bool   `json:"hit"`
	Rank               int    `json:"rank"`
}

// EvalRunView is one run. State is "offline" when WeKnora did not answer: such a
// run is not recorded, because a rate computed without the engine is no rate.
type EvalRunView struct {
	ID      string       `json:"id"`
	State   string       `json:"state"`
	Total   int          `json:"total"`
	Hits    int          `json:"hits"`
	HitRate float64      `json:"hit_rate"`
	RanAt   time.Time    `json:"ran_at"`
	Results []EvalResult `json:"results"`
}

// EvalHitRate is the share of cases that hit, zero when there are no cases.
func EvalHitRate(hits, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// evalMatch judges one case against the search results. A case that names a
// document hits only on that document; a case that names a kind hits on any
// result of that kind. Only the top EvalTopK results count.
func evalMatch(c db.KnowledgeEvalCase, hits []KnowledgeHitView) (bool, int) {
	for i, h := range hits {
		if i >= EvalTopK {
			break
		}
		if c.ExpectedDocumentID != "" && h.DocumentID == c.ExpectedDocumentID {
			return true, i + 1
		}
		if c.ExpectedDocumentID == "" && c.ExpectedKind != "" && h.KnowledgeKind == c.ExpectedKind {
			return true, i + 1
		}
	}
	return false, 0
}

// CreateEvalCase saves a case. Any actor may add one: the boss, a member, or a
// squad that found a bad answer.
func (s *KnowledgeService) CreateEvalCase(ctx context.Context, workspaceID pgtype.UUID, actor KnowledgeActor, in EvalCaseInput) (EvalCaseView, error) {
	in.Question = strings.TrimSpace(in.Question)
	in.ExpectedKind = strings.ToLower(strings.TrimSpace(in.ExpectedKind))
	in.ExpectedDocumentID = strings.TrimSpace(in.ExpectedDocumentID)
	switch {
	case in.Question == "":
		return EvalCaseView{}, &KnowledgeInvalidError{"question is required"}
	case utf8.RuneCountInString(in.Question) > evalQuestionMax:
		return EvalCaseView{}, &KnowledgeInvalidError{fmt.Sprintf("question is longer than %d characters", evalQuestionMax)}
	case (in.ExpectedKind == "") == (in.ExpectedDocumentID == ""):
		return EvalCaseView{}, &KnowledgeInvalidError{"name the expected kind or the expected document, not both and not neither"}
	case len(in.ExpectedDocumentID) > evalDocIDMax:
		return EvalCaseView{}, &KnowledgeInvalidError{"expected_document_id is too long"}
	}
	if in.ExpectedKind != "" {
		if _, err := s.loadKnowledgeKind(ctx, workspaceID, in.ExpectedKind); errors.Is(err, ErrKnowledgeKindNotFound) {
			return EvalCaseView{}, &KnowledgeInvalidError{"unknown kind " + in.ExpectedKind}
		} else if err != nil {
			return EvalCaseView{}, err
		}
	}
	createdBy := actor.ID
	if actor.Type == KnowledgeActorSystem {
		createdBy = pgtype.UUID{}
	}
	row, err := s.Queries.CreateKnowledgeEvalCase(ctx, db.CreateKnowledgeEvalCaseParams{
		WorkspaceID: workspaceID, Question: in.Question, ExpectedKind: in.ExpectedKind,
		ExpectedDocumentID: in.ExpectedDocumentID, OwnerSquadID: in.OwnerSquadID,
		CreatedByType: actor.Type, CreatedByID: createdBy,
	})
	if err != nil {
		return EvalCaseView{}, fmt.Errorf("create eval case: %w", err)
	}
	return evalCaseView(row), nil
}

// ListEvalCases returns the workspace's cases, oldest first.
func (s *KnowledgeService) ListEvalCases(ctx context.Context, workspaceID pgtype.UUID) ([]EvalCaseView, error) {
	rows, err := s.Queries.ListKnowledgeEvalCases(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list eval cases: %w", err)
	}
	out := make([]EvalCaseView, 0, len(rows))
	for _, r := range rows {
		out = append(out, evalCaseView(r))
	}
	return out, nil
}

// DeleteEvalCase removes a case from the workspace.
func (s *KnowledgeService) DeleteEvalCase(ctx context.Context, workspaceID pgtype.UUID, caseID string) error {
	id, err := util.ParseUUID(caseID)
	if err != nil {
		return ErrKnowledgeEvalCaseNotFound
	}
	n, err := s.Queries.DeleteKnowledgeEvalCase(ctx, db.DeleteKnowledgeEvalCaseParams{WorkspaceID: workspaceID, ID: id})
	if err != nil {
		return fmt.Errorf("delete eval case: %w", err)
	}
	if n == 0 {
		return ErrKnowledgeEvalCaseNotFound
	}
	return nil
}

// RunEval runs every case through the search and records the run. A workspace
// with no cases has nothing to measure and records nothing. An offline WeKnora
// returns State "offline" and records nothing.
func (s *KnowledgeService) RunEval(ctx context.Context, workspaceID pgtype.UUID, now time.Time) (EvalRunView, error) {
	cases, err := s.Queries.ListKnowledgeEvalCases(ctx, workspaceID)
	if err != nil {
		return EvalRunView{}, fmt.Errorf("list eval cases: %w", err)
	}
	out := EvalRunView{State: KnowledgeStateReady, Results: []EvalResult{}}
	if len(cases) == 0 {
		return out, nil
	}
	hits := 0
	for _, c := range cases {
		res, err := s.SearchKind(ctx, workspaceID, c.Question, "", "", EvalTopK)
		if err != nil {
			return EvalRunView{}, err
		}
		if res.State != KnowledgeStateReady {
			return EvalRunView{State: res.State, Results: []EvalResult{}}, nil
		}
		hit, rank := evalMatch(c, res.Hits)
		if hit {
			hits++
		}
		out.Results = append(out.Results, EvalResult{
			CaseID: util.UUIDToString(c.ID), Question: c.Question, ExpectedKind: c.ExpectedKind,
			ExpectedDocumentID: c.ExpectedDocumentID, Hit: hit, Rank: rank,
		})
	}
	out.Total, out.Hits = len(cases), hits
	out.HitRate = EvalHitRate(hits, len(cases))
	encoded, err := json.Marshal(out.Results)
	if err != nil {
		return EvalRunView{}, fmt.Errorf("encode eval results: %w", err)
	}
	row, err := s.Queries.CreateKnowledgeEvalRun(ctx, db.CreateKnowledgeEvalRunParams{
		WorkspaceID: workspaceID, Total: int32(out.Total), Hits: int32(out.Hits), HitRate: out.HitRate, Results: encoded,
	})
	if err != nil {
		return EvalRunView{}, fmt.Errorf("record eval run: %w", err)
	}
	out.ID = util.UUIDToString(row.ID)
	out.RanAt = row.RanAt.Time
	return out, nil
}

// ListEvalRuns returns the newest runs, newest first, at most limit of them.
func (s *KnowledgeService) ListEvalRuns(ctx context.Context, workspaceID pgtype.UUID, limit int) ([]EvalRunView, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.Queries.ListKnowledgeEvalRuns(ctx, db.ListKnowledgeEvalRunsParams{WorkspaceID: workspaceID, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("list eval runs: %w", err)
	}
	out := make([]EvalRunView, 0, len(rows))
	for _, r := range rows {
		out = append(out, evalRunView(r))
	}
	return out, nil
}

// DigestRetrieval is the retrieval health the digest reports: the latest eval
// run, the one before it, and the risk when the rate fell by more than
// EvalDropThreshold. It is nil when no run has been recorded.
type DigestRetrieval struct {
	HitRate         float64   `json:"hit_rate"`
	Hits            int       `json:"hits"`
	Total           int       `json:"total"`
	RanAt           time.Time `json:"ran_at"`
	PreviousHitRate *float64  `json:"previous_hit_rate"`
	Dropped         bool      `json:"dropped"`
	Risk            string    `json:"risk,omitempty"`
}

// retrievalHealth builds the digest section from the newest runs first.
func retrievalHealth(runs []db.KnowledgeEvalRun) *DigestRetrieval {
	if len(runs) == 0 {
		return nil
	}
	latest := runs[0]
	out := &DigestRetrieval{
		HitRate: latest.HitRate, Hits: int(latest.Hits), Total: int(latest.Total), RanAt: latest.RanAt.Time,
	}
	if len(runs) < 2 {
		return out
	}
	prev := runs[1].HitRate
	out.PreviousHitRate = &prev
	if prev-latest.HitRate > EvalDropThreshold {
		out.Dropped = true
		out.Risk = fmt.Sprintf("Retrieval hit rate fell from %.0f%% to %.0f%% (%d of %d eval cases hit).",
			prev*100, latest.HitRate*100, latest.Hits, latest.Total)
	}
	return out
}

func evalCaseView(r db.KnowledgeEvalCase) EvalCaseView {
	v := EvalCaseView{
		ID: util.UUIDToString(r.ID), Question: r.Question, ExpectedKind: r.ExpectedKind,
		ExpectedDocumentID: r.ExpectedDocumentID, CreatedByType: r.CreatedByType, CreatedAt: r.CreatedAt.Time,
	}
	if r.OwnerSquadID.Valid {
		id := util.UUIDToString(r.OwnerSquadID)
		v.OwnerSquadID = &id
	}
	return v
}

func evalRunView(r db.KnowledgeEvalRun) EvalRunView {
	out := EvalRunView{
		ID: util.UUIDToString(r.ID), State: KnowledgeStateReady, Total: int(r.Total), Hits: int(r.Hits),
		HitRate: r.HitRate, RanAt: r.RanAt.Time, Results: []EvalResult{},
	}
	_ = json.Unmarshal(r.Results, &out.Results)
	if out.Results == nil {
		out.Results = []EvalResult{}
	}
	return out
}
