package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// claimResumeProbe decodes the three resume-decision fields off a claim
// response: whether a session was handed over, whether the workdir survived
// with it, and whether the run was told to reconstruct instead of continue.
type claimResumeProbe struct {
	Task *struct {
		ID                            string `json:"id"`
		PriorSessionID                string `json:"prior_session_id"`
		PriorWorkDir                  string `json:"prior_work_dir"`
		PriorSessionResumeUnavailable bool   `json:"prior_session_resume_unavailable"`
	} `json:"task"`
}

func claimResumeForRuntime(t *testing.T, runtimeID, daemonID string) claimResumeProbe {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var probe claimResumeProbe
	if err := json.NewDecoder(w.Body).Decode(&probe); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if probe.Task == nil {
		t.Fatal("expected a claimed task in the response")
	}
	return probe
}

// configDigestFixture is one (agent, issue) pair carrying a prior completed
// task that recorded a session, a workdir, and some agent-config digest.
type configDigestFixture struct {
	agentID   string
	issueID   string
	runtimeID string
}

// seedConfigDigestPair creates an agent whose instructions are `instructions`,
// an issue assigned to it, and a completed prior task that recorded
// priorSession / priorWorkDir under priorDigest. Pass a nil priorDigest for a
// row written before the column existed.
func seedConfigDigestPair(t *testing.T, name, instructions, priorSession, priorWorkDir string, priorDigest *string) configDigestFixture {
	t.Helper()
	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, name, []byte("[]"))
	dbfx.Exec(t, `UPDATE agent SET instructions = $2 WHERE id = $1`, agentID, instructions)

	issueID := dbfx.Issue(t, name+" issue", testutil.Cols{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)

	var digest any
	if priorDigest != nil {
		digest = *priorDigest
	}
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":          runtimeID,
		"issue_id":            issueID,
		"status":              "completed",
		"started_at":          testutil.Raw("now() - interval '2 minutes'"),
		"completed_at":        testutil.Raw("now() - interval '2 minutes'"),
		"session_id":          priorSession,
		"work_dir":            priorWorkDir,
		"agent_config_digest": digest,
	})

	return configDigestFixture{agentID: agentID, issueID: issueID, runtimeID: runtimeID}
}

// TestClaimTaskByRuntime_AgentConfigDigestGate covers the (agent, issue) resume
// lookup: a session created under the agent's current instructions is resumed,
// one created under different instructions is not, and a row that predates the
// digest column keeps resuming (GH #8070 / MUL-7082).
func TestClaimTaskByRuntime_AgentConfigDigestGate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("resumes when the delivered configuration is unchanged", func(t *testing.T) {
		current := service.AgentConfigDigest("RULE A")
		fx := seedConfigDigestPair(t, "ConfigDigestSame", "RULE A", "SESSION-SAME", "/tmp/same", &current)
		dbfx.Task(t, fx.agentID, testutil.Cols{
			"runtime_id": fx.runtimeID,
			"issue_id":   fx.issueID,
			"priority":   1000,
		})

		probe := claimResumeForRuntime(t, fx.runtimeID, "config-digest-same")
		if probe.Task.PriorSessionID != "SESSION-SAME" {
			t.Fatalf("expected an unchanged configuration to resume; task=%+v", *probe.Task)
		}
		if probe.Task.PriorSessionResumeUnavailable {
			t.Fatalf("a warm resume must not disclose a continuity gap; task=%+v", *probe.Task)
		}
	})

	t.Run("drops the session but keeps the workdir when instructions changed", func(t *testing.T) {
		stale := service.AgentConfigDigest("RULE A")
		fx := seedConfigDigestPair(t, "ConfigDigestChanged", "RULE B", "SESSION-STALE", "/tmp/changed", &stale)
		dbfx.Task(t, fx.agentID, testutil.Cols{
			"runtime_id": fx.runtimeID,
			"issue_id":   fx.issueID,
			"priority":   1000,
		})

		probe := claimResumeForRuntime(t, fx.runtimeID, "config-digest-changed")
		if probe.Task.PriorSessionID != "" {
			t.Fatalf("expected changed instructions to drop the resume; task=%+v", *probe.Task)
		}
		// #7998: a session that must not be resumed says nothing about the
		// working tree. Dropping both would discard in-progress work on every
		// instruction edit.
		if probe.Task.PriorWorkDir != "/tmp/changed" {
			t.Fatalf("expected the workdir to survive a dropped resume; task=%+v", *probe.Task)
		}
		if !probe.Task.PriorSessionResumeUnavailable {
			t.Fatalf("expected the cold start to be disclosed to the run; task=%+v", *probe.Task)
		}
	})

	t.Run("resumes when the prior task predates the digest column", func(t *testing.T) {
		fx := seedConfigDigestPair(t, "ConfigDigestLegacy", "RULE B", "SESSION-LEGACY", "/tmp/legacy", nil)
		dbfx.Task(t, fx.agentID, testutil.Cols{
			"runtime_id": fx.runtimeID,
			"issue_id":   fx.issueID,
			"priority":   1000,
		})

		probe := claimResumeForRuntime(t, fx.runtimeID, "config-digest-legacy")
		if probe.Task.PriorSessionID != "SESSION-LEGACY" {
			t.Fatalf("a row with no recorded digest is unknown, not stale, and must still resume; task=%+v", *probe.Task)
		}
	})
}

