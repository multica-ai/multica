package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// privateProjectFixture: a private project with one regular member (insider),
// a regular workspace member outside it (outsider), the workspace owner
// (testUserID, not a project member), an issue and a comment inside it, and a
// public issue outside any project.
type privateProjectFixture struct {
	projectID     string
	insider       string
	outsider      string
	secretIssue   string
	secretComment string
	publicIssue   string
	marker        string
}

func newPrivateProjectFixture(t *testing.T) privateProjectFixture {
	t.Helper()
	marker := fmt.Sprintf("pp%d", time.Now().UnixNano())
	f := privateProjectFixture{marker: marker}
	f.insider = createPlainMember(t, marker+"-insider@multica.test")
	f.outsider = createPlainMember(t, marker+"-outsider@multica.test")
	f.projectID = dbfx.Project(t, marker+" secret project", testutil.Cols{"visibility": projectaccess.VisibilityPrivate})
	addProjectMemberRow(t, f.projectID, f.insider)
	// Explicit numbers far above the workspace counter keep these rows clear
	// of the numbers CreateIssue allocates in the same test.
	base := 800000 + int(time.Now().UnixNano()%100000)*2
	f.secretIssue = dbfx.Issue(t, marker+" secret issue", testutil.Cols{"project_id": f.projectID, "number": base})
	f.secretComment = dbfx.Comment(t, f.secretIssue, marker+" secret comment")
	f.publicIssue = dbfx.Issue(t, marker+" public issue", testutil.Cols{"number": base + 1})
	return f
}

func addProjectMemberRow(t *testing.T, projectID, userID string) {
	t.Helper()
	dbfx.InsertNoID(t, "project_member", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"project_id":   projectID,
		"user_id":      userID,
	}, "project_id = $1 AND user_id = $2", projectID, userID)
}

func asUser(req *http.Request, userID string) *http.Request {
	req.Header.Set("X-User-ID", userID)
	return req
}

func TestPrivateProjectIssueDetailMatrix(t *testing.T) {
	f := newPrivateProjectFixture(t)
	get := func(userID, issueID string) int {
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+issueID, nil), userID), "id", issueID)
		rec := httptest.NewRecorder()
		testHandler.GetIssue(rec, req)
		return rec.Code
	}
	for _, tc := range []struct {
		name   string
		userID string
		want   int
	}{
		{"project member", f.insider, http.StatusOK},
		{"workspace owner outside the member list", testUserID, http.StatusOK},
		{"workspace member outside the project", f.outsider, http.StatusNotFound},
	} {
		if got := get(tc.userID, f.secretIssue); got != tc.want {
			t.Errorf("%s: GET secret issue = %d, want %d", tc.name, got, tc.want)
		}
	}
	if got := get(f.outsider, f.publicIssue); got != http.StatusOK {
		t.Errorf("outsider GET public issue = %d, want 200", got)
	}

	// Sub-resources go through the same loader.
	req := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+f.secretIssue+"/comments", nil), f.outsider), "id", f.secretIssue)
	testutil.Call(t, testHandler.ListComments, req).Want(http.StatusNotFound)
}

