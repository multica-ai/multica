package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func kagentLifecycleAPIRouter(authenticate bool) http.Handler {
	r := chi.NewRouter()
	if authenticate {
		r.Use(middleware.DaemonAuth(testHandler.Queries, nil, nil, nil))
	}
	r.Get("/api/daemon/runtimes/{runtimeId}/kagent/agents", testHandler.ListKagentRuntimeAgents)
	r.Post("/api/daemon/runtimes/{runtimeId}/kagent/agents/{agentId}/report", testHandler.ReportKagentRuntimeAgent)
	return r
}

func kagentLifecycleAPIEnrollment(t *testing.T, runtimeID, agentID string, over testutil.Cols) {
	t.Helper()
	cols := testutil.Cols{"agent_id": agentID, "workspace_id": testWorkspaceID, "runtime_id": runtimeID}
	for k, v := range over {
		cols[k] = v
	}
	dbfx.InsertNoID(t, "kagent_enrollment", cols, "agent_id=$1", agentID)
	// Enrollment rows reference a shared workspace, so any leak (tombstones
	// with no agent row to cascade) would trigger the kagent teardown guard
	// for every later test on the shared runtime.
	dbfx.Cleanup(t, `DELETE FROM kagent_enrollment WHERE agent_id=$1`, agentID)
}

func kagentLifecycleAPIPath(runtimeID string) string {
	return "/api/daemon/runtimes/" + runtimeID + "/kagent/agents"
}

func TestKagentLifecycleAPIListScopeAndSanitization(t *testing.T) {
	rt := dbfx.Runtime(t, "Lifecycle API", testutil.Cols{"provider": "kagent"})
	a := dbfx.Agent(t, "Desired native agent", rt, testutil.Cols{
		"description": "private-description-secret", "instructions": "private-instructions-secret",
		"custom_env": testutil.Raw(`'{"TOKEN":"private-token-secret"}'::jsonb`),
	})
	kagentLifecycleAPIEnrollment(t, rt, a, nil)
	dbfx.Agent(t, "Unenrolled agent", rt)
	otherRT := dbfx.Runtime(t, "Other lifecycle runtime", testutil.Cols{"provider": "kagent"})
	otherAgent := dbfx.Agent(t, "Other runtime agent", otherRT)
	kagentLifecycleAPIEnrollment(t, otherRT, otherAgent, nil)
	tombstone := uuid.NewString()
	kagentLifecycleAPIEnrollment(t, rt, tombstone, testutil.Cols{"desired_state": "absent", "phase": "deleted", "applied_generation": 1, "last_error": "private-error-secret"})
	var rows []map[string]any
	response := testutil.Call(t, kagentLifecycleAPIRouter(false).ServeHTTP, newOwnerDaemonRequest("GET", kagentLifecycleAPIPath(rt), nil)).Want(200).JSON(&rows)
	if len(rows) != 2 {
		t.Fatalf("wanted enrolled agent and tombstone, got %s", response.Text())
	}
	found := map[string]bool{}
	for _, row := range rows {
		found[row["agent_id"].(string)] = true
		if len(row) != 8 || row["workspace_id"] != testWorkspaceID || row["runtime_id"] != rt || row["generation"] != float64(1) || row["revision"] != float64(1) {
			t.Fatalf("unexpected desired fields: %#v", row)
		}
	}
	if !found[a] || !found[tombstone] || strings.Contains(response.Text(), "private-") {
		t.Fatalf("incorrect or unsafe list: %s", response.Text())
	}
	empty := dbfx.Runtime(t, "Empty lifecycle runtime", testutil.Cols{"provider": "kagent"})
	if got := testutil.Call(t, kagentLifecycleAPIRouter(false).ServeHTTP, newOwnerDaemonRequest("GET", kagentLifecycleAPIPath(empty), nil)).Want(200).Text(); strings.TrimSpace(got) != "[]" {
		t.Fatalf("empty response: %s", got)
	}
}

