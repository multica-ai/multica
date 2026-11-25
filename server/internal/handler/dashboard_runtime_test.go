package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// runtimeUsageModelPrefix tags every usage row these tests seed, so sums can be
// isolated from rows other tests left in the shared workspace.
const runtimeUsageModelPrefix = "runtime-usage-test-"

type byRuntimeRow struct {
	RuntimeID string `json:"runtime_id"`
	TaskCount int32  `json:"task_count"`
	Usage     []struct {
		Provider    string `json:"provider"`
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	} `json:"usage"`
}

func getByRuntime(t *testing.T, userID, query string) ([]byRuntimeRow, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.GetDashboardUsageByRuntime(w, newRequestAs(userID, http.MethodGet, "/api/dashboard/usage/by-runtime"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("by-runtime%s: expected 200, got %d: %s", query, w.Code, w.Body.String())
	}
	var rows []byRuntimeRow
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("by-runtime%s: decode: %v (%s)", query, err, w.Body.String())
	}
	return rows, w
}

// seededTokensByRuntime sums only rows whose model starts with prefix, per
// runtime id.
func seededTokensByRuntime(rows []byRuntimeRow, prefix string) map[string]int64 {
	out := map[string]int64{}
	for _, row := range rows {
		for _, u := range row.Usage {
			if strings.HasPrefix(u.Model, prefix) {
				out[row.RuntimeID] += u.InputTokens
			}
		}
	}
	return out
}

func runtimeTaskCount(rows []byRuntimeRow, runtimeID string) int32 {
	for _, row := range rows {
		if row.RuntimeID == runtimeID {
			return row.TaskCount
		}
	}
	return 0
}

func sumTokens(m map[string]int64) int64 {
	var n int64
	for _, v := range m {
		n += v
	}
	return n
}

// seededKPITokens is what usage/daily and usage/by-agent report for the rows
// whose model starts with prefix: the Tokens KPI and the leaderboard. usage/daily
// carries one calendar day of headroom that the client trims; fromDate, when
// set, trims it the same way.
func seededKPITokens(t *testing.T, userID, query, prefix string, fromDate ...string) (daily, byAgent int64) {
	t.Helper()
	type usageRow struct {
		Date        string `json:"date"`
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	}
	sum := func(handler http.HandlerFunc, path string, fromDate string) int64 {
		w := httptest.NewRecorder()
		handler(w, newRequestAs(userID, http.MethodGet, path+query, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s%s: expected 200, got %d: %s", path, query, w.Code, w.Body.String())
		}
		var rows []usageRow
		if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}
		var n int64
		for _, row := range rows {
			if strings.HasPrefix(row.Model, prefix) && row.Date >= fromDate {
				n += row.InputTokens
			}
		}
		return n
	}
	from := ""
	if len(fromDate) > 0 {
		from = fromDate[0]
	}
	return sum(testHandler.GetDashboardUsageDaily, "/api/dashboard/usage/daily", from),
		sum(testHandler.GetDashboardUsageByAgent, "/api/dashboard/usage/by-agent", "")
}

func rollupTaskUsageHourly(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup window: %v", err)
	}
}

// seedRuntimeUsage queues one completed run on runtimeID and gives it one
// usage row per entry, all tagged with prefix.
func seedRuntimeUsage(t *testing.T, agentID, runtimeID, prefix string, extra testutil.Cols, usage ...runtimeUsageSeed) string {
	t.Helper()
	cols := testutil.Cols{
		"runtime_id":   runtimeID,
		"status":       "completed",
		"started_at":   testutil.Raw("now() - interval '20 minutes'"),
		"completed_at": testutil.Raw("now() - interval '10 minutes'"),
	}
	for k, v := range extra {
		cols[k] = v
	}
	taskID := dbfx.Task(t, agentID, cols)
	for _, u := range usage {
		at := any(testutil.Raw("now()"))
		if !u.at.IsZero() {
			at = u.at
		}
		dbfx.InsertNoID(t, "task_usage", testutil.Cols{
			"task_id":       taskID,
			"provider":      "claude",
			"model":         prefix + u.model,
			"input_tokens":  u.tokens,
			"output_tokens": 0,
			"created_at":    at,
		}, "task_id = $1", taskID)
	}
	dbfx.Cleanup(t, `DELETE FROM task_usage_hourly WHERE model LIKE $1`, prefix+"%")
	return taskID
}

