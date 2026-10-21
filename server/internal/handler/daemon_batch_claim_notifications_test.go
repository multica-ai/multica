package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/redis/go-redis/v9"
)

// Exercise the production notifier/relay. XADD honors the actual context the
// relay supplies, including its independent two-second timeout.
type stalledWakeupRedis struct {
	redis.UniversalClient
	calls atomic.Int64
}

func (r *stalledWakeupRedis) XAdd(ctx context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	r.calls.Add(1)
	<-ctx.Done()
	cmd := redis.NewStringCmd(ctx)
	cmd.SetErr(ctx.Err())
	return cmd
}

type stalledClaimCache struct {
	redis.UniversalClient
	calls atomic.Int64
}

func (r *stalledClaimCache) ZRem(ctx context.Context, _ string, _ ...any) *redis.IntCmd {
	r.calls.Add(1)
	<-ctx.Done()
	cmd := redis.NewIntCmd(ctx)
	cmd.SetErr(ctx.Err())
	return cmd
}

func (r *stalledClaimCache) Pipeline() redis.Pipeliner { return &stalledClaimPipeline{cache: r} }

type stalledClaimPipeline struct {
	redis.Pipeliner
	cache *stalledClaimCache
}

func (p *stalledClaimPipeline) Incr(ctx context.Context, _ string) *redis.IntCmd {
	return redis.NewIntCmd(ctx)
}

func (p *stalledClaimPipeline) Expire(ctx context.Context, _ string, _ time.Duration) *redis.BoolCmd {
	return redis.NewBoolCmd(ctx)
}

