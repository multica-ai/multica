package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestParseUsageExportBoundUsesTimezoneCalendarDaysAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	from, err := parseUsageExportBound("2025-03-09", loc)
	if err != nil {
		t.Fatal(err)
	}
	to, err := parseUsageExportBound("2025-03-10", loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := to.Sub(from); got != 23*time.Hour {
		t.Fatalf("spring-forward date interval = %s, want 23h", got)
	}
	if got := from.Format(time.RFC3339); got != "2025-03-09T00:00:00-05:00" {
		t.Fatalf("from = %s", got)
	}
	if got := to.Format(time.RFC3339); got != "2025-03-10T00:00:00-04:00" {
		t.Fatalf("to = %s", got)
	}

	exact, err := parseUsageExportBound("2025-03-09T07:30:00Z", loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := exact.Format(time.RFC3339); got != "2025-03-09T07:30:00Z" {
		t.Fatalf("RFC3339 instant changed: %s", got)
	}
}

func TestParseUsageExportGroupByCanonicalizesAndRejectsUnknown(t *testing.T) {
	dims, grouped, err := parseUsageExportGroupBy("model,agent,model")
	if err != nil {
		t.Fatal(err)
	}
	if got := dims; len(got) != 2 || got[0] != "agent" || got[1] != "model" {
		t.Fatalf("dimensions = %#v", got)
	}
	if !grouped["agent"] || !grouped["model"] || grouped["day"] {
		t.Fatalf("group set = %#v", grouped)
	}
	if _, _, err := parseUsageExportGroupBy("agent,cost"); err == nil {
		t.Fatal("expected unsupported dimension error")
	}
}

func TestWorkspaceUsageExportExactHalfOpenIntervalAndPagination(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "usage-export-agent", nil)

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2025, 3, 9, 0, 0, 0, 0, loc)
	to := time.Date(2025, 3, 10, 0, 0, 0, 0, loc)
	seed := func(at time.Time, model string, tokens int64) {
		taskID := dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID,
			"status":     "completed",
			"created_at": at,
		})
		dbfx.Insert(t, "task_usage", testutil.Cols{
			"task_id": taskID, "provider": "Claude", "model": model,
			"input_tokens": tokens, "output_tokens": 0,
			"cache_read_tokens": 0, "cache_write_tokens": 0,
			"created_at": at,
		})
	}
	seed(from.Add(-time.Microsecond), "outside-before", 1000)
	seed(from, "inside-a", 10)
	seed(to.Add(-time.Microsecond), "inside-b", 20)
	seed(to, "outside-after", 2000)

	read := func(cursor string) WorkspaceUsageExportResponse {
		path := "/api/usage/export?from=2025-03-09&to=2025-03-10&timezone=America%2FNew_York&page_size=1"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		w := httptest.NewRecorder()
		testHandler.GetWorkspaceUsageExport(w, newRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", w.Code, w.Body.String())
		}
		var resp WorkspaceUsageExportResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}

	first := read("")
	if first.From != "2025-03-09T00:00:00-05:00" || first.To != "2025-03-10T00:00:00-04:00" {
		t.Fatalf("resolved interval = [%s,%s)", first.From, first.To)
	}
	if len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("first page = %+v", first)
	}
	second := read(*first.NextCursor)
	if len(second.Items) != 1 || second.NextCursor != nil {
		t.Fatalf("second page = %+v", second)
	}
	if first.Items[0].Model != "inside-a" || second.Items[0].Model != "inside-b" {
		t.Fatalf("models = %q, %q", first.Items[0].Model, second.Items[0].Model)
	}
	if first.Items[0].InputTokens+second.Items[0].InputTokens != 30 {
		t.Fatalf("tokens = %d", first.Items[0].InputTokens+second.Items[0].InputTokens)
	}
}

func TestWorkspaceUsageExportRuntimeFilterEnforcesRuntimeReadAccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)
	adminID := dbfx.User(t, "Usage Export Admin", "usage-export-admin@multica.test")
	dbfx.Member(t, testWorkspaceID, adminID, "admin")
	agentID := dbfx.Agent(t, "usage-export-private-runtime-agent", runtimeID, testutil.Cols{
		"owner_id":       runtimeOwnerID,
		"visibility":     "workspace",
		"runtime_id":     runtimeID,
		"runtime_mode":   "cloud",
		"runtime_config": testutil.Raw("'{}'::jsonb"),
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"status":     "completed",
		"created_at": time.Date(2025, 4, 1, 12, 0, 0, 0, time.UTC),
	})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id": taskID, "provider": "Claude", "model": "private-model",
		"input_tokens": 42, "output_tokens": 0,
		"cache_read_tokens": 0, "cache_write_tokens": 0,
		"created_at": time.Date(2025, 4, 1, 12, 0, 0, 0, time.UTC),
	})

	path := "/api/usage/export?workspace_id=" + testWorkspaceID +
		"&from=2025-04-01&to=2025-04-02&runtime_id=" + runtimeID
	request := func(userID string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.GetWorkspaceUsageExport(w, newRequestAs(userID, http.MethodGet, path, nil))
		return w
	}

	ownerResponse := request(runtimeOwnerID)
	if ownerResponse.Code != http.StatusOK {
		t.Fatalf("runtime owner status = %d: %s", ownerResponse.Code, ownerResponse.Body.String())
	}
	var export WorkspaceUsageExportResponse
	if err := json.NewDecoder(ownerResponse.Body).Decode(&export); err != nil {
		t.Fatal(err)
	}
	if len(export.Items) != 1 || export.Items[0].InputTokens != 42 {
		t.Fatalf("runtime owner export = %+v", export.Items)
	}

	for _, tc := range []struct {
		name   string
		userID string
	}{
		{name: "plain member", userID: plainMemberID},
		{name: "workspace admin", userID: adminID},
		{name: "workspace owner", userID: testUserID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(tc.userID)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
			}
		})
	}

	foreignWorkspaceID := dbfx.Workspace(t, "Usage Export Foreign Workspace", "usage-export-foreign-workspace")
	dbfx.Member(t, foreignWorkspaceID, runtimeOwnerID, "owner")
	crossWorkspacePath := "/api/usage/export?workspace_id=" + foreignWorkspaceID +
		"&from=2025-04-01&to=2025-04-02&runtime_id=" + runtimeID
	w := httptest.NewRecorder()
	crossWorkspaceRequest := newRequestAs(runtimeOwnerID, http.MethodGet, crossWorkspacePath, nil)
	// newRequest installs the shared test workspace header by default, and the
	// request resolver intentionally gives that header priority over the query
	// parameter. Override it so this request actually targets the foreign
	// workspace whose authorization boundary the assertion is meant to cover.
	crossWorkspaceRequest.Header.Set("X-Workspace-ID", foreignWorkspaceID)
	testHandler.GetWorkspaceUsageExport(w, crossWorkspaceRequest)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace runtime status = %d, want 404: %s", w.Code, w.Body.String())
	}
}
