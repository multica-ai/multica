package handler

import (
	"fmt"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Keep the large-roster/sparse-work case: array membership can dominate the
// task scan when the planner prefers the global running index (MUL-7838).
// Measure the whole handler, including permission reads and snapshot setup.
func BenchmarkIssueTableWorkingAgentsFacet(b *testing.B) {
	for _, tc := range []struct {
		agents, running int
	}{{60, 40}, {800, 2}} {
		b.Run(fmt.Sprintf("agents=%d/running=%d", tc.agents, tc.running), func(b *testing.B) {
			workspaceID, projectID, want := seedWorkingAgentsFacetBenchmark(b, tc.agents, tc.running)
			b.ResetTimer()
			for range b.N {
				req := workingAgentsFacetRequest(map[string]any{"kind": "project", "project_id": projectID}, nil)
				req.Header.Set("X-Workspace-ID", workspaceID)
				var response issueTableFacetsResponse
				testutil.Call(b, testHandler.ListIssueTableFacets, req).Want(http.StatusOK).JSON(&response)
				got := map[string]int64{}
				for _, value := range response.Facets[0].Values {
					got[value.Key] = value.Count
				}
				if !maps.Equal(got, want) {
					b.Fatalf("working agents = %v, want %v", got, want)
				}
			}
			b.StopTimer()
		})
	}
}

func seedWorkingAgentsFacetBenchmark(tb testutil.TB, agentCount, runningCount int) (string, string, map[string]int64) {
	tb.Helper()
	workspaceID := dbfx.Workspace(tb, "working facet benchmark", fmt.Sprintf("working-facet-bench-%d", time.Now().UnixNano()))
	dbfx.Member(tb, workspaceID, testUserID, "owner")
	fx := testutil.New(testPool, workspaceID, testUserID)
	projectID := fx.Project(tb, "working facet benchmark")
	ids := make([]pgtype.UUID, 0, agentCount)
	want := map[string]int64{}
	for n := range agentCount {
		id := fx.Agent(tb, fmt.Sprintf("working facet benchmark %d", n), "")
		ids = append(ids, parseUUID(id))
	}
	for n := range runningCount {
		// Only the first twelve agents hold work in the small-roster case.
		want[uuidToString(ids[n%12])]++
	}
	fx.Cleanup(tb, `DELETE FROM issue WHERE project_id = $1`, projectID)
	fx.Exec(tb, `INSERT INTO issue (workspace_id, project_id, title, number, creator_type, creator_id, assignee_type, assignee_id)
  SELECT $1, $2, 'working facet benchmark ' || n, n, 'member', $3,
    CASE WHEN n <= 4000 THEN 'member' ELSE NULL END,
    CASE WHEN n <= 4000 THEN $3::uuid ELSE NULL END
  FROM generate_series(1,20000) n`, workspaceID, projectID, testUserID)
	foreignWorkspace := dbfx.Workspace(tb, "working facet benchmark foreign", fmt.Sprintf("working-facet-foreign-%d", time.Now().UnixNano()))
	foreignAgent := dbfx.Agent(tb, "working facet benchmark foreign", "", testutil.Cols{"workspace_id": foreignWorkspace})
	foreignIssue := dbfx.Issue(tb, "working facet benchmark foreign", testutil.Cols{"workspace_id": foreignWorkspace})
	fx.Cleanup(tb, `DELETE FROM agent_task_queue atq
  USING unnest($1::uuid[] || ARRAY[$2::uuid]) cand(agent_id)
  WHERE atq.agent_id = cand.agent_id`, ids, foreignAgent)
	fx.Exec(tb, `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
  SELECT ($1::uuid[])[1 + ((i.number - 1) % 12)], $2, i.id, 'running'
  FROM issue i WHERE i.project_id = $3 AND i.number <= $4`, ids, testRuntimeID, projectID, runningCount)
	fx.Exec(tb, `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
  SELECT $1, $2, $3, 'running' FROM generate_series(1,20000)`, foreignAgent, testRuntimeID, foreignIssue)
	// Historical local work makes agent selectivity alone a poor predictor of
	// running membership, as on a busy long-lived workspace. With only foreign
	// running rows, statistics can trivially exclude the foreign agent instead.
	fx.Exec(tb, `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
  SELECT ($1::uuid[])[1 + ((n - 1) % cardinality($1::uuid[]))], $2, i.id, 'completed'
  FROM generate_series(1,500000) n
  CROSS JOIN (SELECT id FROM issue WHERE project_id = $3 AND number = 1) i`, ids, testRuntimeID, projectID)
	for _, table := range []string{"agent", "issue", "agent_task_queue"} {
		fx.Exec(tb, "ANALYZE "+table)
	}
	return workspaceID, projectID, want
}
