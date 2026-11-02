package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Prior art: a new proposal reports earlier proposals and decisions that look
// related, so the leader can cite them and the boss sees the history. It never
// blocks filing. Every failure in the lookup is logged and leaves the list
// shorter.
const (
	// priorArtWindow bounds the earlier proposals compared by title.
	priorArtWindow = 90 * 24 * time.Hour
	// priorArtTitleMinOverlap is the lowest Jaccard overlap of two titles'
	// words that counts as related. At 0.5, "Pilot usage pricing" matches
	// "Pilot usage-based pricing for new accounts" (3 shared of 6), and a single
	// shared word between two long titles does not match.
	priorArtTitleMinOverlap = 0.5
	// priorArtDecisionMinScore is the lowest retrieval score a decision record
	// needs to be reported (spec 2.4).
	priorArtDecisionMinScore = 0.75
	// priorArtDecisionLimit caps the decision records reported for one proposal.
	priorArtDecisionLimit = 3
	// priorArtTitleLimit caps the earlier proposals reported for one proposal.
	priorArtTitleLimit = 5
	// priorArtSearchTimeout bounds the decisions search, so a slow sidecar
	// delays the filing response by at most this long.
	priorArtSearchTimeout = 2 * time.Second
)

// Kinds of ProposalRelated entry.
const (
	RelatedKindProposal = "proposal"
	RelatedKindDecision = "decision"
)

// ProposalRelated is an earlier proposal or decision that looks related to a
// new one. For a proposal, ID is the proposal id and Score is the title
// overlap. For a decision, ID is the knowledge document id and Score is the
// retrieval score.
type ProposalRelated struct {
	Kind  string
	ID    string
	Title string
	Score float64
	// Set for proposals only.
	State           string
	IssueIdentifier string
	CreatedAt       time.Time
}

// DecisionSearcher is the part of the knowledge service the prior-art lookup
// needs. *KnowledgeService satisfies it.
type DecisionSearcher interface {
	Search(ctx context.Context, workspaceID pgtype.UUID, query, kb string, limit int) (KnowledgeSearchResult, error)
}

// priorArtStopWords are the function words that say nothing about what a
// proposal is about.
var priorArtStopWords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "of": {}, "for": {}, "to": {}, "and": {}, "or": {},
	"in": {}, "on": {}, "with": {}, "by": {}, "at": {}, "from": {}, "as": {}, "is": {},
	"be": {}, "our": {}, "we": {}, "us": {}, "it": {}, "its": {}, "this": {}, "that": {},
}

// titleWords returns the distinct words of a title for comparison: lowercased
// letters and digits, function words dropped, and a trailing plural s removed
// from longer words, so "prices" and "price" compare equal.
func titleWords(title string) map[string]struct{} {
	words := map[string]struct{}{}
	for _, w := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if _, stop := priorArtStopWords[w]; stop {
			continue
		}
		if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
			w = strings.TrimSuffix(w, "s")
		}
		words[w] = struct{}{}
	}
	return words
}

