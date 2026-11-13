package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func requireWorkspaceDispatchPayload(t *testing.T, fb *fakeBroadcaster, want map[string]string) {
	t.Helper()
	found := 0
	for _, call := range fb.workspaceCalls {
		var frame struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(call.msg, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Type != protocol.EventTaskDispatch {
			continue
		}
		found++
		expected := make(map[string]any, len(want))
		for key, value := range want {
			expected[key] = value
		}
		if !reflect.DeepEqual(frame.Payload, expected) {
			t.Errorf("workspace dispatch payload = %v, want only routing ids %v", frame.Payload, expected)
		}
	}
	if found != 1 {
		t.Fatalf("workspace dispatch events = %d, want 1", found)
	}
}

func TestPrivateProjectQuickCreateDispatchOmitsPrompt(t *testing.T) {
	ctx := context.Background()
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	project := fx.Project(t, "Private client project", testutil.Cols{"visibility": "private"})
	runtime := fx.Runtime(t, "Dispatch runtime")
	agent := fx.Agent(t, "Dispatch agent", runtime)
	q := db.New(testPool)
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb, newPrivateProjectAudience(q))
	tasks := service.NewTaskService(q, testPool, nil, bus)
	const prompt = "Confidential client acquisition terms"
	task, err := tasks.EnqueueQuickCreateTask(ctx, ppUUID(testWorkspaceID), ppUUID(testUserID), ppUUID(agent), pgtype.UUID{}, prompt, "", "", ppUUID(project), pgtype.UUID{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id=$1`, task.ID)
	})
	claimed, err := tasks.ClaimTask(ctx, ppUUID(agent))
	if err != nil || claimed == nil {
		t.Fatalf("claim: task=%v error=%v", claimed, err)
	}
	var taskContext service.QuickCreateContext
	if err := json.Unmarshal(claimed.Context, &taskContext); err != nil || taskContext.Prompt != prompt {
		t.Fatalf("claimed task must retain its prompt for the daemon: context=%s error=%v", claimed.Context, err)
	}
	requireWorkspaceDispatchPayload(t, fb, map[string]string{
		"task_id": util.UUIDToString(claimed.ID), "agent_id": agent, "runtime_id": runtime, "issue_id": "",
	})
}

func TestTaskDispatchKeepsIssueAndChatRoutingWithoutContext(t *testing.T) {
	for _, kind := range []string{"issue", "chat"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			fx := testutil.New(testPool, testWorkspaceID, testUserID)
			runtime := fx.Runtime(t, "Dispatch runtime")
			agent := fx.Agent(t, "Dispatch agent", runtime)
			cols := testutil.Cols{"runtime_id": runtime, "context": `{"prompt":"private context","future_field":"private data"}`}
			want := map[string]string{"agent_id": agent, "runtime_id": runtime, "issue_id": ""}
			if kind == "issue" {
				issue := fx.Issue(t, "Dispatch issue")
				cols["issue_id"], want["issue_id"] = issue, issue
			} else {
				chat := fx.ChatSession(t, agent)
				cols["chat_session_id"], want["chat_session_id"] = chat, chat
			}
			want["task_id"] = fx.Task(t, agent, cols)
			q := db.New(testPool)
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListeners(bus, fb, newPrivateProjectAudience(q))
			tasks := service.NewTaskService(q, testPool, nil, bus)
			if claimed, err := tasks.ClaimTask(ctx, ppUUID(agent)); err != nil || claimed == nil {
				t.Fatalf("claim: task=%v error=%v", claimed, err)
			}
			requireWorkspaceDispatchPayload(t, fb, want)
		})
	}
}
