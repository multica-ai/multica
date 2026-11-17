package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The gateway-backed test exercises real HTTP handlers, PostgreSQL, the daemon
// and stock Hermes execution owner. Only the model endpoint is simulated.

func TestHermesNativeGatewayChatSupplementEndToEnd(t *testing.T) {
	if os.Getenv("MULTICA_HERMES_SOURCE") == "" || os.Getenv("MULTICA_HERMES_TEST_PYTHON") == "" {
		t.Skip("explicit stock Hermes source and isolated interpreter required")
	}
	testHermesChatSupplementEndToEnd(t)
}

func testHermesChatSupplementEndToEnd(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := testutil.IsolatedDB(t, dsn)
	fx := testutil.New(pool, "", "")
	fx.UserID = fx.User(t, "Local test", "steering@example.test")
	fx.WorkspaceID = fx.Workspace(t, "Local steering", "local-steering")
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	queries := db.New(pool)
	member, err := queries.GetMemberByUserAndWorkspace(t.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: util.MustParseUUID(fx.UserID), WorkspaceID: util.MustParseUUID(fx.WorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	runtimeID := fx.Runtime(t, "Hermes gateway", testutil.Cols{"provider": "hermes"})
	agentID := fx.Agent(t, "Steer test", runtimeID)
	sessionID := fx.Insert(t, "chat_session", testutil.Cols{"workspace_id": fx.WorkspaceID, "creator_id": fx.UserID, "agent_id": agentID, "title": "Steer test", "explicitly_created_at": testutil.Raw("now()")})
	taskID := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "status": "dispatched"})
	fx.Exec(t, `UPDATE agent_task_queue SET chat_input_task_id=id WHERE id=$1`, taskID)
	fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sessionID, "task_id": taskID, "role": "user", "content": "Wait for guidance"})
	h := handler.New(queries, pool, realtime.NewHub(), events.New(), service.NewEmailService(), nil, nil, nil, handler.Config{})
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-User-ID", fx.UserID)
			next.ServeHTTP(w, r.WithContext(middleware.SetMemberContext(r.Context(), fx.WorkspaceID, member)))
		})
	})
	ack := make(chan struct{})
	var ackOnce sync.Once
	router.Post("/api/daemon/tasks/{taskId}/start", h.StartTask)
	var activations atomic.Int32
	router.Post("/api/daemon/tasks/{taskId}/supplements/claim", func(w http.ResponseWriter, r *http.Request) {
		{
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			if bytes.Contains(body, []byte("active_turn_guidance")) && activations.Add(1) == 1 {
				committed := httptest.NewRecorder()
				h.ClaimTaskSupplement(committed, r)
				if committed.Code != 200 {
					t.Errorf("activation did not commit: %d %s", committed.Code, committed.Body.String())
				}
				http.Error(w, "proxy lost committed activation response", http.StatusBadGateway)
				return
			}
		}
		h.ClaimTaskSupplement(w, r)
	})
	router.Post("/api/daemon/tasks/{taskId}/supplements/{commentId}/ack", func(w http.ResponseWriter, r *http.Request) {
		h.AckTaskSupplement(w, r)
		ackOnce.Do(func() { close(ack) })
	})
	router.Post("/api/daemon/tasks/{taskId}/complete", h.CompleteTask)
	router.Post("/api/chat/sessions/{sessionId}/messages", h.SendChatMessage)
	server := httptest.NewServer(router)
	defer server.Close()
	d := &Daemon{client: NewClient(server.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), taskSupplementPollInterval: 5 * time.Millisecond, taskSupplementReadyInterval: time.Millisecond}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	negotiated, err := d.client.StartTask(ctx, Task{ID: taskID}, protocol.DaemonCapabilityTaskSupplementV1)
	if err != nil || negotiated {
		t.Fatalf("negotiation=%t %v", negotiated, err)
	}
	const marker = "MULTICA_CHAT_E2E_8ef422ab"
	executable := filepath.Join(t.TempDir(), "stock-hermes")
	fixture, err := filepath.Abs("../../pkg/agent/testdata/hermes_native_gateway.py")
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	script := "#!/bin/sh\nexec " + quote(os.Getenv("MULTICA_HERMES_TEST_PYTHON")) + " " + quote(fixture) + "\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "trace.json")
	providerEnv := map[string]string{"HERMES_HOME": t.TempDir(), "GUIDANCE_TRACE": trace, "GUIDANCE_MARKER": marker, "MULTICA_HERMES_SOURCE": os.Getenv("MULTICA_HERMES_SOURCE")}
	backend, err := agent.New("hermes", agent.Config{ExecutablePath: executable, Logger: d.logger, Env: providerEnv})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(ctx, "Wait for guidance", agent.ExecOptions{Cwd: filepath.Dir(executable), EnableTaskSupplement: true})
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for msg := range session.Messages {
			if msg.Type == agent.MessageToolUse {
				select {
				case <-ready:
				default:
					close(ready)
				}
			}
		}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("primary prompt never started")
	}
	select {
	case result := <-session.Result:
		t.Fatalf("run ended before guidance: %+v", result)
	default:
	}
	wakeup, unsubscribe := d.taskSupplementSignals.subscribe(taskID)
	defer unsubscribe()
	loopCtx, stopLoop := context.WithCancel(ctx)
	defer stopLoop()
	loopDone := make(chan struct{})
	go func() { defer close(loopDone); d.runTaskSupplementLoop(loopCtx, session, taskID, wakeup, d.logger) }()
	{
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for fx.Count(t, `SELECT count(*) FROM task_supplement_capability WHERE task_id=$1`, taskID) == 0 {
			select {
			case <-tick.C:
			case <-ctx.Done():
				t.Fatal("deferred capability never activated")
			}
		}
	}
	body, _ := json.Marshal(map[string]any{"content": marker, "steer": true, "submission_id": "0199a4e8-22ce-7b01-bba5-111111111111"})
	response, err := http.Post(fmt.Sprintf("%s/api/chat/sessions/%s/messages", server.URL, sessionID), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	responseBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 201 {
		t.Fatalf("send=%d %s", response.StatusCode, responseBody)
	}
	// Ignore the first response as if HTTP delivery was lost. An explicit
	// retry of the persisted request must not create or inject another input.
	retried, err := http.Post(fmt.Sprintf("%s/api/chat/sessions/%s/messages", server.URL, sessionID), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	retryBody, _ := io.ReadAll(retried.Body)
	retried.Body.Close()
	if retried.StatusCode != 201 {
		t.Fatalf("retry=%d %s", retried.StatusCode, retryBody)
	}
	d.taskSupplementSignals.notify(taskID)
	var result agent.Result
	select {
	case result = <-session.Result:
	case <-ctx.Done():
		t.Fatal("guidance did not unblock primary prompt")
	}
	if result.Status != "completed" || strings.TrimSpace(result.Output) != marker {
		t.Fatalf("result=%+v", result)
	}
	select {
	case <-ack:
	case <-ctx.Done():
		t.Fatal("missing engine receipt")
	}
	stopLoop()
	<-loopDone
	<-drained
	completeBody, _ := json.Marshal(handler.TaskCompleteRequest{Output: result.Output})
	completed, err := http.Post(server.URL+"/api/daemon/tasks/"+taskID+"/complete", "application/json", bytes.NewReader(completeBody))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(completed.Body)
	completed.Body.Close()
	if completed.StatusCode != 200 {
		t.Fatalf("completion=%d %s", completed.StatusCode, data)
	}
	if n := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID); n != 1 {
		t.Fatalf("extra invocation: %d", n)
	}
	if n := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='completed'`, taskID); n != 1 {
		t.Fatal("original task not completed")
	}
	if n := fx.Count(t, `SELECT count(*) FROM chat_task_supplement WHERE task_id=$1 AND status='delivered'`, taskID); n != 1 {
		t.Fatal("missing durable engine receipt")
	}
	if n := fx.Count(t, `SELECT count(*) FROM chat_message WHERE task_id=$1 AND role='assistant' AND content=$2`, taskID, result.Output); n != 1 {
		t.Fatal("marker missing from persisted answer")
	}
	next, err := d.client.ClaimTaskSupplement(ctx, taskID)
	if err != nil || next != nil {
		t.Fatalf("replayed input: %+v %v", next, err)
	}
	traceData, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	var proof struct {
		Kind       string `json:"kind"`
		OwnerPID   int    `json:"ownerPid"`
		ModelCalls int    `json:"modelCalls"`
		PID        int    `json:"pid"`
		Runs       int    `json:"runs"`
		Steers     int    `json:"steers"`
	}
	if err := json.Unmarshal(traceData, &proof); err != nil {
		t.Fatal(err)
	}
	var sent handler.SendChatMessageResponse
	if err := json.Unmarshal(responseBody, &sent); err != nil {
		t.Fatal(err)
	}
	if proof.PID <= 0 || proof.Runs != 1 || proof.Steers != 1 || sent.MessageID == "" {
		t.Fatalf("trace=%s", traceData)
	}
	{
		if activations.Load() != 2 {
			t.Fatalf("lost committed activation not recovered exactly once: %d", activations.Load())
		}
		if proof.Kind != "stock-gateway" || proof.OwnerPID <= 0 || proof.OwnerPID == proof.PID || proof.ModelCalls != 2 || result.Usage["custom:controlled-gateway"].InputTokens != 27 || result.Usage["custom:controlled-gateway"].OutputTokens != 8 {
			t.Fatalf("gateway proof: trace=%s usage=%+v", traceData, result.Usage)
		}
		t.Logf("Hermes gateway task=%s owner PID=%d viewer PID=%d: native /steer, one admission/instruction, durable server receipt, usage 27/8, no successor", taskID, proof.OwnerPID, proof.PID)
	}
}