func TestPrivateProjectHiddenFromLists(t *testing.T) {
	f := newPrivateProjectFixture(t)

	listIssueIDs := func(userID, query string) map[string]bool {
		req := asUser(newRequest(http.MethodGet, "/api/issues?"+query, nil), userID)
		var resp struct {
			Issues []IssueResponse `json:"issues"`
		}
		testutil.Call(t, testHandler.ListIssues, req).Want(http.StatusOK).JSON(&resp)
		ids := map[string]bool{}
		for _, is := range resp.Issues {
			ids[is.ID] = true
		}
		return ids
	}
	for _, query := range []string{"limit=100", "open_only=true"} {
		outsider := listIssueIDs(f.outsider, query)
		if outsider[f.secretIssue] || !outsider[f.publicIssue] {
			t.Errorf("%s: outsider sees secret=%v public=%v", query, outsider[f.secretIssue], outsider[f.publicIssue])
		}
		if insider := listIssueIDs(f.insider, query); !insider[f.secretIssue] {
			t.Errorf("%s: insider does not see the secret issue", query)
		}
	}

	listProjectIDs := func(userID string) map[string]bool {
		var resp struct {
			Projects []ProjectResponse `json:"projects"`
		}
		testutil.Call(t, testHandler.ListProjects, asUser(newRequest(http.MethodGet, "/api/projects", nil), userID)).
			Want(http.StatusOK).JSON(&resp)
		ids := map[string]bool{}
		for _, p := range resp.Projects {
			ids[p.ID] = true
			if p.ID == f.projectID && p.Visibility != projectaccess.VisibilityPrivate {
				t.Errorf("project visibility = %q, want private", p.Visibility)
			}
		}
		return ids
	}
	if listProjectIDs(f.outsider)[f.projectID] {
		t.Error("outsider sees the private project in the project list")
	}
	if !listProjectIDs(testUserID)[f.projectID] {
		t.Error("workspace owner does not see the private project in the project list")
	}

	search := func(userID string) map[string]bool {
		req := asUser(newRequest(http.MethodGet, "/api/issues/search?q="+f.marker, nil), userID)
		var resp struct {
			Issues []struct {
				ID string `json:"id"`
			} `json:"issues"`
		}
		testutil.Call(t, testHandler.SearchIssues, req).Want(http.StatusOK).JSON(&resp)
		ids := map[string]bool{}
		for _, is := range resp.Issues {
			ids[is.ID] = true
		}
		return ids
	}
	if got := search(f.outsider); got[f.secretIssue] || !got[f.publicIssue] {
		t.Errorf("outsider search: secret=%v public=%v", got[f.secretIssue], got[f.publicIssue])
	}
	if !search(f.insider)[f.secretIssue] {
		t.Error("insider search misses the secret issue")
	}

	projectReq := withURLParam(asUser(newRequest(http.MethodGet, "/api/projects/"+f.projectID, nil), f.outsider), "id", f.projectID)
	testutil.Call(t, testHandler.GetProject, projectReq).Want(http.StatusNotFound)
}

func TestPrivateProjectCommentAndAttachmentByID(t *testing.T) {
	f := newPrivateProjectFixture(t)
	react := func(userID string) int {
		req := withURLParam(asUser(newRequest(http.MethodPost, "/api/comments/"+f.secretComment+"/reactions", map[string]any{"emoji": "👍"}), userID), "commentId", f.secretComment)
		rec := httptest.NewRecorder()
		testHandler.AddReaction(rec, req)
		return rec.Code
	}
	if got := react(f.outsider); got != http.StatusNotFound {
		t.Errorf("outsider reaction on secret comment = %d, want 404", got)
	}
	if got := react(f.insider); got >= 400 {
		t.Errorf("insider reaction on secret comment = %d, want success", got)
	}
	testPool.Exec(context.Background(), `DELETE FROM comment_reaction WHERE comment_id = $1`, f.secretComment)

	attachmentID := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      f.secretIssue,
		"uploader_type": "member",
		"uploader_id":   testUserID,
		"filename":      "secret.txt",
		"url":           "https://example.invalid/secret.txt",
		"content_type":  "text/plain",
		"size_bytes":    1,
	})
	req := withURLParam(asUser(newRequest(http.MethodGet, "/api/attachments/"+attachmentID, nil), f.outsider), "id", attachmentID)
	testutil.Call(t, testHandler.GetAttachmentByID, req).Want(http.StatusNotFound)
}

func TestPrivateProjectWritesNeedVisibility(t *testing.T) {
	f := newPrivateProjectFixture(t)
	create := func(userID string, body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.CreateIssue, asUser(newRequest(http.MethodPost, "/api/issues", body), userID))
	}
	create(f.outsider, map[string]any{"title": f.marker + " smuggled", "project_id": f.projectID}).Want(http.StatusBadRequest)
	create(f.outsider, map[string]any{"title": f.marker + " child", "parent_issue_id": f.secretIssue}).Want(http.StatusBadRequest)

	var created IssueResponse
	create(f.insider, map[string]any{"title": f.marker + " insider issue", "project_id": f.projectID}).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID) })

	// Moving a public issue into the private project needs the same access.
	move := withURLParam(asUser(newRequest(http.MethodPut, "/api/issues/"+f.publicIssue, map[string]any{"project_id": f.projectID}), f.outsider), "id", f.publicIssue)
	testutil.Call(t, testHandler.UpdateIssue, move).Want(http.StatusBadRequest)
}

