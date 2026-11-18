//go:build integration && !windows

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/kagent"
)

// Operator prerequisites: a migrated disposable DATABASE_URL and existing HTTP
// loopback port-forwards to Kubernetes and the Kagent alpha7 A2A gateway. The
// namespace, isolated harness and model below must already exist. This test
// creates only its own Agent resource; it never launches kubectl or agent CLIs.
//
// MULTICA_KAGENT_LIVE_TEST=1 DATABASE_URL=... \
// MULTICA_KAGENT_LIVE_KUBERNETES_ENDPOINT=http://127.0.0.1:18001 \
// MULTICA_KAGENT_LIVE_A2A_ENDPOINT=http://127.0.0.1:18083 \
// go test -tags=integration ./internal/daemon -run '^TestKagentLiveDBEndToEnd$' -count=1 -timeout=5m -v
func TestKagentLiveDBEndToEnd(t *testing.T) {
	if os.Getenv("MULTICA_KAGENT_LIVE_TEST") != "1" {
		t.Skip("requires MULTICA_KAGENT_LIVE_TEST=1 and operator-supplied loopback port-forwards")
	}
	kubeEndpoint := kagentLiveLoopbackEndpoint(t, "MULTICA_KAGENT_LIVE_KUBERNETES_ENDPOINT")
	a2aEndpoint := kagentLiveLoopbackEndpoint(t, "MULTICA_KAGENT_LIVE_A2A_ENDPOINT")
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL is required; no default database is used")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("invalid DATABASE_URL")
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("required PostgreSQL is unavailable")
	}
	fx := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	userID := fx.User(t, "Kagent live", "kagent-live-"+suffix+"@example.test")
	wsID := fx.Workspace(t, "Kagent live", "kagent-live-"+suffix)
	fx.UserID, fx.WorkspaceID = userID, wsID
	fx.Member(t, wsID, userID, "owner")
	daemonID := "kagent-live-" + suffix
	runtimeID := fx.Runtime(t, "Kagent live", testutil.Cols{"provider": "kagent", "runtime_mode": "local", "daemon_id": daemonID})
	agentID := fx.Agent(t, "Kagent isolated live", runtimeID, testutil.Cols{
		"runtime_mode": "local", "runtime_config": `{"mode":"native-text"}`, "instructions": "Reply with the single word READY.",
	})
	fx.Exec(t, `INSERT INTO kagent_enrollment (agent_id,workspace_id,runtime_id) VALUES ($1,$2,$3)`, agentID, wsID, runtimeID)
	fx.Cleanup(t, `DELETE FROM kagent_enrollment WHERE agent_id=$1`, agentID)
	sessionID := fx.ChatSession(t, agentID)
	taskID := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID})
	fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sessionID, "role": "user", "content": "Reply with the single word READY."})
	// CompleteTask inserts an assistant reply after the fixture's own rows.
	fx.Cleanup(t, `DELETE FROM chat_message WHERE chat_session_id=$1`, sessionID)
	fx.Cleanup(t, `DELETE FROM task_message WHERE task_id=$1`, taskID)

	// Retain the journal on failure: an ambiguous remote send must not lose its
	// ownership and replay fences when testing.T automatically removes temp dirs.
	dir, err := os.MkdirTemp("", "multica-kagent-live-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("persistent test journal directory=%s", dir)
	tokenPath := filepath.Join(dir, "placeholder-token")
	if err := os.WriteFile(tokenPath, []byte("multica-live-test-placeholder"), 0600); err != nil {
		t.Fatal(err)
	}
	modelConfig := strings.TrimSpace(os.Getenv("MULTICA_KAGENT_LIVE_MODEL_CONFIG"))
	if modelConfig == "" || modelConfig == "default-model-config" {
		t.Fatal("MULTICA_KAGENT_LIVE_MODEL_CONFIG must name an explicitly qualified non-production model config")
	}
	cfg := kagentDaemonConfig{
		JournalDir: filepath.Join(dir, "journal"), KubernetesEndpoint: "https://kubernetes.live.test", A2AEndpoint: "https://a2a.live.test",
		KubernetesTokenFile: tokenPath, A2ATokenFile: tokenPath, Namespace: "kagent",
		Harness: "multica-integration-isolated", ModelConfig: modelConfig,
		Agents: []kagentEnrollment{{AgentID: agentID, WorkspaceID: wsID}},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	t.Setenv("MULTICA_KAGENT_CONFIG", configPath)

	// The production clients still require HTTPS. Only these fixed test hosts
	// map to the operator's plaintext loopback forwards; proxies are disabled.
	// Do not parallelize: backend clients inherit http.DefaultTransport.
	forward := http.DefaultTransport.(*http.Transport).Clone()
	forward.Proxy = nil
	t.Cleanup(forward.CloseIdleConnections)
	previous := http.DefaultTransport
	http.DefaultTransport = kagentWiringTransport(func(req *http.Request) (*http.Response, error) {
		var endpoint *url.URL
		switch req.URL.Host {
		case "kubernetes.live.test":
			endpoint = kubeEndpoint
		case "a2a.live.test":
			endpoint = a2aEndpoint
		default:
			return nil, fmt.Errorf("live test refused unexpected destination")
		}
		if req.URL.Scheme != "https" {
			return nil, fmt.Errorf("live test expected fixed HTTPS destination")
		}
		mapped := req.Clone(req.Context())
		u := *req.URL
		u.Scheme, u.Host = endpoint.Scheme, endpoint.Host
		mapped.URL, mapped.Host = &u, endpoint.Host
		if req.URL.Host == "kubernetes.live.test" {
			// kubectl proxy authenticates upstream with its own credential. This
			// fixed placeholder is not a Kubernetes token and must not override it.
			mapped.Header = req.Header.Clone()
			mapped.Header.Del("Authorization")
		}
		return forward.RoundTrip(mapped)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })

	h := handler.New(db.New(pool), pool, realtime.NewHub(), events.New(), service.NewEmailService(), nil, nil, nil, handler.Config{})
	r := chi.NewRouter()
	// Supply the actual daemon context at the authentication middleware
	// boundary; downstream authorization and all database writes remain real.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			c := middleware.WithDaemonContext(req.Context(), wsID, daemonID)
			c = middleware.WithDaemonTokenOwnerID(c, userID)
			next.ServeHTTP(w, req.WithContext(c))
		})
	})
	var starts, completions atomic.Int32
	r.Post("/api/daemon/runtimes/{runtimeId}/tasks/claim", h.ClaimTaskByRuntime)
	r.Get("/api/daemon/runtimes/{runtimeId}/kagent/agents", h.ListKagentRuntimeAgents)
	r.Post("/api/daemon/runtimes/{runtimeId}/kagent/agents/{agentId}/report", h.ReportKagentRuntimeAgent)
	r.Post("/api/daemon/tasks/{taskId}/messages", h.ReportTaskMessages)
	r.Post("/api/daemon/tasks/{taskId}/start", func(w http.ResponseWriter, req *http.Request) {
		starts.Add(1)
		h.StartTask(w, req)
	})
	r.Post("/api/daemon/tasks/{taskId}/complete", func(w http.ResponseWriter, req *http.Request) {
		completions.Add(1)
		h.CompleteTask(w, req)
	})
	client := NewClient("http://multica.live.test")
	client.client.Transport = kagentWiringTransport(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		resp := rec.Result()
		resp.Request = req
		return resp, nil
	})
	d := &Daemon{cfg: Config{DaemonID: daemonID, AgentTimeout: 2 * time.Minute}, client: client, logger: slog.Default(),
		workspaces:   map[string]*workspaceState{wsID: {workspaceID: wsID, runtimeIDs: []string{runtimeID}}},
		runtimeIndex: map[string]Runtime{runtimeID: {ID: runtimeID, Provider: "kagent"}},
	}
	// Exercise the real periodic lifecycle path, not a pre-provisioned Agent.
	// A newly-created alpha7 Agent needs multiple rounds before Ready.
	deadline := time.Now().Add(90 * time.Second)
	for {
		_ = d.kagentReconcileRound(ctx)
		var phase string
		fx.QueryRow(t, `SELECT phase FROM kagent_enrollment WHERE agent_id=$1`, agentID).Scan(&phase)
		if phase == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent %s failed to become ready (phase=%s); resource and journal retained", agentID, phase)
		}
		time.Sleep(time.Second)
	}
	task, err := client.ClaimTask(ctx, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || task.ID != taskID || task.AgentID != agentID || task.WorkspaceID != wsID || task.RuntimeID != runtimeID || task.ChatSessionID != sessionID {
		t.Fatal("claim did not return the isolated fixture task")
	}
	if !task.StartClaimSupported || task.DispatchedAt == "" {
		t.Fatal("claim must advertise StartTask and include the dispatch generation")
	}
	var status string
	fx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status)
	if status != "dispatched" {
		t.Fatalf("claimed task status=%q", status)
	}
	t.Logf("fixture agent=%s workspace=%s task=%s", agentID, wsID, taskID)
	result, err := d.runTask(ctx, *task, "kagent", 0, slog.Default())
	if err != nil || result.Status != "completed" || d.cleanupUnconfirmed.Load() {
		// An ambiguous outcome is not proof of drain: leave the resource for
		// operator inspection rather than deleting a potentially active agent.
		t.Fatalf("remote completion not fully observed; resource retained for agent %s: result=%+v err=%v", agentID, result, err)
	}
	// Register cleanup before assertions/reporting so a wrong answer or a DB
	// callback failure still cleans up the now-quiescent owned resource.
	t.Cleanup(func() { kagentLiveDelete(t, cfg, agentID) })
	d.reportTaskResult(ctx, taskID, result, slog.Default())
	var output, persistedSession string
	fx.QueryRow(t, `SELECT status, COALESCE(result->>'output',''), COALESCE(session_id,'') FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &output, &persistedSession)
	if starts.Load() != 1 || completions.Load() != 1 {
		t.Errorf("handler calls: start=%d complete=%d", starts.Load(), completions.Load())
	}
	if status != "completed" || strings.TrimSpace(output) != "READY" || strings.TrimSpace(result.Comment) != "READY" || result.SessionID == "" || persistedSession != result.SessionID {
		t.Fatalf("persisted status=%q output=%q session=%q; daemon result=%+v", status, output, persistedSession, result)
	}
}

func kagentLiveLoopbackEndpoint(t *testing.T, env string) *url.URL {
	t.Helper()
	u, err := url.Parse(os.Getenv(env))
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		t.Fatalf("%s must be an HTTP loopback port-forward URL without credentials or path", env)
	}
	host := u.Hostname()
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	port, err := strconv.Atoi(u.Port())
	if ip == nil || !ip.IsLoopback() || err != nil || port < 1 || port > 65535 {
		t.Fatalf("%s must use a loopback IP or localhost and an explicit valid port", env)
	}
	u.Host = net.JoinHostPort(ip.String(), strconv.Itoa(port))
	return u
}

func kagentLiveDelete(t *testing.T, cfg kagentDaemonConfig, agentID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := kagent.NewKubernetesStore(kagent.HTTPConfig{Endpoint: cfg.KubernetesEndpoint, Token: kagentFileToken(cfg.KubernetesTokenFile)}, []string{cfg.Namespace})
	if err != nil {
		t.Error(err)
		return
	}
	reconciler, err := kagent.New(store, kagent.Config{AllowedNamespaces: []string{cfg.Namespace}})
	if err != nil {
		t.Error(err)
		return
	}
	journal, err := kagent.NewJournal(cfg.JournalDir)
	if err != nil {
		t.Error(err)
		return
	}
	lifecycle, err := kagent.NewLifecycle(reconciler, journal)
	if err != nil {
		t.Error(err)
		return
	}
	for {
		err = lifecycle.Delete(ctx, agentID, true)
		if err == nil {
			break
		}
		if !errors.Is(err, kagent.ErrDeletionPending) {
			t.Errorf("owned agent %s cleanup: %v", agentID, err)
			return
		}
		select {
		case <-ctx.Done():
			t.Errorf("owned agent %s deletion not confirmed: %v", agentID, err)
			return
		case <-time.After(time.Second):
		}
	}
	name, err := kagent.ResourceName(agentID)
	if err != nil {
		t.Error(err)
		return
	}
	if _, err := store.Get(ctx, cfg.Namespace, name); !errors.Is(err, kagent.ErrNotFound) {
		t.Errorf("owned agent %s absence not observed: %v", agentID, err)
	}
}