func TestKagentLifecycleAPIDaemonAuthorization(t *testing.T) {
	daemonID := "lifecycle-api-" + uuid.NewString()
	rt := dbfx.Runtime(t, "Protected lifecycle runtime", testutil.Cols{"provider": "kagent", "daemon_id": daemonID})
	a := dbfx.Agent(t, "Protected secret agent", rt)
	kagentLifecycleAPIEnrollment(t, rt, a, nil)
	otherUser := dbfx.User(t, "Other lifecycle owner", uuid.NewString()+"@example.com")
	otherWS := dbfx.Workspace(t, "Other lifecycle workspace", "lifecycle-"+uuid.NewString())
	for _, tc := range []struct {
		name, ws, daemon string
		owner            any
		status           int
	}{
		{"owner", testWorkspaceID, daemonID, testUserID, 200},
		{"wrong daemon", testWorkspaceID, "other-daemon", testUserID, 404},
		{"wrong workspace", otherWS, daemonID, testUserID, 404},
		{"wrong owner", testWorkspaceID, daemonID, otherUser, 404},
		{"unbound owner", testWorkspaceID, daemonID, nil, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := auth.GenerateDaemonToken()
			if err != nil {
				t.Fatal(err)
			}
			dbfx.Insert(t, "daemon_token", testutil.Cols{"token_hash": auth.HashToken(raw), "workspace_id": tc.ws, "daemon_id": tc.daemon, "owner_id": tc.owner, "expires_at": testutil.Raw("now()+interval '1 hour'")})
			for _, method := range []string{"GET", "POST"} {
				path := kagentLifecycleAPIPath(rt)
				var body any
				if method == "POST" {
					path += "/" + a + "/report"
					body = KagentEnrollmentReportRequest{1, 1, "pending", "ready"}
				}
				req := newOwnerDaemonRequest(method, path, body)
				req.Header.Set("Authorization", "Bearer "+raw)
				// A spoofed user header must not replace the daemon token owner.
				req.Header.Set("X-User-ID", testUserID)
				resp := testutil.Call(t, kagentLifecycleAPIRouter(true).ServeHTTP, req).Want(tc.status)
				if tc.status != 200 && (strings.Contains(resp.Text(), a) || strings.Contains(resp.Text(), "Protected secret")) {
					t.Fatalf("authorization leak: %s", resp.Text())
				}
			}
		})
	}
	req := newOwnerDaemonRequest("GET", kagentLifecycleAPIPath(rt), nil)
	testutil.Call(t, kagentLifecycleAPIRouter(true).ServeHTTP, req).Want(401)
}

func TestKagentLifecycleAPIInvalidAndChangedBindings(t *testing.T) {
	rt := dbfx.Runtime(t, "Lifecycle bindings", testutil.Cols{"provider": "kagent"})
	a := dbfx.Agent(t, "Binding agent", rt)
	kagentLifecycleAPIEnrollment(t, rt, a, nil)
	otherRT := dbfx.Runtime(t, "Other binding", testutil.Cols{"provider": "kagent"})
	router := kagentLifecycleAPIRouter(false)
	report := KagentEnrollmentReportRequest{1, 1, "pending", "ready"}
	for _, tc := range []struct {
		method, path string
		body         any
		status       int
	}{
		{"GET", kagentLifecycleAPIPath("invalid"), nil, 400},
		{"POST", kagentLifecycleAPIPath(rt) + "/invalid/report", report, 400},
		{"POST", kagentLifecycleAPIPath(otherRT) + "/" + a + "/report", report, 404},
		{"GET", kagentLifecycleAPIPath(testRuntimeID), nil, 404},
		{"POST", kagentLifecycleAPIPath(testRuntimeID) + "/" + a + "/report", report, 404},
	} {
		testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest(tc.method, tc.path, tc.body)).Want(tc.status)
	}

	otherUser := dbfx.User(t, "Lifecycle member", uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, otherUser, "member")
	for _, method := range []string{"GET", "POST"} {
		path := kagentLifecycleAPIPath(rt)
		var body any
		if method == "POST" {
			path += "/" + a + "/report"
			body = report
		}
		req := newOwnerDaemonRequest(method, path, body)
		req.Header.Set("X-User-ID", otherUser)
		testutil.Call(t, router.ServeHTTP, req).Want(404)
	}
	// A previously read runtime must be reauthorized after its provider changes.
	dbfx.Exec(t, "UPDATE agent_runtime SET provider='claude' WHERE id=$1", rt)
	testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("GET", kagentLifecycleAPIPath(rt), nil)).Want(404)
	dbfx.Exec(t, "UPDATE agent_runtime SET provider='kagent' WHERE id=$1", rt)
	dbfx.Exec(t, "UPDATE agent SET runtime_id=$2 WHERE id=$1", a, otherRT)
	testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("GET", kagentLifecycleAPIPath(rt), nil)).Want(409)
	testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("POST", kagentLifecycleAPIPath(rt)+"/"+a+"/report", report)).Want(409)
	otherWS := dbfx.Workspace(t, "Foreign binding", "lifecycle-foreign-"+uuid.NewString())
	dbfx.Exec(t, "UPDATE agent SET workspace_id=$2 WHERE id=$1", a, otherWS)
	testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("GET", kagentLifecycleAPIPath(rt), nil)).Want(409)
}

