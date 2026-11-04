package main

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestChatTriggerSealedInput(t *testing.T) {
	if testPool == nil {
		t.Skip("requires test database")
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtime := fx.Runtime(t, "trigger-regression")
	agent := fx.Agent(t, "trigger-regression", runtime)
	session := fx.ChatSession(t, agent)
	task := fx.Task(t, agent, testutil.Cols{"status": "completed", "runtime_id": runtime, "chat_session_id": session})
	if _, err := testPool.Exec(context.Background(), "UPDATE agent_task_queue SET chat_input_task_id=id WHERE id=$1", task); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"finance request", " ", "keep original spacing "} {
		fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": session, "task_id": task, "role": "user", "content": content, "channel_ingested": true})
	}
	fx.InsertNoID(t, "channel_task_delivery", testutil.Cols{"task_id": task, "binding_id": uuid.NewString(), "installation_id": uuid.NewString(), "channel_type": "slack", "channel_chat_id": "D-finance:100.001", "chat_type": "private", "channel_message_id": "100.001", "route_revision": 1, "config": `{"channel_id":"D-finance","secret":"never-return"}`}, "task_id=$1", task)
	second := fx.Task(t, agent, testutil.Cols{"status": "completed", "runtime_id": runtime, "chat_session_id": session})
	fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": session, "task_id": second, "role": "user", "content": "new ops request must not replace finance"})
	token := mintAgentTaskToken(t, agent, task, testUserID)
	read := func(token string, want int) map[string]any {
		t.Helper()
		req, err := http.NewRequest("GET", testServer.URL+"/api/chat/trigger?task_id="+second, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Task-ID", second)
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Workspace-ID", uuid.NewString())
		if token == testToken {
			req.Header.Set("X-Workspace-ID", testWorkspaceID)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != want {
			t.Fatalf("status=%d want=%d body=%v", res.StatusCode, want, out)
		}
		return out
	}
	want := map[string]any{"task_id": task, "input": "finance request\n\nkeep original spacing ", "channel_type": "slack", "channel_id": "D-finance", "trigger_message_id": "100.001", "thread_id": ""}
	if got := read(token, 200); !reflect.DeepEqual(got, want) {
		t.Fatalf("trigger=%v want=%v", got, want)
	}
	read(testToken, 403)
	nonChat := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "status": "completed"})
	read(mintAgentTaskToken(t, agent, nonChat, testUserID), 400)
	// Web-origin input must never acquire an external reply target.
	if _, err := testPool.Exec(context.Background(), "UPDATE chat_message SET channel_ingested=false WHERE task_id=$1", task); err != nil {
		t.Fatal(err)
	}
	read(token, 409)
	if _, err := testPool.Exec(context.Background(), "UPDATE chat_message SET channel_ingested=true WHERE task_id=$1", task); err != nil {
		t.Fatal(err)
	}
	// Retries retain the original sealed input, but use their own delivery snapshot.
	retry := fx.Task(t, agent, testutil.Cols{"status": "completed", "runtime_id": runtime, "chat_session_id": session, "chat_input_task_id": task})
	fx.InsertNoID(t, "channel_task_delivery", testutil.Cols{"task_id": retry, "binding_id": uuid.NewString(), "installation_id": uuid.NewString(), "channel_type": "slack", "channel_chat_id": "D-retry", "chat_type": "private", "channel_message_id": "101.001", "channel_thread_id": "99.001", "route_revision": 1}, "task_id=$1", retry)
	want["task_id"], want["channel_id"], want["trigger_message_id"], want["thread_id"] = retry, "D-retry", "101.001", "99.001"
	if got := read(mintAgentTaskToken(t, agent, retry, testUserID), 200); !reflect.DeepEqual(got, want) {
		t.Fatalf("retry=%v want=%v", got, want)
	}
	// Missing ownership, missing delivery, empty input, and corrupt routing fail closed.
	read(mintAgentTaskToken(t, agent, second, testUserID), 409)
	for _, statement := range []string{
		"UPDATE channel_task_delivery SET channel_chat_id='',config='{}' WHERE task_id=$1",
		"DELETE FROM channel_task_delivery WHERE task_id=$1",
	} {
		if _, err := testPool.Exec(context.Background(), statement, task); err != nil {
			t.Fatal(err)
		}
		read(token, 409)
	}
	// Native input remains readable without granting any external destination.
	if _, err := testPool.Exec(context.Background(), "UPDATE chat_message SET channel_ingested=false WHERE task_id=$1", task); err != nil {
		t.Fatal(err)
	}
	native := map[string]any{"task_id": task, "input": "finance request\n\nkeep original spacing ", "channel_type": "", "channel_id": "", "trigger_message_id": "", "thread_id": ""}
	if got := read(token, 200); !reflect.DeepEqual(got, native) {
		t.Fatalf("native trigger=%v want=%v", got, native)
	}
	if _, err := testPool.Exec(context.Background(), "UPDATE agent_task_queue SET chat_input_task_id=$2 WHERE id=$1", retry, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	read(mintAgentTaskToken(t, agent, retry, testUserID), 409)
	// A valid task token must not expose input from a foreign session/workspace.
	foreignWorkspace := fx.Workspace(t, "Foreign trigger workspace", "trigger-"+uuid.NewString())
	foreignSession := fx.ChatSession(t, agent, testutil.Cols{"workspace_id": foreignWorkspace})
	foreign := fx.Task(t, agent, testutil.Cols{"status": "completed", "runtime_id": runtime, "chat_session_id": foreignSession})
	read(mintAgentTaskToken(t, agent, foreign, testUserID), 403)
}
