package handler

// CEREBRO-PATCH(daemon-tool-policy-mandate-key-test): FIR-3403 — the local-runtime
// task mandate snapshots capability keys (built-ins as "tools:<Name>", because the
// daemon capability snapshot reports them under the "tools" kind → key
// "tools:Bash"). The local PreToolUse hook posts the BARE Claude tool name
// ("Bash"), so taskmandate.Authorize(req.ToolName) never matched the mandate and
// every built-in call on a local runtime was denied — the same class of bug
// TECH-2563 already fixed for the policy-chain resolve in the SAME handler, which
// converts through claudehook.PolicyToolKey. This test drives the real claim path
// to prove the mandate holds "tools:Bash" and that the bare name misses while the
// PolicyToolKey-converted key matches.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/cerebro/claudehook"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
	"github.com/multica-ai/multica/server/internal/cerebro/toolaccess"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLocalTaskMandateStoresCapabilityKeyNotBareToolName(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// A local CLI runtime (claude) is the consumer of the daemon tool-policy path.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, 'fir3403-mandate-runtime', 'local', 'claude', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id=$1`, runtimeID) })

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, 'fir3403-mandate-agent', '', 'local', '{}'::jsonb, $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id=$1`, agentID) })

	rtUUID, _ := util.ParseUUID(runtimeID)
	wsUUID, _ := util.ParseUUID(testWorkspaceID)
	agentUUID, _ := util.ParseUUID(agentID)

	// Report built-in tool capabilities exactly as the daemon capability snapshot
	// mirror does: kind "tools" + name → capability key "tools:<Name>"
	// (persistRuntimeCapabilitySnapshotCtx / capabilityItemsFromSnapshot).
	capReg := capabilityregistry.New(testPool)
	reporter := capabilityregistry.Subject{Type: "runtime", ID: runtimeID}
	if _, err := capReg.Report(ctx, wsUUID, reporter, []capabilityregistry.ReportInput{
		{Key: "tools:Bash", Title: "Bash", Category: "tools", Source: "runtime_report", Owners: []capabilityregistry.Subject{reporter}, Users: []capabilityregistry.Subject{reporter}},
		{Key: "tools:Read", Title: "Read", Category: "tools", Source: "runtime_report", Owners: []capabilityregistry.Subject{reporter}, Users: []capabilityregistry.Subject{reporter}},
	}); err != nil {
		t.Fatalf("report capabilities: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM cerebro_capability_subject WHERE capability_id IN (SELECT id FROM cerebro_capability WHERE workspace_id=$1 AND capability_key IN ('tools:Bash','tools:Read'))`, wsUUID)
		testPool.Exec(ctx, `DELETE FROM cerebro_capability WHERE workspace_id=$1 AND capability_key IN ('tools:Bash','tools:Read')`, wsUUID)
	})

	// Resolve the claim-time mandate the production way.
	prevAccess := testHandler.runtimeToolAccess
	testHandler.runtimeToolAccess = realToolAccessAdapter{svc: toolaccess.New(capReg, toolpolicy.NewStore(testPool))}
	t.Cleanup(func() { testHandler.runtimeToolAccess = prevAccess })

	runtime := db.AgentRuntime{ID: rtUUID, WorkspaceID: wsUUID, RuntimeMode: "local", Provider: "claude"}
	agent := &TaskAgentData{ID: agentID, OwnerID: testUserID}
	_, mandate, err := testHandler.cerebroEffectiveToolsForClaim(ctx, runtime, agent, "", "")
	if err != nil {
		t.Fatalf("cerebroEffectiveToolsForClaim: unexpected error %v", err)
	}

	// Empirical ground truth: the mandate holds the capability key, NOT the bare name.
	if !containsString(mandate, "tools:Bash") {
		t.Fatalf("mandate should snapshot the capability key 'tools:Bash'; got %v", mandate)
	}
	if containsString(mandate, "Bash") {
		t.Fatalf("mandate unexpectedly holds the bare tool name 'Bash'; got %v", mandate)
	}

	// A later runtime report is authoritative: when Bash disappears from the
	// installed surface, it disappears from Capabilities and a fresh mandate.
	if _, err := capReg.Report(ctx, wsUUID, reporter, []capabilityregistry.ReportInput{
		{Key: "tools:Read", Title: "Read", Category: "tools", Source: "runtime_report", Owners: []capabilityregistry.Subject{reporter}, Users: []capabilityregistry.Subject{reporter}},
	}); err != nil {
		t.Fatalf("replace runtime capability snapshot: %v", err)
	}
	liveCaps, err := capReg.List(ctx, wsUUID, &reporter, nil)
	if err != nil {
		t.Fatalf("list authoritative capabilities: %v", err)
	}
	for _, capability := range liveCaps {
		if capability.Key == "tools:Bash" {
			t.Fatalf("removed tool remains in Capabilities: %+v", capability)
		}
	}
	_, freshMandate, err := testHandler.cerebroEffectiveToolsForClaim(ctx, runtime, agent, "", "")
	if err != nil {
		t.Fatalf("fresh claim after runtime report: %v", err)
	}
	if containsString(freshMandate, "tools:Bash") {
		t.Fatalf("removed tool remains in fresh mandate: %v", freshMandate)
	}

	// Issue the mandate and exercise Authorize both ways.
	store := taskmandate.NewStore(testPool)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at)
		VALUES ($1, $2, 'running', 0, now()) RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id=$1`, taskID) })
	taskUUID, _ := util.ParseUUID(taskID)
	if err := store.Issue(ctx, taskUUID, wsUUID, agentUUID, mandate, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("issue mandate: %v", err)
	}

	// The bug: the bare Claude tool name the hook posts is outside the mandate.
	if err := store.Authorize(ctx, taskUUID, wsUUID, agentUUID, "Bash"); !errors.Is(err, taskmandate.ErrToolDeny) {
		t.Fatalf("raw 'Bash' should miss the capability-keyed mandate (ErrToolDeny); got %v", err)
	}
	// The fix: convert through the same PolicyToolKey the policy-chain resolve uses.
	if got := claudehook.PolicyToolKey("Bash"); got != "tools:Bash" {
		t.Fatalf("PolicyToolKey(\"Bash\") = %q, want tools:Bash", got)
	}
	if err := store.Authorize(ctx, taskUUID, wsUUID, agentUUID, claudehook.PolicyToolKey("Bash")); err != nil {
		t.Fatalf("PolicyToolKey-converted key must satisfy the mandate; got %v", err)
	}
}
