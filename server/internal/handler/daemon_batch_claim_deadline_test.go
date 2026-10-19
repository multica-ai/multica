package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type slowBatchClaimDB struct {
	db.DBTX
	failingAgent  pgtype.UUID
	expired       bool
	recoveryDelay time.Duration
}

func (d *slowBatchClaimDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if !d.expired && strings.Contains(query, "-- name: GetAgent :one") && args[0] == d.failingAgent {
		d.expired = true
		<-ctx.Done()
		return errRow{err: ctx.Err()}
	}
	if strings.Contains(query, "-- name: RequeueAgentTaskAfterClaimFailure :one") && ctx.Err() == nil {
		timer := time.NewTimer(d.recoveryDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return errRow{err: ctx.Err()}
		}
	}
	return d.DBTX.QueryRow(ctx, query, args...)
}

// A builds successfully, B exhausts the build deadline, and recovery remains
// slow. A must still reach installed WS (7s) and HTTP (5s) clients. This failed
// at about 8s with the previous independent five-second recovery budget.
func TestClaimTasksByRuntime_PartialSuccessFitsClientDeadlines(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, transport := range []string{"ws", "http"} {
		for _, recoveryDelay := range []time.Duration{0, 3 * time.Second} {
			t.Run(fmt.Sprintf("%s/recovery_%s", transport, recoveryDelay), func(t *testing.T) {
				ctx := context.Background()
				runtimeID := createClaimReclaimRuntime(t, ctx, "Partial claim runtime")
				agentA, issueA := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Successful claim")
				taskA := seedQueuedIssueTask(t, ctx, agentA, runtimeID, issueA)
				agentB, issueB := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Timed out claim")
				taskB := seedQueuedIssueTask(t, ctx, agentB, runtimeID, issueB)
				dbfx.Exec(t, `UPDATE agent_task_queue SET priority = 100 WHERE id = $1`, taskA)

				h := *testHandler
				h.Queries = db.New(&slowBatchClaimDB{DBTX: testPool, failingAgent: parseUUID(agentB)})
				hub := daemonws.NewHub()
				recoveryDB := &slowBatchClaimDB{DBTX: testPool, expired: true, recoveryDelay: recoveryDelay}
				h.TaskService = service.NewTaskService(db.New(recoveryDB), testPool, testHandler.TaskService.Hub, testHandler.TaskService.Bus, hub)
				completed := make(chan struct{})
				hub.SetRPCHandler(func(ctx context.Context, identity daemonws.ClientIdentity, method string, body json.RawMessage) (int, json.RawMessage, error) {
					status, response, err := h.DaemonRPCHandler(ctx, identity, method, body)
					daemonws.AfterRPCResponse(ctx, func() { close(completed) })
					return status, response, err
				})
				identity := daemonws.ClientIdentity{
					DaemonID: batchClaimTestDaemonID, UserID: testUserID,
					WorkspaceID: testWorkspaceID, WorkspaceIDs: []string{testWorkspaceID},
					RuntimeIDs: []string{runtimeID}, Capabilities: protocol.DaemonCapabilityClaimPollHintsV1,
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if transport == "ws" {
						hub.HandleWebSocket(w, r, identity)
						return
					}
					defer close(completed)
					r.Header.Set("X-Client-Capabilities", identity.Capabilities)
					h.ClaimTasksByRuntime(w, r.WithContext(middleware.WithDaemonContext(r.Context(), testWorkspaceID, batchClaimTestDaemonID)))
				}))
				defer server.Close()
				body, err := json.Marshal(map[string]any{
					"daemon_id": batchClaimTestDaemonID, "runtime_ids": []string{runtimeID}, "max_tasks": 2,
				})
				if err != nil {
					t.Fatal(err)
				}
				var response batchClaimResponse
				var readErr error
				start := time.Now()
				if transport == "ws" {
					conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					payload, err := json.Marshal(protocol.RPCRequestPayload{
						RequestID: "partial", Method: "tasks.claim", TimeoutMs: 5000, Body: body,
					})
					if err != nil {
						t.Fatal(err)
					}
					if err := conn.WriteJSON(protocol.Message{Type: protocol.EventDaemonRPCRequest, Payload: payload}); err != nil {
						t.Fatal(err)
					}
					if err := conn.SetReadDeadline(start.Add(7 * time.Second)); err != nil {
						t.Fatal(err)
					}
					for {
						var msg protocol.Message
						readErr = conn.ReadJSON(&msg)
						if readErr != nil {
							break
						}
						if msg.Type != protocol.EventDaemonRPCResponse {
							continue
						}
						var rpc protocol.RPCResponsePayload
						if readErr = json.Unmarshal(msg.Payload, &rpc); readErr == nil {
							if rpc.Status != http.StatusOK {
								readErr = fmt.Errorf("RPC status %d: %s", rpc.Status, rpc.Error)
							} else {
								readErr = json.Unmarshal(rpc.Body, &response)
							}
						}
						break
					}
				} else {
					client := &http.Client{Timeout: 5 * time.Second}
					resp, err := client.Post(server.URL, "application/json", bytes.NewReader(body))
					readErr = err
					if err == nil {
						defer resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							readErr = fmt.Errorf("HTTP status %d", resp.StatusCode)
						} else {
							readErr = json.NewDecoder(resp.Body).Decode(&response)
						}
					}
				}
				elapsed := time.Since(start)
				// Collect the handler even when exercising the broken implementation.
				select {
				case <-completed:
				case <-time.After(10 * time.Second):
					t.Fatal("claim handler did not finish")
				}
				if readErr != nil {
					t.Fatalf("successful task missed %s client deadline after %s: %v", transport, elapsed, readErr)
				}
				if elapsed >= 5*time.Second {
					t.Fatalf("claim took %s; must leave response time inside the existing 5s budget", elapsed)
				}
				if len(response.Tasks) != 1 || response.Tasks[0].ID != taskA || response.Tasks[0].AuthToken == "" || response.ClaimPollHintSupported {
					t.Fatalf("partial response lost A or enabled long polling: %+v", response)
				}
				rowA, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskA))
				if err != nil || rowA.Status != "dispatched" || rowA.StartedAt.Valid {
					t.Fatalf("successful claim changed: task=%+v err=%v", rowA, err)
				}
				wantB := "queued"
				if recoveryDelay > 0 {
					wantB = "dispatched" // Slow recovery is cancelled; stale reclaim remains safe.
				}
				rowB, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskB))
				if err != nil || rowB.Status != wantB {
					t.Fatalf("omitted claim status=%s err=%v, want %s", rowB.Status, err, wantB)
				}
				t.Logf("%s response in %s; successful task delivered; omitted task %s", transport, elapsed.Round(time.Millisecond), rowB.Status)
			})
		}
	}
}

