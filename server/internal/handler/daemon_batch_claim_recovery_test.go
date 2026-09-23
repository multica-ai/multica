package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Expire the request only after the service has committed the claim. The next
// request uses the same live WebSocket and database, without the injected stall.
type expireClaimBuildDB struct {
	db.DBTX
	expired bool
}

func (d *expireClaimBuildDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if !d.expired && strings.Contains(query, "-- name: GetWorkspace :one") {
		d.expired = true
		<-ctx.Done()
		return errRow{err: ctx.Err()}
	}
	return d.DBTX.QueryRow(ctx, query, args...)
}

func TestClaimTasksByRuntime_DeadlineRecoveryOverWebSocket(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Claim deadline runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Claim deadline agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	h := *testHandler
	h.Queries = db.New(&expireClaimBuildDB{DBTX: testPool})
	svc := *testHandler.TaskService
	h.TaskService = &svc
	hub := daemonws.NewHub()
	svc.Wakeup = hub
	hub.SetRPCHandler(h.DaemonRPCHandler)
	identity := daemonws.ClientIdentity{
		DaemonID: batchClaimTestDaemonID, UserID: testUserID,
		WorkspaceID: testWorkspaceID, WorkspaceIDs: []string{testWorkspaceID},
		RuntimeIDs: []string{runtimeID}, Capabilities: protocol.DaemonCapabilityClaimPollHintsV1,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	woke := false
	claim := func(requestID string) batchClaimResponse {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"daemon_id": batchClaimTestDaemonID, "runtime_ids": []string{runtimeID}, "max_tasks": 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(protocol.RPCRequestPayload{
			RequestID: requestID, Method: "tasks.claim", TimeoutMs: 5000, Body: body,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.WriteJSON(protocol.Message{Type: protocol.EventDaemonRPCRequest, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
			t.Fatal(err)
		}
		for {
			var msg protocol.Message
			if err := conn.ReadJSON(&msg); err != nil {
				t.Fatal(err)
			}
			if msg.Type == protocol.EventDaemonTaskAvailable {
				woke = true
				continue
			}
			if msg.Type != protocol.EventDaemonRPCResponse {
				continue
			}
			var rpc protocol.RPCResponsePayload
			if err := json.Unmarshal(msg.Payload, &rpc); err != nil {
				t.Fatal(err)
			}
			if rpc.RequestID != requestID || rpc.Status != http.StatusOK {
				t.Fatalf("unexpected RPC response: %+v", rpc)
			}
			var response batchClaimResponse
			if err := json.Unmarshal(rpc.Body, &response); err != nil {
				t.Fatal(err)
			}
			return response
		}
	}

	failed := claim("expired-build")
	if len(failed.Tasks) != 0 || failed.ClaimPollHintSupported {
		t.Fatalf("failed build returned tasks or enabled the long poll: %+v", failed)
	}
	task, err := h.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil || task.Status != "queued" || task.DispatchedAt.Valid {
		t.Fatalf("undelivered claim was not released: task=%+v err=%v", task, err)
	}
	if !woke {
		t.Fatal("recovery did not send task_available on the healthy WebSocket")
	}
	retried := claim("retry")
	if len(retried.Tasks) != 1 || retried.Tasks[0].ID != taskID || retried.Tasks[0].AuthToken == "" {
		t.Fatalf("immediate retry did not deliver the task: %+v", retried)
	}
	if duplicate := claim("no-duplicate"); len(duplicate.Tasks) != 0 {
		t.Fatalf("successful delivery was requeued: %+v", duplicate)
	}
}

func TestRequeueUndeliveredBatchClaims_PreservesOtherGenerationsAndStates(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, state := range []string{"undelivered", "delivered", "started", "cancelled", "newer claim"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := createClaimReclaimRuntime(t, ctx, "Recovery fence runtime")
			agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Recovery fence agent")
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID,
				"status": "dispatched", "dispatched_at": testutil.Raw("now()"),
			})
			task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			var delivered []AgentTaskResponse
			want := "dispatched"
			switch state {
			case "undelivered":
				want = "queued"
			case "delivered":
				delivered = []AgentTaskResponse{{ID: taskID}}
			case "started":
				_, err = testPool.Exec(ctx, `UPDATE agent_task_queue SET started_at = now() WHERE id = $1`, taskID)
			case "cancelled":
				want = "cancelled"
				_, err = testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'cancelled' WHERE id = $1`, taskID)
			case "newer claim":
				_, err = testPool.Exec(ctx, `UPDATE agent_task_queue SET dispatched_at = dispatched_at + interval '1 second' WHERE id = $1`, taskID)
			}
			if err != nil {
				t.Fatal(err)
			}
			expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
			defer cancel()
			claimed := []db.AgentTaskQueue{task}
			if state == "delivered" {
				// Exercise a partial batch, so the delivered task must be
				// excluded even while another task is recovered.
				otherAgent, otherIssue := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Omitted batch agent")
				otherID := dbfx.Task(t, otherAgent, testutil.Cols{
					"runtime_id": runtimeID, "issue_id": otherIssue,
					"status": "dispatched", "dispatched_at": testutil.Raw("now()"),
				})
				other, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(otherID))
				if err != nil {
					t.Fatal(err)
				}
				claimed = append(claimed, other)
			}
			testHandler.requeueUndeliveredBatchClaims(expired, claimed, delivered)
			got, err := testHandler.Queries.GetAgentTask(ctx, task.ID)
			if err != nil || got.Status != want {
				t.Fatalf("recovery status=%s err=%v, want %s", got.Status, err, want)
			}
		})
	}
}