type runtimeUsageSeed struct {
	model  string
	tokens int64
	at     time.Time
}

type runtimeUsageFixture struct {
	alice, bob                         string // plain members; alice owns the three runtimes
	public, private, shared, unrelated string // runtime ids
	projectID, squadID                 string
}

// seedRuntimeUsageFixture seeds four runs, all with usage in the last hours:
//
//	public   (alice, public)             run 1: models a=100 + b=200 now, c=5 two hours ago
//	public                               run 2: a=50, in a project
//	private  (alice, private)            run 3: a=400, squad_id = the squad
//	shared   (alice, shared with bob)    run 4: a=25, usage_squad_id = the squad
func seedRuntimeUsageFixture(t *testing.T) runtimeUsageFixture {
	t.Helper()
	marker := fmt.Sprintf("rtu%d", time.Now().UnixNano())
	f := runtimeUsageFixture{
		alice: createPlainMember(t, marker+"-alice@multica.test"),
		bob:   createPlainMember(t, marker+"-bob@multica.test"),
	}
	f.public = dbfx.Runtime(t, marker+" public", testutil.Cols{"owner_id": f.alice, "visibility": "public"})
	f.private = dbfx.Runtime(t, marker+" private", testutil.Cols{"owner_id": f.alice, "visibility": "private"})
	f.shared = dbfx.Runtime(t, marker+" shared", testutil.Cols{
		"owner_id": f.alice, "visibility": "private",
		"shared_with": testutil.Raw(fmt.Sprintf("ARRAY['%s']::uuid[]", f.bob)),
	})
	agentID := dbfx.Agent(t, marker+" agent", f.public)
	f.projectID = dbfx.Project(t, marker+" project")
	f.squadID = dbfx.Squad(t, marker+" squad", agentID)
	projectIssue := dbfx.Issue(t, marker+" project issue", testutil.Cols{"project_id": f.projectID})

	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	seedRuntimeUsage(t, agentID, f.public, runtimeUsageModelPrefix, nil,
		runtimeUsageSeed{model: "a", tokens: 100},
		runtimeUsageSeed{model: "b", tokens: 200},
		runtimeUsageSeed{model: "c", tokens: 5, at: twoHoursAgo})
	seedRuntimeUsage(t, agentID, f.public, runtimeUsageModelPrefix,
		testutil.Cols{"issue_id": projectIssue},
		runtimeUsageSeed{model: "a", tokens: 50})
	seedRuntimeUsage(t, agentID, f.private, runtimeUsageModelPrefix,
		testutil.Cols{"squad_id": f.squadID},
		runtimeUsageSeed{model: "a", tokens: 400})
	seedRuntimeUsage(t, agentID, f.shared, runtimeUsageModelPrefix,
		testutil.Cols{"usage_squad_id": f.squadID},
		runtimeUsageSeed{model: "a", tokens: 25})
	rollupTaskUsageHourly(t)
	return f
}