func TestBatchClaimDeadlines(t *testing.T) {
	start := time.Now()
	for _, tt := range []struct {
		name            string
		caller          time.Duration
		build, response time.Duration
	}{
		{name: "HTTP without deadline", build: 3750 * time.Millisecond, response: 4750 * time.Millisecond},
		{name: "WS budget", caller: 5 * time.Second, build: 3750 * time.Millisecond, response: 4750 * time.Millisecond},
		{name: "shorter caller", caller: 2 * time.Second, build: 1300 * time.Millisecond, response: 1800 * time.Millisecond},
		{name: "longer caller", caller: 30 * time.Second, build: 3750 * time.Millisecond, response: 4750 * time.Millisecond},
		{name: "expired caller", caller: -time.Second, build: -time.Second, response: -time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.caller != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, start.Add(tt.caller))
				defer cancel()
			}
			build, response := batchClaimDeadlines(ctx, start)
			if build.Sub(start) != tt.build || response.Sub(start) != tt.response {
				t.Fatalf("build=%s response=%s, want %s/%s", build.Sub(start), response.Sub(start), tt.build, tt.response)
			}
		})
	}
}

type retryChatInputDB struct {
	db.DBTX
	failures int
}

func (d *retryChatInputDB) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if d.failures > 0 && strings.Contains(query, "-- name: ListChatInputMessages :many") {
		d.failures--
		return nil, context.DeadlineExceeded
	}
	return d.DBTX.Query(ctx, query, args...)
}

func TestClaimTasksByRuntime_ChatRecoversAfterRepeatedInputFailures(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Chat recovery runtime")
	agentID := dbfx.Agent(t, "Chat recovery agent", runtimeID)
	chatID := dbfx.ChatSession(t, agentID, testutil.Cols{"runtime_id": runtimeID})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": chatID})
	dbfx.Exec(t, `UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`, taskID)
	const input = "preserve this chat input through three failed claims"
	dbfx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id": chatID, "task_id": taskID, "role": "user", "content": input,
	})
	h := *testHandler
	h.Queries = db.New(&retryChatInputDB{DBTX: testPool, failures: 3})
	h.TaskService = service.NewTaskService(testHandler.Queries, testPool, testHandler.TaskService.Hub, testHandler.TaskService.Bus)
	claim := func() []AgentTaskResponse {
		t.Helper()
		var response struct {
			Tasks []AgentTaskResponse `json:"tasks"`
		}
		testutil.Call(t, h.ClaimTasksByRuntime, batchClaimRequest(testWorkspaceID, []string{runtimeID}, 1, "")).Want(http.StatusOK).JSON(&response)
		return response.Tasks
	}
	for attempt := 0; attempt < 3; attempt++ {
		if tasks := claim(); len(tasks) != 0 {
			t.Fatalf("failed input read returned a task: %+v", tasks)
		}
		task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
		if err != nil || task.Status != "queued" || task.DispatchedAt.Valid || task.ChatInputTaskID != task.ID {
			t.Fatalf("claim %d did not preserve retryable chat input: task=%+v err=%v", attempt+1, task, err)
		}
	}
	tasks := claim() // No stale-recovery wait between attempts.
	if len(tasks) != 1 || tasks[0].ID != taskID || !strings.Contains(tasks[0].ChatMessage, input) || tasks[0].AuthToken == "" {
		t.Fatalf("retry did not deliver the original chat input: %+v", tasks)
	}
	if duplicate := claim(); len(duplicate) != 0 {
		t.Fatalf("successful chat delivery was duplicated: %+v", duplicate)
	}
}