// titleOverlap is the Jaccard index of two titles' words: shared words over
// all distinct words. It is 0 when either title has no words.
func titleOverlap(a, b string) float64 {
	wa, wb := titleWords(a), titleWords(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	shared := 0
	for w := range wa {
		if _, ok := wb[w]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(wa)+len(wb)-shared)
}

// relatedCandidate is an earlier proposal read for the title comparison.
type relatedCandidate struct {
	ID         pgtype.UUID
	State      string
	Title      string
	CreatedAt  time.Time
	Identifier string
}

// relatedByTitle keeps the candidates whose title overlaps the new title by at
// least priorArtTitleMinOverlap, most similar first. The proposal being filed
// is skipped.
func relatedByTitle(candidates []relatedCandidate, self pgtype.UUID, title string) []ProposalRelated {
	var out []ProposalRelated
	for _, c := range candidates {
		if c.ID == self {
			continue
		}
		score := titleOverlap(title, c.Title)
		if score < priorArtTitleMinOverlap {
			continue
		}
		out = append(out, ProposalRelated{
			Kind:            RelatedKindProposal,
			ID:              util.UUIDToString(c.ID),
			Title:           c.Title,
			Score:           score,
			State:           c.State,
			IssueIdentifier: c.Identifier,
			CreatedAt:       c.CreatedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > priorArtTitleLimit {
		out = out[:priorArtTitleLimit]
	}
	return out
}

// relatedDecisions keeps the decision-base hits that score at or above
// priorArtDecisionMinScore, best first.
func relatedDecisions(hits []KnowledgeHitView) []ProposalRelated {
	var out []ProposalRelated
	for _, h := range hits {
		if h.KnowledgeBase != DecisionsKnowledgeKey || h.Score < priorArtDecisionMinScore {
			continue
		}
		out = append(out, ProposalRelated{
			Kind:  RelatedKindDecision,
			ID:    h.DocumentID,
			Title: h.Title,
			Score: h.Score,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > priorArtDecisionLimit {
		out = out[:priorArtDecisionLimit]
	}
	return out
}

// findPriorArt returns the related proposals and decisions for a proposal that
// has just been filed. It never fails: each lookup logs its own failure and
// contributes nothing.
func (s *ProposalService) findPriorArt(ctx context.Context, workspaceID, proposalID pgtype.UUID, title, summary string) []ProposalRelated {
	out := []ProposalRelated{}
	out = append(out, s.relatedProposalsByTitle(ctx, workspaceID, proposalID, title)...)
	out = append(out, s.relatedDecisionsFor(ctx, workspaceID, proposalID, title, summary)...)
	return out
}

func (s *ProposalService) relatedProposalsByTitle(ctx context.Context, workspaceID, proposalID pgtype.UUID, title string) []ProposalRelated {
	q := s.Issues.Queries
	rows, err := q.ListRecentProposalsForPriorArt(ctx, db.ListRecentProposalsForPriorArtParams{
		WorkspaceID: workspaceID,
		CreatedAt:   pgtype.Timestamptz{Time: time.Now().Add(-priorArtWindow), Valid: true},
	})
	if err != nil {
		slog.Warn("proposal prior art: could not list earlier proposals; filed without them",
			"workspace_id", util.UUIDToString(workspaceID), "error", err)
		return nil
	}
	prefix := ""
	if ws, err := q.GetWorkspace(ctx, workspaceID); err == nil {
		prefix = ws.IssuePrefix
	}
	candidates := make([]relatedCandidate, 0, len(rows))
	for _, r := range rows {
		candidates = append(candidates, relatedCandidate{
			ID:         r.ID,
			State:      r.State,
			Title:      r.Title,
			CreatedAt:  r.CreatedAt.Time,
			Identifier: IssueIdentifier(prefix, r.Number),
		})
	}
	return relatedByTitle(candidates, proposalID, title)
}

// relatedDecisionsFor searches the decisions base with the proposal's title and
// summary. Knowledge that is off, offline or failing contributes nothing; only
// an unexpected failure is logged at warning level.
func (s *ProposalService) relatedDecisionsFor(ctx context.Context, workspaceID, proposalID pgtype.UUID, title, summary string) []ProposalRelated {
	if s.DecisionSearch == nil {
		return nil
	}
	query := knowledgeIssueQuery(title, summary)
	if query == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, priorArtSearchTimeout)
	defer cancel()
	res, err := s.DecisionSearch.Search(ctx, workspaceID, query, DecisionsKnowledgeKey, priorArtDecisionLimit)
	if err != nil {
		var notReady *KnowledgeNotReadyError
		if errors.As(err, &notReady) || errors.Is(err, ErrKnowledgeBaseNotFound) {
			return nil
		}
		slog.Warn("proposal prior art: decisions search failed; filed without it",
			"workspace_id", util.UUIDToString(workspaceID), "proposal_id", util.UUIDToString(proposalID), "error", err)
		return nil
	}
	if res.State != KnowledgeStateReady {
		return nil
	}
	return relatedDecisions(res.Hits)
}

// ProposalPriorArtSubject is a proposal a read reports related proposals for.
type ProposalPriorArtSubject struct {
	ID        pgtype.UUID
	Title     string
	CreatedAt time.Time
}

// RelatedProposalsByTitle returns, for each subject, the earlier proposals of
// the workspace that look related by title. "Earlier" means filed within
// priorArtWindow before the subject, the same window a new filing is compared
// against, so a read gives the same answer whenever it is made. Reads never
// search the decisions base: that costs a knowledge call per proposal.
//
// The map is keyed by proposal id. A subject with no matches has no entry. The
// lookup is bounded by the newest 500 proposals in the window (the query's
// limit), so a subject older than that may see fewer matches.
func (s *ProposalService) RelatedProposalsByTitle(ctx context.Context, workspaceID pgtype.UUID, subjects []ProposalPriorArtSubject, prefix string) (map[string][]ProposalRelated, error) {
	out := map[string][]ProposalRelated{}
	if len(subjects) == 0 {
		return out, nil
	}
	oldest := subjects[0].CreatedAt
	for _, sub := range subjects {
		if sub.CreatedAt.Before(oldest) {
			oldest = sub.CreatedAt
		}
	}
	rows, err := s.Issues.Queries.ListRecentProposalsForPriorArt(ctx, db.ListRecentProposalsForPriorArtParams{
		WorkspaceID: workspaceID,
		CreatedAt:   pgtype.Timestamptz{Time: oldest.Add(-priorArtWindow), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list proposals for prior art: %w", err)
	}
	candidates := make([]relatedCandidate, 0, len(rows))
	for _, r := range rows {
		candidates = append(candidates, relatedCandidate{
			ID:         r.ID,
			State:      r.State,
			Title:      r.Title,
			CreatedAt:  r.CreatedAt.Time,
			Identifier: IssueIdentifier(prefix, r.Number),
		})
	}
	for _, sub := range subjects {
		var earlier []relatedCandidate
		for _, c := range candidates {
			if c.CreatedAt.Before(sub.CreatedAt) && !c.CreatedAt.Before(sub.CreatedAt.Add(-priorArtWindow)) {
				earlier = append(earlier, c)
			}
		}
		if related := relatedByTitle(earlier, sub.ID, sub.Title); len(related) > 0 {
			out[util.UUIDToString(sub.ID)] = related
		}
	}
	return out, nil
}
