package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type delegatedChatHandoffFixture struct {
	pool          *pgxpool.Pool
	service       *TaskService
	bus           *events.Bus
	workspaceID   string
	userID        string
	runtimeID     string
	mikaID        string
	analystID     string
	chatSessionID string
	sourceTaskID  string
	installID     string
	bindingID     string
}

func newDelegatedChatHandoffFixture(t *testing.T) *delegatedChatHandoffFixture {
	t.Helper()
	ctx := context.Background()
	pool := newCancelFinalizePool(t)
	f := &delegatedChatHandoffFixture{pool: pool}
	suffix := time.Now().UnixNano()

	mustScan := func(label, query string, args []any, dest ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, query, args...).Scan(dest...); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	mustScan("create user", `INSERT INTO "user" (name,email) VALUES ('Handoff User',$1) RETURNING id`, []any{fmt.Sprintf("handoff-%d@multica.ai", suffix)}, &f.userID)
	mustScan("create workspace", `INSERT INTO workspace (name,slug,description,issue_prefix) VALUES ('Handoff', $1, '', 'HOF') RETURNING id`, []any{fmt.Sprintf("handoff-%d", suffix)}, &f.workspaceID)
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'owner')`, f.workspaceID, f.userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	mustScan("create runtime", `INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider,status,device_info,metadata,last_seen_at,visibility,owner_id) VALUES ($1,'Handoff Runtime','cloud','test','online','test','{}',now(),'private',$2) RETURNING id`, []any{f.workspaceID, f.userID}, &f.runtimeID)
	mustScan("create Mika", `INSERT INTO agent (workspace_id,name,description,runtime_mode,runtime_config,runtime_id,visibility,max_concurrent_tasks,owner_id,system_key) VALUES ($1,'Mika','','cloud','{}',$2,'private',1,$3,$4) RETURNING id`, []any{f.workspaceID, f.runtimeID, f.userID, MikaSystemKey}, &f.mikaID)
	mustScan("create analyst", `INSERT INTO agent (workspace_id,name,description,runtime_mode,runtime_config,runtime_id,visibility,max_concurrent_tasks,owner_id) VALUES ($1,'Analyst','','cloud','{}',$2,'private',1,$3) RETURNING id`, []any{f.workspaceID, f.runtimeID, f.userID}, &f.analystID)
	mustScan("create chat", `INSERT INTO chat_session (workspace_id,agent_id,creator_id,title,explicitly_created_at) VALUES ($1,$2,$3,'Mika',now()) RETURNING id`, []any{f.workspaceID, f.mikaID, f.userID}, &f.chatSessionID)
	mustScan("create source task", `INSERT INTO agent_task_queue (agent_id,runtime_id,chat_session_id,status,priority,initiator_user_id,originator_user_id,accountable_user_id,originator_source,trigger_evidence_kind,trigger_evidence_ref_id,completed_at) VALUES ($1,$2,$3,'completed',2,$4,$4,$4,'direct_human','chat',$3,now()) RETURNING id`, []any{f.mikaID, f.runtimeID, f.chatSessionID, f.userID}, &f.sourceTaskID)
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET chat_input_task_id=id WHERE id=$1`, f.sourceTaskID); err != nil {
		t.Fatalf("stamp source input owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO chat_message (chat_session_id,role,content,task_id,channel_ingested,channel_context_revision) VALUES ($1,'user','请分析这个产品方案',$2,true,1)`, f.chatSessionID, f.sourceTaskID); err != nil {
		t.Fatalf("create source message: %v", err)
	}
	mustScan("create installation", `INSERT INTO channel_installation (workspace_id,agent_id,channel_type,config,status,installer_user_id) VALUES ($1,$2,'feishu','{}','active',$3) RETURNING id`, []any{f.workspaceID, f.mikaID, f.userID}, &f.installID)
	mustScan("create binding", `INSERT INTO channel_chat_session_binding (chat_session_id,installation_id,channel_type,channel_chat_id,chat_type,last_message_id,last_thread_id,config,context_revision,route_revision) VALUES ($1,$2,'feishu','oc_chat','p2p','om_member',NULL,'{}',1,1) RETURNING id`, []any{f.chatSessionID, f.installID}, &f.bindingID)
	if _, err := pool.Exec(ctx, `INSERT INTO channel_chat_context_generation (chat_session_id,revision,initiator_user_id) VALUES ($1,1,$2)`, f.chatSessionID, f.userID); err != nil {
		t.Fatalf("create context generation: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channel_task_delivery (task_id,binding_id,installation_id,channel_type,channel_chat_id,chat_type,channel_message_id,route_revision,config) VALUES ($1,$2,$3,'feishu','oc_chat','p2p','om_member',1,'{}')`, f.sourceTaskID, f.bindingID, f.installID); err != nil {
		t.Fatalf("create source delivery: %v", err)
	}

	f.bus = events.New()
	f.service = NewTaskService(db.New(pool), pool, nil, f.bus)
	t.Cleanup(func() {
		cleanup := context.Background()
		pool.Exec(cleanup, `DELETE FROM channel_task_delivery WHERE installation_id=$1`, f.installID)
		pool.Exec(cleanup, `DELETE FROM channel_chat_context_generation WHERE chat_session_id=$1`, f.chatSessionID)
		pool.Exec(cleanup, `DELETE FROM channel_chat_session_binding WHERE installation_id=$1`, f.installID)
		pool.Exec(cleanup, `DELETE FROM channel_installation WHERE id=$1`, f.installID)
		pool.Exec(cleanup, `DELETE FROM chat_message WHERE chat_session_id=$1`, f.chatSessionID)
		pool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE agent_id IN ($1,$2)`, f.mikaID, f.analystID)
		pool.Exec(cleanup, `DELETE FROM issue WHERE workspace_id=$1`, f.workspaceID)
		pool.Exec(cleanup, `DELETE FROM chat_session WHERE id=$1`, f.chatSessionID)
		pool.Exec(cleanup, `DELETE FROM agent WHERE id IN ($1,$2)`, f.mikaID, f.analystID)
		pool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id=$1`, f.runtimeID)
		pool.Exec(cleanup, `DELETE FROM member WHERE workspace_id=$1`, f.workspaceID)
		pool.Exec(cleanup, `DELETE FROM workspace WHERE id=$1`, f.workspaceID)
		pool.Exec(cleanup, `DELETE FROM "user" WHERE id=$1`, f.userID)
	})
	return f
}

