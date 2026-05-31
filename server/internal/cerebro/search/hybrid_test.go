package search

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestBuildHybrid_ShapeWiresEverything(t *testing.T) {
	in := QueryInput{
		WorkspaceID: pgtype.UUID{Valid: true},
		Text:        "deploy production",
		Limit:       20,
		Offset:      0,
	}
	q := BuildHybrid(HybridInput{
		Query:          in,
		QueryVectorLit: "[0.1,0.2,0.3]",
	})

	for _, want := range []string{
		"WITH fts_pool AS",
		"issue_neighbours AS",
		"comment_neighbours AS",
		"vec_pool AS",
		"FULL OUTER JOIN vec_pool",
		"rrf_score",
		"COUNT(*) OVER()",
		"JOIN issue i ON i.id = c.id",
		"ORDER BY c.rrf_score DESC",
	} {
		if !strings.Contains(q.SQL, want) {
			t.Errorf("BuildHybrid SQL missing fragment %q", want)
		}
	}
	if !q.HasText {
		t.Error("BuildHybrid should set HasText=true")
	}
	if len(q.Args) == 0 {
		t.Error("BuildHybrid should produce args")
	}
}

func TestBuildHybrid_AppliesFiltersInOuterWhere(t *testing.T) {
	in := QueryInput{
		WorkspaceID:  pgtype.UUID{Valid: true},
		Text:         "deploy",
		StatusValues: []string{"todo"},
		ProjectIDs:   []any{pgtype.UUID{Valid: true}},
		Limit:        10,
	}
	q := BuildHybrid(HybridInput{
		Query:          in,
		QueryVectorLit: "[0.1]",
	})
	// Filter clauses must appear AT LEAST TWICE — once for the FTS CTE and
	// once for the outer hydrate. Otherwise vector-only candidates would
	// bypass the filters.
	statusOccurrences := strings.Count(q.SQL, "i.status IN")
	if statusOccurrences < 2 {
		t.Errorf("expected status filter to appear in both FTS CTE and outer WHERE, got %d occurrences", statusOccurrences)
	}
	projectOccurrences := strings.Count(q.SQL, "i.project_id IN")
	if projectOccurrences < 2 {
		t.Errorf("expected project filter to appear in both FTS CTE and outer WHERE, got %d occurrences", projectOccurrences)
	}
}

func TestBuildHybrid_WorkspaceClauseAlwaysPresent(t *testing.T) {
	in := QueryInput{
		WorkspaceID: pgtype.UUID{Valid: true},
		Text:        "x",
		Limit:       10,
	}
	q := BuildHybrid(HybridInput{
		Query:          in,
		QueryVectorLit: "[0.1]",
	})
	// Workspace clause shows up THREE times: FTS CTE, issue_neighbours,
	// comment_neighbours, outer WHERE.
	count := strings.Count(q.SQL, "workspace_id")
	if count < 4 {
		t.Errorf("expected workspace_id clause at least 4 times (FTS + 2 vec CTEs + outer), got %d", count)
	}
}
