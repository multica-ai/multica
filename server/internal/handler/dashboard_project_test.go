package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type byProjectRow struct {
	ProjectID    string `json:"project_id"`
	ProjectTitle string `json:"project_title"`
	TaskCount    int32  `json:"task_count"`
	Usage        []struct {
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	} `json:"usage"`
}

// seededTokensByProject sums only rows whose model starts with prefix, per
// project key.
func seededTokensByProject(rows []byProjectRow, prefix string) map[string]int64 {
	out := map[string]int64{}
	for _, row := range rows {
		for _, u := range row.Usage {
			if strings.HasPrefix(u.Model, prefix) {
				out[row.ProjectID] += u.InputTokens
			}
		}
	}
	return out
}

func projectRowOf(rows []byProjectRow, key string) (byProjectRow, bool) {
	for _, row := range rows {
		if row.ProjectID == key {
			return row, true
		}
	}
	return byProjectRow{}, false
}

func TestDashboardUsageByProject(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedMemberUsageFixture(t)
	ctx := context.Background()
	// A second model on the project run: still one run.
	dbfx.InsertNoID(t, "task_usage", testutil.Cols{
		"task_id": f.tasks["owner-project"], "provider": "claude", "model": memberUsageModelPrefix + "project-extra",
		"input_tokens": 13, "output_tokens": 0, "created_at": testutil.Raw("now()"),
	}, "task_id = $1 AND model = $2", f.tasks["owner-project"], memberUsageModelPrefix+"project-extra")
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup window: %v", err)
	}
	const path = "/api/dashboard/usage/by-project?days=7"
	get := func(userID, query string) ([]byProjectRow, string) {
		t.Helper()
		w := dashboardGet(t, testHandler.GetDashboardUsageByProject, userID, path+query)
		return decodeOK[[]byProjectRow](t, "by-project"+query, w), w.Body.String()
	}

	// ---- owner/admin: every run, the project plus the no-project bucket -----
	admin, body := get(f.ownerViewer, "")
	tokens := seededTokensByProject(admin, memberUsageModelPrefix)
	if tokens[f.projectID] != 38 || tokens[noProjectID] != 1050 || len(tokens) != 2 {
		t.Errorf("owner view: got %v, want project=38 (25+13), no project=1050", tokens)
	}
	project, _ := projectRowOf(admin, f.projectID)
	if project.TaskCount != 1 || project.ProjectTitle != "member-usage project" {
		t.Errorf("project row = %+v, want one run (two models counted once) and its title", project)
	}
	if none, _ := projectRowOf(admin, noProjectID); none.ProjectTitle != "" || none.TaskCount < 4 {
		t.Errorf("no-project row = %+v, want no title and at least the 4 seeded runs", none)
	}
	if strings.Contains(body, f.privateAgentID) || strings.Contains(body, f.publicAgentID) {
		t.Errorf("by-project leaked an agent id: %s", body)
	}

	// ---- the project rows add up to the workspace totals ---------------------
	type usageRow struct {
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	}
	var byAgent int64
	for _, row := range decodeOK[[]usageRow](t, "usage/by-agent", dashboardGet(t,
		testHandler.GetDashboardUsageByAgent, f.ownerViewer, "/api/dashboard/usage/by-agent?days=7")) {
		if strings.HasPrefix(row.Model, memberUsageModelPrefix) {
			byAgent += row.InputTokens
		}
	}
	if byAgent != 1088 || tokens[f.projectID]+tokens[noProjectID] != byAgent {
		t.Errorf("by-project total %d, usage/by-agent %d: want both 1088",
			tokens[f.projectID]+tokens[noProjectID], byAgent)
	}

	// ---- page filters -----------------------------------------------------------
	for _, filter := range []string{"&project_id=" + f.projectID, "&squad_id=" + f.squadID} {
		rows, _ := get(f.ownerViewer, filter)
		if got := seededTokensByProject(rows, memberUsageModelPrefix); len(got) != 1 || got[f.projectID] != 38 {
			t.Errorf("%s: got %v, want only project=38", filter, got)
		}
	}
	standalone, _ := get(f.ownerViewer, "&squad_id="+standaloneSquadFilterSentinel)
	if got := seededTokensByProject(standalone, memberUsageModelPrefix); len(got) != 1 || got[noProjectID] != 1050 {
		t.Errorf("standalone filter: got %v, want only no project=1050", got)
	}
	for _, bad := range []string{"&project_id=nope", "&squad_id=nope"} {
		if w := dashboardGet(t, testHandler.GetDashboardUsageByProject, f.ownerViewer, path+bad); w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", bad, w.Code)
		}
	}

	// ---- plain members: only the runs they are accountable for ---------------
	mine, _ := get(f.member, "")
	if got := seededTokensByProject(mine, memberUsageModelPrefix); len(got) != 1 || got[noProjectID] != 350 {
		t.Errorf("member view: got %v, want only no project=350 (300+50)", got)
	}
	if none, _ := projectRowOf(mine, noProjectID); none.TaskCount != 2 {
		t.Errorf("member view: no-project task_count = %d, want 2", none.TaskCount)
	}
	theirs, _ := get(f.privateOwner, "")
	if got := seededTokensByProject(theirs, memberUsageModelPrefix); got[f.projectID] != 38 || got[noProjectID] != 300 {
		t.Errorf("privateOwner view: got %v, want project=38 no project=300", got)
	}
}

func TestDashboardProjectTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedMemberUsageFixture(t)
	tasksPath := func(projectID string) string {
		return "/api/dashboard/usage/project-tasks?days=7&project_id=" + projectID
	}
	get := func(userID, path string) map[string]memberTaskResp {
		t.Helper()
		out := map[string]memberTaskResp{}
		w := dashboardGet(t, testHandler.GetDashboardProjectTasks, userID, path)
		for _, row := range decodeOK[[]memberTaskResp](t, path, w) {
			out[row.TaskID] = row
		}
		return out
	}
	seeded := func(rows map[string]memberTaskResp) map[string]memberTaskResp {
		out := map[string]memberTaskResp{}
		for label, id := range f.tasks {
			if row, ok := rows[id]; ok {
				out[label] = row
			}
		}
		return out
	}

	// ---- owner/admin: every run of the project, or outside any project -------
	project := seeded(get(f.ownerViewer, tasksPath(f.projectID)))
	if run, ok := project["owner-project"]; len(project) != 1 || !ok || run.Kind != "issue" || run.IssueID == nil {
		t.Fatalf("project runs: got %+v, want only the project's issue run", project)
	}
	none := seeded(get(f.ownerViewer, tasksPath(noProjectID)))
	for _, label := range []string{"owner-two-models", "member-private-agent", "member-chat", "unattributed"} {
		if _, ok := none[label]; !ok {
			t.Errorf("no-project runs: %s missing", label)
		}
	}
	if _, ok := none["owner-project"]; ok {
		t.Error("no-project runs include the project's run")
	}
	// member-tasks' per-run rules apply: a chat is named only to its creator.
	if chat := none["member-chat"]; chat.Kind != "chat" || chat.ChatTitle != nil || chat.ChatSessionID != nil {
		t.Errorf("owner sees another member's chat title/session: %+v", chat)
	}
	if got := none["member-private-agent"].AgentID; got != f.privateAgentID {
		t.Errorf("owner view: private agent id = %q, want %q", got, f.privateAgentID)
	}
	squad := seeded(get(f.ownerViewer, tasksPath(f.projectID)+"&squad_id="+standaloneSquadFilterSentinel))
	if len(squad) != 0 {
		t.Errorf("standalone filter on the squad-only project: got %+v, want none", squad)
	}

	// ---- plain member: only their own runs, with the same per-run rules ------
	mine := seeded(get(f.member, tasksPath(noProjectID)))
	if len(mine) != 2 {
		t.Fatalf("member view: got %v, want the member's 2 runs", mine)
	}
	if got := mine["member-private-agent"].AgentID; got != restrictedAgentsRowID {
		t.Errorf("run on someone else's private agent: agent_id = %q, want restricted bucket", got)
	}
	if chat := mine["member-chat"]; chat.ChatTitle == nil || *chat.ChatTitle != f.chatTitle {
		t.Errorf("chat creator should see their own chat title: %+v", chat)
	}
	if got := seeded(get(f.member, tasksPath(f.projectID))); len(got) != 0 {
		t.Errorf("member view of a project they ran nothing in: got %v, want none", got)
	}

	// ---- bad input -------------------------------------------------------------
	for _, p := range []string{
		"/api/dashboard/usage/project-tasks?days=7",
		tasksPath("nope"),
		tasksPath(restrictedProjectsID),
		tasksPath(f.projectID) + "&squad_id=nope",
	} {
		if w := dashboardGet(t, testHandler.GetDashboardProjectTasks, f.ownerViewer, p); w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", p, w.Code)
		}
	}
}

