package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	fenceOwnerA = "g00000000000000000001:owner-A"
	fenceOwnerB = "g00000000000000000002:owner-B"
)

type delayedRuntimeTxStarter struct {
	query   string
	started chan struct{}
	release chan struct{}
}

func (s delayedRuntimeTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := testPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return delayedRuntimeTx{Tx: tx, query: s.query, started: s.started, release: s.release}, nil
}

type delayedRuntimeTx struct {
	pgx.Tx
	query   string
	started chan struct{}
	release chan struct{}
}

type delayedHeartbeatDBTX struct {
	db.DBTX
	started chan struct{}
	release chan struct{}
}

type gatedHeartbeatUpdateStore struct {
	UpdateStore
	entered chan struct{}
	release chan struct{}
}

func (s *gatedHeartbeatUpdateStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return s.UpdateStore.HasPending(ctx, runtimeID)
}

func (d delayedHeartbeatDBTX) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "-- name: GetAgentRuntimeHeartbeatState :one") {
		close(d.started)
		<-d.release
	}
	return d.DBTX.QueryRow(ctx, sql, args...)
}

func (tx delayedRuntimeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.query == "LockAgentRuntime" && strings.Contains(sql, "-- name: LockAgentRuntime :one") {
		close(tx.started)
		<-tx.release
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (tx delayedRuntimeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx.query == "RecoverOrphanedTasksForRuntime" && strings.Contains(sql, "-- name: RecoverOrphanedTasksForRuntime :many") {
		close(tx.started)
		<-tx.release
	}
	return tx.Tx.Query(ctx, sql, args...)
}

func runtimeFenceFixture(t *testing.T) (string, string, string) {
	t.Helper()
	daemonID := "runtime-owner-fence-" + strings.ReplaceAll(t.Name(), "/", "-")
	runtimeID := dbfx.Runtime(t, "fenced runtime", testutil.Cols{
		"daemon_id": daemonID, "runtime_mode": "local", "provider": "codex", "status": "online",
	})
	return runtimeID, dbfx.Agent(t, "fenced agent", runtimeID), daemonID
}

func registerFenceOwner(t *testing.T, runtimeID, daemonID, generation, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.DaemonRegister(w, newDaemonTokenRequest(http.MethodPost, "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID, "daemon_id": daemonID,
		"runtimes": []map[string]any{{"name": "codex", "type": "codex", "status": status, "owner_generation": generation}},
	}, testWorkspaceID, daemonID))
	if w.Code != http.StatusOK {
		t.Fatalf("register %s: %d %s", generation, w.Code, w.Body.String())
	}
	var actual string
	dbfx.QueryRow(t, `SELECT metadata->>'owner_generation' FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&actual)
	if actual != generation {
		t.Fatalf("registered generation = %q, want %q", actual, generation)
	}
}

func recoverFenceOwner(handler *Handler, runtimeID, daemonID, generation string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	var body any
	if generation != "" {
		body = map[string]string{"owner_generation": generation}
	}
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/recover-orphans", body, testWorkspaceID, daemonID)
	handler.RecoverOrphanedTasks(w, withURLParam(req, "runtimeId", runtimeID))
	return w
}

func TestRecoverOrphansRejectsOldOwnerAfterTakeover(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID, agentID, daemonID := runtimeFenceFixture(t)
	registerFenceOwner(t, runtimeID, daemonID, fenceOwnerA, "online")
	gate := delayedRuntimeTxStarter{query: "LockAgentRuntime", started: make(chan struct{}), release: make(chan struct{})}
	h := New(db.New(testPool), gate, testHandler.Hub, testHandler.Bus, testHandler.EmailService, nil, nil, analytics.NoopClient{}, Config{})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- recoverFenceOwner(h, runtimeID, daemonID, fenceOwnerA) }()
	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("old recovery did not reach runtime lock")
	}
	registerFenceOwner(t, runtimeID, daemonID, fenceOwnerB, "online")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running"})
	close(gate.release)
	select {
	case w := <-done:
		if w.Code != http.StatusConflict {
			t.Fatalf("old recovery = %d: %s", w.Code, w.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old recovery did not finish")
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "running" {
		t.Fatalf("new owner's task status = %q", status)
	}
	if w := recoverFenceOwner(testHandler, runtimeID, daemonID, fenceOwnerB); w.Code != http.StatusOK {
		t.Fatalf("current recovery = %d: %s", w.Code, w.Body.String())
	}
	var reason string
	dbfx.QueryRow(t, `SELECT status, failure_reason FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &reason)
	if status != "failed" || reason != "runtime_recovery" {
		t.Fatalf("current recovery task = %q/%q", status, reason)
	}
}

func TestRecoverOrphansSerializesWithTakeover(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID, agentID, daemonID := runtimeFenceFixture(t)
	registerFenceOwner(t, runtimeID, daemonID, fenceOwnerA, "online")
	oldTask := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running"})
	gate := delayedRuntimeTxStarter{query: "RecoverOrphanedTasksForRuntime", started: make(chan struct{}), release: make(chan struct{})}
	h := New(db.New(testPool), gate, testHandler.Hub, testHandler.Bus, testHandler.EmailService, nil, nil, analytics.NoopClient{}, Config{})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- recoverFenceOwner(h, runtimeID, daemonID, fenceOwnerA) }()
	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not reach destructive update after row lock")
	}
	// A holds FOR UPDATE through recovery. B's takeover cannot commit until A finishes.
	registerDone := make(chan struct{})
	go func() {
		defer close(registerDone)
		w := httptest.NewRecorder()
		testHandler.DaemonRegister(w, newDaemonTokenRequest(http.MethodPost, "/api/daemon/register", map[string]any{
			"workspace_id": testWorkspaceID, "daemon_id": daemonID,
			"runtimes": []map[string]any{{"name": "codex", "type": "codex", "status": "online", "owner_generation": fenceOwnerB}},
		}, testWorkspaceID, daemonID))
		if w.Code != http.StatusOK {
			t.Errorf("takeover = %d: %s", w.Code, w.Body.String())
		}
	}()
	close(gate.release)
	select {
	case w := <-done:
		if w.Code != http.StatusOK {
			t.Fatalf("old recovery = %d: %s", w.Code, w.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old recovery did not finish")
	}
	select {
	case <-registerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("takeover did not finish")
	}
	newTask := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running"})
	var oldStatus, newStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, oldTask).Scan(&oldStatus)
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, newTask).Scan(&newStatus)
	if oldStatus != "failed" || newStatus != "running" {
		t.Fatalf("tasks after takeover = old %q, new %q", oldStatus, newStatus)
	}
}

