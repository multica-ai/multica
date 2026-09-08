package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

// agentConfigDigestFor returns the digest a claim computes for a plain
// (non-system, non-squad) test agent. A fixture that seeds a prior session by
// hand needs it to vouch for that session the way the real prior claim would:
// since MUL-7082 only an exact match resumes, and a hand-written row with no
// digest reads as "cannot vouch" and starts fresh.
func agentConfigDigestFor(t *testing.T, agentID string) string {
	t.Helper()
	var instructions string
	dbfx.QueryRow(t, `SELECT COALESCE(instructions, '') FROM agent WHERE id = $1`, agentID).Scan(&instructions)
	return service.AgentConfigDigest(instructions)
}

// TestClaimTaskByRuntime_AgentConfigDigestGate covers the (agent, issue) resume
// lookup. Only a row that vouches for the session — an exact digest match —
// resumes; different instructions and a row with no digest at all both start
// fresh, and a redelivery of the same configuration costs nothing
// (GH #8070 / MUL-7082).
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

	t.Run("starts fresh when the prior task predates the digest column", func(t *testing.T) {
		fx := seedConfigDigestPair(t, "ConfigDigestLegacy", "RULE B", "SESSION-LEGACY", "/tmp/legacy", nil)
		dbfx.Task(t, fx.agentID, testutil.Cols{
			"runtime_id": fx.runtimeID,
			"issue_id":   fx.issueID,
			"priority":   1000,
		})

		probe := claimResumeForRuntime(t, fx.runtimeID, "config-digest-legacy")
		// Resuming an unvouched session is not a one-round delay, it is a
		// permanent mislabel: this run would record ITS digest against a
		// session built from something else, and every later run would then
		// match it. One cold start per live pair on the deploy instead.
		if probe.Task.PriorSessionID != "" {
			t.Fatalf("a session no row can vouch for must not be resumed; task=%+v", *probe.Task)
		}
		if probe.Task.PriorWorkDir != "/tmp/legacy" {
			t.Fatalf("expected the workdir to survive the cold start; task=%+v", *probe.Task)
		}
		if !probe.Task.PriorSessionResumeUnavailable {
			t.Fatalf("expected the cold start to be disclosed to the run; task=%+v", *probe.Task)
		}
	})

	t.Run("keeps resuming when a redelivery carried the same configuration", func(t *testing.T) {
		current := service.AgentConfigDigest("RULE A")
		fx := seedConfigDigestPair(t, "ConfigDigestRedeliverySame", "RULE A", "SESSION-REDELIVERED", "/tmp/same-redelivery", &current)
		taskID := dbfx.Task(t, fx.agentID, testutil.Cols{
			"runtime_id": fx.runtimeID,
			"issue_id":   fx.issueID,
			"priority":   1000,
		})

		// Two deliveries of one row are only ambiguous when they carried
		// DIFFERENT configurations. A plain lost-response reclaim must not
		// cost the conversation.
		claimResumeForRuntime(t, fx.runtimeID, "config-digest-redelivery-same-1")
		dbfx.Exec(t, `UPDATE agent_task_queue
			SET status = 'dispatched', dispatched_at = now(), started_at = NULL
			WHERE id = $1`, taskID)
		if _, err := testHandler.TaskService.FinalizeTaskClaim(context.Background(),
			mustLoadTask(t, taskID), newDigestTestToken(t, taskID, fx.agentID, "redelivery-same"),
			nil, false, service.AgentConfigDigest("RULE A")); err != nil {
			t.Fatalf("second delivery of the same configuration must be accepted: %v", err)
		}

		var recorded string
		dbfx.QueryRow(t, `SELECT COALESCE(agent_config_digest, '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&recorded)
		if recorded != current {
			t.Fatalf("redelivering the same configuration must keep the digest; got %q", recorded)
		}
	})
}

// TestFinalizeTaskClaim_RejectsSupersededClaim covers the claim-generation
// fence. A handler whose claim was superseded by a stale reclaim must not
// commit its token: it would hand a daemon a payload the row does not
// describe, and the next run would compare against the reclaim's digest while
// the session that actually ran came from the superseded payload.
func TestFinalizeTaskClaim_RejectsSupersededClaim(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	fx := seedConfigDigestPair(t, "ConfigDigestSuperseded", "RULE B", "SESSION-SUPERSEDED", "/tmp/superseded", nil)
	taskID := dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id":    fx.runtimeID,
		"issue_id":      fx.issueID,
		"status":        "dispatched",
		"dispatched_at": testutil.Raw("now() - interval '2 minutes'"),
	})
	superseded := mustLoadTask(t, taskID)

	// A stale reclaim re-delivered the row and recorded its own payload.
	reclaimed := service.AgentConfigDigest("RULE B")
	dbfx.Exec(t, `UPDATE agent_task_queue SET dispatched_at = now(), agent_config_digest = $2 WHERE id = $1`,
		taskID, reclaimed)

	tokenHash := "superseded-claim-" + taskID
	_, err := testHandler.TaskService.FinalizeTaskClaim(ctx, superseded,
		newDigestTestTokenHash(t, taskID, fx.agentID, tokenHash),
		nil, false, service.AgentConfigDigest("RULE A"))
	if err == nil {
		t.Fatal("expected a superseded claim to be rejected rather than delivered")
	}

	if tokens := dbfx.Count(t, `SELECT count(*) FROM task_token WHERE token_hash = $1`, tokenHash); tokens != 0 {
		t.Fatalf("a rejected claim must roll its token back; found %d", tokens)
	}
	var recorded string
	dbfx.QueryRow(t, `SELECT COALESCE(agent_config_digest, '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&recorded)
	if recorded != reclaimed {
		t.Fatalf("the live reclaim's digest must survive a superseded finalize; got %q", recorded)
	}
}

// TestClaimTaskByRuntime_RedeliveryWithNewConfigStartsFresh covers the second
// way one row can end up describing a run it did not deliver. A stale reclaim
// refreshes dispatched_at without starting the task, and StartAgentTask admits
// whichever delivery calls it first — so after two deliveries with different
// configurations the row cannot name the one that ran, and the next run must
// not trust it.
func TestClaimTaskByRuntime_RedeliveryWithNewConfigStartsFresh(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	current := service.AgentConfigDigest("RULE B")
	fx := seedConfigDigestPair(t, "ConfigDigestRedelivery", "RULE B", "SESSION-REDELIVERY", "/tmp/redelivery", &current)
	taskID := dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id":    fx.runtimeID,
		"issue_id":      fx.issueID,
		"status":        "dispatched",
		"dispatched_at": testutil.Raw("now() - interval '2 minutes'"),
	})

	// First delivery carried RULE A and is still preparing.
	first := mustLoadTask(t, taskID)
	if _, err := testHandler.TaskService.FinalizeTaskClaim(ctx, first,
		newDigestTestToken(t, taskID, fx.agentID, "redelivery-first"),
		nil, false, service.AgentConfigDigest("RULE A")); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	// Its lease expired and the reclaim re-delivered the row, by then carrying
	// RULE B.
	dbfx.Exec(t, `UPDATE agent_task_queue SET dispatched_at = now() WHERE id = $1`, taskID)
	if _, err := testHandler.TaskService.FinalizeTaskClaim(ctx, mustLoadTask(t, taskID),
		newDigestTestToken(t, taskID, fx.agentID, "redelivery-second"),
		nil, false, service.AgentConfigDigest("RULE B")); err != nil {
		t.Fatalf("redelivery: %v", err)
	}

	var recorded string
	dbfx.QueryRow(t, `SELECT COALESCE(agent_config_digest, '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&recorded)
	if recorded != service.AgentConfigDigestAmbiguous {
		t.Fatalf("two deliveries under different configurations must mark the row ambiguous; got %q", recorded)
	}

	// Sticky: a third delivery matching neither must not launder the row back
	// into a definite answer.
	dbfx.Exec(t, `UPDATE agent_task_queue SET dispatched_at = now() WHERE id = $1`, taskID)
	if _, err := testHandler.TaskService.FinalizeTaskClaim(ctx, mustLoadTask(t, taskID),
		newDigestTestToken(t, taskID, fx.agentID, "redelivery-third"),
		nil, false, service.AgentConfigDigest("RULE B")); err != nil {
		t.Fatalf("third delivery: %v", err)
	}
	dbfx.QueryRow(t, `SELECT COALESCE(agent_config_digest, '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&recorded)
	if recorded != service.AgentConfigDigestAmbiguous {
		t.Fatalf("the ambiguity marker must be sticky; got %q", recorded)
	}

	// The session this row goes on to report cannot be trusted by the next run.
	dbfx.Exec(t, `UPDATE agent_task_queue
		SET status = 'completed', started_at = now(), completed_at = now(),
		    session_id = 'SESSION-AMBIGUOUS', work_dir = '/tmp/redelivery'
		WHERE id = $1`, taskID)
	dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id": fx.runtimeID,
		"issue_id":   fx.issueID,
		"priority":   1000,
	})

	probe := claimResumeForRuntime(t, fx.runtimeID, "config-digest-redelivery")
	if probe.Task.PriorSessionID != "" {
		t.Fatalf("a session no delivery can vouch for must not be resumed; task=%+v", *probe.Task)
	}
	if probe.Task.PriorWorkDir != "/tmp/redelivery" {
		t.Fatalf("expected the workdir to survive the cold start; task=%+v", *probe.Task)
	}
}

// TestClaimTaskByRuntime_SupersededDeliveryCannotVouchForSession is the
// end-to-end form of the redelivery hazard, driven through two real claims and
// a real StartTask (review of PR #8160).
//
// StartAgentTask admits a delivery by task id and status alone, with no
// claim-generation check, so after a stale reclaim BOTH the superseded
// delivery and the reclaim can be the one that starts — that lifecycle hole
// predates this change and is not what the digest can fix. What the digest
// must not do is name one of them: the row stops claiming to know, and the
// next run treats the session it reports as unvouched.
func TestClaimTaskByRuntime_SupersededDeliveryCannotVouchForSession(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "Superseded delivery runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Superseded delivery agent")
	dbfx.Exec(t, `UPDATE agent SET instructions = 'RULE A' WHERE id = $1`, agentID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID})

	first, firstBody := claimTaskByRuntimeForTest(t, runtimeID)
	if first == nil || first.ID != taskID {
		t.Fatalf("first claim: %s", firstBody)
	}

	// That daemon is still preparing under RULE A when its lease expires, the
	// instructions change, and the stale reclaim re-delivers the same row.
	dbfx.Exec(t, `UPDATE agent_task_queue
		SET dispatched_at = now() - interval '2 minutes', prepare_lease_expires_at = NULL
		WHERE id = $1`, taskID)
	dbfx.Exec(t, `UPDATE agent SET instructions = 'RULE B' WHERE id = $1`, agentID)
	second, secondBody := claimTaskByRuntimeForTest(t, runtimeID)
	if second == nil || second.ID != taskID {
		t.Fatalf("reclaim: %s", secondBody)
	}

	var recorded string
	dbfx.QueryRow(t, `SELECT COALESCE(agent_config_digest, '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&recorded)
	if recorded == service.AgentConfigDigest("RULE B") {
		t.Fatal("the row must not name the reclaim while the superseded RULE A delivery can still start")
	}
	if recorded != service.AgentConfigDigestAmbiguous {
		t.Fatalf("two deliveries under different configurations must mark the row ambiguous; got %q", recorded)
	}

	// The superseded delivery starting is the pre-existing hole; the row is
	// already refusing to vouch either way, so the session it goes on to
	// record cannot be resumed by the next run.
	if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(taskID)); err != nil {
		t.Fatalf("start superseded delivery: %v", err)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue
		SET status = 'completed', completed_at = now(),
		    session_id = 'SESSION-SUPERSEDED-DELIVERY', work_dir = '/tmp/superseded-delivery'
		WHERE id = $1`, taskID)
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
		"priority":   1000,
	})

	probe := claimResumeForRuntime(t, runtimeID, "superseded-delivery")
	if probe.Task.PriorSessionID != "" {
		t.Fatalf("a session no delivery can vouch for must not be resumed; task=%+v", *probe.Task)
	}
	if probe.Task.PriorWorkDir != "/tmp/superseded-delivery" {
		t.Fatalf("expected the workdir to survive the cold start; task=%+v", *probe.Task)
	}
}

func mustLoadTask(t *testing.T, taskID string) db.AgentTaskQueue {
	t.Helper()
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatalf("load task %s: %v", taskID, err)
	}
	return task
}

func newDigestTestToken(t *testing.T, taskID, agentID, label string) db.CreateTaskTokenParams {
	t.Helper()
	return newDigestTestTokenHash(t, taskID, agentID, label+"-"+taskID)
}

func newDigestTestTokenHash(t *testing.T, taskID, agentID, tokenHash string) db.CreateTaskTokenParams {
	t.Helper()
	return db.CreateTaskTokenParams{
		TokenHash:   tokenHash,
		TaskID:      parseUUID(taskID),
		AgentID:     parseUUID(agentID),
		WorkspaceID: parseUUID(testWorkspaceID),
		UserID:      parseUUID(testUserID),
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}
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
