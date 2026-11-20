package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func historyDecision(action entitlement.Action, days int) entitlement.Decision {
	gate := entitlement.Gate{Action: action}
	if action != entitlement.ActionOff {
		gate.Limit = &days
	}
	return entitlement.Decision{Gate: gate}
}

// useDashboardHistoryLimit points the shared test handler at a stub that
// returns decision for dashboard_history_days in the test workspace, and
// clears the workspace's recorded reads before and after.
func useDashboardHistoryLimit(t *testing.T, decision entitlement.Decision) {
	t.Helper()
	stub := entitlementtest.New()
	stub.Set(uuid.MustParse(testWorkspaceID), entitlement.GateDashboardHistoryDays, decision)
	prior := testHandler.Entitlements
	testHandler.Entitlements = stub
	clearDashboardRangeViews(t)
	t.Cleanup(func() {
		testHandler.Entitlements = prior
		clearDashboardRangeViews(t)
	})
}

func clearDashboardRangeViews(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM dashboard_range_view WHERE workspace_id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("clear dashboard range views: %v", err)
	}
}

// startOfDayBefore is local midnight `days` days before now's calendar day.
func startOfDayBefore(now time.Time, loc *time.Location, days int) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day()-days, 0, 0, 0, 0, loc)
}

// The cutoff every dashboard endpoint reads, across plans, actions, both
// cutoff conventions and time zones at the edges of the offset range.
func TestDashboardCutoffAppliesTheHistoryLimit(t *testing.T) {
	workspaceID := uuid.New()
	request := func(days int, tz string) *http.Request {
		return httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/dashboard/usage/daily?days=%d&tz=%s", days, tz), nil)
	}

	t.Run("enforce cuts every endpoint to the most recent N calendar days", func(t *testing.T) {
		// Request sizes the weekly chart sends for 30/90/365-day ranges.
		for _, tc := range []struct{ limit, requested int }{{30, 35}, {90, 91}, {365, 371}} {
			for _, tz := range []string{"UTC", "Pacific/Kiritimati", "Etc/GMT+12", "Asia/Kolkata"} {
				for _, trim := range []int{0, 1} {
					name := fmt.Sprintf("limit %d requested %d %s trim %d", tc.limit, tc.requested, tz, trim)
					t.Run(name, func(t *testing.T) {
						loc := loadViewingLocation(tz)
						now := pinDayWindowClock(t, time.Date(2026, 11, 20, 23, 30, 0, 0, time.UTC))
						stub := entitlementtest.New()
						stub.Set(workspaceID, entitlement.GateDashboardHistoryDays, historyDecision(entitlement.ActionEnforce, tc.limit))
						h := &Handler{Entitlements: stub}
						rec := httptest.NewRecorder()

						window := h.dashboardCutoff(rec, request(tc.requested, tz), workspaceID.String(), tz, trim)

						want := startOfDayBefore(now, loc, tc.limit-1)
						if !window.since.Time.Equal(want) {
							t.Fatalf("since = %s, want %s (first of the last %d local days)", window.since.Time.In(loc), want, tc.limit)
						}
						if !window.truncated || window.limitDays != tc.limit || window.requestedDays != tc.requested {
							t.Fatalf("window = %+v", window)
						}
						if got := rec.Header().Get(HeaderDashboardHistoryLimit); got != strconv.Itoa(tc.limit) {
							t.Fatalf("limit header = %q, want %d", got, tc.limit)
						}
					})
				}
			}
		}
	})

	t.Run("enforce cuts the headroom day of a request exactly at the limit", func(t *testing.T) {
		now := pinDayWindowClock(t, time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
		stub := entitlementtest.New()
		stub.Set(workspaceID, entitlement.GateDashboardHistoryDays, historyDecision(entitlement.ActionEnforce, 30))
		h := &Handler{Entitlements: stub}

		dated := h.dashboardCutoff(httptest.NewRecorder(), request(30, "UTC"), workspaceID.String(), "UTC", 0)
		if want := startOfDayBefore(now, time.UTC, 29); !dated.since.Time.Equal(want) {
			t.Fatalf("date series since = %s, want %s: the N+1 headroom day is outside the plan", dated.since.Time, want)
		}
		exact := h.dashboardCutoff(httptest.NewRecorder(), request(30, "UTC"), workspaceID.String(), "UTC", 1)
		if want := startOfDayBefore(now, time.UTC, 29); !exact.since.Time.Equal(want) || exact.truncated {
			t.Fatalf("per-agent window = %+v, want untouched exact 30 days", exact)
		}
		short := h.dashboardCutoff(httptest.NewRecorder(), request(7, "UTC"), workspaceID.String(), "UTC", 1)
		if want := startOfDayBefore(now, time.UTC, 6); !short.since.Time.Equal(want) || short.truncated {
			t.Fatalf("7-day window = %+v, want untouched", short)
		}
	})

	t.Run("the boundary follows the viewing zone across a DST change", func(t *testing.T) {
		// 2026-03-08 is the US spring-forward day; 30 local days back crosses it.
		loc := loadViewingLocation("America/New_York")
		now := pinDayWindowClock(t, time.Date(2026, 3, 20, 15, 0, 0, 0, time.UTC))
		stub := entitlementtest.New()
		stub.Set(workspaceID, entitlement.GateDashboardHistoryDays, historyDecision(entitlement.ActionEnforce, 30))
		h := &Handler{Entitlements: stub}

		window := h.dashboardCutoff(httptest.NewRecorder(), request(35, "America/New_York"), workspaceID.String(), "America/New_York", 0)
		want := time.Date(2026, 2, 19, 0, 0, 0, 0, loc)
		if !window.since.Time.Equal(want) || !window.since.Time.Equal(startOfDayBefore(now, loc, 29)) {
			t.Fatalf("since = %s, want local midnight %s", window.since.Time.In(loc), want)
		}
	})

	t.Run("off, observe, a downgraded enforce and self-host are unlimited", func(t *testing.T) {
		now := pinDayWindowClock(t, time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
		for name, provider := range map[string]entitlement.Provider{
			"off":        stubWith(workspaceID, historyDecision(entitlement.ActionOff, 0)),
			"observe":    stubWith(workspaceID, historyDecision(entitlement.ActionObserve, 30)),
			"downgraded": stubWith(workspaceID, entitlement.Decision{Gate: historyDecision(entitlement.ActionObserve, 30).Gate, Downgraded: true}),
			"self-host":  nil,
		} {
			t.Run(name, func(t *testing.T) {
				h := &Handler{Entitlements: provider}
				rec := httptest.NewRecorder()
				window := h.dashboardCutoff(rec, request(371, "UTC"), workspaceID.String(), "UTC", 0)
				if want := startOfDayBefore(now, time.UTC, 371); !window.since.Time.Equal(want) || window.truncated {
					t.Fatalf("window = %+v, want the full 371-day request", window)
				}
				if got := rec.Header().Get(HeaderDashboardHistoryLimit); got != "" {
					t.Fatalf("limit header = %q, want none", got)
				}
				if window.recordable != (provider != nil) {
					t.Fatalf("recordable = %v; only a Cloud-connected read is recorded", window.recordable)
				}
			})
		}
	})

	t.Run("the dashboard accepts 371 days, the runtime pages still stop at 365", func(t *testing.T) {
		now := pinDayWindowClock(t, time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
		h := &Handler{}
		window := h.dashboardCutoff(httptest.NewRecorder(), request(371, "UTC"), workspaceID.String(), "UTC", 0)
		if window.requestedDays != 371 {
			t.Fatalf("dashboard days = %d, want 371", window.requestedDays)
		}
		if got := h.dashboardCutoff(httptest.NewRecorder(), request(372, "UTC"), workspaceID.String(), "UTC", 0); got.requestedDays != 30 {
			t.Fatalf("days=372 = %d, want the default 30", got.requestedDays)
		}
		runtime := parseSinceParamInTZ(request(366, "UTC"), 90, "UTC")
		if want := startOfDayBefore(now, time.UTC, 90); !runtime.Time.Equal(want) {
			t.Fatalf("runtime days=366 since = %s, want the default 90-day cutoff %s", runtime.Time, want)
		}
	})
}

func stubWith(workspaceID uuid.UUID, decision entitlement.Decision) *entitlementtest.Stub {
	stub := entitlementtest.New()
	stub.Set(workspaceID, entitlement.GateDashboardHistoryDays, decision)
	return stub
}

// All six dashboard endpoints, against the database: under enforce none of
// them returns a row from before the plan's range, whatever `days` asked for.
func TestDashboardEndpointsStopAtTheHistoryLimit(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	now := pinDayWindowClock(t, time.Now().UTC())

	type endpoint struct {
		path    string
		handler http.HandlerFunc
		// count is how many fixture runs the response reflects.
		count func(t *testing.T, body []byte) int
		// dated responses also name the day of every row.
		dated bool
	}
	countRows := func(field string) func(t *testing.T, body []byte) int {
		return func(t *testing.T, body []byte) int {
			t.Helper()
			var rows []map[string]any
			if err := json.Unmarshal(body, &rows); err != nil {
				t.Fatalf("decode: %v", err)
			}
			n := 0
			for _, row := range rows {
				if v, ok := row[field].(float64); ok {
					n += int(v)
				}
			}
			return n
		}
	}
	// task_usage_hourly's task_count comes from event_count only after a
	// rollup; the two usage endpoints are read by tokens instead.
	endpoints := []endpoint{
		{"/api/dashboard/usage/daily", testHandler.GetDashboardUsageDaily, countTokens, true},
		{"/api/dashboard/usage/by-agent", testHandler.GetDashboardUsageByAgent, countTokens, false},
		{"/api/dashboard/agent-runtime", testHandler.GetDashboardAgentRunTime, countRows("task_count"), false},
		{"/api/dashboard/runtime/daily", testHandler.GetDashboardRunTimeDaily, countRows("task_count"), true},
		{"/api/dashboard/failures/daily", testHandler.GetDashboardFailuresDaily, countRows("task_count"), true},
		{"/api/dashboard/failures/by-agent", testHandler.GetDashboardFailuresByAgent, countRows("task_count"), false},
	}

	// Each plan's limit with the request its weekly chart sends. Fixtures sit
	// at noon on the plan's oldest day and on the day before it.
	for _, plan := range []struct {
		name             string
		limit, requested int
	}{
		{"free", 30, 35},
		{"pro", 90, 91},
		{"business", 365, 371},
	} {
		t.Run(plan.name, func(t *testing.T) {
			inside := startOfDayBefore(now, time.UTC, plan.limit-1).Add(12 * time.Hour)
			outside := startOfDayBefore(now, time.UTC, plan.limit).Add(12 * time.Hour)
			runtimeID := dbfx.Runtime(t, "history limit runtime "+plan.name)
			agentID := dbfx.Agent(t, "history limit agent "+plan.name, runtimeID)
			projectID := dbfx.Project(t, "history limit project "+plan.name)
			issueID := dbfx.Issue(t, "history limit issue", testutil.Cols{"project_id": projectID})
			t.Cleanup(func() {
				testPool.Exec(ctx, `DELETE FROM task_usage_hourly WHERE runtime_id = $1`, runtimeID)
			})
			for _, at := range []time.Time{inside, outside} {
				dbfx.Task(t, agentID, testutil.Cols{
					"issue_id":       issueID,
					"runtime_id":     runtimeID,
					"status":         "failed",
					"failure_reason": "timeout",
					"started_at":     at.Add(-10 * time.Minute),
					"completed_at":   at,
					"created_at":     at.Add(-10 * time.Minute),
				})
				dbfx.Exec(t, `
					INSERT INTO task_usage_hourly (
						bucket_hour, workspace_id, runtime_id, agent_id, project_id,
						provider, model,
						input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, event_count
					) VALUES ($1, $2, $3, $4, $5, 'history-limit', 'history-limit-model', 100, 0, 0, 0, 1)
				`, at.Truncate(time.Hour), testWorkspaceID, runtimeID, agentID, projectID)
			}
			call := func(t *testing.T, ep endpoint) *httptest.ResponseRecorder {
				t.Helper()
				rec := httptest.NewRecorder()
				ep.handler(rec, newRequest(http.MethodGet,
					fmt.Sprintf("%s?days=%d&tz=UTC&project_id=%s", ep.path, plan.requested, projectID), nil))
				if rec.Code != http.StatusOK {
					t.Fatalf("%s: status %d: %s", ep.path, rec.Code, rec.Body.String())
				}
				return rec
			}

			for _, tc := range []struct {
				name      string
				decision  *entitlement.Decision
				want      int
				wantLimit string
			}{
				{"enforce", ptrTo(historyDecision(entitlement.ActionEnforce, plan.limit)), 1, strconv.Itoa(plan.limit)},
				{"observe", ptrTo(historyDecision(entitlement.ActionObserve, plan.limit)), 2, ""},
				{"self-host", nil, 2, ""},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if tc.decision != nil {
						useDashboardHistoryLimit(t, *tc.decision)
					} else {
						prior := testHandler.Entitlements
						testHandler.Entitlements = nil
						t.Cleanup(func() { testHandler.Entitlements = prior })
					}
					for _, ep := range endpoints {
						rec := call(t, ep)
						if got := ep.count(t, rec.Body.Bytes()); got != tc.want {
							t.Errorf("%s: %d fixture runs, want %d", ep.path, got, tc.want)
						}
						if got := rec.Header().Get(HeaderDashboardHistoryLimit); got != tc.wantLimit {
							t.Errorf("%s: limit header %q, want %q", ep.path, got, tc.wantLimit)
						}
						if !ep.dated || tc.want != 1 {
							continue
						}
						var rows []struct {
							Date string `json:"date"`
						}
						_ = json.Unmarshal(rec.Body.Bytes(), &rows)
						for _, row := range rows {
							if row.Date < inside.Format("2006-01-02") {
								t.Errorf("%s: row dated %s is before the plan's first day %s", ep.path, row.Date, inside.Format("2006-01-02"))
							}
						}
					}
				})
			}
		})
	}
}

func ptrTo[T any](v T) *T { return &v }

func countTokens(t *testing.T, body []byte) int {
	t.Helper()
	var rows []struct {
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var tokens int64
	for _, row := range rows {
		if row.Model == "history-limit-model" {
			tokens += row.InputTokens
		}
	}
	return int(tokens / 100)
}

// Reads of usage/by-agent are recorded per workspace, day, requested and
// returned range, with the outcome under the limit.
func TestDashboardRangeViewsAreRecorded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	read := func(t *testing.T, days int) {
		t.Helper()
		rec := httptest.NewRecorder()
		testHandler.GetDashboardUsageByAgent(rec, newRequest(http.MethodGet, fmt.Sprintf("/api/dashboard/usage/by-agent?days=%d&tz=UTC", days), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
	}
	type row struct {
		requested, returned                  int32
		views, observed, degraded, truncated int64
		limit                                *int32
		action                               string
	}
	waitRows := func(t *testing.T, wantViews int64) []row {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			rows, err := testHandler.Queries.ListWorkspaceDashboardRangeViews(context.Background(), parseUUID(testWorkspaceID))
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			var total int64
			out := make([]row, 0, len(rows))
			for _, r := range rows {
				total += r.ViewCount
				var limit *int32
				if r.LastLimitDays.Valid {
					v := r.LastLimitDays.Int32
					limit = &v
				}
				out = append(out, row{r.RequestedDays, r.ReturnedDays, r.ViewCount, r.ObservedOverLimitCount,
					r.DegradedOverLimitCount, r.TruncatedCount, limit, r.LastAction})
			}
			if total >= wantViews || time.Now().After(deadline) {
				return out
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	find := func(rows []row, requested, returned int32) row {
		for _, r := range rows {
			if r.requested == requested && r.returned == returned {
				return r
			}
		}
		return row{}
	}

	t.Run("enforce records requested and returned range", func(t *testing.T) {
		useDashboardHistoryLimit(t, historyDecision(entitlement.ActionEnforce, 30))
		read(t, 90)
		read(t, 90)
		read(t, 7)
		rows := waitRows(t, 3)
		cut := find(rows, 90, 30)
		if cut.views != 2 || cut.truncated != 2 || cut.observed != 0 || cut.limit == nil || *cut.limit != 30 || cut.action != "enforce" {
			t.Fatalf("90-day reads = %+v, want 2 views truncated to 30", cut)
		}
		if within := find(rows, 7, 7); within.views != 1 || within.truncated != 0 {
			t.Fatalf("7-day read = %+v", within)
		}
	})

	t.Run("observe and a downgraded enforce are counted apart", func(t *testing.T) {
		useDashboardHistoryLimit(t, historyDecision(entitlement.ActionObserve, 30))
		read(t, 90)
		rows := waitRows(t, 1)
		if got := find(rows, 90, 90); got.views != 1 || got.observed != 1 || got.degraded != 0 || got.truncated != 0 {
			t.Fatalf("observed read = %+v", got)
		}

		downgraded := historyDecision(entitlement.ActionObserve, 30)
		downgraded.Downgraded = true
		useDashboardHistoryLimit(t, downgraded)
		read(t, 90)
		rows = waitRows(t, 1)
		if got := find(rows, 90, 90); got.views != 1 || got.degraded != 1 || got.observed != 0 {
			t.Fatalf("downgraded read = %+v", got)
		}
	})

	t.Run("off still counts the read with no limit", func(t *testing.T) {
		useDashboardHistoryLimit(t, historyDecision(entitlement.ActionOff, 0))
		read(t, 365)
		rows := waitRows(t, 1)
		if got := find(rows, 365, 365); got.views != 1 || got.limit != nil || got.action != "off" {
			t.Fatalf("off read = %+v", got)
		}
	})

	t.Run("self-host writes nothing", func(t *testing.T) {
		clearDashboardRangeViews(t)
		prior := testHandler.Entitlements
		testHandler.Entitlements = nil
		t.Cleanup(func() { testHandler.Entitlements = prior })
		read(t, 90)
		time.Sleep(200 * time.Millisecond)
		if n := dbfx.Count(t, `SELECT count(*) FROM dashboard_range_view WHERE workspace_id = $1`, testWorkspaceID); n != 0 {
			t.Fatalf("self-host recorded %d rows", n)
		}
	})

	t.Run("a locked table never holds the response", func(t *testing.T) {
		useDashboardHistoryLimit(t, historyDecision(entitlement.ActionEnforce, 30))
		tx, err := testPool.Begin(context.Background())
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(context.Background())
		if _, err := tx.Exec(context.Background(), `LOCK TABLE dashboard_range_view IN ACCESS EXCLUSIVE MODE`); err != nil {
			t.Fatalf("lock: %v", err)
		}
		started := time.Now()
		read(t, 90)
		if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
			t.Fatalf("GET took %s with the table locked; the record must stay off the request path", elapsed)
		}
	})
}

// Record drops the row, rather than waiting, when the table stays locked
// past its timeout.
func TestDashboardRangeViewRecordTimesOut(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	clearDashboardRangeViews(t)
	t.Cleanup(func() { clearDashboardRangeViews(t) })
	tx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(context.Background(), `LOCK TABLE dashboard_range_view IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock: %v", err)
	}
	views := service.DashboardRangeViews{Queries: db.New(testPool), RecordTimeout: 100 * time.Millisecond}
	started := time.Now()
	views.Record(context.Background(), service.DashboardRangeView{
		WorkspaceID: parseUUID(testWorkspaceID), RequestedDays: 90, ReturnedDays: 30,
		Decision: historyDecision(entitlement.ActionEnforce, 30),
	})
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Record took %s, want it bounded by its timeout", elapsed)
	}
}

func TestGetWorkspaceFeaturesReportsTheDashboardHistoryLimit(t *testing.T) {
	limit := func(t *testing.T) *int {
		t.Helper()
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/features", nil), testUserID), "id", testWorkspaceID)
		var resp WorkspaceFeaturesResponse
		testutil.Call(t, testHandler.GetWorkspaceFeatures, req).Want(http.StatusOK).JSON(&resp)
		return resp.Limits.DashboardHistoryDays
	}
	t.Run("self-host", func(t *testing.T) {
		prior := testHandler.Entitlements
		testHandler.Entitlements = nil
		t.Cleanup(func() { testHandler.Entitlements = prior })
		if got := limit(t); got != nil {
			t.Fatalf("limit = %d, want null", *got)
		}
	})
	downgraded := historyDecision(entitlement.ActionObserve, 30)
	downgraded.Downgraded = true
	for name, tc := range map[string]struct {
		decision entitlement.Decision
		want     *int
	}{
		"off":        {historyDecision(entitlement.ActionOff, 0), nil},
		"observe":    {historyDecision(entitlement.ActionObserve, 30), nil},
		"downgraded": {downgraded, nil},
		"enforce":    {historyDecision(entitlement.ActionEnforce, 90), intPtr(90)},
	} {
		t.Run(name, func(t *testing.T) {
			useDashboardHistoryLimit(t, tc.decision)
			got := limit(t)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("limit = %v, want %v", got, tc.want)
			}
		})
	}
}

func intPtr(v int) *int { return &v }

// A record that lands after its workspace was deleted must not leave a row
// behind: the insert reads the workspace under FOR KEY SHARE, so it either
// waits for the deletion and finds nothing, or runs first and is deleted.
func TestDashboardRangeViewDoesNotOutliveItsWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	views := service.DashboardRangeViews{Queries: db.New(testPool), RecordTimeout: 10 * time.Second}
	record := func(workspaceID string) {
		views.Record(ctx, service.DashboardRangeView{
			WorkspaceID: parseUUID(workspaceID), RequestedDays: 90, ReturnedDays: 30,
			Decision: historyDecision(entitlement.ActionEnforce, 30),
		})
	}
	rows := func(workspaceID string) int {
		return dbfx.Count(t, `SELECT count(*) FROM dashboard_range_view WHERE workspace_id = $1`, workspaceID)
	}
	newWorkspace := func(t *testing.T) string {
		slug := "range-view-" + uuid.NewString()[:8]
		id := dbfx.Workspace(t, "range view", slug)
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM dashboard_range_view WHERE workspace_id = $1`, id)
		})
		return id
	}

	t.Run("after the deletion committed", func(t *testing.T) {
		id := newWorkspace(t)
		dbfx.Exec(t, `DELETE FROM workspace WHERE id = $1`, id)
		record(id)
		if n := rows(id); n != 0 {
			t.Fatalf("rows = %d for a deleted workspace", n)
		}
	})

	t.Run("racing a deletion in progress", func(t *testing.T) {
		id := newWorkspace(t)
		tx, err := testPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		// What DeleteWorkspace takes first.
		if _, err := tx.Exec(ctx, `SELECT id FROM workspace WHERE id = $1 FOR UPDATE`, id); err != nil {
			t.Fatalf("lock: %v", err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			record(id)
		}()
		select {
		case <-done:
			t.Fatal("the record did not wait for the workspace lock")
		case <-time.After(300 * time.Millisecond):
		}
		if _, err := tx.Exec(ctx, `DELETE FROM dashboard_range_view WHERE workspace_id = $1`, id); err != nil {
			t.Fatalf("delete views: %v", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, id); err != nil {
			t.Fatalf("delete workspace: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
		<-done
		if n := rows(id); n != 0 {
			t.Fatalf("rows = %d after the workspace was deleted", n)
		}
	})
}
