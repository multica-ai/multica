package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MUL-8154: /api/daemon/* accepts any workspace member's PAT/JWT. These tests
// pin that workspace membership alone no longer lets a member act as another
// member's daemon, that registration cannot take a runtime over, that legacy
// merges need ownership of both rows, that daemon tokens are bound to their
// own daemon, and that tasks without a runtime are not writable.

type ownershipFixture struct {
	victimRuntime string
	victimDaemon  string
	agentID       string
	issueID       string
	queuedTask    string
	runningTask   string
	attackerID    string
}

func newOwnershipFixture(t *testing.T, label string) ownershipFixture {
	t.Helper()
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	f := ownershipFixture{victimDaemon: "victim-" + label + "-" + suffix}
	// Victim = testUserID, who owns every handler fixture runtime.
	f.victimRuntime = dbfx.Runtime(t, label+" victim runtime", testutil.Cols{
		"daemon_id": f.victimDaemon,
		"provider":  "claude",
	})
	f.agentID, f.issueID = createClaimReclaimAgentAndIssue(t, ctx, f.victimRuntime, label+" victim agent")
	f.queuedTask = seedQueuedIssueTask(t, ctx, f.agentID, f.victimRuntime, f.issueID)
	// A second agent holds the running task so the first agent's
	// concurrency slot stays free for the owner's positive-control claim.
	runningAgent := dbfx.Agent(t, label+" victim running agent", f.victimRuntime)
	f.runningTask = dbfx.Task(t, runningAgent, testutil.Cols{
		"runtime_id": f.victimRuntime,
		"issue_id":   f.issueID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})
	f.attackerID = dbfx.User(t, label+" attacker", label+"-attacker-"+suffix+"@example.com")
	dbfx.Member(t, testWorkspaceID, f.attackerID, "member")
	return f
}

func taskStatusOf(t *testing.T, taskID string) string {
	t.Helper()
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	return status
}

func runtimeOwnerOf(t *testing.T, runtimeID string) pgtype.UUID {
	t.Helper()
	var owner pgtype.UUID
	dbfx.QueryRow(t, `SELECT owner_id FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&owner)
	return owner
}

func TestDaemonRuntimeOwnership_MemberCannotActOnAnotherMembersRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newOwnershipFixture(t, "crossowner")
	as := func(method, path string, body any, params ...string) *http.Request {
		req := newRequestAs(f.attackerID, method, path, body)
		if len(params) > 0 {
			req = withURLParams(req, params...)
		}
		return req
	}

	t.Run("single claim", func(t *testing.T) {
		testutil.Call(t, testHandler.ClaimTaskByRuntime,
			as("POST", "/api/daemon/runtimes/"+f.victimRuntime+"/tasks/claim", nil, "runtimeId", f.victimRuntime)).
			Want(http.StatusNotFound)
	})
	t.Run("batch claim with the victim's daemon_id", func(t *testing.T) {
		w := testutil.Call(t, testHandler.ClaimTasksByRuntime, as("POST", "/api/daemon/tasks/claim", map[string]any{
			"daemon_id": f.victimDaemon, "runtime_ids": []string{f.victimRuntime}, "max_tasks": 5,
		})).Want(http.StatusOK)
		if strings.Contains(w.Body.String(), f.queuedTask) {
			t.Fatalf("attacker claimed the victim's task: %s", w.Body.String())
		}
	})
	if got := taskStatusOf(t, f.queuedTask); got != "queued" {
		t.Fatalf("victim's queued task status = %q after refused claims, want queued", got)
	}

	t.Run("heartbeat", func(t *testing.T) {
		testutil.Call(t, testHandler.DaemonHeartbeat,
			as("POST", "/api/daemon/heartbeat", map[string]any{"runtime_id": f.victimRuntime})).
			Want(http.StatusNotFound)
	})
	t.Run("deregister is a no-op", func(t *testing.T) {
		testutil.Call(t, testHandler.DaemonDeregister,
			as("POST", "/api/daemon/deregister", map[string]any{"runtime_ids": []string{f.victimRuntime}})).
			Want(http.StatusOK)
		var status string
		dbfx.QueryRow(t, `SELECT status FROM agent_runtime WHERE id = $1`, f.victimRuntime).Scan(&status)
		if status != "online" {
			t.Fatalf("victim runtime status = %q, want online", status)
		}
	})
	t.Run("websocket handshake", func(t *testing.T) {
		w := httptest.NewRecorder()
		if _, ok := testHandler.buildDaemonWebSocketIdentity(w, as("GET", "/api/daemon/ws", nil), []string{f.victimRuntime}, f.attackerID); ok {
			t.Fatal("attacker's websocket was authorized for the victim's runtime")
		}
		if w.Code != http.StatusNotFound {
			t.Fatalf("ws status = %d, want 404", w.Code)
		}
	})

	taskPath := "/api/daemon/tasks/" + f.runningTask
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		method  string
		suffix  string
		body    any
	}{
		{"complete", testHandler.CompleteTask, "POST", "/complete", map[string]any{"output": "hijacked"}},
		{"fail", testHandler.FailTask, "POST", "/fail", map[string]any{"error": "hijacked"}},
		{"report messages", testHandler.ReportTaskMessages, "POST", "/messages", map[string]any{
			"messages": []any{map[string]any{"seq": 1, "type": "text", "content": "forged"}},
		}},
		{"list messages", testHandler.ListTaskMessages, "GET", "/messages", nil},
		{"status", testHandler.GetTaskStatus, "GET", "/status", nil},
		{"gc check", testHandler.GetTaskGCCheck, "GET", "/gc-check", nil},
	} {
		t.Run("task "+tc.name, func(t *testing.T) {
			testutil.Call(t, tc.handler, as(tc.method, taskPath+tc.suffix, tc.body, "taskId", f.runningTask)).
				Want(http.StatusNotFound)
		})
	}
	if got := taskStatusOf(t, f.runningTask); got != "running" {
		t.Fatalf("victim's running task status = %q after refused writes, want running", got)
	}
	var messages int
	dbfx.QueryRow(t, `SELECT count(*) FROM task_message WHERE task_id = $1`, f.runningTask).Scan(&messages)
	if messages != 0 {
		t.Fatalf("attacker wrote %d messages into the victim's task", messages)
	}

	// The owner's own credential is unaffected — and this positive control
	// proves the refused claims above were refused by authorization, not by
	// an unclaimable fixture.
	t.Run("owner batch claim", func(t *testing.T) {
		w := testutil.Call(t, testHandler.ClaimTasksByRuntime, newOwnerDaemonRequest("POST", "/api/daemon/tasks/claim", map[string]any{
			"daemon_id": f.victimDaemon, "runtime_ids": []string{f.victimRuntime}, "max_tasks": 5,
		})).Want(http.StatusOK)
		if !strings.Contains(w.Body.String(), f.queuedTask) {
			t.Fatalf("owner could not claim their own task: %s", w.Body.String())
		}
	})
	// The owner's own credential is unaffected.
	t.Run("owner heartbeat", func(t *testing.T) {
		testutil.Call(t, testHandler.DaemonHeartbeat,
			newOwnerDaemonRequest("POST", "/api/daemon/heartbeat", map[string]any{"runtime_id": f.victimRuntime})).
			Want(http.StatusOK)
	})
	t.Run("owner task status", func(t *testing.T) {
		testutil.Call(t, testHandler.GetTaskStatus,
			withURLParams(newOwnerDaemonRequest("GET", taskPath+"/status", nil), "taskId", f.runningTask)).
			Want(http.StatusOK)
	})
}

func registerAs(userID, daemonID string, legacyIDs ...string) *http.Request {
	body := map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    daemonID,
		"device_name":  "ownership-test",
		"runtimes":     []map[string]any{{"name": "claude", "type": "claude", "version": "test", "status": "online"}},
	}
	if len(legacyIDs) > 0 {
		body["legacy_daemon_ids"] = legacyIDs
	}
	return newRequestAs(userID, "POST", "/api/daemon/register", body)
}

func TestDaemonRegister_RefusesTakeoverOfAnotherMembersRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newOwnershipFixture(t, "regtakeover")

	w := testutil.Call(t, testHandler.DaemonRegister, registerAs(f.attackerID, f.victimDaemon)).Want(http.StatusForbidden)
	if strings.Contains(w.Body.String(), f.victimRuntime) {
		t.Fatalf("refused registration leaked the runtime: %s", w.Body.String())
	}
	if owner := runtimeOwnerOf(t, f.victimRuntime); uuidToString(owner) != testUserID {
		t.Fatalf("runtime owner = %s after refused takeover, want the victim", uuidToString(owner))
	}

	// An ownerless row must not be claimable by ordinary registration either.
	ownerlessDaemon := "ownerless-" + uuid.NewString()
	ownerless := dbfx.Runtime(t, "ownerless runtime", testutil.Cols{
		"daemon_id": ownerlessDaemon, "provider": "claude", "runtime_mode": "local", "owner_id": nil,
	})
	testutil.Call(t, testHandler.DaemonRegister, registerAs(f.attackerID, ownerlessDaemon)).Want(http.StatusForbidden)
	if owner := runtimeOwnerOf(t, ownerless); owner.Valid {
		t.Fatalf("ownerless runtime was claimed by %s", uuidToString(owner))
	}

	// The owner re-registers normally.
	testutil.Call(t, testHandler.DaemonRegister, registerAs(testUserID, f.victimDaemon)).Want(http.StatusOK)
}

// TestUpsertAgentRuntime_OwnerFence pins the SQL-level fence that backs the
// handler pre-check against a concurrent registration.
func TestUpsertAgentRuntime_OwnerFence(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newOwnershipFixture(t, "sqlfence")
	ctx := context.Background()
	params := func(owner, required pgtype.UUID) db.UpsertAgentRuntimeParams {
		return db.UpsertAgentRuntimeParams{
			WorkspaceID: parseUUID(testWorkspaceID), DaemonID: strToText(f.victimDaemon),
			Name: "claude", RuntimeMode: "local", Provider: "claude", Status: "online",
			Metadata: []byte(`{}`), OwnerID: owner, RequiredOwnerID: required,
		}
	}
	attacker := parseUUID(f.attackerID)
	if _, err := testHandler.Queries.UpsertAgentRuntime(ctx, params(attacker, attacker)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("fenced upsert err = %v, want pgx.ErrNoRows", err)
	}
	if owner := runtimeOwnerOf(t, f.victimRuntime); uuidToString(owner) != testUserID {
		t.Fatalf("owner changed to %s through the fenced upsert", uuidToString(owner))
	}
	victim := parseUUID(testUserID)
	if _, err := testHandler.Queries.UpsertAgentRuntime(ctx, params(victim, victim)); err != nil {
		t.Fatalf("owner upsert: %v", err)
	}
	// Daemon-token path: no fence, no owner — the existing owner is kept.
	row, err := testHandler.Queries.UpsertAgentRuntime(ctx, params(pgtype.UUID{}, pgtype.UUID{}))
	if err != nil {
		t.Fatalf("daemon-token upsert: %v", err)
	}
	if uuidToString(row.OwnerID) != testUserID {
		t.Fatalf("daemon-token upsert owner = %s, want kept", uuidToString(row.OwnerID))
	}
}

// TestDaemonRegister_ConcurrentRegistrationsHaveOneOwner races two members
// registering the same fresh daemon_id: exactly one becomes the owner and
// every request from the other is refused.
func TestDaemonRegister_ConcurrentRegistrationsHaveOneOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newOwnershipFixture(t, "regrace")
	daemonID := "race-" + uuid.NewString()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE workspace_id = $1 AND daemon_id = $2`, testWorkspaceID, daemonID)
	})

	users := []string{testUserID, f.attackerID}
	codes := make(map[string][]int)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		user := users[i%2]
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			testHandler.DaemonRegister(w, registerAs(user, daemonID))
			mu.Lock()
			codes[user] = append(codes[user], w.Code)
			mu.Unlock()
		}()
	}
	wg.Wait()

	var owner pgtype.UUID
	dbfx.QueryRow(t, `SELECT owner_id FROM agent_runtime WHERE workspace_id = $1 AND daemon_id = $2`, testWorkspaceID, daemonID).Scan(&owner)
	winner := uuidToString(owner)
	if winner != testUserID && winner != f.attackerID {
		t.Fatalf("runtime owner = %q, want one of the two registrants", winner)
	}
	for user, got := range codes {
		want := http.StatusForbidden
		if user == winner {
			want = http.StatusOK
		}
		for _, code := range got {
			if code != want {
				t.Fatalf("user %s got %v, want all %d (winner %s)", user, got, want, winner)
			}
		}
	}
}

func TestDaemonRegister_LegacyMergeRequiresOwnershipOfBothRows(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newOwnershipFixture(t, "legacymerge")
	attackerDaemon := "attacker-" + uuid.NewString()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE workspace_id = $1 AND daemon_id = $2`, testWorkspaceID, attackerDaemon)
	})

	// The attacker's own registration succeeds, but naming the victim's
	// daemon as a legacy id must not move or delete anything.
	testutil.Call(t, testHandler.DaemonRegister, registerAs(f.attackerID, attackerDaemon, f.victimDaemon)).Want(http.StatusOK)
	if !rowExists(t, "agent_runtime", f.victimRuntime) {
		t.Fatal("the victim's runtime was deleted by the attacker's legacy merge")
	}
	var agentRuntime, taskRuntime string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, f.agentID).Scan(&agentRuntime)
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent_task_queue WHERE id = $1`, f.queuedTask).Scan(&taskRuntime)
	if agentRuntime != f.victimRuntime || taskRuntime != f.victimRuntime {
		t.Fatalf("agent/task moved to %s/%s, want both still on %s", agentRuntime, taskRuntime, f.victimRuntime)
	}

	// The locked-row check refuses directly too.
	var attackerRuntime string
	dbfx.QueryRow(t, `SELECT id FROM agent_runtime WHERE workspace_id = $1 AND daemon_id = $2`, testWorkspaceID, attackerDaemon).Scan(&attackerRuntime)
	err := testHandler.mergeLegacyRuntime(context.Background(), parseUUID(attackerRuntime), parseUUID(f.victimRuntime),
		f.victimDaemon, "claude", parseUUID(f.attackerID))
	if !errors.Is(err, errRuntimeMergeUnauthorized) {
		t.Fatalf("mergeLegacyRuntime = %v, want errRuntimeMergeUnauthorized", err)
	}

	// The owner migrating their own machine still merges.
	ownerDaemon := "owner-new-" + uuid.NewString()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE workspace_id = $1 AND daemon_id = $2`, testWorkspaceID, ownerDaemon)
	})
	testutil.Call(t, testHandler.DaemonRegister, registerAs(testUserID, ownerDaemon, f.victimDaemon)).Want(http.StatusOK)
	if rowExists(t, "agent_runtime", f.victimRuntime) {
		t.Fatal("the owner's legacy runtime survived the owner's own merge")
	}
}

func TestDaemonToken_BoundToItsOwnDaemon(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newOwnershipFixture(t, "mdtbind")
	peer := func(method, path string, body any, params ...string) *http.Request {
		req := newDaemonTokenRequest(method, path, body, testWorkspaceID, "peer-daemon-"+f.victimDaemon)
		if len(params) > 0 {
			req = withURLParams(req, params...)
		}
		return req
	}

	testutil.Call(t, testHandler.ClaimTaskByRuntime,
		peer("POST", "/api/daemon/runtimes/"+f.victimRuntime+"/tasks/claim", nil, "runtimeId", f.victimRuntime)).
		Want(http.StatusNotFound)
	testutil.Call(t, testHandler.GetTaskStatus,
		peer("GET", "/api/daemon/tasks/"+f.runningTask+"/status", nil, "taskId", f.runningTask)).
		Want(http.StatusNotFound)
	testutil.Call(t, testHandler.CompleteTask,
		peer("POST", "/api/daemon/tasks/"+f.runningTask+"/complete", map[string]any{"output": "x"}, "taskId", f.runningTask)).
		Want(http.StatusNotFound)
	w := httptest.NewRecorder()
	if _, ok := testHandler.buildDaemonWebSocketIdentity(w, peer("GET", "/api/daemon/ws", nil), []string{f.victimRuntime}, ""); ok {
		t.Fatal("peer daemon token authorized a websocket for another daemon's runtime")
	}
	if got := taskStatusOf(t, f.queuedTask); got != "queued" {
		t.Fatalf("queued task status = %q, want queued", got)
	}

	// A runtime with no daemon_id is never reachable by a daemon token.
	unpinned := dbfx.Runtime(t, "mdtbind unpinned runtime")
	testutil.Call(t, testHandler.DaemonHeartbeat,
		peer("POST", "/api/daemon/heartbeat", map[string]any{"runtime_id": unpinned})).
		Want(http.StatusNotFound)

	// The matching daemon token works.
	testutil.Call(t, testHandler.DaemonHeartbeat,
		newDaemonTokenRequest("POST", "/api/daemon/heartbeat", map[string]any{"runtime_id": f.victimRuntime}, testWorkspaceID, f.victimDaemon)).
		Want(http.StatusOK)
}

func TestDaemonTaskRoutes_UnboundTaskIsNotWritable(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "unbound task runtime")
	agentID := dbfx.Agent(t, "unbound task agent", runtimeID)
	issueID := dbfx.Issue(t, "unbound task issue")
	// The shape a deleted runtime leaves behind: terminal history, no runtime.
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": nil, "issue_id": issueID, "status": "completed",
		"started_at": testutil.Raw("now() - interval '1 minute'"), "completed_at": testutil.Raw("now()"),
	})
	path := "/api/daemon/tasks/" + taskID

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		method  string
		suffix  string
		body    any
	}{
		{"complete", testHandler.CompleteTask, "POST", "/complete", map[string]any{"output": "x"}},
		{"report messages", testHandler.ReportTaskMessages, "POST", "/messages", map[string]any{
			"messages": []any{map[string]any{"seq": 1, "type": "text", "content": "forged"}},
		}},
		{"list messages", testHandler.ListTaskMessages, "GET", "/messages", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Call(t, tc.handler, withURLParams(newOwnerDaemonRequest(tc.method, path+tc.suffix, tc.body), "taskId", taskID)).
				Want(http.StatusNotFound)
		})
	}
	if got := taskStatusOf(t, taskID); got != "completed" {
		t.Fatalf("unbound task status = %q, want untouched", got)
	}
	var messages int
	dbfx.QueryRow(t, `SELECT count(*) FROM task_message WHERE task_id = $1`, taskID).Scan(&messages)
	if messages != 0 {
		t.Fatalf("%d messages written into an unbound task", messages)
	}

	// Status stays readable so a daemon can still see a terminal answer.
	testutil.Call(t, testHandler.GetTaskStatus, withURLParams(newOwnerDaemonRequest("GET", path+"/status", nil), "taskId", taskID)).
		Want(http.StatusOK)
}