func TestDashboardUsageByRuntimeAggregatesAndScopes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedRuntimeUsageFixture(t)
	const query = "?days=7"
	const total = 780 // 100 + 200 + 5 + 50 + 400 + 25

	// ---- the runtime owner can read all three runtimes ------------------------
	rows, _ := getByRuntime(t, f.alice, query)
	tokens := seededTokensByRuntime(rows, runtimeUsageModelPrefix)
	if tokens[f.public] != 355 || tokens[f.private] != 400 || tokens[f.shared] != 25 {
		t.Errorf("alice: tokens = %v, want public=355 private=400 shared=25", tokens)
	}
	// Run 1 used three models across two hours: still one run.
	if got := runtimeTaskCount(rows, f.public); got != 2 {
		t.Errorf("alice: public task_count = %d, want 2 (a multi-model, multi-hour run counts once)", got)
	}
	if runtimeTaskCount(rows, f.private) != 1 || runtimeTaskCount(rows, f.shared) != 1 {
		t.Errorf("alice: private/shared task_count = %d/%d, want 1/1",
			runtimeTaskCount(rows, f.private), runtimeTaskCount(rows, f.shared))
	}

	// ---- everyone else: the runtimes they cannot read are folded --------------
	for _, tc := range []struct {
		name     string
		viewer   string
		named    map[string]int64
		hidden   []string
		restrict int64
	}{
		// Workspace owner: can list alice's private runtimes, cannot read their
		// usage (the runtime usage gate), so they are folded too.
		{"workspace owner", testUserID, map[string]int64{f.public: 355}, []string{f.private, f.shared}, 425},
		{"shared-with member", f.bob, map[string]int64{f.public: 355, f.shared: 25}, []string{f.private}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, w := getByRuntime(t, tc.viewer, query)
			got := seededTokensByRuntime(rows, runtimeUsageModelPrefix)
			for id, want := range tc.named {
				if got[id] != want {
					t.Errorf("runtime %s: tokens = %d, want %d", id, got[id], want)
				}
			}
			if got[restrictedRuntimesRowID] != tc.restrict {
				t.Errorf("restricted bucket: tokens = %d, want %d", got[restrictedRuntimesRowID], tc.restrict)
			}
			for _, id := range tc.hidden {
				if strings.Contains(w.Body.String(), id) {
					t.Errorf("response names a runtime the caller cannot read (%s): %s", id, w.Body.String())
				}
			}
			if runtimeTaskCount(rows, restrictedRuntimesRowID) < int32(len(tc.hidden)) {
				t.Errorf("restricted bucket: task_count = %d, want >= %d",
					runtimeTaskCount(rows, restrictedRuntimesRowID), len(tc.hidden))
			}
			// The bucket is one row per (provider, model): no per-runtime split.
			seen := map[string]bool{}
			for _, row := range rows {
				if row.RuntimeID != restrictedRuntimesRowID {
					continue
				}
				for _, u := range row.Usage {
					key := u.Provider + "/" + u.Model
					if seen[key] {
						t.Errorf("restricted bucket repeats %s", key)
					}
					seen[key] = true
				}
			}
			// Named + folded rows add up to the Tokens KPI and the leaderboard.
			daily, byAgent := seededKPITokens(t, tc.viewer, query, runtimeUsageModelPrefix)
			if sum := sumTokens(got); sum != total || daily != total || byAgent != total {
				t.Errorf("by-runtime %d, usage/daily %d, usage/by-agent %d: want all %d", sum, daily, byAgent, total)
			}
		})
	}

	// ---- project and squad filters --------------------------------------------
	for _, tc := range []struct {
		filter string
		want   map[string]int64
	}{
		{"&project_id=" + f.projectID, map[string]int64{f.public: 50}},
		// squad_id on one run, usage_squad_id on the other: both count.
		{"&squad_id=" + f.squadID, map[string]int64{f.private: 400, f.shared: 25}},
		{"&squad_id=" + standaloneSquadFilterSentinel, map[string]int64{f.public: 355}},
	} {
		rows, _ := getByRuntime(t, f.alice, query+tc.filter)
		got := seededTokensByRuntime(rows, runtimeUsageModelPrefix)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.filter, got, tc.want)
		}
		for id, want := range tc.want {
			if got[id] != want {
				t.Errorf("%s: runtime %s tokens = %d, want %d", tc.filter, id, got[id], want)
			}
		}
		daily, byAgent := seededKPITokens(t, f.alice, query+tc.filter, runtimeUsageModelPrefix)
		if sum := sumTokens(got); daily != sum || byAgent != sum {
			t.Errorf("%s: by-runtime %d, usage/daily %d, usage/by-agent %d: want equal", tc.filter, sum, daily, byAgent)
		}
	}
	if rows, _ := getByRuntime(t, f.alice, query+"&project_id="+f.projectID); runtimeTaskCount(rows, f.public) != 1 {
		t.Errorf("project filter: public task_count = %d, want 1", runtimeTaskCount(rows, f.public))
	}
	// The run count applies the squad filter like the rollup does, through
	// COALESCE(usage_squad_id, squad_id).
	if rows, _ := getByRuntime(t, f.alice, query+"&squad_id="+f.squadID); runtimeTaskCount(rows, f.private) != 1 ||
		runtimeTaskCount(rows, f.shared) != 1 || runtimeTaskCount(rows, f.public) != 0 {
		t.Errorf("squad filter: task_count private/shared/public = %d/%d/%d, want 1/1/0",
			runtimeTaskCount(rows, f.private), runtimeTaskCount(rows, f.shared), runtimeTaskCount(rows, f.public))
	}
	for _, bad := range []string{"&project_id=nope", "&squad_id=nope"} {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByRuntime(w, newRequestAs(f.alice, http.MethodGet, "/api/dashboard/usage/by-runtime"+query+bad, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", bad, w.Code)
		}
	}
}