func TestKagentLifecycleAPIReadinessCAS(t *testing.T) {
	rt := dbfx.Runtime(t, "Lifecycle CAS", testutil.Cols{"provider": "kagent"})
	a := dbfx.Agent(t, "Readiness agent", rt)
	kagentLifecycleAPIEnrollment(t, rt, a, nil)
	path := kagentLifecycleAPIPath(rt) + "/" + a + "/report"
	router := kagentLifecycleAPIRouter(false)
	send := func(revision, generation int64, expected, phase string, status int) {
		t.Helper()
		testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("POST", path, KagentEnrollmentReportRequest{revision, generation, expected, phase})).Want(status)
	}
	send(1, 2, "pending", "ready", 409)
	send(1, 1, "ready", "blocked", 409)
	send(1, 1, "pending", "ready", 200)
	send(1, 1, "pending", "blocked", 409)
	var applied int64
	dbfx.QueryRow(t, "SELECT applied_generation FROM kagent_enrollment WHERE agent_id=$1", a).Scan(&applied)
	if applied != 1 {
		t.Fatalf("applied generation: %d", applied)
	}
	send(2, 1, "ready", "blocked", 200)
	var lastError string
	dbfx.QueryRow(t, "SELECT last_error FROM kagent_enrollment WHERE agent_id=$1", a).Scan(&lastError)
	if lastError != kagentReadinessBlockedMessage {
		t.Fatalf("unsafe diagnostic: %q", lastError)
	}
	send(3, 1, "blocked", "pending", 200)
	send(4, 1, "pending", "blocked", 200)
	send(5, 1, "blocked", "pending", 200)
	_, err := testHandler.Queries.RequestKagentEnrollmentDesiredState(t.Context(), db.RequestKagentEnrollmentDesiredStateParams{
		WorkspaceID: parseUUID(testWorkspaceID), RuntimeID: parseUUID(rt), AgentID: parseUUID(a), ExpectedGeneration: 1, DesiredState: "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	send(6, 1, "pending", "ready", 409)
	send(6, 2, "pending", "ready", 409)
	send(7, 1, "pending", "ready", 409)
	send(7, 2, "pending", "ready", 200)
	dbfx.QueryRow(t, "SELECT applied_generation, last_error FROM kagent_enrollment WHERE agent_id=$1", a).Scan(&applied, &lastError)
	if applied != 2 || lastError != "" {
		t.Fatalf("unexpected applied state %d %q", applied, lastError)
	}

	for _, raw := range []string{`null`, `{`, `{"expected_generation":2,"expected_phase":"ready","phase":"blocked"} {}`, strings.Repeat(" ", 4097) + `{}`} {
		req := httptest.NewRequest("POST", path, strings.NewReader(raw))
		req.Header.Set("X-User-ID", testUserID)
		testutil.Call(t, router.ServeHTTP, req).Want(400)
	}
	for _, phase := range []string{"deleted", "draining", "deleting", "ready"} {
		send(8, 2, "ready", phase, 400)
	}
	for _, body := range []any{
		map[string]any{"expected_revision": 8, "expected_generation": 2, "expected_phase": "ready", "phase": "blocked", "last_error": "Bearer secret"},
		map[string]any{"expected_revision": 8, "expected_generation": 2, "expected_phase": "deleting", "phase": "deleted", "remote_deletion_verified": true},
		KagentEnrollmentReportRequest{1, 0, "ready", "blocked"},
		KagentEnrollmentReportRequest{0, 2, "ready", "blocked"},
		KagentEnrollmentReportRequest{-1, 2, "ready", "blocked"},
		map[string]any{"expected_generation": 2, "expected_phase": "ready", "phase": "blocked"},
	} {
		testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("POST", path, body)).Want(400)
	}
	dbfx.Exec(t, "UPDATE kagent_enrollment SET desired_state='absent', phase='blocked' WHERE agent_id=$1", a)
	send(8, 2, "blocked", "pending", 409)
	for _, phase := range []string{"draining", "deleting", "deleted"} {
		send(8, 2, "blocked", phase, 400)
	}
}

