package service

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// Automatic context for an issue run. The server runs one retrieval with the
// issue's title and description and hands the best few passages to the run.
// Retrieval always returns its closest matches however weak, so a score floor
// decides what is worth the agent's attention.
const (
	// knowledgeIssueContextTimeout bounds the retrieval so a slow sidecar
	// delays the claim by at most this long and never fails it.
	knowledgeIssueContextTimeout = 2 * time.Second
	// knowledgeIssueContextLimit is how many passages a run receives.
	knowledgeIssueContextLimit = 3
	// knowledgeIssueContextMinScore is the lowest score a passage needs to be
	// attached. Below it the match is noise, and the run is better without it.
	knowledgeIssueContextMinScore = 0.5
	// knowledgeIssueQueryRunes caps the query, so a long description cannot
	// make the retrieval itself slow or unfocused.
	knowledgeIssueQueryRunes = 500
)

// knowledgeIssueQuery is the retrieval query for an issue: its title and
// description, flattened to one line. Empty when the issue has no title.
func knowledgeIssueQuery(title, description string) string {
	if strings.TrimSpace(title) == "" {
		return ""
	}
	query := strings.Join(strings.Fields(title+" "+description), " ")
	return truncateForSummary(query, knowledgeIssueQueryRunes)
}

// selectIssueContext keeps the best passages that clear the score floor, best
// first, up to the run's limit. It returns an empty slice when nothing clears
// the floor, which is the common case for an unrelated issue.
func selectIssueContext(hits []KnowledgeHitView) []KnowledgeHitView {
	eligible := make([]KnowledgeHitView, 0, len(hits))
	for _, hit := range hits {
		if hit.Score >= knowledgeIssueContextMinScore {
			eligible = append(eligible, hit)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool { return eligible[i].Score > eligible[j].Score })
	if len(eligible) > knowledgeIssueContextLimit {
		eligible = eligible[:knowledgeIssueContextLimit]
	}
	return eligible
}

// IssueContext returns the company knowledge an issue run starts with. Any
// failure (knowledge not ready, the sidecar down, the two-second budget spent,
// a malformed response) yields nil and is logged at debug level: automatic
// context is an aid, and the run must proceed without it.
func (s *KnowledgeService) IssueContext(ctx context.Context, workspaceID pgtype.UUID, title, description string) []KnowledgeHitView {
	query := knowledgeIssueQuery(title, description)
	if query == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, knowledgeIssueContextTimeout)
	defer cancel()
	// Search checks readiness itself, so a workspace without knowledge stops
	// here with an error that is dropped below.
	// Automatic context is not any one agent's reading: it sees the
	// workspace-visible bases only, never a restricted one.
	result, err := s.SearchKindFor(ctx, workspaceID, KnowledgeActor{Type: KnowledgeActorAgent}, query, "", "", knowledgeIssueContextLimit)
	if err != nil {
		slog.Debug("knowledge: issue context skipped", "workspace_id", util.UUIDToString(workspaceID), "error", err)
		return nil
	}
	if result.State != KnowledgeStateReady {
		return nil
	}
	return selectIssueContext(result.Hits)
}
