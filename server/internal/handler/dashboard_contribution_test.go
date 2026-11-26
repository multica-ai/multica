package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/contribution"
)

// The fixture lives in its own project and every request filters on it, so
// rows other tests leave in the shared workspace never reach the assertions.
func TestDashboardContributionEndpoints(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	pinDayWindowClock(t, time.Now())

	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	projectID := dbfx.Project(t, "contribution dashboard test")

	mkIssue := func(title string) string {
		var id string
		dbfx.QueryRow(t, `
			INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
			VALUES ($1, $2, $3, 'member', $4,
				(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
			RETURNING id
		`, testWorkspaceID, title, testUserID, projectID).Scan(&id)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}
	doneIssue := mkIssue("contribution done issue")
	mkIssue("contribution open issue")

	dbfx.Exec(t, `
		INSERT INTO comment (issue_id, author_type, author_id, content, type, workspace_id)
		VALUES ($1, 'agent', $2, 'looking', 'comment', $3),
		       ($1, 'agent', $2, 'progress', 'progress_update', $3)
	`, doneIssue, agentID, testWorkspaceID)
	dbfx.Exec(t, `
		INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details)
		VALUES ($1, $2, 'member', $3, 'status_changed', '{"from":"todo","to":"done"}')
	`, testWorkspaceID, doneIssue, testUserID)
	dbfx.Exec(t, `UPDATE issue SET status = 'done' WHERE id = $1`, doneIssue)

	base := "?days=1&" + dashboardFixtureTZParam + "&project_id=" + projectID

	t.Run("summary", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardContributionSummary(w, newRequest("GET", "/api/dashboard/contribution/summary"+base, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var s contribution.Summary
		if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
			t.Fatal(err)
		}
		// doneIssue ended done: it leaves the stock and the active count.
		if s.KPIs.Total != 1 || s.KPIs.Touched != 2 || s.KPIs.Active != 1 || s.KPIs.Created != 2 || s.KPIs.Completed != 1 {
			t.Fatalf("kpis = %+v", s.KPIs)
		}
		actors := map[string]contribution.ActorStats{}
		for _, a := range s.Actors {
			actors[a.ActorType+":"+a.ActorID] = a
		}
		if m := actors["member:"+testUserID]; m.Created != 2 || m.Completed != 1 {
			t.Fatalf("member = %+v", m)
		}
		// The progress update is not a comment.
		if a := actors["agent:"+agentID]; a.Commented != 1 || a.Participated != 1 {
			t.Fatalf("agent = %+v", a)
		}
	})

	t.Run("events", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardContributionEvents(w, newRequest("GET", "/api/dashboard/contribution/events"+base+"&actors=agent:"+agentID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp ContributionEventsResponse
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp.Total != 1 || len(resp.Events) != 1 {
			t.Fatalf("events = %+v", resp)
		}
		e := resp.Events[0]
		if e.Kind != contribution.KindCommented || e.IssueID != doneIssue || !strings.Contains(e.Identifier, "-") {
			t.Fatalf("event = %+v", e)
		}
	})

	t.Run("issues", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardContributionIssues(w, newRequest("GET", "/api/dashboard/contribution/issues"+base+"&sort=interactions", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp ContributionIssuesResponse
		_ = json.NewDecoder(w.Body).Decode(&resp)
		// doneIssue: one comment + one status change; the progress update is not counted.
		if resp.Total != 2 || resp.Issues[0].IssueID != doneIssue || resp.Issues[0].Interactions != 2 {
			t.Fatalf("issues = %+v", resp)
		}
		bad := httptest.NewRecorder()
		testHandler.GetDashboardContributionIssues(bad, newRequest("GET", "/api/dashboard/contribution/issues"+base+"&sort=bogus", nil))
		if bad.Code != http.StatusBadRequest {
			t.Fatalf("bad sort: %d", bad.Code)
		}
	})
}
