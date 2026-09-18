package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestParseRuntimeRouting(t *testing.T) {
	for _, raw := range []string{"", "null"} {
		routing, err := ParseRuntimeRouting([]byte(raw))
		if err != nil || routing != nil {
			t.Fatalf("legacy snapshot %q: routing=%v err=%v", raw, routing, err)
		}
	}
	for _, raw := range []string{
		`{`, `{"version":2,"execution_user_id":"11111111-1111-1111-1111-111111111111","routes":{}}`,
		`{"version":1,"execution_user_id":"invalid","routes":{}}`,
		`{"version":1,"execution_user_id":"11111111-1111-1111-1111-111111111111"}`,
	} {
		if _, err := ParseRuntimeRouting([]byte(raw)); err == nil {
			t.Fatalf("accepted malformed snapshot %q", raw)
		}
	}
}

func TestPersonalTaskRuntimeRouting(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "qwen"} {
		for _, target := range []string{"claude", "codex", "qwen"} {
			t.Run(provider+"-to-"+target, func(t *testing.T) { testPersonalTaskRuntimeRouting(t, provider, target) })
		}
	}
}

func testPersonalTaskRuntimeRouting(t *testing.T, provider, target string) {
	pool := sharedTestPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(sql string, args ...any) pgtype.UUID {
		t.Helper()
		var out pgtype.UUID
		if err := tx.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	owner := id(`INSERT INTO "user"(name,email) VALUES ('owner',gen_random_uuid()::text || '@routing.test') RETURNING id`)
	user := id(`INSERT INTO "user"(name,email) VALUES ('executor',gen_random_uuid()::text || '@routing.test') RETURNING id`)
	ws := id(`INSERT INTO workspace(name,slug) VALUES ('routing',gen_random_uuid()::text) RETURNING id`)
	exec(`INSERT INTO member(workspace_id,user_id,role) VALUES ($1,$2,'owner'),($1,$3,'member')`, ws, owner, user)
	runtime := func(owner pgtype.UUID) pgtype.UUID {
		return id(`INSERT INTO agent_runtime(workspace_id,name,runtime_mode,provider,status,owner_id,visibility,daemon_id) VALUES ($1,'test','local',$3,'online',$2,'public',gen_random_uuid()::text) RETURNING id`, ws, owner, provider)
	}
	defaultRuntime, personalRuntime, nextRuntime := runtime(owner), runtime(user), runtime(user)
	exec(`UPDATE agent_runtime SET provider=$2 WHERE id IN ($1,$3)`, personalRuntime, target, nextRuntime)
	makeAgent := func() pgtype.UUID {
		agent := id(`INSERT INTO agent(workspace_id,name,runtime_mode,owner_id,runtime_id,permission_mode) VALUES ($1,gen_random_uuid()::text,'local',$2,$3,'public_to') RETURNING id`, ws, owner, defaultRuntime)
		exec(`INSERT INTO agent_invocation_target(agent_id,target_type,target_id) VALUES ($1,'workspace',$2)`, agent, ws)
		return agent
	}
	agentID, childAgentID := makeAgent(), makeAgent()
	q := db.New(tx)
	svc := NewTaskService(q, nil, nil, events.New())
	pref := func(agentID, runtimeID pgtype.UUID) {
		t.Helper()
		_, err := q.UpsertAgentRuntimePreference(ctx, db.UpsertAgentRuntimePreferenceParams{WorkspaceID: ws, UserID: user, AgentID: agentID, RuntimeID: runtimeID, ModelMode: "runtime_default"})
		if err != nil {
			t.Fatal(err)
		}
	}
	agent, err := q.GetAgent(ctx, agentID)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := svc.EffectiveRuntimeForUser(ctx, agent, user)
	if err != nil || selected != defaultRuntime {
		t.Fatalf("default selection: %v %v", selected, err)
	}
	pref(agentID, personalRuntime)
	pref(childAgentID, personalRuntime)
	issueID := id(`INSERT INTO issue(workspace_id,title,status,priority,creator_id,creator_type,assignee_type,assignee_id,number,position) VALUES ($1,'root','todo','none',$2,'member','agent',$3,1,0) RETURNING id`, ws, user, agentID)
	issue, err := q.GetIssue(ctx, issueID)
	if err != nil {
		t.Fatal(err)
	}
	root, err := svc.EnqueueTaskForIssue(ctx, issue)
	if err != nil || root.RuntimeID != personalRuntime {
		t.Fatalf("enqueue personal issue: runtime=%v err=%v", root.RuntimeID, err)
	}
	snapshot, err := ParseRuntimeRouting(root.RuntimeRouting)
	if err != nil || snapshot == nil || snapshot.ExecutionUserID != util.UUIDToString(user) {
		t.Fatalf("root snapshot: %v %v", snapshot, err)
	}
	// Changing both preference and global default cannot redirect descendants.
	pref(agentID, nextRuntime)
	pref(childAgentID, nextRuntime)
	exec(`UPDATE agent SET runtime_id=$2 WHERE id=$1`, agentID, nextRuntime)
	commentID := id(`INSERT INTO comment(issue_id,workspace_id,author_type,author_id,content,source_task_id) VALUES ($1,$2,'agent',$3,'continue',$4) RETURNING id`, issueID, ws, agentID, root.ID)
	child, err := svc.enqueueMentionTask(ctx, issue, childAgentID, commentID, false, pgtype.UUID{}, false, "", pgtype.UUID{}, pgtype.UUID{}, OriginDerived)
	if err != nil || child.RuntimeID != personalRuntime {
		t.Fatalf("descendant changed runtime: %v %v", child.RuntimeID, err)
	}
	var rootJSON, childJSON any
	json.Unmarshal(root.RuntimeRouting, &rootJSON)
	json.Unmarshal(child.RuntimeRouting, &childJSON)
	if !reflect.DeepEqual(rootJSON, childJSON) {
		t.Fatal("descendant did not inherit complete root snapshot")
	}
	// Claim by the task's selected runtime works after a global agent rebind.
	claimed, err := svc.claimTask(ctx, agentID, personalRuntime)
	if err != nil || claimed == nil || claimed.ID != root.ID {
		t.Fatalf("personal runtime could not claim root: %v %v", claimed, err)
	}
	exec(`UPDATE agent_task_queue SET status='failed' WHERE id=$1`, root.ID)
	rerun, err := svc.enqueueIssueTask(ctx, issue, commentID, true, "", user, root.ID, pgtype.Timestamptz{}, OriginDerived)
	if err != nil || rerun.RuntimeID != nextRuntime {
		t.Fatalf("manual rerun did not resolve new root: %v %v", rerun.RuntimeID, err)
	}
	// A deleted preference target stays invalid rather than using the default.
	missing := util.MustParseUUID("ffffffff-ffff-ffff-ffff-ffffffffffff")
	pref(agentID, missing)
	if _, err := svc.EffectiveRuntimeForUser(ctx, agent, user); !errors.Is(err, ErrTaskRuntimeUnavailable) {
		t.Fatalf("invalid preference silently fell back: %v", err)
	}
	// Attribution fallback must not select somebody else's personal machine.
	legacy, err := svc.resolveTaskRuntime(ctx, q, agent, pgtype.UUID{}, pgtype.UUID{})
	if err != nil || len(legacy.Routing) != 0 || legacy.RuntimeID != defaultRuntime {
		t.Fatalf("unattributed root inherited preferences: %v %v", legacy, err)
	}
	// Source permission revocation invalidates the original frozen route too.
	exec(`DELETE FROM agent_invocation_target WHERE agent_id=$1`, childAgentID)
	childAgent, _ := q.GetAgent(ctx, childAgentID)
	if _, err := svc.resolveTaskRuntime(ctx, q, childAgent, user, root.ID); !errors.Is(err, ErrTaskRuntimeUnavailable) {
		t.Fatalf("revoked descendant accepted: %v", err)
	}
}

func TestPersonalRuntimeUsesExecutionUserConnectedApps(t *testing.T) {
	owner := util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	user := util.MustParseUUID("22222222-2222-2222-2222-222222222222")
	builder := &stubOverlayBuilder{resp: json.RawMessage(`{"mcpServers":{}}`)}
	svc := &TaskService{Composio: builder, FeatureFlags: composioMCPAppsTestFlags(true)}
	agent := db.Agent{OwnerID: owner}
	svc.buildRoutedRuntimeMCPOverlay(context.Background(), owner, agent, taskRuntimeSelection{Source: "personal", ExecutionUserID: user})
	if builder.lastUser != user || builder.lastAgent.OwnerID != user {
		t.Fatal("personal execution requested source owner's connected apps")
	}
	if agent.OwnerID != owner {
		t.Fatal("shared agent identity was mutated")
	}
}

func TestRuntimeRoutingSelectionIsFrozen(t *testing.T) {
	const agentID = "11111111-1111-1111-1111-111111111111"
	routing := RuntimeRouting{Version: 1, ExecutionUserID: "22222222-2222-2222-2222-222222222222", Routes: map[string]RuntimeRoute{
		agentID: {RuntimeID: "33333333-3333-3333-3333-333333333333", RuntimeOwnerID: "22222222-2222-2222-2222-222222222222", Provider: "claude-code", Source: "personal"},
	}}
	raw, err := json.Marshal(routing)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseRuntimeRouting(raw)
	if err != nil {
		t.Fatal(err)
	}
	route, err := parsed.Route(agentID)
	if err != nil || route != routing.Routes[agentID] {
		t.Fatalf("route changed: %v %v", route, err)
	}
	if _, err := parsed.Route("44444444-4444-4444-4444-444444444444"); err == nil {
		t.Fatal("a descendant absent from the root snapshot must not pick current preferences")
	}
	parsed.Routes[agentID] = RuntimeRoute{RuntimeID: route.RuntimeID, Source: "personal"}
	if _, err := parsed.Route(agentID); err == nil {
		t.Fatal("accepted deleted runtime without owner/provider")
	}
}
