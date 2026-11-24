package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// docTaskRow is the part of a doc discussion run these tests assert on.
type docTaskRow struct {
	ID, AgentID, Status, ThreadID, CommentID, Originator string
}

func docTasksFor(t *testing.T, docID string) []docTaskRow {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT id::text, agent_id::text, status, doc_thread_id::text, doc_comment_id::text,
			COALESCE(originator_user_id::text, '')
		FROM agent_task_queue WHERE doc_id = $1 ORDER BY created_at, id`, docID)
	if err != nil {
		t.Fatalf("list doc tasks: %v", err)
	}
	defer rows.Close()
	var out []docTaskRow
	for rows.Next() {
		var r docTaskRow
		if err := rows.Scan(&r.ID, &r.AgentID, &r.Status, &r.ThreadID, &r.CommentID, &r.Originator); err != nil {
			t.Fatalf("scan doc task: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func TestDocCommentMentionsStartAgentRunsInTheDiscussion(t *testing.T) {
	doc := createDocForTest(t)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, "DELETE FROM agent_task_queue WHERE doc_id = $1", doc.ID)
		testPool.Exec(ctx, "DELETE FROM doc_comment WHERE doc_id = $1", doc.ID)
	})
	stamp := time.Now().UnixNano()
	writer := createHandlerTestAgent(t, fmt.Sprintf("Doc Writer %d", stamp), nil)
	reviewer := createHandlerTestAgent(t, fmt.Sprintf("Doc Reviewer %d", stamp), nil)
	colleague := dbfx.User(t, "Lin Mo", fmt.Sprintf("lin-mo-%d@example.test", stamp))
	dbfx.Member(t, testWorkspaceID, colleague, "member")
	squad := dbfx.Squad(t, fmt.Sprintf("Doc Squad %d", stamp), writer)

	post := func(body map[string]any) DocCommentResponse {
		t.Helper()
		var c DocCommentResponse
		testutil.Call(t, testHandler.CreateDocComment, withURLParam(newRequest("POST", "/comments", body), "id", doc.ID)).Want(http.StatusCreated).JSON(&c)
		return c
	}
	asAgent := func(req *http.Request, agentID, taskID string) *http.Request {
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", taskID)
		return req
	}
	mention := func(kind, id string) string { return fmt.Sprintf("[@X](mention://%s/%s)", kind, id) }

	// A mention starts a run on the discussion, scoped to the doc.
	root := post(map[string]any{"content": "Tighten this. " + mention("agent", writer), "anchor_quote": "Original"})
	tasks := docTasksFor(t, doc.ID)
	if len(tasks) != 1 || tasks[0].AgentID != writer || tasks[0].ThreadID != root.ID || tasks[0].CommentID != root.ID || tasks[0].Status != "queued" {
		t.Fatalf("mention should queue one run for the discussion: %+v", tasks)
	}
	if tasks[0].Originator != testUserID {
		t.Fatalf("the run acts for the member who asked: %+v", tasks[0])
	}
	var runs struct {
		Tasks []DocTaskRunResponse `json:"tasks"`
	}
	testutil.Call(t, testHandler.ListDocTaskRuns, withURLParam(newRequest("GET", "/task-runs", nil), "id", doc.ID)).Want(http.StatusOK).JSON(&runs)
	if len(runs.Tasks) != 1 || runs.Tasks[0].DocThreadID != root.ID || runs.Tasks[0].Status != "queued" || !runs.Tasks[0].CanCancel {
		t.Fatalf("task-runs: %+v", runs.Tasks)
	}

	// A follow-up before the run starts folds into it.
	followUp := post(map[string]any{"content": "Also keep it short.", "parent_id": root.ID})
	tasks = docTasksFor(t, doc.ID)
	if len(tasks) != 1 || tasks[0].CommentID != followUp.ID {
		t.Fatalf("a queued run should absorb the follow-up: %+v", tasks)
	}

	// Once it ran, a reply without a mention continues with the same agent.
	dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1", tasks[0].ID)
	firstRun := tasks[0].ID
	post(map[string]any{"content": "One more pass please.", "parent_id": root.ID})
	tasks = docTasksFor(t, doc.ID)
	if len(tasks) != 2 || tasks[1].AgentID != writer || tasks[1].Status != "queued" {
		t.Fatalf("a plain reply should continue with the discussion's agent: %+v", tasks)
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1", tasks[1].ID)

	// Talking to a person, or naming a squad, starts nothing.
	post(map[string]any{"content": "What do you think? " + mention("member", colleague), "parent_id": root.ID})
	post(map[string]any{"content": "Squad, take a look " + mention("squad", squad), "parent_id": root.ID})
	if got := len(docTasksFor(t, doc.ID)); got != 2 {
		t.Fatalf("member and squad mentions must not start runs, have %d", got)
	}

	// The agent replies in the discussion; its own reply does not wake it.
	var agentReply DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, asAgent(withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "Done — tightened.", "parent_id": root.ID,
	}), "id", doc.ID), writer, firstRun)).Want(http.StatusCreated).JSON(&agentReply)
	if agentReply.AuthorType != "agent" || agentReply.AuthorID != writer {
		t.Fatalf("agent reply author: %+v", agentReply)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM doc_comment WHERE id = $1 AND source_task_id = $2", agentReply.ID, firstRun); n != 1 {
		t.Fatal("an agent reply records the run that wrote it")
	}
	if got := len(docTasksFor(t, doc.ID)); got != 2 {
		t.Fatalf("an agent's own reply must not start a run, have %d", got)
	}

	// Agents cannot start a discussion.
	testutil.Call(t, testHandler.CreateDocComment, asAgent(withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "New topic", "anchor_quote": "Original",
	}), "id", doc.ID), writer, firstRun)).Want(http.StatusForbidden)

	// An agent handing off to another agent acts for the same person.
	testutil.Call(t, testHandler.CreateDocComment, asAgent(withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "Can you review? " + mention("agent", reviewer), "parent_id": root.ID,
	}), "id", doc.ID), writer, firstRun)).Want(http.StatusCreated)
	tasks = docTasksFor(t, doc.ID)
	if len(tasks) != 3 || tasks[2].AgentID != reviewer || tasks[2].Originator != testUserID {
		t.Fatalf("agent mention should start the reviewer's run for the same person: %+v", tasks)
	}

	// A doc edit made by a discussion run links back to the discussion.
	var updated DocResponse
	testutil.Call(t, testHandler.UpdateDoc, asAgent(withURLParam(newRequest("PATCH", "/api/docs/"+doc.ID, map[string]any{
		"base_revision": doc.Revision, "content": "# First\n\nTighter",
	}), "id", doc.ID), writer, firstRun)).Want(http.StatusOK).JSON(&updated)
	var versions struct {
		Versions []DocVersionResponse `json:"versions"`
	}
	testutil.Call(t, testHandler.ListDocVersions, withURLParam(newRequest("GET", "/versions", nil), "id", doc.ID)).Want(http.StatusOK).JSON(&versions)
	if len(versions.Versions) == 0 || versions.Versions[0].DocThreadID == nil || *versions.Versions[0].DocThreadID != root.ID ||
		versions.Versions[0].DocThreadQuote == nil || *versions.Versions[0].DocThreadQuote != "Original" {
		t.Fatalf("latest version should name its discussion: %+v", versions.Versions)
	}
	if last := versions.Versions[len(versions.Versions)-1]; last.DocThreadID != nil {
		t.Fatalf("a member's version names no discussion: %+v", last)
	}

	// Deleting the doc stops its pending runs.
	testutil.Call(t, testHandler.DeleteDoc, withURLParam(newRequest("DELETE", "/api/docs", nil), "id", doc.ID)).Want(http.StatusNoContent)
	for _, task := range docTasksFor(t, doc.ID) {
		if task.Status != "completed" && task.Status != "cancelled" {
			t.Fatalf("doc delete left an active run: %+v", task)
		}
	}
}

func TestDocCommentClaimCarriesDiscussionAndResumesDocSession(t *testing.T) {
	doc := createDocForTest(t)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, "DELETE FROM agent_task_queue WHERE doc_id = $1", doc.ID)
		testPool.Exec(ctx, "DELETE FROM doc_comment WHERE doc_id = $1", doc.ID)
	})
	agentID := createHandlerTestAgent(t, fmt.Sprintf("Doc Claim %d", time.Now().UnixNano()), nil)

	var root DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "Fix this [@A](mention://agent/" + agentID + ")", "anchor_quote": "Original",
	}), "id", doc.ID)).Want(http.StatusCreated).JSON(&root)
	tasks := docTasksFor(t, doc.ID)
	if len(tasks) != 1 {
		t.Fatalf("want one run, have %+v", tasks)
	}
	// An earlier run of this agent on the same doc left a session behind.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now(),
		session_id = 'doc-session', work_dir = '/tmp/doc-workdir' WHERE id = $1`, tasks[0].ID)
	var next DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "And this one", "parent_id": root.ID,
	}), "id", doc.ID)).Want(http.StatusCreated).JSON(&next)
	tasks = docTasksFor(t, doc.ID)
	if len(tasks) != 2 {
		t.Fatalf("want a second run, have %+v", tasks)
	}

	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(tasks[1].ID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	resp := taskToResponse(task, testWorkspaceID)
	req := newRequest("POST", "/claim", nil)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityDocCommentV1)
	if failure := testHandler.applyDocCommentClaim(req, &task, &resp, uuidToString(task.RuntimeID), testWorkspaceID); failure != nil {
		t.Fatalf("claim failed: %+v", failure)
	}
	if resp.WorkspaceID != testWorkspaceID || resp.DocTitle != doc.Title || resp.DocThreadQuote != "Original" ||
		resp.DocCommentContent != "And this one" || resp.DocCommentAuthorName == "" {
		t.Fatalf("claim context: %+v", resp)
	}
	if resp.PriorSessionID != "doc-session" || resp.PriorWorkDir != "/tmp/doc-workdir" {
		t.Fatalf("claim should resume the agent's session on this doc: session=%q workdir=%q", resp.PriorSessionID, resp.PriorWorkDir)
	}

	// A run whose doc is gone is cancelled instead of dispatched.
	testPool.Exec(context.Background(), "UPDATE agent_task_queue SET doc_id = gen_random_uuid() WHERE id = $1", tasks[1].ID)
	orphan, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(tasks[1].ID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	orphanResp := taskToResponse(orphan, testWorkspaceID)
	if failure := testHandler.applyDocCommentClaim(req, &orphan, &orphanResp, uuidToString(orphan.RuntimeID), testWorkspaceID); failure == nil {
		t.Fatal("a run whose doc is gone must not be dispatched")
	}
	var status string
	dbfx.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id = $1", tasks[1].ID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("orphaned doc run status = %q, want cancelled", status)
	}
	testPool.Exec(context.Background(), "DELETE FROM agent_task_queue WHERE id = $1", tasks[1].ID)
}

