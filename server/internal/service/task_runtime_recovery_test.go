package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestDelegatedFailureRecoveryKeepsPersonalRouteWithoutDefault(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	fx := testutil.New(f.pool, f.workspaceID, f.userID)
	userB := fx.User(t, "personal executor", "personal-recovery-"+f.workspaceID+"@example.com")
	fx.Member(t, f.workspaceID, userB, "member")
	runtimeB := fx.Runtime(t, "personal recovery", testutil.Cols{"owner_id": userB, "provider": "codex"})
	for _, agent := range []string{f.coordinator, f.worker} {
		fx.Exec(t, `UPDATE agent SET permission_mode='public_to' WHERE id=$1`, agent)
		fx.InsertNoID(t, "agent_invocation_target", testutil.Cols{"agent_id": agent, "target_type": "member", "target_id": userB}, "agent_id=$1 AND target_id=$2", agent, userB)
	}
	model := "personal-model"
	routing := RuntimeRouting{Version: 1, ExecutionUserID: userB, Routes: map[string]RuntimeRoute{}}
	for _, agent := range []string{f.coordinator, f.worker} {
		routing.Routes[agent] = RuntimeRoute{RuntimeID: runtimeB, RuntimeOwnerID: userB, Provider: "codex", Source: "personal", Model: &model}
	}
	raw, err := json.Marshal(routing)
	if err != nil {
		t.Fatal(err)
	}
	failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
	fx.Exec(t, `UPDATE agent_task_queue SET runtime_id=$2,runtime_routing=$3 WHERE id=$1 OR id=$4`, f.sourceTask, runtimeB, raw, failedID)
	fx.Exec(t, `UPDATE agent SET runtime_id=NULL WHERE id=$1`, f.coordinator)
	ctx := context.Background()
	target, created, err := svc.ensureDelegatedFailureRecoveryComment(ctx, failedID)
	if err != nil || !created || target == nil {
		t.Fatalf("recovery signal: %v %v", created, err)
	}
	// Simulate a restart between durable signal creation and its dispatch.
	result, err := svc.RecoverPendingDelegatedFailures(ctx, 100)
	if err != nil || result.Replayed != 1 {
		t.Fatalf("recovery sweep: %+v %v", result, err)
	}
	var runtimeID string
	var snapshot []byte
	if err := f.pool.QueryRow(ctx, `SELECT runtime_id::text,runtime_routing FROM agent_task_queue WHERE trigger_evidence_kind='delegated_failure' AND trigger_evidence_ref_id=$1`, failedID).Scan(&runtimeID, &snapshot); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseRuntimeRouting(snapshot)
	if err != nil || parsed == nil || parsed.ExecutionUserID != userB || runtimeID != runtimeB {
		t.Fatalf("recovery switched execution user or runtime: %s %s %v", runtimeID, snapshot, err)
	}
	route, err := parsed.Route(f.coordinator)
	if err != nil || route.Model == nil || *route.Model != model {
		t.Fatalf("recovery changed frozen model: %+v %v", route, err)
	}
	if _, err := svc.EffectiveRuntimeForUser(ctx, target.agent, util.MustParseUUID(userB)); err == nil {
		t.Fatal("fixture must have no current default or personal preference: only the frozen chain permits recovery")
	}
}
