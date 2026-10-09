package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Shared helpers for the leaderboard's Model and Runtime scopes. Both scopes
// are a PAIR of endpoints (a token rollup plus a run-time rollup) and the
// contract under test is that a pair behaves like the Agent scope's pair:
// same window, same terminal-task filter, same cost split. The regressions
// below each pin one of those three, because all three were individually
// plausible-looking and only wrong next to the Agent scope.

// The runtime, agent and issue every scope fixture below hangs off.
func leaderboardScopeFixture(t *testing.T) (agentID, runtimeID, issueID string) {
	t.Helper()
	dbfx.QueryRow(t, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID)
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	issueID = dbfx.Issue(t, "leaderboard scope parity test")
	return agentID, runtimeID, issueID
}

// TestDashboardLeaderboardScopesUseExactWindow is the Model/Runtime-scope
// analogue of TestDashboardPerAgentRollupsUseExactWindow.
//
// All four new endpoints return rows with no date dimension, so the client
// cannot trim `parseSinceParamInTZ`'s surplus calendar day the way it trims
// the chart endpoints'. Serving them on the N+1 cutoff made the leaderboard
// cover one day more than the Cost / Tokens KPI beside it — and made the
// numbers change when the user moved the scope selector, because the Agent
// scope was on the exact cutoff while Model/Runtime were not. Same page,
// same window, different answer purely from which scope was open.
func TestDashboardLeaderboardScopesUseExactWindow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID, issueID := leaderboardScopeFixture(t)
	const model = "scope-parity-yesterday"
	const seededTokens = 7777
	const seededSeconds = 900

	// A token bucket and a terminal task that both completed at noon YESTERDAY,
	// under a provider/model pair no other fixture uses so the assertions
	// cannot be satisfied by ambient rows. Noon keeps both clear of midnight.
	// InsertNoID, not Insert: the rollup is keyed by the composite
	// (bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider,
	// model) and carries no surrogate id column.
	dbfx.InsertNoID(t, "task_usage_hourly", testutil.Cols{
		"bucket_hour":  testutil.Raw("((CURRENT_DATE - 1)::timestamp + interval '12 hours') AT TIME ZONE 'UTC'"),
		"workspace_id": testWorkspaceID,
		"runtime_id":   runtimeID,
		"agent_id":     agentID,
		"provider":     "scope-parity-test",
		"model":        model,
		"input_tokens": seededTokens,
		"event_count":  1,
		"task_count":   1,
	}, `provider = 'scope-parity-test' AND model = $1`, model)

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":     issueID,
		"runtime_id":   runtimeID,
		"status":       "completed",
		"started_at":   testutil.Raw("((CURRENT_DATE - 1)::timestamp + interval '11 hours 45 minutes') AT TIME ZONE 'UTC'"),
		"completed_at": testutil.Raw("((CURRENT_DATE - 1)::timestamp + interval '12 hours') AT TIME ZONE 'UTC'"),
		"created_at":   testutil.Raw("now()"),
	})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":      taskID,
		"provider":     "scope-parity-test",
		"model":        model,
		"input_tokens": 100,
		"created_at":   testutil.Raw("now()"),
	})

	// Both token endpoints key the fixture off `model`; both run-time
	// endpoints expose one key field, so match on whichever it carries.
	tokensFor := func(body []byte, model2 string) int64 {
		var rows []struct {
			Model       string `json:"model"`
			InputTokens int64  `json:"input_tokens"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			t.Fatalf("decode token rollup: %v", err)
		}
		var n int64
		for _, r := range rows {
			if r.Model == model2 {
				n += r.InputTokens
			}
		}
		return n
	}
	secondsFor := func(body []byte, key string) int64 {
		var rows []struct {
			RuntimeID    string `json:"runtime_id"`
			Model        string `json:"model"`
			TotalSeconds int64  `json:"total_seconds"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			t.Fatalf("decode run-time rollup: %v", err)
		}
		var n int64
		for _, r := range rows {
			if r.RuntimeID == key || r.Model == key {
				n += r.TotalSeconds
			}
		}
		return n
	}

	for _, tc := range []struct {
		name  string
		path  string
		serve func(http.ResponseWriter, *http.Request)
		// key is what the response is attributed back on; a run-time row also
		// carries model, so the same key works for both shapes.
		key    string
		rollup string // "tokens" or "seconds"
	}{
		{"by-model", "/api/dashboard/usage/by-model", testHandler.GetDashboardUsageByModel, model, "tokens"},
		{"by-runtime", "/api/dashboard/usage/by-runtime", testHandler.GetDashboardUsageByRuntime, model, "tokens"},
		{"model-runtime", "/api/dashboard/model-runtime", testHandler.GetDashboardModelRunTime, model, "seconds"},
		{"runtime-duration", "/api/dashboard/runtime-duration", testHandler.GetDashboardRuntimeDuration, runtimeID, "seconds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := func(path string) int64 {
				w := httptest.NewRecorder()
				tc.serve(w, newRequest("GET", path, nil))
				if w.Code != http.StatusOK {
					t.Fatalf("%s: expected 200, got %d: %s", path, w.Code, w.Body.String())
				}
				if tc.rollup == "tokens" {
					return tokensFor(w.Body.Bytes(), tc.key)
				}
				return secondsFor(w.Body.Bytes(), tc.key)
			}

			// days=1 means "today": no scope may reach yesterday.
			if got := read(tc.path + "?days=1&tz=UTC"); got != 0 {
				t.Errorf("%s: days=1 must not reach yesterday, counted %d", tc.name, got)
			}
			// days=2 covers today + yesterday, so the fixture must appear.
			// This is what makes the assertion above a closed window rather
			// than an unreachable fixture.
			want := int64(seededTokens)
			if tc.rollup == "seconds" {
				want = seededSeconds
			}
			if got := read(tc.path + "?days=2&tz=UTC"); got < want {
				t.Errorf("%s: days=2 must include yesterday's %d, got %d", tc.name, want, got)
			}
		})
	}
}