func (f *delegatedChatHandoffFixture) backgroundTaskWithStatus(t *testing.T, number int, status string) (string, string) {
	t.Helper()
	ctx := context.Background()
	var issueID, taskID string
	if err := f.pool.QueryRow(ctx, `INSERT INTO issue (workspace_id,title,status,priority,creator_id,creator_type,number,position,assignee_type,assignee_id) VALUES ($1,$2,$3,'none',$4,'agent',$5,0,'agent',$6) RETURNING id`, f.workspaceID, fmt.Sprintf("Analysis %d", number), status, f.mikaID, number, f.analystID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO agent_task_queue (agent_id,runtime_id,issue_id,status,priority,started_at,originator_user_id,accountable_user_id,originator_source,delegated_from_task_id,trigger_evidence_kind,trigger_evidence_ref_id) VALUES ($1,$2,$3,'running',2,now(),$4,$4,'delegation',$5,'delegation',$5) RETURNING id`, f.analystID, f.runtimeID, issueID, f.userID, f.sourceTaskID).Scan(&taskID); err != nil {
		t.Fatalf("create background task: %v", err)
	}
	return issueID, taskID

}

func (f *delegatedChatHandoffFixture) backgroundTask(t *testing.T, number int) string {
	t.Helper()
	_, taskID := f.backgroundTaskWithStatus(t, number, "in_review")
	return taskID
}

func (f *delegatedChatHandoffFixture) complete(t *testing.T, taskID string) {
	t.Helper()
	result, _ := json.Marshal(protocol.TaskCompletedPayload{Output: "analysis complete"})
	if _, err := f.service.CompleteTask(context.Background(), util.MustParseUUID(taskID), result, "", "", "", false, "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
}

func TestDelegatedReviewCompletionReturnsToMikaOncePerMemberTurn(t *testing.T) {
	f := newDelegatedChatHandoffFixture(t)
	first := f.backgroundTask(t, 1)
	f.complete(t, first)

	var handoffTaskID, note, evidenceRef string
	if err := f.pool.QueryRow(context.Background(), `SELECT id::text, handoff_note, trigger_evidence_ref_id::text FROM agent_task_queue WHERE chat_session_id=$1 AND trigger_evidence_kind='delegated_completion'`, f.chatSessionID).Scan(&handoffTaskID, &note, &evidenceRef); err != nil {
		t.Fatalf("load Mika handoff task: %v", err)
	}
	if evidenceRef != first {
		t.Fatalf("handoff evidence = %s, want completed task %s", evidenceRef, first)
	}
	if note == "" {
		t.Fatal("handoff task has no run-scoped instruction")
	}
	if !strings.Contains(note, "recommended answer") {
		t.Fatalf("handoff note does not preserve the grill-me recommendation contract: %q", note)
	}
	var kind string
	var channelIngested bool
	if err := f.pool.QueryRow(context.Background(), `SELECT message_kind,channel_ingested FROM chat_message WHERE task_id=$1 AND role='user'`, handoffTaskID).Scan(&kind, &channelIngested); err != nil {
		t.Fatalf("load hidden handoff input: %v", err)
	}
	if kind != protocol.ChatMessageKindDelegationHandoff || !channelIngested {
		t.Fatalf("handoff input = kind %q channel_ingested %v", kind, channelIngested)
	}
	var deliveries int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM channel_task_delivery WHERE task_id=$1`, handoffTaskID).Scan(&deliveries); err != nil || deliveries != 1 {
		t.Fatalf("handoff delivery snapshots = %d err=%v, want 1", deliveries, err)
	}

	// Replaying the same completion cannot create a second handoff.
	f.complete(t, first)
	var handoffTasks int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND trigger_evidence_kind='delegated_completion'`, f.chatSessionID).Scan(&handoffTasks); err != nil || handoffTasks != 1 {
		t.Fatalf("handoff tasks after replay = %d err=%v, want 1", handoffTasks, err)
	}

	// A second background completion before the member answers is queued as
	// hidden context for their next turn, not emitted as another Mika question.
	second := f.backgroundTask(t, 2)
	f.complete(t, second)
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND trigger_evidence_kind='delegated_completion'`, f.chatSessionID).Scan(&handoffTasks); err != nil || handoffTasks != 1 {
		t.Fatalf("handoff tasks after second completion = %d err=%v, want 1", handoffTasks, err)
	}
	var pending int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM chat_message WHERE chat_session_id=$1 AND message_kind='delegation_handoff' AND task_id IS NULL`, f.chatSessionID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("pending handoffs = %d err=%v, want 1", pending, err)
	}
}

func TestDelegatedTodoCompletionFallsBackToReviewAndReturnsToMika(t *testing.T) {
	f := newDelegatedChatHandoffFixture(t)
	issueID, taskID := f.backgroundTaskWithStatus(t, 1, "todo")
	var statusEvents []events.Event
	f.bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		statusEvents = append(statusEvents, e)
	})

	f.complete(t, taskID)

	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if status != "in_review" {
		t.Fatalf("issue status = %q, want in_review", status)
	}

	var handoffTasks int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND trigger_evidence_kind='delegated_completion'`, f.chatSessionID).Scan(&handoffTasks); err != nil {
		t.Fatalf("count Mika handoff tasks: %v", err)
	}
	if handoffTasks != 1 {
		t.Fatalf("Mika handoff tasks = %d, want 1", handoffTasks)
	}
	if len(statusEvents) != 1 {
		t.Fatalf("issue status events = %d, want 1", len(statusEvents))
	}
	payload, ok := statusEvents[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("issue status payload is %T, want map[string]any", statusEvents[0].Payload)
	}
	if payload[TaskCompletionStatusFallbackField] != true || payload["prev_status"] != "todo" || payload["source_task_id"] != taskID {
		t.Fatalf("unexpected issue status fallback payload: %#v", payload)
	}

	// Replaying the terminal callback must not create another handoff or rewrite
	// the issue status a second time.
	f.complete(t, taskID)
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND trigger_evidence_kind='delegated_completion'`, f.chatSessionID).Scan(&handoffTasks); err != nil {
		t.Fatalf("count Mika handoff tasks after replay: %v", err)
	}
	if handoffTasks != 1 {
		t.Fatalf("Mika handoff tasks after replay = %d, want 1", handoffTasks)
	}
	if len(statusEvents) != 1 {
		t.Fatalf("issue status events after replay = %d, want 1", len(statusEvents))
	}
}

func TestDelegatedTodoCompletionDoesNotSettleWhileFollowUpWorkExists(t *testing.T) {
	f := newDelegatedChatHandoffFixture(t)
	issueID, taskID := f.backgroundTaskWithStatus(t, 1, "todo")
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			originator_user_id, accountable_user_id, originator_source,
			delegated_from_task_id, trigger_evidence_kind, trigger_evidence_ref_id
		) VALUES ($1,$2,$3,'queued',2,$4,$4,'delegation',$5,'delegation',$5)
	`, f.analystID, f.runtimeID, issueID, f.userID, f.sourceTaskID); err != nil {
		t.Fatalf("create follow-up task: %v", err)
	}

	f.complete(t, taskID)

	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if status != "todo" {
		t.Fatalf("issue status = %q, want todo while follow-up work exists", status)
	}
	var handoffTasks int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1 AND trigger_evidence_kind='delegated_completion'`, f.chatSessionID).Scan(&handoffTasks); err != nil {
		t.Fatalf("count Mika handoff tasks: %v", err)
	}
	if handoffTasks != 0 {
		t.Fatalf("Mika handoff tasks = %d, want 0 while follow-up work exists", handoffTasks)
	}
}

func TestDelegatedTaskOwnsIssue(t *testing.T) {
	agentID := util.MustParseUUID("00000000-0000-0000-0000-000000000001")
	otherID := util.MustParseUUID("00000000-0000-0000-0000-000000000002")
	squadID := util.MustParseUUID("00000000-0000-0000-0000-000000000003")

	tests := []struct {
		name  string
		task  db.AgentTaskQueue
		issue db.Issue
		want  bool
	}{
		{
			name:  "current agent owns issue",
			task:  db.AgentTaskQueue{AgentID: agentID},
			issue: db.Issue{AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: agentID},
			want:  true,
		},
		{
			name:  "different agent owns issue",
			task:  db.AgentTaskQueue{AgentID: agentID},
			issue: db.Issue{AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: otherID},
		},
		{
			name:  "squad leader owns workflow",
			task:  db.AgentTaskQueue{AgentID: agentID, SquadID: squadID, IsLeaderTask: true},
			issue: db.Issue{AssigneeType: pgtype.Text{String: "squad", Valid: true}, AssigneeID: squadID},
			want:  true,
		},
		{
			name:  "squad member does not own workflow",
			task:  db.AgentTaskQueue{AgentID: agentID, SquadID: squadID},
			issue: db.Issue{AssigneeType: pgtype.Text{String: "squad", Valid: true}, AssigneeID: squadID},
		},
		{
			name:  "member owned issue",
			task:  db.AgentTaskQueue{AgentID: agentID},
			issue: db.Issue{AssigneeType: pgtype.Text{String: "member", Valid: true}, AssigneeID: otherID},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := delegatedTaskOwnsIssue(tc.task, tc.issue); got != tc.want {
				t.Fatalf("delegatedTaskOwnsIssue() = %v, want %v", got, tc.want)
			}
		})
	}
}
