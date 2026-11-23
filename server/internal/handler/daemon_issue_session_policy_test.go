package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestClaimTaskIssueSessionPolicy(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, tc := range []struct {
		name       string
		config     string
		wantResume bool
		lineage    string
	}{
		{name: "default policy resumes", config: `{}`, wantResume: true},
		{name: "unknown policy resumes", config: `{"issue_session_policy":"other"}`, wantResume: true},
		{name: "fresh policy applies to manual reruns", config: `{"issue_session_policy":"fresh"}`, lineage: "rerun_of_task_id"},
		{name: "fresh policy applies to automatic retries", config: `{"issue_session_policy":"fresh"}`, lineage: "retry_of_task_id"},
		{name: "fresh policy drops prior session and workdir", config: `{"issue_session_policy":"fresh"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, t.Context())
			dbfx.Cleanup(t, "DELETE FROM agent WHERE id = $1", agentID)
			issueID := dbfx.Issue(t, "Issue session policy", testutil.Cols{"status": "in_progress"})
			parentID := dbfx.Task(t, agentID, testutil.Cols{
				"issue_id": issueID, "runtime_id": runtimeID, "status": "completed",
				"session_id": "previous-session", "work_dir": "/tmp/previous-workdir",
				"started_at": testutil.Raw("now()"), "completed_at": testutil.Raw("now()"),
			})
			task := testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID}
			if tc.lineage != "" {
				task[tc.lineage] = parentID
				task["force_fresh_session"] = true
			}
			if tc.lineage == "retry_of_task_id" {
				task["work_dir"] = "/tmp/previous-workdir"
			}
			dbfx.Task(t, agentID, task)
			update := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
				"runtime_config": json.RawMessage(tc.config),
			})
			testutil.Call(t, testHandler.UpdateAgent, withURLParam(update, "id", agentID)).Want(http.StatusOK)

			claim := claimTaskForRuntimeGuardWithCapabilities(t, runtimeID, daemonID, protocol.DaemonCapabilityCheckoutKeepsWorkV1)

			var wantSession, wantWorkDir string
			if tc.wantResume {
				wantSession = "previous-session"
				wantWorkDir = "/tmp/previous-workdir"
			}
			if claim.PriorSessionID != wantSession || claim.PriorWorkDir != wantWorkDir {
				t.Fatalf("prior session/workdir = %q/%q, want %q/%q", claim.PriorSessionID, claim.PriorWorkDir, wantSession, wantWorkDir)
			}
		})
	}
}