// TestDashboardLeaderboardScopesCountCancelledRuns pins the terminal-task
// filter on the new run-time queries.
//
// A run the user stopped mid-flight burned real agent time. The Agent scope
// counts it — ListDashboardAgentRunTime filters on cancelled and explains why
// in the query — but the new Model and Runtime queries originally filtered on
// ('completed', 'failed') only, so a cancelled run's 30 minutes showed in the
// Time column and then vanished as soon as the user moved the selector off
// Agent. Same run, two answers.
func TestDashboardLeaderboardScopesCountCancelledRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID, issueID := leaderboardScopeFixture(t)
	const model = "scope-cancelled-model"
	const durationSeconds = 1800

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":     issueID,
		"runtime_id":   runtimeID,
		"status":       "cancelled",
		"started_at":   testutil.Raw("now() - interval '32 minutes'"),
		"completed_at": testutil.Raw("now() - interval '2 minutes'"),
		"created_at":   testutil.Raw("now()"),
	})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":      taskID,
		"provider":     "scope-cancelled-test",
		"model":        model,
		"input_tokens": 100,
		"created_at":   testutil.Raw("now()"),
	})

	t.Run("runtime duration counts a cancelled run", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardRuntimeDuration(w, newRequest("GET", "/api/dashboard/runtime-duration?days=1&tz=UTC", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []struct {
			RuntimeID      string `json:"runtime_id"`
			TotalSeconds   int64  `json:"total_seconds"`
			TaskCount      int32  `json:"task_count"`
			CancelledCount int32  `json:"cancelled_count"`
		}
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var seconds int64
		var cancelled, tasks int32
		for _, r := range rows {
			if r.RuntimeID != runtimeID {
				continue
			}
			seconds += r.TotalSeconds
			cancelled += r.CancelledCount
			tasks += r.TaskCount
		}
		if seconds < durationSeconds {
			t.Errorf("cancelled run dropped from runtime-duration: got %ds, want >=%d", seconds, durationSeconds)
		}
		if cancelled < 1 {
			t.Errorf("cancelled_count must report the stopped run, got %d", cancelled)
		}
		if tasks < cancelled {
			t.Errorf("task_count %d below cancelled_count %d; not a subset", tasks, cancelled)
		}
	})

	t.Run("model run time counts a cancelled run", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardModelRunTime(w, newRequest("GET", "/api/dashboard/model-runtime?days=1&tz=UTC", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []struct {
			Model          string `json:"model"`
			TotalSeconds   int64  `json:"total_seconds"`
			CancelledCount int32  `json:"cancelled_count"`
		}
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var seconds int64
		var cancelled int32
		for _, r := range rows {
			if r.Model != model {
				continue
			}
			seconds += r.TotalSeconds
			cancelled += r.CancelledCount
		}
		if seconds < durationSeconds {
			t.Errorf("cancelled run dropped from model-runtime: got %ds, want >=%d", seconds, durationSeconds)
		}
		if cancelled < 1 {
			t.Errorf("cancelled_count must report the stopped run, got %d", cancelled)
		}
	})
}

// TestDashboardLeaderboardScopesCarryProviderCost pins the cost split on the
// new token rollups.
//
// `estimateCost` trusts the server's `cost_usd_ticks` and only falls back to
// the local rate table for the `uncosted_*` remainder. The new queries
// selected neither, so `authoritative` was always 0: a model the rate table
// has no entry for priced at $0.00 in the Model and Runtime scopes while
// reading correctly in the Agent scope, and those scopes stopped summing to
// the Cost KPI directly above them.
func TestDashboardLeaderboardScopesCarryProviderCost(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID, _ := leaderboardScopeFixture(t)
	const model = "scope-nopricing-v1"
	// 2.5e9 ticks at 1e-10 USD per tick = $0.25, and the same 1000 tokens are
	// marked uncosted so the rate-table half is exercised alongside it.
	const costTicks = 2_500_000_000
	const uncostedTokens = 1000

	dbfx.InsertNoID(t, "task_usage_hourly", testutil.Cols{
		"bucket_hour":           testutil.Raw("date_trunc('hour', now())"),
		"workspace_id":          testWorkspaceID,
		"runtime_id":            runtimeID,
		"agent_id":              agentID,
		"provider":              "scope-cost-test",
		"model":                 model,
		"input_tokens":          uncostedTokens,
		"cost_usd_ticks":        costTicks,
		"uncosted_input_tokens": uncostedTokens,
		"event_count":           1,
		"task_count":            1,
	}, `provider = 'scope-cost-test' AND model = $1`, model)

	read := func(body []byte) (ticks, uncosted int64) {
		var rows []struct {
			Model               string `json:"model"`
			CostUSDTicks        int64  `json:"cost_usd_ticks"`
			UncostedInputTokens int64  `json:"uncosted_input_tokens"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			t.Fatalf("decode token rollup: %v", err)
		}
		for _, r := range rows {
			if r.Model == model {
				ticks += r.CostUSDTicks
				uncosted += r.UncostedInputTokens
			}
		}
		return ticks, uncosted
	}

	for _, tc := range []struct {
		name  string
		path  string
		serve func(http.ResponseWriter, *http.Request)
	}{
		{"by-model", "/api/dashboard/usage/by-model?days=1&tz=UTC", testHandler.GetDashboardUsageByModel},
		{"by-runtime", "/api/dashboard/usage/by-runtime?days=1&tz=UTC", testHandler.GetDashboardUsageByRuntime},
	} {
		t.Run(tc.name+" reports the provider's own price", func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.serve(w, newRequest("GET", tc.path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			ticks, uncosted := read(w.Body.Bytes())
			if ticks < costTicks {
				t.Errorf("%s: cost_usd_ticks = %d, want >= %d — a model with no rate entry would price at $0.00", tc.name, ticks, costTicks)
			}
			if uncosted < uncostedTokens {
				t.Errorf("%s: uncosted_input_tokens = %d, want >= %d so the rate table still prices the rest", tc.name, uncosted, uncostedTokens)
			}
		})
	}
}