// A run that ends without replying still answers its discussion with its
// final output; one that replied gets no second reply.
func TestDocCommentRunWithoutReplyPostsItsOutput(t *testing.T) {
	doc := createDocForTest(t)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, "DELETE FROM agent_task_queue WHERE doc_id = $1", doc.ID)
		testPool.Exec(ctx, "DELETE FROM doc_comment WHERE doc_id = $1", doc.ID)
	})
	agentID := createHandlerTestAgent(t, fmt.Sprintf("Doc Fallback %d", time.Now().UnixNano()), nil)
	var root DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "Summarize [@A](mention://agent/" + agentID + ")", "anchor_quote": "Original",
	}), "id", doc.ID)).Want(http.StatusCreated).JSON(&root)
	tasks := docTasksFor(t, doc.ID)
	if len(tasks) != 1 {
		t.Fatalf("want one run, have %+v", tasks)
	}
	complete := func(taskID string) {
		t.Helper()
		dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'running', started_at = now(), dispatched_at = now() WHERE id = $1", taskID)
		if _, err := testHandler.TaskService.CompleteTask(context.Background(),
			parseUUID(taskID), []byte(`{"output":"The section says we ship Friday."}`), "", "", "", false, "", ""); err != nil {
			t.Fatalf("CompleteTask: %v", err)
		}
	}
	replies := func() int {
		return dbfx.Count(t, "SELECT count(*) FROM doc_comment WHERE doc_id = $1 AND parent_id = $2 AND author_type = 'agent' AND author_id = $3", doc.ID, root.ID, agentID)
	}

	// A comment the run left in another discussion does not answer this one.
	var other DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "Unrelated", "anchor_quote": "First",
	}), "id", doc.ID)).Want(http.StatusCreated).JSON(&other)
	elsewhere := withURLParam(newRequest("POST", "/comments", map[string]any{"content": "Noted.", "parent_id": other.ID}), "id", doc.ID)
	elsewhere.Header.Set("X-Actor-Source", "task_token")
	elsewhere.Header.Set("X-Agent-ID", agentID)
	elsewhere.Header.Set("X-Task-ID", tasks[0].ID)
	testutil.Call(t, testHandler.CreateDocComment, elsewhere).Want(http.StatusCreated)
	notified := func() int {
		return dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE recipient_id = $1 AND type = 'doc_comment' AND details->>'thread_id' = $2", testUserID, root.ID)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DELETE FROM inbox_item WHERE details->>'doc_id' = $1", doc.ID)
	})

	complete(tasks[0].ID)
	if notified() != 1 {
		t.Fatalf("the fallback reply should notify the discussion's author like any reply, have %d", notified())
	}
	if replies() != 1 {
		t.Fatalf("a run that never replied should answer with its output, have %d agent replies", replies())
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM doc_comment WHERE parent_id = $1 AND source_task_id = $2 AND content = 'The section says we ship Friday.'", root.ID, tasks[0].ID); n != 1 {
		t.Fatal("the fallback reply records its run and carries the output")
	}

	// The next run replies itself; completion adds nothing.
	testutil.Call(t, testHandler.CreateDocComment, withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "Shorter please", "parent_id": root.ID,
	}), "id", doc.ID)).Want(http.StatusCreated)
	tasks = docTasksFor(t, doc.ID)
	if len(tasks) != 2 {
		t.Fatalf("want a second run, have %+v", tasks)
	}
	req := withURLParam(newRequest("POST", "/comments", map[string]any{"content": "Friday.", "parent_id": root.ID}), "id", doc.ID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", tasks[1].ID)
	testutil.Call(t, testHandler.CreateDocComment, req).Want(http.StatusCreated)
	complete(tasks[1].ID)
	if replies() != 2 {
		t.Fatalf("a run that replied must not get a fallback reply, have %d agent replies", replies())
	}
}