func TestKagentLifecycleAPIConcurrentReadinessCAS(t *testing.T) {
	rt := dbfx.Runtime(t, "Lifecycle concurrent CAS", testutil.Cols{"provider": "kagent"})
	a := dbfx.Agent(t, "Concurrent readiness", rt)
	kagentLifecycleAPIEnrollment(t, rt, a, nil)
	path := kagentLifecycleAPIPath(rt) + "/" + a + "/report"
	router := kagentLifecycleAPIRouter(false)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, phase := range []string{"ready", "blocked"} {
		req := newOwnerDaemonRequest("POST", path, KagentEnrollmentReportRequest{1, 1, "pending", phase})
		wg.Add(1)
		go func(req *http.Request) {
			defer wg.Done()
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			statuses <- w.Code
		}(req)
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	var revision int64
	dbfx.QueryRow(t, "SELECT revision FROM kagent_enrollment WHERE agent_id=$1", a).Scan(&revision)
	if revision != 2 {
		t.Fatalf("concurrent reports advanced revision %d times", revision-1)
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("expected one CAS winner and one conflict: %v", counts)
	}
}

func TestKagentLifecycleAPIDelayedABAReport(t *testing.T) {
	rt := dbfx.Runtime(t, "Lifecycle ABA", testutil.Cols{"provider": "kagent"})
	a := dbfx.Agent(t, "ABA readiness", rt)
	kagentLifecycleAPIEnrollment(t, rt, a, nil)
	router := kagentLifecycleAPIRouter(false)
	path := kagentLifecycleAPIPath(rt) + "/" + a + "/report"
	var rows []KagentEnrollmentResponse
	testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("GET", kagentLifecycleAPIPath(rt), nil)).Want(200).JSON(&rows)
	current := rows[0]
	delayed := KagentEnrollmentReportRequest{ExpectedRevision: current.Revision, ExpectedGeneration: current.Generation, ExpectedPhase: current.Phase, Phase: "ready"}
	for _, phase := range []string{"ready", "blocked", "pending", "blocked", "pending"} {
		previous := current
		request := KagentEnrollmentReportRequest{ExpectedRevision: previous.Revision, ExpectedGeneration: previous.Generation, ExpectedPhase: previous.Phase, Phase: phase}
		testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("POST", path, request)).Want(200).JSON(&current)
		if current.Revision != previous.Revision+1 || current.Generation != previous.Generation || current.Phase != phase {
			t.Fatalf("mutation must advance revision exactly once: before=%+v after=%+v", previous, current)
		}
		testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("POST", path, delayed)).Want(409)
	}
	// Generation and phase now match the delayed request, but its revision does not.
	var after KagentEnrollmentResponse
	testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("POST", path, KagentEnrollmentReportRequest{current.Revision, current.Generation, current.Phase, "ready"})).Want(200).JSON(&after)
	if after.Revision != current.Revision+1 {
		t.Fatalf("failed reports advanced revision: %+v", after)
	}
}