// A deleted runtime's tasks are unbound (runtime_id NULL, migration 251).
// Until the next rollup its hourly rows still carry the old id: they fold onto
// the restricted bucket rather than surfacing a bare id, and the totals still
// match. After the rollup the usage has left both, like every rollup.
func TestDashboardUsageByRuntimeDeletedRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	marker := fmt.Sprintf("rtdel%d", time.Now().UnixNano())
	prefix := runtimeUsageModelPrefix + marker + "-"
	bound := dbfx.Runtime(t, marker+" bound", testutil.Cols{"visibility": "public"})
	gone := dbfx.Runtime(t, marker+" gone", testutil.Cols{"visibility": "public"})
	agentID := dbfx.Agent(t, marker+" agent", bound)
	seedRuntimeUsage(t, agentID, gone, prefix, nil, runtimeUsageSeed{model: "a", tokens: 70})
	rollupTaskUsageHourly(t)

	rows, _ := getByRuntime(t, testUserID, "?days=7")
	if got := seededTokensByRuntime(rows, prefix); got[gone] != 70 {
		t.Fatalf("before delete: tokens = %v, want %s=70", got, gone)
	}

	dbfx.Exec(t, `UPDATE agent_task_queue SET runtime_id = NULL WHERE runtime_id = $1`, gone)
	dbfx.Exec(t, `DELETE FROM agent_runtime WHERE id = $1`, gone)

	rows, w := getByRuntime(t, testUserID, "?days=7")
	got := seededTokensByRuntime(rows, prefix)
	if strings.Contains(w.Body.String(), gone) || got[restrictedRuntimesRowID] != 70 {
		t.Errorf("deleted, before the rollup: tokens = %v, want only the restricted bucket = 70", got)
	}
	daily, byAgent := seededKPITokens(t, testUserID, "?days=7", prefix)
	if daily != 70 || byAgent != 70 {
		t.Errorf("deleted, before the rollup: usage/daily %d, usage/by-agent %d, want 70 each", daily, byAgent)
	}

	rollupTaskUsageHourly(t)
	rows, _ = getByRuntime(t, testUserID, "?days=7")
	daily, byAgent = seededKPITokens(t, testUserID, "?days=7", prefix)
	if sum := sumTokens(seededTokensByRuntime(rows, prefix)); sum != 0 || daily != 0 || byAgent != 0 {
		t.Errorf("deleted, after the rollup: by-runtime %d, usage/daily %d, usage/by-agent %d, want 0", sum, daily, byAgent)
	}
}