// TestClaimTaskByRuntime_RecordsDeliveredAgentConfigDigest closes the loop the
// two gate tests above open by hand: the claim itself must record what it
// delivered, or the next run has nothing to compare against and the gate never
// fires in production.
func TestClaimTaskByRuntime_RecordsDeliveredAgentConfigDigest(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "ConfigDigestRecorded", []byte("[]"))
	dbfx.Exec(t, `UPDATE agent SET instructions = $2 WHERE id = $1`, agentID, "RULE A")

	issueID := dbfx.Issue(t, "config digest recorded issue", testutil.Cols{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
		"priority":   1000,
	})

	probe := claimResumeForRuntime(t, runtimeID, "config-digest-recorded")
	if probe.Task.ID != taskID {
		t.Fatalf("claimed the wrong task: got %s, want %s", probe.Task.ID, taskID)
	}

	var recorded string
	dbfx.QueryRow(t, `SELECT COALESCE(agent_config_digest, '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&recorded)
	if want := service.AgentConfigDigest("RULE A"); recorded != want {
		t.Fatalf("claim recorded digest %q, want %q", recorded, want)
	}
}

// TestClaimTaskByRuntime_RerunSourceAgentConfigChanged covers the other branch
// that hands back a session: a manual rerun resolves its resume from the exact
// source task, and that source is subject to the same gate. This is the path
// both rerun buttons in the web app take.
func TestClaimTaskByRuntime_RerunSourceAgentConfigChanged(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	stale := service.AgentConfigDigest("RULE A")
	fx := seedConfigDigestPair(t, "ConfigDigestRerun", "RULE B", "SESSION-RERUN", "/tmp/rerun", &stale)

	var srcID string
	dbfx.QueryRow(t,
		`SELECT id FROM agent_task_queue WHERE issue_id = $1 AND status = 'completed'`,
		fx.issueID,
	).Scan(&srcID)

	dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id":          fx.runtimeID,
		"issue_id":            fx.issueID,
		"priority":            1000,
		"rerun_of_task_id":    srcID,
		"force_fresh_session": true,
	})

	probe := claimResumeForRuntime(t, fx.runtimeID, "config-digest-rerun")
	if probe.Task.PriorSessionID != "" {
		t.Fatalf("expected a rerun to drop a session built under different instructions; task=%+v", *probe.Task)
	}
	if probe.Task.PriorWorkDir != "/tmp/rerun" {
		t.Fatalf("expected the rerun to keep reusing the source workdir; task=%+v", *probe.Task)
	}
	if !probe.Task.PriorSessionResumeUnavailable {
		t.Fatalf("expected the cold start to be disclosed to the run; task=%+v", *probe.Task)
	}
}