func (p *stalledClaimPipeline) Exec(ctx context.Context) ([]redis.Cmder, error) {
	p.cache.calls.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

type claimRecoveryCacheDB struct {
	db.DBTX
	tasks *service.TaskService
	cache *stalledClaimCache
}

func (d *claimRecoveryCacheDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	// Only stall recovery caches, not the preceding ordinary claim operations.
	if ctx.Err() == nil && strings.Contains(query, "-- name: RequeueAgentTaskAfterClaimFailure :one") {
		d.tasks.EmptyClaim = service.NewEmptyClaimCache(d.cache)
		d.tasks.ReclaimCheck = service.NewReclaimCheckCache(d.cache)
	}
	return d.DBTX.QueryRow(ctx, query, args...)
}

func TestClaimTasksByRuntime_HTTPFollowupDoesNotWaitForRecovery(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "HTTP follow-up runtime")
	agentA, issueA := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Delivered before recovery")
	taskA := seedQueuedIssueTask(t, ctx, agentA, runtimeID, issueA)
	agentB, issueB := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Retried during recovery")
	taskB := seedQueuedIssueTask(t, ctx, agentB, runtimeID, issueB)
	dbfx.Exec(t, `UPDATE agent_task_queue SET priority = 100 WHERE id = $1`, taskA)

	// Hold the first handler inside its post-response recovery, after the
	// omitted task is queued. A follow-up claim must run before this is released.
	bus := events.New()
	eventEntered, eventRelease := make(chan struct{}), make(chan struct{})
	releaseEvent := sync.OnceFunc(func() { close(eventRelease) })
	bus.Subscribe(protocol.EventTaskQueued, func(events.Event) {
		close(eventEntered)
		<-eventRelease
	})
	h := *testHandler
	h.TaskService = service.NewTaskService(testHandler.Queries, testPool, testHandler.TaskService.Hub, bus)
	firstHandler := h
	firstHandler.Queries = db.New(&slowBatchClaimDB{DBTX: testPool, failingAgent: parseUUID(agentB)})
	var requests atomic.Int64
	firstCompleted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped := chimw.NewWrapResponseWriter(chimw.NewWrapResponseWriter(w, r.ProtoMajor), r.ProtoMajor)
		r = r.WithContext(middleware.WithDaemonContext(r.Context(), testWorkspaceID, batchClaimTestDaemonID))
		if requests.Add(1) == 1 {
			defer close(firstCompleted)
			firstHandler.ClaimTasksByRuntime(wrapped, r)
			return
		}
		h.ClaimTasksByRuntime(wrapped, r)
	}))
	defer server.Close()
	defer releaseEvent() // Unblock the handler before server shutdown on failure.
	client := &http.Client{
		Timeout:   protocol.DaemonBatchClaimRequestTimeout,
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
	}
	defer client.CloseIdleConnections()
	body, err := json.Marshal(map[string]any{
		"daemon_id": batchClaimTestDaemonID, "runtime_ids": []string{runtimeID}, "max_tasks": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := func() (batchClaimResponse, bool) {
		t.Helper()
		var reused atomic.Bool
		traceCtx := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
			reused.Store(info.Reused)
		}})
		req, err := http.NewRequestWithContext(traceCtx, http.MethodPost, server.URL, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("claim blocked by the preceding handler's recovery (connection reused=%t): %v", reused.Load(), err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body) // Consume the full body so keep-alive is eligible.
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("claim status=%d: %s", resp.StatusCode, data)
		}
		var response batchClaimResponse
		if err := json.Unmarshal(data, &response); err != nil {
			t.Fatal(err)
		}
		return response, reused.Load()
	}
	first, _ := claim()
	if len(first.Tasks) != 1 || first.Tasks[0].ID != taskA || first.Tasks[0].AuthToken == "" {
		t.Fatalf("first claim lost the successful payload: %+v", first)
	}
	select {
	case <-eventEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("first claim did not reach recovery notification")
	}
	start := time.Now()
	second, reused := claim()
	elapsed := time.Since(start)
	if len(second.Tasks) != 1 || second.Tasks[0].ID != taskB || second.Tasks[0].AuthToken == "" {
		t.Fatalf("follow-up claim lost the recovered payload: %+v", second)
	}
	if reused {
		t.Fatal("follow-up claim reused the connection held by recovery")
	}
	select {
	case <-firstCompleted:
		t.Fatal("recovery finished before the follow-up claim; regression was not exercised")
	default:
	}
	// Only responses with omissions need a new connection. Complete batches
	// must retain ordinary keep-alive and must not deliver either task twice.
	third, reused := claim()
	if len(third.Tasks) != 0 || !reused {
		t.Fatalf("complete batch did not retain keep-alive or duplicated delivery: reused=%t tasks=%+v", reused, third.Tasks)
	}
	releaseEvent()
	select {
	case <-firstCompleted:
	case <-time.After(3 * time.Second):
		t.Fatal("first claim did not finish recovery")
	}
	t.Logf("follow-up delivered the recovered task in %s while the first handler remained blocked", elapsed.Round(time.Millisecond))
}

