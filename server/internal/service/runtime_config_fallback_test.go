package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// The resolution order puts a usable personal runtime above a waiting offline
// one: an offline recommended-CLI match must not mask another online machine
// the originator owns. With no online machine left, the offline match stays so
// admission can wait for it.
func TestResolveEffectiveRuntimeConfigFallsBackToOnlinePersonalRuntime(t *testing.T) {
	ctx := context.Background()
	f, ownerID := newPrincipalFixture(t)
	offline := f.Runtime(t, fmt.Sprintf("rt-offline-%d", principalSeq.Add(1)), testutil.Cols{
		"owner_id": ownerID, "provider": "codex", "status": "offline",
	})
	online := f.Runtime(t, fmt.Sprintf("rt-online-%d", principalSeq.Add(1)), testutil.Cols{
		"owner_id": ownerID, "provider": "claude", "status": "online",
	})
	// Fixture.Agent binds a runtime by default; a smart template stores none,
	// so the bind is cleared after creation and the recommendation stays a CLI.
	agentID := f.Agent(t, fmt.Sprintf("agent-smart-%d", principalSeq.Add(1)), online, testutil.Cols{
		"share_runtime": false, "recommended_cli": "codex", "owner_id": ownerID,
	})
	if _, err := f.Pool.Exec(ctx, "UPDATE agent SET runtime_id = NULL WHERE id = $1", util.MustParseUUID(agentID)); err != nil {
		t.Fatal(err)
	}
	agent, err := f.q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	svc := &TaskService{Queries: f.q, TxStarter: f.Pool, Bus: events.New()}

	effective, rerr := svc.ResolveEffectiveRuntimeConfig(ctx, agent, util.MustParseUUID(ownerID))
	if rerr != nil {
		t.Fatalf("resolve with an offline CLI match and an online personal runtime: %v", rerr)
	}
	if util.UUIDToString(effective.RuntimeID) != online || effective.Source != RuntimeConfigSourceDefaultUserRuntime {
		t.Fatalf("resolution landed on %s (source %s), want the online personal runtime %s as %s",
			util.UUIDToString(effective.RuntimeID), effective.Source, online, RuntimeConfigSourceDefaultUserRuntime)
	}

	// Without an online machine the offline match is preserved so a caller can
	// wait for it instead of losing the recommendation.
	if _, err := f.Pool.Exec(ctx, "UPDATE agent_runtime SET status='offline' WHERE id=$1", util.MustParseUUID(online)); err != nil {
		t.Fatal(err)
	}
	effective, rerr = svc.ResolveEffectiveRuntimeConfig(ctx, agent, util.MustParseUUID(ownerID))
	if rerr == nil {
		t.Fatalf("all-offline resolution succeeded on %s, want an offline verdict", util.UUIDToString(effective.RuntimeID))
	}
	status, _, ok := RuntimeConfigResolutionFromError(rerr)
	if !ok || status != RuntimeConfigResolutionRuntimeOffline {
		t.Fatalf("offline verdict = %v (ok %v), want %s", status, ok, RuntimeConfigResolutionRuntimeOffline)
	}
	_ = offline
}

// Sticky recovery: after a smart task ran on Alice's machine, a later trigger
// from Bob on the same issue continues on that run's owner, not on Bob's own
// machine. An explicit override still wins.
func TestResolveOriginatorForIssueTaskRecoversLatestRuntimeOwner(t *testing.T) {
	ctx := context.Background()
	f, aliceID := newPrincipalFixture(t)
	bobID := f.member(t, "sticky bob")
	aliceRuntime := f.Runtime(t, fmt.Sprintf("rt-alice-%d", principalSeq.Add(1)), testutil.Cols{
		"owner_id": aliceID, "provider": "codex", "status": "online",
	})
	agentID := f.Agent(t, fmt.Sprintf("agent-sticky-%d", principalSeq.Add(1)), aliceRuntime, testutil.Cols{
		"share_runtime": false, "recommended_cli": "codex", "owner_id": aliceID,
	})
	if _, err := f.Pool.Exec(ctx, "UPDATE agent SET runtime_id = NULL, recommended_cli = 'codex', share_runtime = FALSE WHERE id = $1", util.MustParseUUID(agentID)); err != nil {
		t.Fatal(err)
	}
	issueID := f.Issue(t, "Sticky recovery", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
	f.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", issueID)

	// Alice's completed smart run: the stamp names the personal source, so the
	// latest-task recovery reads the machine owner.
	if _, err := f.Pool.Exec(ctx, `INSERT INTO agent_task_queue
		(agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id,
		 effective_model, runtime_config_source, created_at)
		VALUES ($1, $2, $3, 'completed', 4, $4, $4, 'codex-test-model', 'recommended_match', now() - interval '1 minute')`,
		util.MustParseUUID(agentID), util.MustParseUUID(aliceRuntime), util.MustParseUUID(issueID), util.MustParseUUID(aliceID)); err != nil {
		t.Fatal(err)
	}

	svc := &TaskService{Queries: f.q, TxStarter: f.Pool, Bus: events.New()}
	issue, err := f.q.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	recovered := svc.resolveOriginatorForIssueTask(ctx, issue, pgtype.UUID{})
	if util.UUIDToString(recovered) != aliceID {
		t.Fatalf("sticky recovery returned %s, want the latest run's owner %s", util.UUIDToString(recovered), aliceID)
	}

	// The enqueue path honors an explicit takeover over the recovered owner.
	override := svc.resolveOriginatorForIssueTaskWithOverride(ctx, issue, pgtype.UUID{}, util.MustParseUUID(bobID))
	if util.UUIDToString(override) != bobID {
		t.Fatalf("explicit override returned %s, want the requester %s", util.UUIDToString(override), bobID)
	}

	// End to end: Bob's comment-less trigger queues on Alice's machine.
	task, err := svc.EnqueueTaskForIssueByActor(ctx, issue, util.MustParseUUID(bobID))
	if err != nil {
		t.Fatal(err)
	}
	if util.UUIDToString(task.RuntimeID) != aliceRuntime {
		t.Fatalf("follow-up runtime = %s, want Alice's machine %s", util.UUIDToString(task.RuntimeID), aliceRuntime)
	}
}
