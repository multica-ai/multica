package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The batched skill-file read introduced for GH #7688 owns EVERY skill's files
// in one query, so a failure there is not a smaller payload — it is a
// different one: all supporting files vanish at once, and the ref hashes the
// daemon validates against are computed over that same truncated content. The
// daemon therefore accepts and caches it, and the agent runs with rules
// silently missing. These tests pin that neither claim call site nor the
// resolve endpoint dispatches on a failed read.

var errInjectedSkillFileRead = errors.New("injected skill file read failure")

// skillFileBatchFailDBTX passes every statement through to the real pool
// EXCEPT the batched skill-file read, which it fails.
type skillFileBatchFailDBTX struct{ inner db.DBTX }

func (f skillFileBatchFailDBTX) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return f.inner.Exec(ctx, sql, args...)
}

func (f skillFileBatchFailDBTX) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "ListSkillFilesBySkillIDs") {
		return nil, errInjectedSkillFileRead
	}
	return f.inner.Query(ctx, sql, args...)
}

func (f skillFileBatchFailDBTX) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.inner.QueryRow(ctx, sql, args...)
}

// newSkillFileReadFailureHandler is testHandler with the skill-file read
// broken. It shares the same pool, hub and bus so everything else on the claim
// path behaves normally.
func newSkillFileReadFailureHandler(t *testing.T) *Handler {
	t.Helper()
	return New(
		db.New(skillFileBatchFailDBTX{inner: testPool}),
		testPool,
		testHandler.Hub,
		testHandler.Bus,
		testHandler.EmailService,
		nil,
		nil,
		analytics.NoopClient{},
		Config{},
	)
}

// seedSkillLoadFixture creates a runtime, an agent carrying one workspace
// skill with a supporting file, and a queued task for it.
func seedSkillLoadFixture(t *testing.T, ctx context.Context, name string) (runtimeID, taskID, skillID string) {
	t.Helper()

	runtimeID = createClaimReclaimRuntime(t, ctx, name+" runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name+" agent")

	skillID = dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         name + "-skill",
		"description":  "Skill read failure fixture",
		"content":      "main skill content",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_skill WHERE skill_id = $1`, skillID)
		testPool.Exec(ctx, `DELETE FROM skill_file WHERE skill_id = $1`, skillID)
		testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, skillID)
	})
	dbfx.Exec(t, `INSERT INTO skill_file (skill_id, path, content) VALUES ($1, 'rules.md', 'rules content')`, skillID)
	dbfx.Exec(t, `INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`, agentID, skillID)

	taskID = dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
	})
	return runtimeID, taskID, skillID
}

// TestClaimTaskByRuntime_SkillReadFailurePreservesTask covers both claim call
// sites — the slim skill-refs claim and the legacy inline-skills claim. Each
// must refuse the claim and LEAVE THE TASK DISPATCHED so the stale-dispatched
// reclaim redelivers it, rather than settling it or handing the daemon an
// agent whose skills silently lost their files.
func TestClaimTaskByRuntime_SkillReadFailurePreservesTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name       string
		fixture    string
		capability string
	}{
		{name: "slim skill refs claim", fixture: "skillrefsfail", capability: protocol.DaemonCapabilitySkillBundlesV1},
		{name: "inline skills claim", fixture: "skillinlinefail", capability: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			failing := newSkillFileReadFailureHandler(t)
			runtimeID, taskID, _ := seedSkillLoadFixture(t, ctx, tc.fixture)

			req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "skill-read-fail-daemon")
			if tc.capability != "" {
				req.Header.Set("X-Client-Capabilities", tc.capability)
			}
			req = withURLParam(req, "runtimeId", runtimeID)
			testutil.Call(t, failing.ClaimTaskByRuntime, req).Want(http.StatusInternalServerError)

			// Preserved, not settled: still dispatched and never started, which
			// is exactly what the stale-dispatched reclaim picks back up.
			var status string
			var started bool
			dbfx.QueryRow(t, `SELECT status, started_at IS NOT NULL FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &started)
			if status != "dispatched" {
				t.Fatalf("task status = %q, want dispatched (a failed skill read must not settle the task)", status)
			}
			if started {
				t.Fatal("task was marked started despite the claim being refused")
			}
		})
	}
}

// TestResolveTaskSkillBundles_SkillReadFailureReturns500 pins the resolve side:
// a 5xx is what the daemon's existing resolve retry recovers from, and it is
// the only answer that keeps a bundle built from a failed read out of the
// daemon's on-disk cache — the daemon validates a bundle against a ref derived
// from that same bundle, so a truncated one would validate.
func TestResolveTaskSkillBundles_SkillReadFailureReturns500(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID, taskID, skillID := seedSkillLoadFixture(t, ctx, "skillresolvefail")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`, taskID)

	body := resolveSkillBundlesRequest{Skills: []resolveSkillBundleRef{{
		ID:     skillID,
		Source: "workspace",
		Hash:   "sha256:whatever",
	}}}

	// The same request succeeds on the healthy handler, so the 500 below is
	// the injected read failure and not a malformed fixture.
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/"+taskID+"/skill-bundles/resolve", body, testWorkspaceID, "skill-read-fail-daemon")
	req = withURLParams(req, "runtimeId", runtimeID, "taskId", taskID)
	testutil.Call(t, testHandler.ResolveTaskSkillBundles, req).Want(http.StatusOK)

	failing := newSkillFileReadFailureHandler(t)
	req = newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/"+taskID+"/skill-bundles/resolve", body, testWorkspaceID, "skill-read-fail-daemon")
	req = withURLParams(req, "runtimeId", runtimeID, "taskId", taskID)
	testutil.Call(t, failing.ResolveTaskSkillBundles, req).Want(http.StatusInternalServerError)
}