// Private projects (MUL-8472): the usage of a project the caller cannot see
// stays in the totals, under one bucket that names no project, and its runs
// cannot be listed. Workspace admins outside the project are no exception.
func TestDashboardUsageByProjectPrivateProject(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPrivateProjectFixture(t)
	agentID := createHandlerTestAgent(t, f.marker+"-pdash", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)
	model := memberUsageModelPrefix + f.marker
	secretTitle := f.marker + " secret project"
	for _, userID := range []string{f.insider, f.outsider} {
		taskID := dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "issue_id": f.secretIssue, "status": "completed",
			"accountable_user_id": userID,
		})
		dbfx.InsertNoID(t, "task_usage", testutil.Cols{
			"task_id": taskID, "provider": "claude", "model": model, "input_tokens": 10, "output_tokens": 0,
		}, "task_id = $1", taskID)
	}
	byProject := func(userID, query string) (map[string]int64, string) {
		w := dashboardGet(t, testHandler.GetDashboardUsageByProject, userID, "/api/dashboard/usage/by-project?days=7"+query)
		return seededTokensByProject(decodeOK[[]byProjectRow](t, "by-project"+query, w), model), w.Body.String()
	}
	tasks := func(userID, projectID string) string {
		w := dashboardGet(t, testHandler.GetDashboardProjectTasks, userID,
			"/api/dashboard/usage/project-tasks?days=7&project_id="+projectID)
		if w.Code != http.StatusOK {
			t.Fatalf("project-tasks as %s: got %d: %s", userID, w.Code, w.Body.String())
		}
		return strings.TrimSpace(w.Body.String())
	}
	missing := "00000000-0000-0000-0000-0000000000ff"

	// Project members see the project by name: the owner both runs, the insider
	// their own.
	if got, body := byProject(testUserID, ""); got[f.projectID] != 20 || !strings.Contains(body, secretTitle) {
		t.Errorf("owner (project member): got %v, want project=20 under its title", got)
	}
	if got, _ := byProject(f.insider, ""); got[f.projectID] != 10 || len(got) != 1 {
		t.Errorf("insider: got %v, want project=10", got)
	}
	if body := tasks(f.insider, f.projectID); !strings.Contains(body, f.secretIssue) {
		t.Errorf("insider: project-tasks lacks the secret issue: %s", body)
	}

	// Outside the project, admin or not: the usage stays, the project is never named.
	for _, viewer := range []struct {
		name, userID string
		want         int64
	}{{"outside admin", f.outsideAdmin, 20}, {"outsider", f.outsider, 10}} {
		got, body := byProject(viewer.userID, "")
		if got[restrictedProjectsID] != viewer.want || len(got) != 1 {
			t.Errorf("%s: got %v, want only restricted=%d", viewer.name, got, viewer.want)
		}
		if strings.Contains(body, f.projectID) || strings.Contains(body, secretTitle) {
			t.Errorf("%s: by-project names the private project: %s", viewer.name, body)
		}
		_, hiddenBody := byProject(viewer.userID, "&project_id="+f.projectID)
		_, absentBody := byProject(viewer.userID, "&project_id="+missing)
		if hiddenBody != absentBody {
			t.Errorf("%s: hidden project_id = %s, want the missing-project answer %s", viewer.name, hiddenBody, absentBody)
		}
		if hidden, absent := tasks(viewer.userID, f.projectID), tasks(viewer.userID, missing); hidden != "[]" || absent != "[]" {
			t.Errorf("%s: project-tasks hidden = %s, missing = %s, want [] for both", viewer.name, hidden, absent)
		}
	}
}

// The dashboard refuses guests (MUL-9420); the project endpoints are no
// exception, even for a guest's own runs.
func TestDashboardUsageByProjectRefusesGuests(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	guest := dbfx.User(t, "Project Usage Guest", fmt.Sprintf("project-usage-guest-%d@multica.test", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, guest, "guest")
	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "project-usage-guest-agent", []byte("[]"))
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "status": "completed", "accountable_user_id": guest,
	})
	dbfx.InsertNoID(t, "task_usage", testutil.Cols{
		"task_id": taskID, "provider": "claude", "model": memberUsageModelPrefix + "guest",
		"input_tokens": 7, "output_tokens": 0,
	}, "task_id = $1", taskID)

	for _, tc := range []struct {
		handler http.HandlerFunc
		path    string
	}{
		{testHandler.GetDashboardUsageByProject, "/api/dashboard/usage/by-project?days=7"},
		{testHandler.GetDashboardProjectTasks, "/api/dashboard/usage/project-tasks?days=7&project_id=" + noProjectID},
	} {
		w := dashboardGet(t, tc.handler, guest, tc.path)
		if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), taskID) {
			t.Errorf("%s as guest: got %d %s, want 403", tc.path, w.Code, w.Body.String())
		}
	}
}