func TestPrivateProjectVisibilityAndMembersAPI(t *testing.T) {
	f := newPrivateProjectFixture(t)
	createPrivate := func() *testutil.Response {
		return testutil.Call(t, testHandler.CreateProject, asUser(newRequest(http.MethodPost, "/api/projects", map[string]any{
			"title":      f.marker + " new private",
			"visibility": "private",
			"member_ids": []string{f.insider},
		}), f.insider))
	}

	withFeatureFlag(t, testHandler, featureflags.PrivateProjectsV1, false)
	if got := createPrivate().Want(http.StatusForbidden).Map()["code"]; got != "private_projects_disabled" {
		t.Fatalf("flag off: code = %v", got)
	}

	withFeatureFlag(t, testHandler, featureflags.PrivateProjectsV1, true)
	var project ProjectResponse
	createPrivate().Want(http.StatusCreated).JSON(&project)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, project.ID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, project.ID)
	})
	if project.Visibility != projectaccess.VisibilityPrivate {
		t.Fatalf("visibility = %q", project.Visibility)
	}

	members := func(userID string) *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/projects/"+project.ID+"/members", nil), userID), "id", project.ID)
		return testutil.Call(t, testHandler.ListProjectMembers, req)
	}
	members(f.outsider).Want(http.StatusNotFound)
	var list struct {
		Members []ProjectMemberResponse `json:"members"`
	}
	members(f.insider).Want(http.StatusOK).JSON(&list)
	if len(list.Members) != 1 || list.Members[0].UserID != f.insider {
		t.Fatalf("members = %+v, want only the creator", list.Members)
	}

	// A project member may add; adding a non-workspace user is rejected.
	add := func(userID string, ids ...string) *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodPost, "/api/projects/"+project.ID+"/members", map[string]any{"user_ids": ids}), userID), "id", project.ID)
		return testutil.Call(t, testHandler.AddProjectMembers, req)
	}
	add(f.insider, "00000000-0000-0000-0000-00000000beef").Want(http.StatusBadRequest)
	add(f.insider, f.outsider).Want(http.StatusOK)

	remove := func(actor, target string) *testutil.Response {
		req := testutil.WithURLParams(asUser(newRequest(http.MethodDelete, "/api/projects/"+project.ID+"/members/"+target, nil), actor), "id", project.ID, "userId", target)
		return testutil.Call(t, testHandler.RemoveProjectMember, req)
	}
	// Removing someone else takes an admin or the lead; leaving does not.
	remove(f.outsider, f.insider).Want(http.StatusForbidden)
	remove(f.outsider, f.outsider).Want(http.StatusNoContent)
	members(f.outsider).Want(http.StatusNotFound)

	// Only an admin or the lead switches visibility; the owner can.
	update := func(userID string, body map[string]any) *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodPut, "/api/projects/"+project.ID, body), userID), "id", project.ID)
		return testutil.Call(t, testHandler.UpdateProject, req)
	}
	update(f.insider, map[string]any{"visibility": "workspace"}).Want(http.StatusForbidden)
	var updated ProjectResponse
	update(testUserID, map[string]any{"visibility": "workspace"}).Want(http.StatusOK).JSON(&updated)
	if updated.Visibility != projectaccess.VisibilityWorkspace {
		t.Fatalf("visibility after switch = %q", updated.Visibility)
	}
	members(f.outsider).Want(http.StatusOK)
}