// A daemon that cannot run doc discussions must not get one: it would build an
// issue prompt with no issue. The run fails before launch with an update hint.
func TestDocCommentClaimFailsOnDaemonWithoutDocSupport(t *testing.T) {
	doc := createDocForTest(t)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, "DELETE FROM agent_task_queue WHERE doc_id = $1", doc.ID)
		testPool.Exec(ctx, "DELETE FROM doc_comment WHERE doc_id = $1", doc.ID)
	})
	agentID := createHandlerTestAgent(t, fmt.Sprintf("Doc Old Daemon %d", time.Now().UnixNano()), nil)
	testutil.Call(t, testHandler.CreateDocComment, withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "Fix this [@A](mention://agent/" + agentID + ")", "anchor_quote": "Original",
	}), "id", doc.ID)).Want(http.StatusCreated)
	tasks := docTasksFor(t, doc.ID)
	if len(tasks) != 1 {
		t.Fatalf("want one run, have %+v", tasks)
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1", tasks[0].ID)
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(tasks[0].ID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	resp := taskToResponse(task, testWorkspaceID)
	if failure := testHandler.applyDocCommentClaim(newRequest("POST", "/claim", nil), &task, &resp, uuidToString(task.RuntimeID), testWorkspaceID); failure == nil {
		t.Fatal("a daemon without doc support must not get the run")
	}
	var status, reason string
	dbfx.QueryRow(t, "SELECT status, COALESCE(failure_reason, '') FROM agent_task_queue WHERE id = $1", tasks[0].ID).Scan(&status, &reason)
	if status != "failed" || reason != string(taskfailure.ReasonAgentRuntimeVersionUnsupported) {
		t.Fatalf("run = %s/%s, want failed with runtime_version_unsupported", status, reason)
	}
}

// A follow-up folded into a queued run moves the run to the follow-up's
// author: who it acts for, and the connected apps it carries.
func TestDocCommentFollowUpMovesQueuedRunToItsAuthor(t *testing.T) {
	doc := createDocForTest(t)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, "DELETE FROM agent_task_queue WHERE doc_id = $1", doc.ID)
		testPool.Exec(ctx, "DELETE FROM doc_comment WHERE doc_id = $1", doc.ID)
	})
	stamp := time.Now().UnixNano()
	agentID := createHandlerTestAgent(t, fmt.Sprintf("Doc Fold %d", stamp), nil)
	colleague := dbfx.User(t, "Qin Lu", fmt.Sprintf("qin-lu-%d@example.test", stamp))
	dbfx.Member(t, testWorkspaceID, colleague, "member")

	var root DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "Check this [@A](mention://agent/" + agentID + ")", "anchor_quote": "Original",
	}), "id", doc.ID)).Want(http.StatusCreated).JSON(&root)
	tasks := docTasksFor(t, doc.ID)
	if len(tasks) != 1 || tasks[0].Originator != testUserID {
		t.Fatalf("want one run for the first member, have %+v", tasks)
	}
	// Stand in for the first member's connected apps on the queued run.
	dbfx.Exec(t, `UPDATE agent_task_queue SET runtime_mcp_overlay = '{"servers":["first-member"]}'::jsonb,
		runtime_connected_apps = '[{"toolkit":"first-member"}]'::jsonb WHERE id = $1`, tasks[0].ID)

	req := withURLParam(newRequest("POST", "/comments", map[string]any{"content": "Also the intro", "parent_id": root.ID}), "id", doc.ID)
	req.Header.Set("X-User-ID", colleague)
	testutil.Call(t, testHandler.CreateDocComment, req).Want(http.StatusCreated)

	tasks = docTasksFor(t, doc.ID)
	if len(tasks) != 1 || tasks[0].Originator != colleague {
		t.Fatalf("the folded run should act for the follow-up's author: %+v", tasks)
	}
	var accountable, evidence string
	var keptApps bool
	dbfx.QueryRow(t, `SELECT COALESCE(accountable_user_id::text, ''), COALESCE(trigger_evidence_ref_id::text, ''),
		runtime_mcp_overlay IS NOT NULL OR runtime_connected_apps IS NOT NULL
		FROM agent_task_queue WHERE id = $1`, tasks[0].ID).Scan(&accountable, &evidence, &keptApps)
	if accountable != colleague || evidence != tasks[0].CommentID {
		t.Fatalf("attribution must move with the run: accountable=%s evidence=%s", accountable, evidence)
	}
	if keptApps {
		t.Fatal("the run must not keep the first member's connected apps")
	}
}