// With a viewing time zone whose midnight falls mid-hour, the rollup only keeps
// the buckets starting at or after the cutoff, so the token rows drop usage
// from the cutoff's partial hour. The run count must drop the same runs.
func TestDashboardUsageByRuntimeCountMatchesTheHourlyCutoff(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	prior := testHandler.Entitlements
	testHandler.Entitlements = nil
	t.Cleanup(func() { testHandler.Entitlements = prior })

	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Skipf("tz data unavailable: %v", err)
	}
	// Pinned well in the past, so every timestamp below is too.
	now := pinDayWindowClock(t, startOfDayBefore(time.Now(), kolkata, 20).Add(12*time.Hour))
	cutoff := startOfDayBefore(now, kolkata, 0) // 18:30 UTC the day before
	if cutoff.UTC().Minute() != 30 {
		t.Fatalf("cutoff %s is not mid-hour", cutoff.UTC())
	}

	marker := fmt.Sprintf("rthour%d", time.Now().UnixNano())
	prefix := runtimeUsageModelPrefix + marker + "-"
	runtimeID := dbfx.Runtime(t, marker+" runtime", testutil.Cols{"visibility": "public"})
	agentID := dbfx.Agent(t, marker+" agent", runtimeID)
	// Inside the window by the clock, but in the hour bucket that starts
	// before the cutoff: the rollup drops it.
	seedRuntimeUsage(t, agentID, runtimeID, prefix, nil, runtimeUsageSeed{model: "a", tokens: 1, at: cutoff.Add(10 * time.Minute)})
	// In the first whole hour after the cutoff: kept.
	seedRuntimeUsage(t, agentID, runtimeID, prefix, nil, runtimeUsageSeed{model: "a", tokens: 10, at: cutoff.Add(40 * time.Minute)})
	rollupTaskUsageHourly(t)

	const query = "?days=1&tz=Asia/Kolkata"
	rows, _ := getByRuntime(t, testUserID, query)
	tokens := seededTokensByRuntime(rows, prefix)[runtimeID]
	daily, byAgent := seededKPITokens(t, testUserID, query, prefix, cutoff.Format("2006-01-02"))
	if tokens != 10 || daily != 10 || byAgent != 10 {
		t.Errorf("tokens: by-runtime %d, usage/daily %d, usage/by-agent %d, want 10 each", tokens, daily, byAgent)
	}
	if got := runtimeTaskCount(rows, runtimeID); got != 1 {
		t.Errorf("task_count = %d, want 1: the run in the cutoff's partial hour is not in the token rows", got)
	}
}

func TestDashboardRuntimeUsageHourCutoff(t *testing.T) {
	at := func(s string) pgtype.Timestamptz {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return pgtype.Timestamptz{Time: v, Valid: true}
	}
	for _, tc := range []struct{ in, want string }{
		{"2026-03-01T00:00:00Z", "2026-03-01T00:00:00Z"},        // whole hour: unchanged
		{"2026-02-28T18:30:00Z", "2026-02-28T19:00:00Z"},        // Asia/Kolkata midnight
		{"2026-02-28T18:15:00Z", "2026-02-28T19:00:00Z"},        // Asia/Kathmandu midnight
		{"2026-03-01T00:00:00.000001Z", "2026-03-01T01:00:00Z"}, // just past
	} {
		got := usageHourCutoff(at(tc.in))
		if want := at(tc.want); !got.Time.Equal(want.Time) || !got.Valid {
			t.Errorf("usageHourCutoff(%s) = %s, want %s", tc.in, got.Time.UTC(), want.Time.UTC())
		}
	}
}