func TestClaimTasksByRuntime_ResponsePrecedesRecoveryNotifications(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, transport := range []string{"http", "ws"} {
		t.Run(transport, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := createClaimReclaimRuntime(t, ctx, "Notification deadline runtime")
			agentA, issueA := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Successful payload")
			taskA := seedQueuedIssueTask(t, ctx, agentA, runtimeID, issueA)
			agentB, issueB := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Failed payload")
			taskB := seedQueuedIssueTask(t, ctx, agentB, runtimeID, issueB)
			dbfx.Exec(t, `UPDATE agent_task_queue SET priority = 100 WHERE id = $1`, taskA)
			h := *testHandler
			h.Queries = db.New(&slowBatchClaimDB{DBTX: testPool, failingAgent: parseUUID(agentB)})
			hub := daemonws.NewHub()
			rdb := &stalledWakeupRedis{}
			relay := realtime.NewShardedStreamRelay(testHandler.TaskService.Hub, rdb, rdb, realtime.DefaultShardedStreamRelayConfig())
			bus := events.New()
			eventEntered, eventRelease := make(chan struct{}), make(chan struct{})
			releaseEvent := sync.OnceFunc(func() { close(eventRelease) })
			bus.Subscribe(protocol.EventTaskQueued, func(events.Event) {
				close(eventEntered)
				<-eventRelease
			})
			h.TaskService = service.NewTaskService(testHandler.Queries, testPool, testHandler.TaskService.Hub, bus, daemonws.NewRelayNotifier(hub, relay))
			cache := &stalledClaimCache{}
			h.TaskService.Queries = db.New(&claimRecoveryCacheDB{DBTX: testPool, tasks: h.TaskService, cache: cache})
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
				// The production logger and metrics both wrap the HTTP writer.
				wrapped := chimw.NewWrapResponseWriter(chimw.NewWrapResponseWriter(w, r.ProtoMajor), r.ProtoMajor)
				h.ClaimTasksByRuntime(wrapped, r.WithContext(middleware.WithDaemonContext(r.Context(), testWorkspaceID, batchClaimTestDaemonID)))
			}))
			defer server.Close()
			defer releaseEvent() // Release a blocked event before server shutdown on failure.
			body, err := json.Marshal(map[string]any{
				"daemon_id": batchClaimTestDaemonID, "runtime_ids": []string{runtimeID}, "max_tasks": 2,
			})
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			var response batchClaimResponse
			var clientErr error
			var conn *websocket.Conn
			if transport == "http" {
				client := &http.Client{Timeout: 5 * time.Second}
				resp, err := client.Post(server.URL, "application/json", bytes.NewReader(body))
				clientErr = err
				if resp != nil {
					clientErr = json.NewDecoder(resp.Body).Decode(&response)
					resp.Body.Close()
				}
			} else {
				conn, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				payload, err := json.Marshal(protocol.RPCRequestPayload{RequestID: "notifications", Method: "tasks.claim", TimeoutMs: 5000, Body: body})
				if err != nil {
					t.Fatal(err)
				}
				if err := conn.WriteJSON(protocol.Message{Type: protocol.EventDaemonRPCRequest, Payload: payload}); err != nil {
					t.Fatal(err)
				}
				if err := conn.SetReadDeadline(start.Add(7 * time.Second)); err != nil {
					t.Fatal(err)
				}
				var msg protocol.Message
				clientErr = conn.ReadJSON(&msg)
				if clientErr == nil {
					if msg.Type != protocol.EventDaemonRPCResponse {
						t.Fatalf("message before recovery notification = %q, want RPC response", msg.Type)
					}
					var rpc protocol.RPCResponsePayload
					if err := json.Unmarshal(msg.Payload, &rpc); err != nil {
						t.Fatal(err)
					}
					clientErr = json.Unmarshal(rpc.Body, &response)
				}
			}
			elapsed := time.Since(start)
			select {
			case <-eventEntered:
			case <-time.After(3 * time.Second):
				releaseEvent()
				t.Fatal("recovery did not publish its queued event")
			}
			releaseEvent()
			select {
			case <-completed:
			case <-time.After(10 * time.Second):
				t.Fatal("recovery did not finish in the request worker")
			}
			if clientErr != nil || elapsed >= 5*time.Second {
				t.Fatalf("%s payload waited for recovery notifications: elapsed=%s error=%v", transport, elapsed, clientErr)
			}
			if len(response.Tasks) != 1 || response.Tasks[0].ID != taskA || response.Tasks[0].AuthToken == "" {
				t.Fatalf("successful payload missing: %+v", response)
			}
			if cache.calls.Load() != 2 || rdb.calls.Load() != 1 {
				t.Fatalf("recovery skipped side effects: cache=%d wakeup=%d", cache.calls.Load(), rdb.calls.Load())
			}
			for _, task := range []struct{ id, status string }{{taskA, "dispatched"}, {taskB, "queued"}} {
				row, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(task.id))
				if err != nil || row.Status != task.status {
					t.Fatalf("task %s status=%s error=%v, want %s", task.id, row.Status, err, task.status)
				}
			}
			if conn != nil {
				var wakeup protocol.Message
				if err := conn.ReadJSON(&wakeup); err != nil || wakeup.Type != protocol.EventDaemonTaskAvailable {
					t.Fatalf("local wakeup was lost: type=%s err=%v", wakeup.Type, err)
				}
			}
			t.Logf("%s received complete payload in %s; slow caches, event and production relay completed afterward", transport, elapsed.Round(time.Millisecond))
		})
	}
}
