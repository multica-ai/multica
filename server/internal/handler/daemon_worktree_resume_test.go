package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestClaimTaskAntigravityWorktreeResumeSource(t *testing.T) {
	const daemonID = "antigravity-worktree-resume-daemon"
	const repo = "/local/project"
	const cwd = "/workspaces/project/first-task/worktree"
	const session = "resumable-acp-session"

	runtimeID := dbfx.Runtime(t, "Antigravity ACP", testutil.Cols{
		"daemon_id": daemonID, "provider": "antigravity", "runtime_mode": "local",
	})
	otherRuntime := dbfx.Runtime(t, "Other Antigravity", testutil.Cols{
		"provider": "antigravity",
	})
	agentID := dbfx.Agent(t, "Antigravity Worktree Resume", runtimeID, testutil.Cols{
		"runtime_mode": "local", "custom_args": `["--multica-antigravity-acp"]`,
	})
	projectID := dbfx.Project(t, "Antigravity Worktree Resume")
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id": projectID, "workspace_id": testWorkspaceID, "resource_type": "local_directory",
		"resource_ref": `{"daemon_id":"` + daemonID + `","local_path":"` + repo + `","execution_mode":"worktree"}`,
	})
	issueID := dbfx.Issue(t, "Antigravity Worktree Resume", testutil.Cols{"project_id": projectID})
	otherIssue := dbfx.Issue(t, "Other Antigravity Worktree")
	addHistory := func(runtime, issue, durablePath, branch string, created testutil.Raw) string {
		t.Helper()
		return dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtime, "issue_id": issue, "status": "completed",
			"started_at": created, "completed_at": created, "created_at": created,
			"session_id": session, "work_dir": cwd, "durable_work_dir": durablePath,
			"branch_name": branch,
		})
	}
	addHistory(otherRuntime, issueID, "/wrong/runtime", "wrong", "now() - interval '3 hours'")
	addHistory(runtimeID, otherIssue, "/wrong/issue", "wrong", "now() - interval '3 hours'")
	first := addHistory(runtimeID, issueID, repo, "agent/old-name", "now() - interval '2 hours'")
	addHistory(runtimeID, issueID, repo, "agent/latest-branch", "now() - interval '1 hour'")
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "priority": 1000})

	req := newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil)
	req = testutil.WithURLParams(req, "runtimeId", runtimeID)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityLocalWorktreeV1)
	var resp struct {
		Task *AgentTaskResponse `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&resp)
	if resp.Task == nil || resp.Task.PriorSessionID != session || resp.Task.PriorWorkDir != cwd {
		t.Fatalf("claim did not select the expected session/cwd pair: %+v", resp.Task)
	}
	want := protocol.LocalWorktreeResumeSource{RootTaskID: first, LocalPath: repo, Branch: "agent/latest-branch"}
	if source := resp.Task.LocalWorktreeResumeSource; source == nil || *source != want {
		t.Fatalf("resume source = %+v, want %+v", source, want)
	}
}
