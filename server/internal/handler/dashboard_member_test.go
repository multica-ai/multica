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
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// memberUsageModelPrefix tags every usage row these tests seed, so sums can be
// isolated from rows other tests left in the shared workspace.
const memberUsageModelPrefix = "member-usage-test-"

type memberUsageFixture struct {
	ownerViewer    string // workspace owner (testUserID)
	member         string // plain member: only ever sees their own row
	privateOwner   string // plain member who owns the private agent
	privateAgentID string
	publicAgentID  string
	projectID      string
	squadID        string
	chatTitle      string
	issueTitle     string
	tasks          map[string]string // label -> task id
}

// seedMemberUsageFixture seeds five runs, all with usage "now":
//
//	privateOwner  issue run on the public agent, two models            100 + 200
//	member        issue run on privateOwner's private agent (delegated) 300
//	member        chat run in member's own chat session                  50
//	(nobody)      unattributed issue run on the public agent            400
//	privateOwner  issue run in a project, public agent, via a squad      25
func seedMemberUsageFixture(t *testing.T) memberUsageFixture {
	t.Helper()
	ctx := context.Background()

	privateAgentID, privateOwner, member := privateAgentTestFixture(t)
	runtimeID := handlerTestRuntimeID(t)

	f := memberUsageFixture{
		ownerViewer:    testUserID,
		member:         member,
		privateOwner:   privateOwner,
		privateAgentID: privateAgentID,
		chatTitle:      "member-usage secret chat title",
		issueTitle:     "member-usage issue",
		tasks:          map[string]string{},
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'member-usage-public-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 'public_to', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&f.publicAgentID); err != nil {
		t.Fatalf("create public agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, f.publicAgentID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
	`, f.publicAgentID, testWorkspaceID); err != nil {
		t.Fatalf("grant workspace invocation target: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_invocation_target WHERE agent_id = $1`, f.publicAgentID)
	})

	f.projectID = dbfx.Project(t, "member-usage project")
	f.squadID = dbfx.Squad(t, "member-usage squad", f.publicAgentID)
	plainIssue := dbfx.Issue(t, f.issueTitle)
	projectIssue := dbfx.Issue(t, "member-usage project issue", testutil.Cols{"project_id": f.projectID})

	chatID := dbfx.Insert(t, "chat_session", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"agent_id":     f.publicAgentID,
		"creator_id":   member,
		"title":        f.chatTitle,
		"status":       "active",
	})

	seed := func(label, agentID string, extra testutil.Cols, usage map[string]int64) {
		cols := testutil.Cols{
			"runtime_id":   runtimeID,
			"status":       "completed",
			"started_at":   testutil.Raw("now() - interval '20 minutes'"),
			"completed_at": testutil.Raw("now() - interval '10 minutes'"),
			"created_at":   testutil.Raw("now()"),
		}
		for k, v := range extra {
			cols[k] = v
		}
		taskID := dbfx.Task(t, agentID, cols)
		for model, tokens := range usage {
			dbfx.InsertNoID(t, "task_usage", testutil.Cols{
				"task_id":       taskID,
				"provider":      "claude",
				"model":         memberUsageModelPrefix + model,
				"input_tokens":  tokens,
				"output_tokens": 0,
				"created_at":    testutil.Raw("now()"),
			}, "task_id = $1", taskID)
		}
		f.tasks[label] = taskID
	}
	seed("owner-two-models", f.publicAgentID,
		testutil.Cols{"accountable_user_id": privateOwner, "issue_id": plainIssue},
		map[string]int64{"a": 100, "b": 200})
	seed("member-private-agent", privateAgentID,
		testutil.Cols{"accountable_user_id": member, "issue_id": plainIssue},
		map[string]int64{"a": 300})
	seed("member-chat", f.publicAgentID,
		testutil.Cols{"accountable_user_id": member, "chat_session_id": chatID},
		map[string]int64{"a": 50})
	seed("unattributed", f.publicAgentID,
		testutil.Cols{"issue_id": plainIssue},
		map[string]int64{"a": 400})
	seed("owner-project", f.publicAgentID,
		testutil.Cols{"accountable_user_id": privateOwner, "issue_id": projectIssue, "squad_id": f.squadID},
		map[string]int64{"a": 25})

	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM task_usage_hourly WHERE model LIKE $1`, memberUsageModelPrefix+"%")
	})
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup window: %v", err)
	}
	return f
}

type byMemberRow struct {
	UserID    string `json:"user_id"`
	TaskCount int32  `json:"task_count"`
	Usage     []struct {
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	} `json:"usage"`
}

type memberTaskResp struct {
	TaskID          string  `json:"task_id"`
	AgentID         string  `json:"agent_id"`
	Kind            string  `json:"kind"`
	IssueID         *string `json:"issue_id"`
	IssueIdentifier *string `json:"issue_identifier"`
	IssueTitle      *string `json:"issue_title"`
	ChatSessionID   *string `json:"chat_session_id"`
	ChatTitle       *string `json:"chat_title"`
	Usage           []struct {
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	} `json:"usage"`
}

func dashboardGet(t *testing.T, handler http.HandlerFunc, userID, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handler(w, newRequestAs(userID, http.MethodGet, path, nil))
	return w
}

func decodeOK[T any](t *testing.T, label string, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if w.Code != http.StatusOK {
		t.Fatalf("%s: expected 200, got %d: %s", label, w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: decode: %v (%s)", label, err, w.Body.String())
	}
	return out
}

// seededTokensByMember sums only this test's rows, per user id.
func seededTokensByMember(rows []byMemberRow) map[string]int64 {
	out := map[string]int64{}
	for _, row := range rows {
		for _, u := range row.Usage {
			if strings.HasPrefix(u.Model, memberUsageModelPrefix) {
				out[row.UserID] += u.InputTokens
			}
		}
	}
	return out
}

func taskCountOf(rows []byMemberRow, userID string) int32 {
	for _, row := range rows {
		if row.UserID == userID {
			return row.TaskCount
		}
	}
	return 0
}

func TestUsageHourCutoffRoundsUpToTheHour(t *testing.T) {
	at := func(s string) pgtype.Timestamptz {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return pgtype.Timestamptz{Time: v, Valid: true}
	}
	for _, tc := range []struct{ in, want string }{
		{"2026-03-01T00:00:00Z", "2026-03-01T00:00:00Z"},           // whole hour: unchanged
		{"2026-02-28T18:30:00Z", "2026-02-28T19:00:00Z"},           // Asia/Kolkata midnight
		{"2026-02-28T18:15:00Z", "2026-02-28T19:00:00Z"},           // Asia/Kathmandu midnight
		{"2026-03-01T00:00:00.000001Z", "2026-03-01T01:00:00Z"},    // just past
		{"2026-03-01T05:30:00+05:30", "2026-03-01T00:00:00Z"},      // offset form of a whole UTC hour
		{"2026-03-01T09:45:00+09:30", "2026-03-01T01:00:00+00:00"}, // Australia/Darwin
	} {
		got := usageHourCutoff(at(tc.in))
		if want := at(tc.want); !got.Time.Equal(want.Time) || !got.Valid {
			t.Errorf("usageHourCutoff(%s) = %s, want %s", tc.in, got.Time.UTC(), want.Time.UTC())
		}
	}
}

func TestDashboardUsageByMemberAggregatesAndScopes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedMemberUsageFixture(t)
	const path = "/api/dashboard/usage/by-member?days=7"

	// ---- owner/admin: everyone, plus the unattributed bucket ----------------
	admin := decodeOK[[]byMemberRow](t, "by-member (owner)",
		dashboardGet(t, testHandler.GetDashboardUsageByMember, f.ownerViewer, path))
	tokens := seededTokensByMember(admin)
	if got := tokens[f.privateOwner]; got != 325 {
		t.Errorf("owner view: privateOwner tokens = %d, want 325 (100+200+25)", got)
	}
	if got := tokens[f.member]; got != 350 {
		t.Errorf("owner view: member tokens = %d, want 350 (300+50)", got)
	}
	if got := tokens[unattributedMemberID]; got != 400 {
		t.Errorf("owner view: unattributed tokens = %d, want 400", got)
	}
	// A run that used two models is one run, not two.
	if got := taskCountOf(admin, f.privateOwner); got != 2 {
		t.Errorf("owner view: privateOwner task_count = %d, want 2 (two-model run counted once)", got)
	}
	if got := taskCountOf(admin, f.member); got != 2 {
		t.Errorf("owner view: member task_count = %d, want 2", got)
	}
	if got := taskCountOf(admin, unattributedMemberID); got < 1 {
		t.Errorf("owner view: unattributed task_count = %d, want >= 1", got)
	}
	// by-member has no agent dimension: no agent id may appear at all.
	body := dashboardGet(t, testHandler.GetDashboardUsageByMember, f.ownerViewer, path).Body.String()
	if strings.Contains(body, f.privateAgentID) || strings.Contains(body, f.publicAgentID) {
		t.Errorf("by-member leaked an agent id: %s", body)
	}

	// ---- per-member rows add up to the workspace totals ----------------------
	type usageRow struct {
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	}
	sumSeeded := func(rows []usageRow) int64 {
		var n int64
		for _, row := range rows {
			if strings.HasPrefix(row.Model, memberUsageModelPrefix) {
				n += row.InputTokens
			}
		}
		return n
	}
	var memberSeeded int64
	for _, v := range tokens {
		memberSeeded += v
	}
	byAgent := sumSeeded(decodeOK[[]usageRow](t, "usage/by-agent", dashboardGet(t,
		testHandler.GetDashboardUsageByAgent, f.ownerViewer, "/api/dashboard/usage/by-agent?days=7")))
	daily := sumSeeded(decodeOK[[]usageRow](t, "usage/daily", dashboardGet(t,
		testHandler.GetDashboardUsageDaily, f.ownerViewer, "/api/dashboard/usage/daily?days=7")))
	if memberSeeded != 1075 || byAgent != memberSeeded || daily != memberSeeded {
		t.Errorf("by-member total %d, usage/by-agent %d, usage/daily %d: want all 1075", memberSeeded, byAgent, daily)
	}

	// ---- project and squad filters --------------------------------------------
	for _, filter := range []string{"&project_id=" + f.projectID, "&squad_id=" + f.squadID} {
		got := seededTokensByMember(decodeOK[[]byMemberRow](t, "by-member"+filter,
			dashboardGet(t, testHandler.GetDashboardUsageByMember, f.ownerViewer, path+filter)))
		if len(got) != 1 || got[f.privateOwner] != 25 {
			t.Errorf("%s: got %v, want only privateOwner=25", filter, got)
		}
	}
	standalone := seededTokensByMember(decodeOK[[]byMemberRow](t, "by-member standalone",
		dashboardGet(t, testHandler.GetDashboardUsageByMember, f.ownerViewer, path+"&squad_id="+standaloneSquadFilterSentinel)))
	if standalone[f.privateOwner] != 300 || standalone[f.member] != 350 || standalone[unattributedMemberID] != 400 {
		t.Errorf("standalone filter: got %v, want privateOwner=300 member=350 unattributed=400", standalone)
	}
	for _, bad := range []string{"&project_id=nope", "&squad_id=nope"} {
		if w := dashboardGet(t, testHandler.GetDashboardUsageByMember, f.ownerViewer, path+bad); w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", bad, w.Code)
		}
	}

	// ---- plain member: exactly their own row, no unattributed ---------------
	w := dashboardGet(t, testHandler.GetDashboardUsageByMember, f.member, path)
	mine := decodeOK[[]byMemberRow](t, "by-member (member)", w)
	if len(mine) != 1 || mine[0].UserID != f.member {
		t.Errorf("member view: want exactly the caller's row, got %+v", mine)
	}
	if got := seededTokensByMember(mine)[f.member]; got != 350 {
		t.Errorf("member view: own tokens = %d, want 350", got)
	}
	if got := taskCountOf(mine, f.member); got != 2 {
		t.Errorf("member view: own task_count = %d, want 2", got)
	}
	if b := w.Body.String(); strings.Contains(b, unattributedMemberID) ||
		strings.Contains(b, f.privateOwner) || strings.Contains(b, testUserID) {
		t.Errorf("member view leaked another member or the unattributed bucket: %s", b)
	}
}

func TestDashboardMemberTasksPermissions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedMemberUsageFixture(t)
	tasksPath := func(userID string) string {
		return "/api/dashboard/usage/member-tasks?days=7&user_id=" + userID
	}
	byID := func(rows []memberTaskResp) map[string]memberTaskResp {
		out := map[string]memberTaskResp{}
		for _, r := range rows {
			out[r.TaskID] = r
		}
		return out
	}

	// ---- plain member reading their own runs ---------------------------------
	w := dashboardGet(t, testHandler.GetDashboardMemberTasks, f.member, tasksPath(f.member))
	own := byID(decodeOK[[]memberTaskResp](t, "member-tasks (self)", w))
	if len(own) != 2 {
		t.Fatalf("member-tasks (self): got %d tasks, want 2: %s", len(own), w.Body.String())
	}
	delegated := own[f.tasks["member-private-agent"]]
	if delegated.AgentID != restrictedAgentsRowID {
		t.Errorf("run on someone else's private agent: agent_id = %q, want restricted bucket", delegated.AgentID)
	}
	if strings.Contains(w.Body.String(), f.privateAgentID) {
		t.Errorf("member-tasks leaked private agent id: %s", w.Body.String())
	}
	if len(delegated.Usage) != 1 || delegated.Usage[0].InputTokens != 300 {
		t.Errorf("folded run must keep its usage: %+v", delegated.Usage)
	}
	if delegated.Kind != "issue" || delegated.IssueTitle == nil || *delegated.IssueTitle != f.issueTitle ||
		delegated.IssueIdentifier == nil || !strings.HasPrefix(*delegated.IssueIdentifier, "HAN-") {
		t.Errorf("issue run: want kind issue with title and HAN-N identifier, got %+v", delegated)
	}
	chat := own[f.tasks["member-chat"]]
	if chat.Kind != "chat" || chat.ChatTitle == nil || *chat.ChatTitle != f.chatTitle || chat.ChatSessionID == nil {
		t.Errorf("chat creator should see their own chat title/link: %+v", chat)
	}

	// ---- plain member asking for someone else, or for the unattributed bucket -
	for _, target := range []string{f.privateOwner, testUserID, unattributedMemberID} {
		w := dashboardGet(t, testHandler.GetDashboardMemberTasks, f.member, tasksPath(target))
		if w.Code != http.StatusForbidden {
			t.Errorf("member-tasks as plain member for %s: got %d, want 403: %s", target, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), f.tasks["owner-two-models"]) ||
			strings.Contains(w.Body.String(), f.tasks["unattributed"]) {
			t.Errorf("403 body leaked task data: %s", w.Body.String())
		}
	}

	// ---- owner/admin reading a member's runs ---------------------------------
	w = dashboardGet(t, testHandler.GetDashboardMemberTasks, f.ownerViewer, tasksPath(f.member))
	asAdmin := byID(decodeOK[[]memberTaskResp](t, "member-tasks (owner -> member)", w))
	if len(asAdmin) != 2 {
		t.Fatalf("owner view of member: got %d tasks, want 2", len(asAdmin))
	}
	adminChat := asAdmin[f.tasks["member-chat"]]
	if adminChat.Kind != "chat" || adminChat.ChatTitle != nil || adminChat.ChatSessionID != nil {
		t.Errorf("admin must not see another member's chat title/session: %+v", adminChat)
	}
	if strings.Contains(w.Body.String(), f.chatTitle) {
		t.Errorf("admin response leaked chat title: %s", w.Body.String())
	}
	// Owner/admin are not restricted by per-agent visibility (same as by-agent).
	if got := asAdmin[f.tasks["member-private-agent"]].AgentID; got != f.privateAgentID {
		t.Errorf("owner view: private agent id = %q, want %q", got, f.privateAgentID)
	}

	// Two-model run: one task, both models.
	w = dashboardGet(t, testHandler.GetDashboardMemberTasks, f.ownerViewer, tasksPath(f.privateOwner))
	ownerTasks := byID(decodeOK[[]memberTaskResp](t, "member-tasks (owner -> privateOwner)", w))
	two := ownerTasks[f.tasks["owner-two-models"]]
	if len(ownerTasks) != 2 || len(two.Usage) != 2 || two.Kind != "issue" || two.IssueID == nil {
		t.Errorf("two-model run: got %d tasks, run=%+v", len(ownerTasks), two)
	}
	// The squad filter applies to member-tasks too.
	w = dashboardGet(t, testHandler.GetDashboardMemberTasks, f.ownerViewer, tasksPath(f.privateOwner)+"&squad_id="+f.squadID)
	squadTasks := byID(decodeOK[[]memberTaskResp](t, "member-tasks (squad)", w))
	if _, ok := squadTasks[f.tasks["owner-project"]]; len(squadTasks) != 1 || !ok {
		t.Errorf("squad filter: got %d tasks, want only the squad run: %s", len(squadTasks), w.Body.String())
	}

	// Unattributed bucket is readable by owner/admin.
	w = dashboardGet(t, testHandler.GetDashboardMemberTasks, f.ownerViewer, tasksPath(unattributedMemberID))
	if _, ok := byID(decodeOK[[]memberTaskResp](t, "member-tasks (unattributed)", w))[f.tasks["unattributed"]]; !ok {
		t.Errorf("unattributed run missing from admin's unattributed list: %s", w.Body.String())
	}

	// ---- bad input -------------------------------------------------------------
	for _, p := range []string{
		"/api/dashboard/usage/member-tasks?days=7",
		tasksPath("not-a-uuid"),
		tasksPath(f.member) + "&squad_id=nope",
	} {
		if w := dashboardGet(t, testHandler.GetDashboardMemberTasks, f.ownerViewer, p); w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", p, w.Code)
		}
	}
}

// A user id from another workspace, even one with runs of its own there, must
// come back empty, not as an error that confirms anything about it.
func TestDashboardMemberTasksForeignUserIsEmpty(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	const email = "member-usage-foreign@multica.test"
	const slug = "member-usage-foreign"
	foreignUser := dbfx.User(t, "Foreign Usage", email)
	foreignWS := dbfx.Workspace(t, "Member Usage Foreign", slug, testutil.Cols{"issue_prefix": "MUF"})
	dbfx.Member(t, foreignWS, foreignUser, "owner")
	foreignRuntime := dbfx.Runtime(t, "Member Usage Foreign Runtime", testutil.Cols{"workspace_id": foreignWS, "owner_id": foreignUser})
	foreignAgent := dbfx.Agent(t, "member-usage-foreign-agent", foreignRuntime, testutil.Cols{"workspace_id": foreignWS, "owner_id": foreignUser})
	foreignTask := dbfx.Task(t, foreignAgent, testutil.Cols{
		"runtime_id": foreignRuntime, "status": "completed", "accountable_user_id": foreignUser,
	})
	dbfx.InsertNoID(t, "task_usage", testutil.Cols{
		"task_id": foreignTask, "provider": "claude", "model": memberUsageModelPrefix + "foreign",
		"input_tokens": 999, "output_tokens": 0,
	}, "task_id = $1", foreignTask)

	w := dashboardGet(t, testHandler.GetDashboardMemberTasks, testUserID,
		"/api/dashboard/usage/member-tasks?days=7&user_id="+foreignUser)
	rows := decodeOK[[]memberTaskResp](t, "member-tasks (foreign user)", w)
	if len(rows) != 0 || strings.Contains(w.Body.String(), foreignTask) {
		t.Errorf("foreign user's runs leaked into this workspace: %s", w.Body.String())
	}
	// And the foreign run does not show up in this workspace's by-member either.
	w = dashboardGet(t, testHandler.GetDashboardUsageByMember, testUserID, "/api/dashboard/usage/by-member?days=7")
	if strings.Contains(w.Body.String(), foreignUser) {
		t.Errorf("by-member included a user from another workspace: %s", w.Body.String())
	}
}

// If per-agent visibility cannot be resolved, member-tasks must fail closed
// (500) rather than return unfolded agent ids.
func TestDashboardMemberTasksFailsClosedWhenAgentAccessUnresolved(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedMemberUsageFixture(t)
	ctx := context.Background()

	// Hold an exclusive lock on the table restrictedAgentIDs reads for plain
	// members; the handler's pinned connection gives up after lock_timeout.
	lockTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	defer lockTx.Rollback(context.Background())
	if _, err := lockTx.Exec(ctx, `LOCK TABLE agent_invocation_target IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock agent_invocation_target: %v", err)
	}
	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET lock_timeout = '200ms'`); err != nil {
		t.Fatalf("set lock_timeout: %v", err)
	}
	defer conn.Exec(context.Background(), `RESET lock_timeout`)
	h := *testHandler
	h.Queries = db.New(conn)

	req := newRequestAs(f.member, http.MethodGet, "/api/dashboard/usage/member-tasks?days=7&user_id="+f.member, nil)
	reqCtx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
	defer cancel()
	w := httptest.NewRecorder()
	h.GetDashboardMemberTasks(w, req.WithContext(reqCtx))
	lockTx.Rollback(context.Background())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when agent access cannot be resolved, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), f.privateAgentID) || strings.Contains(w.Body.String(), f.tasks["member-private-agent"]) {
		t.Errorf("fail-closed response leaked data: %s", w.Body.String())
	}
}

// Both member endpoints stop at the plan's history limit like the other
// dashboard endpoints (see TestDashboardEndpointsStopAtTheHistoryLimit).
func TestDashboardMemberUsageStopsAtTheHistoryLimit(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	now := pinDayWindowClock(t, time.Now().UTC())
	const limit, requested = 30, 35
	const model = memberUsageModelPrefix + "history"
	inside := startOfDayBefore(now, time.UTC, limit-1).Add(12 * time.Hour)
	outside := startOfDayBefore(now, time.UTC, limit).Add(12 * time.Hour)

	runtimeID := dbfx.Runtime(t, "member usage history runtime")
	agentID := dbfx.Agent(t, "member usage history agent", runtimeID)
	for _, at := range []time.Time{inside, outside} {
		taskID := dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "status": "completed", "accountable_user_id": testUserID,
			"created_at": at.Add(-10 * time.Minute),
		})
		dbfx.InsertNoID(t, "task_usage", testutil.Cols{
			"task_id": taskID, "provider": "claude", "model": model,
			"input_tokens": 100, "output_tokens": 0, "created_at": at,
		}, "task_id = $1", taskID)
	}

	runs := func(t *testing.T) (byMember int, memberTasks int, headers [2]string) {
		t.Helper()
		query := fmt.Sprintf("?days=%d&tz=UTC", requested)
		w := dashboardGet(t, testHandler.GetDashboardUsageByMember, testUserID, "/api/dashboard/usage/by-member"+query)
		for _, row := range decodeOK[[]byMemberRow](t, "by-member", w) {
			for _, u := range row.Usage {
				if u.Model == model {
					byMember += int(u.InputTokens / 100)
				}
			}
		}
		headers[0] = w.Header().Get(HeaderDashboardHistoryLimit)
		w = dashboardGet(t, testHandler.GetDashboardMemberTasks, testUserID,
			"/api/dashboard/usage/member-tasks"+query+"&user_id="+testUserID)
		for _, task := range decodeOK[[]memberTaskResp](t, "member-tasks", w) {
			for _, u := range task.Usage {
				if u.Model == model {
					memberTasks++
				}
			}
		}
		headers[1] = w.Header().Get(HeaderDashboardHistoryLimit)
		return byMember, memberTasks, headers
	}

	t.Run("enforce", func(t *testing.T) {
		useDashboardHistoryLimit(t, historyDecision(entitlement.ActionEnforce, limit))
		byMember, memberTasks, headers := runs(t)
		if byMember != 1 || memberTasks != 1 {
			t.Errorf("under a %d-day limit: by-member %d runs, member-tasks %d runs, want 1 each", limit, byMember, memberTasks)
		}
		if headers != [2]string{"30", "30"} {
			t.Errorf("limit headers = %v, want 30 on both", headers)
		}
	})
	t.Run("self-host", func(t *testing.T) {
		prior := testHandler.Entitlements
		testHandler.Entitlements = nil
		t.Cleanup(func() { testHandler.Entitlements = prior })
		byMember, memberTasks, headers := runs(t)
		if byMember != 2 || memberTasks != 2 {
			t.Errorf("without a limit: by-member %d runs, member-tasks %d runs, want 2 each", byMember, memberTasks)
		}
		if headers != [2]string{"", ""} {
			t.Errorf("limit headers = %v, want none", headers)
		}
	})
}

// The member endpoints follow the private-project rule of the other rollups
// (MUL-8472): a project_id the caller cannot see answers like a project that
// does not exist, the unfiltered totals keep the run, and member-tasks never
// names an issue in such a project.
func TestDashboardMemberUsagePrivateProject(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPrivateProjectFixture(t)
	agentID := createHandlerTestAgent(t, f.marker+"-mdash", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)
	model := memberUsageModelPrefix + f.marker
	secretTitle := f.marker + " secret issue"
	// One run per member on the secret issue, so each sees their own run.
	for _, userID := range []string{f.insider, f.outsider} {
		taskID := dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "issue_id": f.secretIssue, "status": "completed",
			"accountable_user_id": userID,
		})
		dbfx.InsertNoID(t, "task_usage", testutil.Cols{
			"task_id": taskID, "provider": "claude", "model": model, "input_tokens": 10, "output_tokens": 0,
		}, "task_id = $1", taskID)
	}

	secret := "&project_id=" + f.projectID
	missing := "&project_id=00000000-0000-0000-0000-0000000000ff"

	t.Run("/api/dashboard/usage/by-member", func(t *testing.T) {
		get := func(userID, query string) (rows []byMemberRow, body string) {
			w := dashboardGet(t, testHandler.GetDashboardUsageByMember, userID, "/api/dashboard/usage/by-member?days=7"+query)
			return decodeOK[[]byMemberRow](t, "by-member"+query, w), w.Body.String()
		}
		hasModel := func(rows []byMemberRow) bool {
			for _, row := range rows {
				for _, u := range row.Usage {
					if u.Model == model {
						return true
					}
				}
			}
			return false
		}
		if rows, _ := get(f.insider, secret); !hasModel(rows) {
			t.Fatal("project member: the private project's usage is missing")
		}
		_, hiddenBody := get(f.outsider, secret)
		_, absentBody := get(f.outsider, missing)
		if hiddenBody != absentBody {
			t.Fatalf("outsider: hidden project_id = %s, want the missing-project answer %s", hiddenBody, absentBody)
		}
		// Without the filter the workspace totals still include the run.
		if rows, _ := get(f.outsider, ""); !hasModel(rows) {
			t.Fatal("outsider: the unfiltered rollup lost the private project's usage")
		}
	})

	t.Run("/api/dashboard/usage/member-tasks", func(t *testing.T) {
		ownRun := func(userID, query string) (memberTaskResp, string, bool) {
			w := dashboardGet(t, testHandler.GetDashboardMemberTasks, userID,
				"/api/dashboard/usage/member-tasks?days=7&user_id="+userID+query)
			rows := decodeOK[[]memberTaskResp](t, "member-tasks"+query, w)
			for _, row := range rows {
				for _, u := range row.Usage {
					if u.Model == model {
						return row, w.Body.String(), true
					}
				}
			}
			return memberTaskResp{}, w.Body.String(), false
		}

		if row, _, ok := ownRun(f.insider, ""); !ok || row.Kind != "issue" || row.IssueID == nil || *row.IssueID != f.secretIssue {
			t.Fatalf("project member: own run = %+v (found %v), want the secret issue", row, ok)
		}
		row, body, ok := ownRun(f.outsider, "")
		if !ok {
			t.Fatal("outsider: own run on the private project is missing; its usage must stay")
		}
		if row.IssueID != nil || row.IssueIdentifier != nil || row.Kind == "issue" ||
			strings.Contains(body, secretTitle) || strings.Contains(body, f.secretIssue) {
			t.Fatalf("outsider: run names the private project's issue: %s", body)
		}
		w := dashboardGet(t, testHandler.GetDashboardMemberTasks, f.outsider,
			"/api/dashboard/usage/member-tasks?days=7&user_id="+f.outsider+secret)
		if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
			t.Fatalf("outsider: hidden project_id = %d %s, want 200 []", w.Code, w.Body.String())
		}
	})
}
