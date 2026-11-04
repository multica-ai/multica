package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Performance checks for the company (plan 12.4). They build a company of
// 50 squads and 200 initiatives in the test database, then time the digest, and
// the pipeline board's list query with 200 cards. They are slow, so they run only
// when asked:
//
//	MULTICA_COMPANY_PERF=1 go test ./internal/service -run TestCompanyPerf -count=1 -v
//
// Results are recorded in docs/engineering/runbooks/performance.md.
func TestCompanyPerf(t *testing.T) {
	if os.Getenv("MULTICA_COMPANY_PERF") != "1" {
		t.Skip("set MULTICA_COMPANY_PERF=1 to run the company performance checks")
	}
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	q := db.New(pool)
	f, owner := newPrincipalFixture(t)
	ws := f.WorkspaceID
	wsUUID := util.MustParseUUID(ws)
	runtimeID := f.Runtime(t, "perf runtime")

	// 50 squads on six floors, each with one in-progress and one done issue.
	floors := make([]string, 6)
	for i := range floors {
		floors[i] = f.Insert(t, "company_floor", testutil.Cols{"workspace_id": ws, "name": fmt.Sprintf("Floor %d", i), "position": i})
	}
	squads := make([]string, 50)
	for i := range squads {
		agent := f.Agent(t, fmt.Sprintf("perf agent %d", i), runtimeID)
		squads[i] = f.Squad(t, fmt.Sprintf("perf squad %d", i), agent, testutil.Cols{"floor_id": floors[i%6]})
		f.SquadMember(t, squads[i], "agent", agent, testutil.Cols{"role": "leader"})
		f.Issue(t, fmt.Sprintf("perf in progress %d", i), testutil.Cols{"status": "in_progress", "assignee_type": "squad", "assignee_id": squads[i], "number": 1000 + i})
		f.Issue(t, fmt.Sprintf("perf done %d", i), testutil.Cols{"status": "done", "assignee_type": "squad", "assignee_id": squads[i], "number": 2000 + i})
	}

	// The default pipeline, resolved to the first squads, then 200 initiatives.
	resolve := func(_ context.Context, key string) (pgtype.UUID, bool, error) {
		switch key {
		case "product_discovery", "support":
			return util.MustParseUUID(squads[0]), true, nil
		case "product_analysis":
			return util.MustParseUUID(squads[1]), true, nil
		case "build", "learn":
			return util.MustParseUUID(squads[2]), true, nil
		}
		return pgtype.UUID{}, false, nil
	}
	if _, err := NewPipelineService(q, pool).InstallDefault(ctx, InstallPipelineParams{WorkspaceID: wsUUID, ResolveSquad: resolve}); err != nil {
		t.Fatalf("install pipeline: %v", err)
	}
	ini, _ := NewInitiativeSet(NewIssueService(q, pool, nil, nil, nil), nil)
	start := time.Now()
	for i := 0; i < 200; i++ {
		if _, err := ini.Create(ctx, CreateInitiativeParams{
			WorkspaceID: wsUUID, Title: fmt.Sprintf("perf initiative %d", i), Thesis: "Customers pay late.",
			Actor: InitiativeActor{Type: "member", ID: util.MustParseUUID(owner)},
		}); err != nil {
			t.Fatalf("create initiative %d: %v", i, err)
		}
	}
	t.Logf("setup: 50 squads, 6 floors, 200 initiatives created in %v", time.Since(start).Round(time.Millisecond))

	// Digest: median and worst of 20 runs over the whole company.
	digest := &DigestService{Queries: q, Now: time.Now}
	var dur []time.Duration
	var last Digest
	for i := 0; i < 20; i++ {
		s := time.Now()
		d, err := digest.Compute(ctx, wsUUID, time.Now().Add(-24*time.Hour), "PERF")
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		dur = append(dur, time.Since(s))
		last = d
	}
	raw, _ := json.Marshal(last)
	t.Logf("digest, 50 squads and 200 initiatives: median %v worst %v over %d runs; squads in full %d, omitted %d, pipeline rows %d, JSON %d bytes",
		percentileDur(dur, 0.5), percentileDur(dur, 1), len(dur), len(last.Squads), last.OmittedSquadCount, len(last.Pipeline), len(raw))

	// Board: the pipeline's 200 cards, as the board's list query reads them.
	var pipelineID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM pipeline WHERE workspace_id = $1 LIMIT 1`, wsUUID).Scan(&pipelineID); err != nil {
		t.Fatalf("pipeline id: %v", err)
	}
	params := db.ListInitiativesParams{WorkspaceID: wsUUID, PipelineID: util.MustParseUUID(pipelineID)}
	dur = dur[:0]
	for i := 0; i < 50; i++ {
		s := time.Now()
		rows, err := q.ListInitiatives(ctx, params)
		if err != nil {
			t.Fatalf("list initiatives: %v", err)
		}
		dur = append(dur, time.Since(s))
		if len(rows) != 200 {
			t.Fatalf("the board should list 200 cards, got %d", len(rows))
		}
	}
	t.Logf("board list query, 200 cards: median %v worst %v over %d runs", percentileDur(dur, 0.5), percentileDur(dur, 1), len(dur))
	plan, err := explainAnalyze(ctx, pool, `SELECT * FROM initiative
WHERE workspace_id = $1::uuid
  AND ($2::uuid IS NULL OR pipeline_id = $2::uuid)
  AND ($3::text IS NULL OR status = $3::text)
  AND ($4::uuid IS NULL OR owner_squad_id = $4::uuid)
ORDER BY seq DESC
LIMIT 500`, wsUUID, params.PipelineID, pgtype.Text{}, pgtype.UUID{})
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	for _, line := range plan {
		if strings.Contains(line, "Execution Time") || strings.Contains(line, "Planning Time") || strings.Contains(line, "Scan") || strings.Contains(line, "Sort") {
			t.Logf("explain: %s", strings.TrimSpace(line))
		}
	}

	// Brief length: a company of 50 squads with every optional section filled to
	// the spec's caps, measured against the 20000-character column limit and the
	// two-minute reading target.
	body := companyDayMaxBrief()
	t.Logf("brief, 50 squads with the phase 3 and 4 sections at their caps: %d bytes, %d words, %d headings",
		len(body), len(strings.Fields(body)), strings.Count(body, "\n## "))
}

// explainAnalyze runs EXPLAIN ANALYZE on a parameterised query and returns the plan lines.
func explainAnalyze(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) ([]string, error) {
	rows, err := pool.Query(ctx, "EXPLAIN ANALYZE "+sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		out = append(out, line)
	}
	return out, rows.Err()
}

// companyDayMaxBrief is a brief body with every section of the phase 3 and 4
// layout filled to the caps the spec sets: a three-line headline, five decisions,
// one progress line per floor, five blockers, and the optional sections.
func companyDayMaxBrief() string {
	decision := "Approve the pricing pilot for SMB accounts: two weeks of engineering, churn risk on the pilot cohort, recommended by the Product leader after the teardown and the analysis."
	var b strings.Builder
	b.WriteString("## Headline\n")
	b.WriteString("Two decisions wait, one initiative passed validate, and the Build floor is over its weekly budget soft line.\n")
	b.WriteString("Three squads have news; the rest are quiet.\n")
	b.WriteString("Goals are on track except one at risk, with the cause named below.\n\n")
	b.WriteString("## Needs your decision\n")
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&b, "- %s Options: keep seats, pilot usage pricing. Link: /issues/PERF-%d\n", decision, i)
	}
	b.WriteString("\n## Pipeline\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&b, "- INI-%d perf initiative: define, owner Analysis, stage age 4 days.\n", i)
	}
	b.WriteString("\n## Goals\n- Activation: on track, 62 of 80 readings.\n\n")
	b.WriteString("## Progress\n")
	for i := 0; i < 6; i++ {
		fmt.Fprintf(&b, "- Floor %d: 8 squads, 14 done, 5 in progress, one squad with news in full.\n", i)
	}
	b.WriteString("\n## Budget\n- Build floor at 84 percent of its monthly budget; holds in force: 1.\n\n")
	b.WriteString("## People\n- One review published this week; one org change recommended.\n\n")
	b.WriteString("## Blockers and risks\n")
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&b, "- Blocker %d: waiting on a boss decision; proposed way out is to approve or refuse at the next review.\n", i)
	}
	b.WriteString("\n## Watch\nA competitor moved to usage pricing; two market notes from Research.\n\n")
	b.WriteString("## FYI\nSpend is at 61 percent of the company budget; one auto-approved proposal under the standing approval.\n")
	return b.String()
}

func percentileDur(d []time.Duration, p float64) time.Duration {
	c := append([]time.Duration(nil), d...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	if p >= 1 {
		return c[len(c)-1]
	}
	return c[int(float64(len(c)-1)*p)]
}