func TestPrivateProjectTaskTranscriptAndRunHistory(t *testing.T) {
	f := newPrivateProjectFixture(t)
	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`, testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":   f.secretIssue,
		"runtime_id": testRuntimeID,
		"status":     "completed",
		"result":     testutil.Raw(`'{"output":"secret result"}'::jsonb`),
	})

	transcript := func(userID string) int {
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/tasks/"+taskID+"/messages", nil), userID), "taskId", taskID)
		req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
		rec := httptest.NewRecorder()
		testHandler.ListTaskMessagesByUser(rec, req)
		return rec.Code
	}
	if got := transcript(f.outsider); got != http.StatusNotFound {
		t.Errorf("outsider transcript = %d, want 404", got)
	}
	if got := transcript(f.insider); got != http.StatusOK {
		t.Errorf("insider transcript = %d, want 200", got)
	}

	history := func(userID string) AgentTaskResponse {
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks", nil), userID), "id", agentID)
		var tasks []AgentTaskResponse
		testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&tasks)
		for _, task := range tasks {
			if task.ID == taskID {
				return task
			}
		}
		t.Fatalf("run %s missing from %s's history", taskID, userID)
		return AgentTaskResponse{}
	}
	hidden := history(f.outsider)
	if !hidden.Restricted || hidden.IssueID != "" || hidden.Result != nil || hidden.Status != "completed" {
		t.Errorf("outsider run = restricted %v issue %q result %v status %q; want a redacted completed run",
			hidden.Restricted, hidden.IssueID, hidden.Result, hidden.Status)
	}
	visible := history(f.insider)
	if visible.Restricted || visible.IssueID != f.secretIssue {
		t.Errorf("insider run = restricted %v issue %q", visible.Restricted, visible.IssueID)
	}
}

func TestPrivateProjectEnqueueNeedsAgentOwnerAccess(t *testing.T) {
	f := newPrivateProjectFixture(t)
	ctx := context.Background()
	outsiderAgent := dbfx.Agent(t, f.marker+" outsider agent", testRuntimeID, testutil.Cols{"owner_id": f.outsider})
	insiderAgent := dbfx.Agent(t, f.marker+" insider agent", testRuntimeID, testutil.Cols{"owner_id": f.insider})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.secretIssue)
	})

	issue := func(agentID string) db.Issue {
		testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, f.secretIssue, agentID)
		row, err := testHandler.Queries.GetIssue(ctx, parseUUID(f.secretIssue))
		if err != nil {
			t.Fatalf("load issue: %v", err)
		}
		return row
	}
	_, err := testHandler.TaskService.EnqueueTaskForIssueByActor(ctx, issue(outsiderAgent), parseUUID(f.insider))
	if !errors.Is(err, service.ErrProjectAccessDenied) {
		t.Fatalf("outsider-owned agent: err = %v, want ErrProjectAccessDenied", err)
	}
	_, err = testHandler.TaskService.EnqueueTaskForIssueByActor(ctx, issue(insiderAgent), parseUUID(f.insider))
	if errors.Is(err, service.ErrProjectAccessDenied) {
		t.Fatalf("insider-owned agent refused for project access")
	}

	// Assignment surfaces the same rule as an explicit 403.
	rec := httptest.NewRecorder()
	req := asUser(newRequest(http.MethodPut, "/api/issues/"+f.secretIssue, nil), f.insider)
	ok := testHandler.requireAssigneeCanAccessProject(rec, req, parseUUID(testWorkspaceID),
		pgtype.Text{String: "agent", Valid: true}, parseUUID(outsiderAgent), parseUUID(f.projectID))
	if ok || rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), string(ReasonProjectAccessBlocked)) {
		t.Fatalf("assignment pre-check = %v %d %s", ok, rec.Code, rec.Body.String())
	}
}

func TestPrivateProjectInboxSkipsOutsiders(t *testing.T) {
	f := newPrivateProjectFixture(t)
	ctx := context.Background()
	write := func(recipient string) error {
		_, err := testHandler.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   parseUUID(testWorkspaceID),
			RecipientType: "member",
			RecipientID:   parseUUID(recipient),
			Type:          "mentioned",
			Severity:      "info",
			IssueID:       parseUUID(f.secretIssue),
			Title:         f.marker + " secret notification",
		})
		return err
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, f.secretIssue)
	})
	if err := write(f.outsider); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("outsider notification: err = %v, want skipped", err)
	}
	if err := write(f.insider); err != nil {
		t.Fatalf("insider notification: %v", err)
	}
	if err := write(testUserID); err != nil {
		t.Fatalf("owner notification: %v", err)
	}
}

func removeProjectMemberAsOwner(t *testing.T, f privateProjectFixture, userID string) {
	t.Helper()
	req := testutil.WithURLParams(newRequest(http.MethodDelete, "/api/projects/"+f.projectID+"/members/"+userID, nil), "id", f.projectID, "userId", userID)
	testutil.Call(t, testHandler.RemoveProjectMember, req).Want(http.StatusNoContent)
}

func TestPrivateProjectIssueUnreadRunReferencesAfterRevocation(t *testing.T) {
	f := newPrivateProjectFixture(t)
	agentID := dbfx.Agent(t, f.marker+" unread agent", testRuntimeID, testutil.Cols{"owner_id": f.insider})
	secretTask := dbfx.Task(t, agentID, testutil.Cols{"issue_id": f.secretIssue, "runtime_id": testRuntimeID, "status": "completed"})
	publicTask := dbfx.Task(t, agentID, testutil.Cols{"issue_id": f.publicIssue, "runtime_id": testRuntimeID, "status": "completed"})
	secretResult := dbfx.Comment(t, f.secretIssue, "private run result", testutil.Cols{"source_task_id": secretTask})
	publicResult := dbfx.Comment(t, f.publicIssue, "public run result", testutil.Cols{"source_task_id": publicTask})
	dbfx.Cleanup(t, `DELETE FROM issue_unread WHERE user_id = $1`, f.insider)
	dbfx.Exec(t, `
INSERT INTO issue_unread (workspace_id, user_id, issue_id, outcome, agent_id, task_id)
VALUES ($1, $2, $3, 'completed', $4, $5), ($1, $2, $6, 'completed', $4, $7)`,
		testWorkspaceID, f.insider, f.secretIssue, agentID, secretTask, f.publicIssue, publicTask)

	before := listIssueUnreadAs(t, f.insider)
	if len(before) != 2 {
		t.Fatalf("member should see both unread issues, got %+v", before)
	}
	for _, item := range before {
		wantTask, wantComment := publicTask, publicResult
		if item.IssueID == f.secretIssue {
			wantTask, wantComment = secretTask, secretResult
		} else if item.IssueID != f.publicIssue {
			t.Fatalf("unexpected unread issue: %+v", item)
		}
		if item.TaskID == nil || *item.TaskID != wantTask || item.CommentID == nil || *item.CommentID != wantComment {
			t.Fatalf("unread issue should link to task %s and comment %s, got %+v", wantTask, wantComment, item)
		}
	}

	removeProjectMemberAsOwner(t, f, f.insider)

	after := listIssueUnreadAs(t, f.insider)
	if len(after) != 1 || after[0].IssueID != f.publicIssue {
		t.Fatalf("revoked member should only see the public unread issue, got %+v", after)
	}
	if after[0].TaskID == nil || *after[0].TaskID != publicTask || after[0].CommentID == nil || *after[0].CommentID != publicResult {
		t.Fatalf("public unread issue should retain its run references after revocation, got %+v", after[0])
	}
}

func TestPrivateProjectArchivedInboxAndUnreadAfterRevocation(t *testing.T) {
	f := newPrivateProjectFixture(t)
	dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": testWorkspaceID, "recipient_type": "member", "recipient_id": f.insider,
		"type": "mentioned", "severity": "info", "title": f.marker + " archived secret", "issue_id": f.secretIssue,
		"archived": true,
	})
	unreadID := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": testWorkspaceID, "recipient_type": "member", "recipient_id": f.insider,
		"type": "mentioned", "severity": "info", "title": f.marker + " unread secret", "issue_id": f.secretIssue,
	})
	countUnread := func() int64 {
		req := asUser(newRequest(http.MethodGet, "/api/inbox/unread-count", nil), f.insider)
		var resp struct {
			Count int64 `json:"count"`
		}
		testutil.Call(t, inboxWorkspaceHandler(testHandler.CountUnreadInbox), req).Want(http.StatusOK).JSON(&resp)
		return resp.Count
	}
	if got := countUnread(); got < 1 {
		t.Fatalf("member unread count before removal = %d, want the private notification counted", got)
	}
	before := countUnread()

	removeProjectMemberAsOwner(t, f, f.insider)

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"archived list", testHandler.ListArchivedInbox},
		{"archived page", testHandler.ListArchivedInboxPage},
		{"archived facets", testHandler.GetArchivedInboxFacets},
	} {
		req := asUser(newRequest(http.MethodGet, "/api/inbox/archived", nil), f.insider)
		body := testutil.Call(t, inboxWorkspaceHandler(tc.handler), req).Want(http.StatusOK).Text()
		if strings.Contains(body, f.secretIssue) || strings.Contains(body, f.marker) {
			t.Errorf("%s still returns the private notification after removal: %s", tc.name, body)
		}
	}
	if got := countUnread(); got != before-1 {
		t.Errorf("unread count after removal = %d, want %d (notification %s no longer counted)", got, before-1, unreadID)
	}
}

func TestPrivateProjectLeadChangeCannotGrantAccess(t *testing.T) {
	f := newPrivateProjectFixture(t)
	isMember := func(userID string) bool {
		var ok bool
		if err := testPool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM project_member WHERE project_id = $1 AND user_id = $2)`, f.projectID, userID,
		).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1 AND user_id = $2`, f.projectID, f.outsider)
	})
	setLead := func(req *http.Request) *testutil.Response {
		return testutil.Call(t, testHandler.UpdateProject, withURLParam(req, "id", f.projectID))
	}
	body := map[string]any{"lead_type": "member", "lead_id": f.outsider}

	// An agent run of a project member cannot hand the project to an outsider.
	agent := dbfx.Agent(t, f.marker+" insider agent", testRuntimeID, testutil.Cols{"owner_id": f.insider})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": f.secretIssue, "status": "running",
		"originator_user_id": f.insider, "accountable_user_id": f.insider})
	req := asUser(newRequest(http.MethodPut, "/api/projects/"+f.projectID, body), f.insider)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agent)
	req.Header.Set("X-Task-ID", task)
	setLead(req).Want(http.StatusForbidden)
	if isMember(f.outsider) {
		t.Fatal("agent lead change granted the outsider project membership")
	}
	var lead *string
	testPool.QueryRow(context.Background(), `SELECT lead_id::text FROM project WHERE id = $1`, f.projectID).Scan(&lead)
	if lead != nil {
		t.Fatalf("refused update still wrote lead_id = %s", *lead)
	}

	// A human project member may, and the new lead becomes a member.
	setLead(asUser(newRequest(http.MethodPut, "/api/projects/"+f.projectID, body), f.insider)).Want(http.StatusOK)
	if !isMember(f.outsider) {
		t.Fatal("a member lead of a private project must be a project member")
	}
}

func TestPrivateProjectRevocationDeletesProjectFromSearchIndex(t *testing.T) {
	f := newPrivateProjectFixture(t)
	cursor := searchIndexManifest(t, testWorkspaceID).Cursor
	removeProjectMemberAsOwner(t, f, f.insider)
	req := asUser(searchIndexRequest(http.MethodPost, "/api/search-index/changes", testWorkspaceID, map[string]any{"cursor": cursor, "limit": 1000}), f.insider)
	var changes SearchIndexChangesResponse
	testutil.Call(t, testHandler.ListSearchIndexChanges, req).Want(http.StatusOK).JSON(&changes)
	for _, id := range changes.Deleted.Projects {
		if id == f.projectID {
			return
		}
	}
	t.Errorf("revoked project missing from search deletions; projects=%v issues=%v comments=%v",
		changes.Deleted.Projects, changes.Deleted.Issues, changes.Deleted.Comments)
}

func TestPrivateProjectQuickCreateNeedsAgentOwnerAccess(t *testing.T) {
	f := newPrivateProjectFixture(t)
	runtime := dbfx.Runtime(t, f.marker+" runtime", testutil.Cols{"owner_id": f.outsider, "metadata": testutil.Raw(`'{"cli_version":"99.0.0"}'::jsonb`)})
	agent := dbfx.Agent(t, f.marker+" outsider agent", runtime, testutil.Cols{"owner_id": f.outsider, "permission_mode": "public_to", "visibility": "workspace"})
	dbfx.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": agent, "target_type": "workspace", "target_id": testWorkspaceID})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent) })
	for _, body := range []map[string]any{
		{"prompt": "Create a task in this project", "agent_id": agent, "project_id": f.projectID},
		{"prompt": "Create a sub-issue", "agent_id": agent, "parent_issue_id": f.secretIssue},
	} {
		rec := httptest.NewRecorder()
		testHandler.QuickCreateIssue(rec, newRequest(http.MethodPost, "/api/issues/quick-create", body))
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), string(ReasonProjectAccessBlocked)) {
			t.Errorf("quick-create %v = %d %s, want 403 %s", body, rec.Code, rec.Body.String(), ReasonProjectAccessBlocked)
		}
	}
	var queued int
	testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, agent).Scan(&queued)
	if queued != 0 {
		t.Fatalf("refused quick-create queued %d task(s)", queued)
	}
}

func TestPrivateProjectCommentSubIssuePreviewHidden(t *testing.T) {
	f := newPrivateProjectFixture(t)
	preview := func(userID string) *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/comments/"+f.secretComment+"/sub-issue/preview", nil), userID), "commentId", f.secretComment)
		return testutil.Call(t, testHandler.PreviewCommentSubIssue, req)
	}
	if body := preview(f.outsider).Text(); strings.Contains(body, f.marker) {
		t.Fatalf("outsider preview leaks the private thread: %s", body)
	}
	preview(f.insider).Want(http.StatusOK)
}
