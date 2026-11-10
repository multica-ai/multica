package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func issueEnvironmentFixture(t *testing.T) (issueID, taskID string) {
	t.Helper()
	runtimeID := dbfx.Runtime(t, "issue-environment runtime")
	agentID := dbfx.Agent(t, "issue-environment agent", runtimeID)
	issueID = dbfx.Issue(t, "issue-environment issue")
	taskID = dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running"})
	dbfx.Cleanup(t, `DELETE FROM issue_environment WHERE task_id = $1`, taskID)
	return issueID, taskID
}

func reportEnvironments(t *testing.T, taskID string, envs ...map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/environments",
		map[string]any{"environments": envs}), "taskId", taskID)
	return testutil.Call(t, testHandler.ReportTaskEnvironments, req)
}

func listEnvironments(t *testing.T, issueID string) map[string]IssueEnvironmentResponse {
	t.Helper()
	var out IssueEnvironmentsResponse
	testutil.Call(t, testHandler.ListIssueEnvironments,
		withURLParam(newRequest(http.MethodGet, "/api/issues/"+issueID+"/environments", nil), "id", issueID)).
		Want(http.StatusOK).JSON(&out)
	byKey := make(map[string]IssueEnvironmentResponse, len(out.Environments))
	for _, env := range out.Environments {
		byKey[env.Name] = env
	}
	return byKey
}

func webEnvironment(state string) map[string]any {
	return map[string]any{
		"key": "config:web", "source": "config", "name": "web", "label": "Web app",
		"url": "http://localhost:41000", "port": 41000, "state": state,
		"started_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
		"expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
}

func TestReportTaskEnvironments_SnapshotLifecycle(t *testing.T) {
	issueID, taskID := issueEnvironmentFixture(t)
	vite := map[string]any{"key": "detected:abc", "source": "detected", "name": "node", "url": "http://localhost:5173", "port": 5173, "state": "running"}

	reportEnvironments(t, taskID, webEnvironment("running"), vite).Want(http.StatusOK)
	envs := listEnvironments(t, issueID)
	if len(envs) != 2 || envs["web"].State != "running" || envs["node"].State != "running" {
		t.Fatalf("after first snapshot = %+v", envs)
	}
	if envs["web"].Label != "Web app" || envs["web"].URL != "http://localhost:41000" || envs["web"].Port == nil || *envs["web"].Port != 41000 {
		t.Fatalf("web environment = %+v", envs["web"])
	}
	if envs["web"].ExpiresAt == nil || envs["web"].EndedAt != nil {
		t.Fatalf("web environment times = %+v", envs["web"])
	}

	// The detected server is gone from the next snapshot: it has stopped.
	reportEnvironments(t, taskID, webEnvironment("running")).Want(http.StatusOK)
	envs = listEnvironments(t, issueID)
	if envs["node"].State != "stopped" || envs["node"].EndedAt == nil {
		t.Fatalf("omitted environment = %+v, want stopped and ended", envs["node"])
	}
	if envs["web"].State != "running" || envs["web"].EndedAt != nil {
		t.Fatalf("still-reported environment = %+v", envs["web"])
	}

	// A crashed environment keeps saying so after the daemon drops it.
	reportEnvironments(t, taskID, webEnvironment("exited")).Want(http.StatusOK)
	reportEnvironments(t, taskID).Want(http.StatusOK)
	envs = listEnvironments(t, issueID)
	if envs["web"].State != "exited" || envs["web"].EndedAt == nil {
		t.Fatalf("exited environment = %+v, want exited and ended", envs["web"])
	}
}

func TestReportTaskEnvironments_RestartReopensRow(t *testing.T) {
	issueID, taskID := issueEnvironmentFixture(t)
	reportEnvironments(t, taskID, webEnvironment("running")).Want(http.StatusOK)
	reportEnvironments(t, taskID).Want(http.StatusOK)
	reportEnvironments(t, taskID, webEnvironment("running")).Want(http.StatusOK)

	envs := listEnvironments(t, issueID)
	if len(envs) != 1 || envs["web"].State != "running" || envs["web"].EndedAt != nil {
		t.Fatalf("restarted environment = %+v, want one live row", envs)
	}
}

func TestListIssueEnvironments_UnreachableWhenNotSeen(t *testing.T) {
	issueID, taskID := issueEnvironmentFixture(t)
	reportEnvironments(t, taskID, webEnvironment("running")).Want(http.StatusOK)
	dbfx.Exec(t, `UPDATE issue_environment SET last_seen_at = now() - interval '10 minutes' WHERE task_id = $1`, taskID)

	if got := listEnvironments(t, issueID)["web"].State; got != "unreachable" {
		t.Fatalf("state of an environment not seen for 10 minutes = %q, want unreachable", got)
	}
}

func TestReportTaskEnvironments_RejectsInvalidSnapshot(t *testing.T) {
	_, taskID := issueEnvironmentFixture(t)
	cases := map[string]map[string]any{
		"script url":     {"key": "a", "source": "config", "name": "web", "url": "javascript:alert(1)", "state": "running"},
		"unknown source": {"key": "a", "source": "cloud", "name": "web", "state": "running"},
		"unknown state":  {"key": "a", "source": "config", "name": "web", "state": "healthy"},
		"empty name":     {"key": "a", "source": "config", "name": " ", "state": "running"},
		"bad port":       {"key": "a", "source": "config", "name": "web", "port": 70000, "state": "running"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			reportEnvironments(t, taskID, env).Want(http.StatusBadRequest)
		})
	}
	t.Run("duplicate key", func(t *testing.T) {
		reportEnvironments(t, taskID, webEnvironment("running"), webEnvironment("running")).Want(http.StatusBadRequest)
	})
	if n := dbfx.Count(t, `SELECT count(*) FROM issue_environment WHERE task_id = $1`, taskID); n != 0 {
		t.Fatalf("rejected snapshots stored %d rows", n)
	}
}

func TestReportTaskEnvironments_OtherWorkspaceDaemonCannotReport(t *testing.T) {
	issueID, taskID := issueEnvironmentFixture(t)
	req := withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/environments",
		map[string]any{"environments": []any{webEnvironment("running")}},
		"00000000-0000-0000-0000-000000000000", "attacker-daemon"), "taskId", taskID)
	testutil.Call(t, testHandler.ReportTaskEnvironments, req).Want(http.StatusNotFound)

	if envs := listEnvironments(t, issueID); len(envs) != 0 {
		t.Fatalf("environments after a foreign daemon's report = %+v", envs)
	}
}

func TestReportTaskEnvironments_TaskWithoutIssueIsIgnored(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "issue-environment chat runtime")
	agentID := dbfx.Agent(t, "issue-environment chat agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running"})
	dbfx.Cleanup(t, `DELETE FROM issue_environment WHERE task_id = $1`, taskID)

	// A task with no issue may not resolve to a workspace at all; either way it
	// must not store anything.
	reportEnvironments(t, taskID, webEnvironment("running")).WantOneOf(http.StatusOK, http.StatusNotFound)
	if n := dbfx.Count(t, `SELECT count(*) FROM issue_environment WHERE task_id = $1`, taskID); n != 0 {
		t.Fatalf("issue-less task stored %d environments", n)
	}
}