func TestRecoverOrphansRequiresGenerationOnlyForFencedRows(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID, agentID, daemonID := runtimeFenceFixture(t)
	registerFenceOwner(t, runtimeID, daemonID, fenceOwnerB, "online")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running"})
	for _, generation := range []string{"", "malformed", fenceOwnerA} {
		if w := recoverFenceOwner(testHandler, runtimeID, daemonID, generation); w.Code != http.StatusConflict {
			t.Fatalf("generation %q recovery = %d: %s", generation, w.Code, w.Body.String())
		}
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "running" {
		t.Fatalf("fenced task status = %q", status)
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET metadata = '{}'::jsonb WHERE id = $1`, runtimeID)
	if w := recoverFenceOwner(testHandler, runtimeID, daemonID, ""); w.Code != http.StatusOK {
		t.Fatalf("legacy recovery = %d: %s", w.Code, w.Body.String())
	}
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "failed" {
		t.Fatalf("legacy recovery task status = %q", status)
	}
}

func TestHeartbeatRejectsStaleHTTPAndWebSocketOwners(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID, _, daemonID := runtimeFenceFixture(t)
	registerFenceOwner(t, runtimeID, daemonID, fenceOwnerA, "online")
	store := &fakeLivenessStore{available: true, aliveOK: true}
	h := New(db.New(testPool), testPool, testHandler.Hub, testHandler.Bus, testHandler.EmailService, nil, nil, analytics.NoopClient{}, Config{})
	h.LivenessStore = store
	h.runtimeOwnerGate = testHandler.runtimeOwnerGate
	updates := NewInMemoryUpdateStore()
	h.UpdateStore = updates
	pending, err := updates.Create(context.Background(), runtimeID, "1.2.3", testUserID)
	if err != nil {
		t.Fatal(err)
	}
	wsReq := newDaemonTokenRequest(http.MethodGet, "/api/daemon/ws", nil, testWorkspaceID, daemonID)
	header, _ := json.Marshal(map[string]string{runtimeID: fenceOwnerA})
	wsReq.Header.Set("X-Runtime-Owner-Generations", string(header))
	identity, ok := h.buildDaemonWebSocketIdentity(httptest.NewRecorder(), wsReq, []string{runtimeID}, "")
	if !ok || identity.RuntimeLeases[runtimeID] == nil {
		t.Fatal("owner A WebSocket lease was rejected")
	}
	gate := delayedHeartbeatDBTX{DBTX: testPool, started: make(chan struct{}), release: make(chan struct{})}
	staleHandler := New(db.New(gate), testPool, testHandler.Hub, testHandler.Bus, testHandler.EmailService, nil, nil, analytics.NoopClient{}, Config{})
	staleHandler.LivenessStore = store
	staleHandler.runtimeOwnerGate = testHandler.runtimeOwnerGate
	staleHandler.UpdateStore = updates
	staleHTTP := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		staleHandler.DaemonHeartbeat(w, newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]string{
			"runtime_id": runtimeID, "owner_generation": fenceOwnerA,
		}, testWorkspaceID, daemonID))
		staleHTTP <- w
	}()
	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("old HTTP heartbeat did not reach owner check")
	}
	registerFenceOwner(t, runtimeID, daemonID, fenceOwnerB, "offline")
	close(gate.release)
	select {
	case w := <-staleHTTP:
		if w.Code != http.StatusConflict {
			t.Fatalf("delayed stale HTTP heartbeat = %d: %s", w.Code, w.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old HTTP heartbeat did not finish")
	}
	beforeStatus, beforeSeen, _ := readRuntimeRow(t, runtimeID)
	if beforeStatus != "offline" {
		t.Fatalf("replacement status = %q", beforeStatus)
	}
	heartbeat := func(generation string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.DaemonHeartbeat(w, newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]string{
			"runtime_id": runtimeID, "owner_generation": generation,
		}, testWorkspaceID, daemonID))
		return w
	}
	for _, generation := range []string{"", "malformed", fenceOwnerA} {
		if w := heartbeat(generation); w.Code != http.StatusConflict {
			t.Fatalf("stale HTTP heartbeat %q = %d: %s", generation, w.Code, w.Body.String())
		}
	}
	ack, err := h.HandleDaemonWSHeartbeat(context.Background(), identity, runtimeID, false)
	if err != nil || ack == nil || !ack.RuntimeGone {
		t.Fatalf("stale WS heartbeat ack = %+v, err = %v", ack, err)
	}
	afterStatus, afterSeen, _ := readRuntimeRow(t, runtimeID)
	if afterStatus != "offline" || !afterSeen.Equal(beforeSeen) || store.touchCount() != 0 {
		t.Fatalf("stale heartbeats changed state: status %q, seen %s, Redis touches %d", afterStatus, afterSeen, store.touchCount())
	}
	if got, err := updates.Get(context.Background(), pending.ID); err != nil || got.Status != UpdatePending {
		t.Fatalf("stale owner consumed pending update: %+v, %v", got, err)
	}
	if w := heartbeat(fenceOwnerB); w.Code != http.StatusOK {
		t.Fatalf("current HTTP heartbeat = %d: %s", w.Code, w.Body.String())
	}
	if got, err := updates.Get(context.Background(), pending.ID); err != nil || got.Status != UpdateRunning {
		t.Fatalf("current owner did not receive pending update: %+v, %v", got, err)
	}
	if status, _, _ := readRuntimeRow(t, runtimeID); status != "online" || store.touchCount() != 1 {
		t.Fatalf("current owner heartbeat: status %q, Redis touches %d", status, store.touchCount())
	}
	header, _ = json.Marshal(map[string]string{runtimeID: fenceOwnerB})
	wsReq.Header.Set("X-Runtime-Owner-Generations", string(header))
	identity, ok = h.buildDaemonWebSocketIdentity(httptest.NewRecorder(), wsReq, []string{runtimeID}, "")
	if !ok {
		t.Fatal("current owner WebSocket lease was rejected")
	}
	ack, err = h.HandleDaemonWSHeartbeat(context.Background(), identity, runtimeID, false)
	if err != nil || ack == nil || ack.RuntimeGone || store.touchCount() != 2 {
		t.Fatalf("current WS heartbeat ack = %+v, err = %v, Redis touches %d", ack, err, store.touchCount())
	}
	wsReq.Header.Del("X-Runtime-Owner-Generations")
	w := httptest.NewRecorder()
	if _, ok := h.buildDaemonWebSocketIdentity(w, wsReq, []string{runtimeID}, ""); ok || w.Code != http.StatusConflict {
		t.Fatalf("missing fenced WS generation = %d, authorized %v", w.Code, ok)
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET metadata = '{}'::jsonb WHERE id = $1`, runtimeID)
	if w := heartbeat(""); w.Code != http.StatusOK {
		t.Fatalf("legacy HTTP heartbeat = %d: %s", w.Code, w.Body.String())
	}
	if _, ok := h.buildDaemonWebSocketIdentity(httptest.NewRecorder(), wsReq, []string{runtimeID}, ""); !ok {
		t.Fatal("legacy WebSocket lease was rejected")
	}
}

func TestHeartbeatPendingStoreDoesNotHoldRuntimeRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID, _, daemonID := runtimeFenceFixture(t)
	registerFenceOwner(t, runtimeID, daemonID, fenceOwnerA, "online")
	h := New(db.New(testPool), testPool, testHandler.Hub, testHandler.Bus, testHandler.EmailService, nil, nil, analytics.NoopClient{}, Config{})
	h.runtimeOwnerGate = testHandler.runtimeOwnerGate
	store := &gatedHeartbeatUpdateStore{UpdateStore: h.UpdateStore, entered: make(chan struct{}), release: make(chan struct{})}
	h.UpdateStore = store
	pending, err := store.Create(context.Background(), runtimeID, "v2", testUserID)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		h.DaemonHeartbeat(w, newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]string{
			"runtime_id": runtimeID, "owner_generation": fenceOwnerA,
		}, testWorkspaceID, daemonID))
		finished <- w
	}()
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat did not enter pending store")
	}
	defer func() {
		select {
		case <-store.release:
		default:
			close(store.release)
		}
	}()
	if _, err := testPool.Exec(context.Background(), `SELECT id FROM agent_runtime WHERE id = $1 FOR UPDATE NOWAIT`, runtimeID); err != nil {
		t.Fatalf("pending store held runtime row lock: %v", err)
	}
	takeover := httptest.NewRecorder()
	h.DaemonRegister(takeover, newDaemonTokenRequest(http.MethodPost, "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID, "daemon_id": daemonID,
		"runtimes": []map[string]any{{"name": "codex", "type": "codex", "status": "online", "owner_generation": fenceOwnerB}},
	}, testWorkspaceID, daemonID))
	if takeover.Code != http.StatusOK {
		t.Fatalf("takeover while pending blocked = %d: %s", takeover.Code, takeover.Body.String())
	}
	close(store.release)
	select {
	case w := <-finished:
		if w.Code != http.StatusConflict {
			t.Fatalf("stale heartbeat = %d: %s", w.Code, w.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat did not finish")
	}
	if got, err := store.Get(context.Background(), pending.ID); err != nil || got.Status != UpdatePending {
		t.Fatalf("stale heartbeat claimed pending work: %+v, %v", got, err)
	}
}