// Answering an agent's comment goes to that agent, even when another agent
// spoke in the discussion since.
func TestDocCommentReplyGoesToTheAgentItAnswers(t *testing.T) {
	doc := createDocForTest(t)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, "DELETE FROM agent_task_queue WHERE doc_id = $1", doc.ID)
		testPool.Exec(ctx, "DELETE FROM doc_comment WHERE doc_id = $1", doc.ID)
	})
	stamp := time.Now().UnixNano()
	first := createHandlerTestAgent(t, fmt.Sprintf("Doc First %d", stamp), nil)
	second := createHandlerTestAgent(t, fmt.Sprintf("Doc Second %d", stamp), nil)
	var root DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "[@A](mention://agent/" + first + ") [@B](mention://agent/" + second + ") thoughts?", "anchor_quote": "Original",
	}), "id", doc.ID)).Want(http.StatusCreated).JSON(&root)
	tasks := docTasksFor(t, doc.ID)
	if len(tasks) != 2 {
		t.Fatalf("want a run per agent, have %+v", tasks)
	}
	runOf := map[string]string{tasks[0].AgentID: tasks[0].ID, tasks[1].AgentID: tasks[1].ID}
	dbfx.Exec(t, "UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE doc_id = $1", doc.ID)
	reply := func(agentID, content string) DocCommentResponse {
		t.Helper()
		var c DocCommentResponse
		req := withURLParam(newRequest("POST", "/comments", map[string]any{"content": content, "parent_id": root.ID}), "id", doc.ID)
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", runOf[agentID])
		testutil.Call(t, testHandler.CreateDocComment, req).Want(http.StatusCreated).JSON(&c)
		return c
	}
	firstReply := reply(first, "First take.")
	reply(second, "Second take.")

	testutil.Call(t, testHandler.CreateDocComment, withURLParam(newRequest("POST", "/comments", map[string]any{
		"content": "Can you expand on that?", "parent_id": firstReply.ID,
	}), "id", doc.ID)).Want(http.StatusCreated)
	tasks = docTasksFor(t, doc.ID)
	if len(tasks) != 3 || tasks[2].AgentID != first {
		t.Fatalf("a reply to the first agent's comment should go to it: %+v", tasks)
	}
}