// by-runtime stops at the plan's history limit like the other dashboard
// endpoints (see TestDashboardEndpointsStopAtTheHistoryLimit).
func TestDashboardUsageByRuntimeStopsAtTheHistoryLimit(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	now := pinDayWindowClock(t, time.Now().UTC())
	const limit, requested = 30, 35
	inside := startOfDayBefore(now, time.UTC, limit-1).Add(12 * time.Hour)
	outside := startOfDayBefore(now, time.UTC, limit).Add(12 * time.Hour)

	marker := fmt.Sprintf("rthist%d", time.Now().UnixNano())
	prefix := runtimeUsageModelPrefix + marker + "-"
	runtimeID := dbfx.Runtime(t, marker+" runtime", testutil.Cols{"visibility": "public"})
	agentID := dbfx.Agent(t, marker+" agent", runtimeID)
	for _, at := range []time.Time{inside, outside} {
		seedRuntimeUsage(t, agentID, runtimeID, prefix, nil, runtimeUsageSeed{model: "a", tokens: 100, at: at})
	}
	rollupTaskUsageHourly(t)

	query := fmt.Sprintf("?days=%d&tz=UTC", requested)
	read := func(t *testing.T) (tokens int64, runs int32, header string) {
		t.Helper()
		rows, w := getByRuntime(t, testUserID, query)
		return seededTokensByRuntime(rows, prefix)[runtimeID], runtimeTaskCount(rows, runtimeID),
			w.Header().Get(HeaderDashboardHistoryLimit)
	}

	t.Run("enforce", func(t *testing.T) {
		useDashboardHistoryLimit(t, historyDecision(entitlement.ActionEnforce, limit))
		tokens, runs, header := read(t)
		if tokens != 100 || runs != 1 || header != "30" {
			t.Errorf("under a %d-day limit: tokens %d, runs %d, header %q; want 100, 1, \"30\"", limit, tokens, runs, header)
		}
	})
	t.Run("self-host", func(t *testing.T) {
		prior := testHandler.Entitlements
		testHandler.Entitlements = nil
		t.Cleanup(func() { testHandler.Entitlements = prior })
		tokens, runs, header := read(t)
		if tokens != 200 || runs != 2 || header != "" {
			t.Errorf("without a limit: tokens %d, runs %d, header %q; want 200, 2, none", tokens, runs, header)
		}
	})
}

// by-runtime follows the private-project rule of the other rollups (MUL-8472):
// a project_id the caller cannot see answers like a project that does not
// exist, and the unfiltered totals keep the run.
func TestDashboardUsageByRuntimePrivateProject(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPrivateProjectFixture(t)
	prefix := runtimeUsageModelPrefix + f.marker + "-"
	runtimeID := dbfx.Runtime(t, f.marker+" runtime", testutil.Cols{"visibility": "public"})
	agentID := dbfx.Agent(t, f.marker+" agent", runtimeID)
	seedRuntimeUsage(t, agentID, runtimeID, prefix, testutil.Cols{"issue_id": f.secretIssue},
		runtimeUsageSeed{model: "a", tokens: 10})
	rollupTaskUsageHourly(t)

	secret := "?days=7&project_id=" + f.projectID
	missing := "?days=7&project_id=00000000-0000-0000-0000-0000000000ff"
	if rows, _ := getByRuntime(t, f.insider, secret); seededTokensByRuntime(rows, prefix)[runtimeID] != 10 {
		t.Fatal("project member: the private project's usage is missing")
	}
	_, hidden := getByRuntime(t, f.outsider, secret)
	_, absent := getByRuntime(t, f.outsider, missing)
	if hidden.Body.String() != absent.Body.String() {
		t.Fatalf("outsider: hidden project_id = %s, want the missing-project answer %s", hidden.Body.String(), absent.Body.String())
	}
	if rows, _ := getByRuntime(t, f.outsider, "?days=7"); seededTokensByRuntime(rows, prefix)[runtimeID] != 10 {
		t.Fatal("outsider: the unfiltered rollup lost the private project's usage")
	}
}