// The run list shows each run's state to every doc reader, but its error and
// the way to stop it only to those who can see the agent.
func TestDocTaskRunsHideDetailsOfAgentsTheViewerCannotSee(t *testing.T) {
	doc := createDocForTest(t)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, "DELETE FROM agent_task_queue WHERE doc_id = $1", doc.ID)
	})
	stamp := time.Now().UnixNano()
	private := dbfx.Agent(t, fmt.Sprintf("Doc Private %d", stamp), handlerTestRuntimeID(t), testutil.Cols{"visibility": "private"})
	t.Cleanup(func() { testPool.Exec(context.Background(), "DELETE FROM agent WHERE id = $1", private) })
	colleague := dbfx.User(t, "He Yu", fmt.Sprintf("he-yu-%d@example.test", stamp))
	dbfx.Member(t, testWorkspaceID, colleague, "member")
	thread := "01a20000-0000-7000-8000-000000000001"
	docContext := testutil.Raw(fmt.Sprintf(`'{"type":"doc_comment","workspace_id":"%s"}'::jsonb`, testWorkspaceID))
	failedRun := dbfx.Task(t, private, testutil.Cols{"doc_id": doc.ID, "doc_thread_id": thread, "doc_comment_id": thread,
		"status": "failed", "error": "secret path /home/owner/work", "failure_reason": "timeout",
		"runtime_id": handlerTestRuntimeID(t), "completed_at": testutil.Raw("now()"), "context": docContext})
	dbfx.Task(t, private, testutil.Cols{"doc_id": doc.ID, "doc_thread_id": thread, "doc_comment_id": thread, "status": "running",
		"runtime_id": handlerTestRuntimeID(t), "started_at": testutil.Raw("now()"), "context": docContext})

	list := func(as string) []DocTaskRunResponse {
		t.Helper()
		var out struct {
			Tasks []DocTaskRunResponse `json:"tasks"`
		}
		req := withURLParam(newRequest("GET", "/task-runs", nil), "id", doc.ID)
		req.Header.Set("X-User-ID", as)
		testutil.Call(t, testHandler.ListDocTaskRuns, req).Want(http.StatusOK).JSON(&out)
		return out.Tasks
	}
	for _, run := range list(colleague) {
		if run.Error != nil || run.CanCancel {
			t.Fatalf("a member who cannot see the agent got its details: %+v", run)
		}
		if run.Status == "" || run.DocThreadID != thread {
			t.Fatalf("a member still sees the run's state: %+v", run)
		}
	}
	var sawError, canStop bool
	for _, run := range list(testUserID) {
		sawError = sawError || run.Error != nil
		canStop = canStop || (run.Status == "running" && run.CanCancel)
	}
	if !sawError || !canStop {
		t.Fatal("the agent's owner sees the error and can stop the running run")
	}

	// The run's transcript follows the agent's visibility too.
	transcript := func(as string) *testutil.Response {
		req := withURLParam(newRequestAs(as, http.MethodGet, "/api/tasks/"+failedRun+"/messages", nil), "taskId", failedRun)
		req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
		return testutil.Call(t, testHandler.ListTaskMessagesByUser, req)
	}
	transcript(colleague).Want(http.StatusNotFound)
	transcript(testUserID).Want(http.StatusOK)

	// A long-running run stays listed however many runs finished after it.
	dbfx.Exec(t, `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, doc_id, doc_thread_id, doc_comment_id, created_at, completed_at)
		SELECT $1, $2, 'completed', 0, $3, $4, $4, now() + make_interval(secs => g), now() + make_interval(secs => g)
		FROM generate_series(1, 201) AS g`, private, handlerTestRuntimeID(t), doc.ID, thread)
	var running int
	for _, run := range list(testUserID) {
		if run.Status == "running" {
			running++
		}
	}
	if running != 1 {
		t.Fatalf("the running run must stay listed behind 201 newer finished runs, have %d running", running)
	}
}
