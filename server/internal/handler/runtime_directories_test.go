package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func exerciseDirectoryStore(t *testing.T, a, b DirectoryListStore) {
	t.Helper()
	ctx := context.Background()
	req, err := a.Create(ctx, "runtime", "daemon", "/项目 with spaces")
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != "pending" {
		t.Fatalf("status = %s", req.Status)
	}
	if got, err := b.PopPending(ctx, "other"); err != nil || got != nil {
		t.Fatalf("cross-runtime claim = %+v %v", got, err)
	}
	got, err := b.PopPending(ctx, "runtime")
	if err != nil || got == nil || got.Status != "running" || got.DaemonID != "daemon" || got.Path != req.Path {
		t.Fatalf("claim = %+v %v", got, err)
	}
	if again, err := a.PopPending(ctx, "runtime"); err != nil || again != nil {
		t.Fatalf("double claim = %+v %v", again, err)
	}
	result := &protocol.DirectoryListing{Path: req.Path, Entries: []protocol.DirectoryEntry{}, Selectable: true}
	if err := b.Finish(ctx, req.ID, result, ""); err != nil {
		t.Fatal(err)
	}
	got, err = a.Get(ctx, req.ID)
	if err != nil || got.Status != "completed" || got.Result == nil || got.Result.Path != req.Path {
		t.Fatalf("completion = %+v %v", got, err)
	}
	if err := b.Finish(ctx, req.ID, nil, "late failure"); err != nil {
		t.Fatal(err)
	}
	got, _ = a.Get(ctx, req.ID)
	if got.Status != "completed" {
		t.Fatalf("late report changed terminal status: %s", got.Status)
	}
}

func TestInMemoryDirectoryListStore(t *testing.T) {
	s := NewInMemoryDirectoryListStore()
	exerciseDirectoryStore(t, s, s)
}
func TestRedisDirectoryListStore(t *testing.T) {
	rdb := newRedisTestClient(t)
	exerciseDirectoryStore(t, NewRedisDirectoryListStore(rdb), NewRedisDirectoryListStore(rdb))
}
func TestDirectoryListTimeout(t *testing.T) {
	s := NewInMemoryDirectoryListStore()
	req, _ := s.Create(context.Background(), "runtime", "daemon", "")
	s.requests[req.ID].CreatedAt = time.Now().Add(-directoryPendingTimeout - time.Second)
	got, _ := s.Get(context.Background(), req.ID)
	if got.Status != "timeout" {
		t.Fatalf("status = %s", got.Status)
	}
	if pending, _ := s.PopPending(context.Background(), "runtime"); pending != nil {
		t.Fatal("timed out request dispatched")
	}
}

func TestDirectoryListOwnerAndCapabilityGates(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "Directory browser", testutil.Cols{"daemon_id": "directory-daemon", "runtime_mode": "local", "provider": "claude"})
	admin := dbfx.User(t, "Directory admin", fmt.Sprintf("directories-%d@example.com", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, admin, "admin")
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET visibility='public' WHERE id=$1`, runtimeID); err != nil {
		t.Fatal(err)
	}
	h := *testHandler
	h.DirectoryListStore = NewInMemoryDirectoryListStore()
	recorder := &runtimeLocalSkillPendingWorkRecorder{}
	h.DaemonPendingWork = recorder
	post := func(user string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.InitiateListDirectories(w, withURLParams(newRequestAsUser(user, http.MethodPost, "/", map[string]string{"path": "/项目 with spaces"}), "runtimeId", runtimeID))
		return w
	}
	if w := post(admin); w.Code != http.StatusForbidden {
		t.Fatalf("nonowner: %d %s", w.Code, w.Body.String())
	}
	if w := post(testUserID); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("old daemon: %d %s", w.Code, w.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET metadata='{"capabilities":["chat-working-directory-v1"]}'::jsonb WHERE id=$1`, runtimeID); err != nil {
		t.Fatal(err)
	}
	w := post(testUserID)
	if w.Code != http.StatusOK {
		t.Fatalf("owner: %d %s", w.Code, w.Body.String())
	}
	if len(recorder.hints) != 1 || recorder.hints[0] != runtimeID+":directories" {
		t.Fatalf("hints = %v", recorder.hints)
	}
	var req RuntimeDirectoryListRequest
	if err := json.Unmarshal(w.Body.Bytes(), &req); err != nil {
		t.Fatal(err)
	}
	if req.Path != "/项目 with spaces" {
		t.Fatalf("path = %q", req.Path)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET status='offline' WHERE id=$1`, runtimeID); err != nil {
		t.Fatal(err)
	}
	if w := post(testUserID); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline: %d %s", w.Code, w.Body.String())
	}
}

func TestDirectoryListReportIdentityAndFailure(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "Directory report", testutil.Cols{"daemon_id": "directory-daemon", "runtime_mode": "local", "provider": "claude"})
	otherID := dbfx.Runtime(t, "Other directory runtime", testutil.Cols{"daemon_id": "directory-daemon", "runtime_mode": "local", "provider": "codex"})
	h := *testHandler
	h.DirectoryListStore = NewInMemoryDirectoryListStore()
	req, err := h.DirectoryListStore.Create(ctx, runtimeID, "directory-daemon", "/missing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.DirectoryListStore.PopPending(ctx, runtimeID); err != nil {
		t.Fatal(err)
	}
	report := func(runtime string, body any) *http.Request {
		return withURLParams(newOwnerDaemonRequest(http.MethodPost, "/", body), "runtimeId", runtime, "requestId", req.ID)
	}
	body := map[string]string{"status": "failed", "error": "directory does not exist"}
	testutil.Call(t, h.ReportDirectoryListResult, report(otherID, body)).Want(http.StatusNotFound)
	testutil.Call(t, h.ReportDirectoryListResult, withURLParams(newDaemonTokenRequest(http.MethodPost, "/", body, testWorkspaceID, "wrong-daemon"), "runtimeId", runtimeID, "requestId", req.ID)).Want(http.StatusNotFound)
	testutil.Call(t, h.ReportDirectoryListResult, report(runtimeID, map[string]string{"status": "completed"})).Want(http.StatusBadRequest)
	testutil.Call(t, h.ReportDirectoryListResult, report(runtimeID, body)).Want(http.StatusOK)
	got, _ := h.DirectoryListStore.Get(ctx, req.ID)
	if got.Status != "failed" || got.Error != "directory does not exist" {
		t.Fatalf("failure = %+v", got)
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id='replacement-daemon' WHERE id=$1`, runtimeID)
	testutil.Call(t, h.ReportDirectoryListResult, report(runtimeID, body)).Want(http.StatusNotFound)
	testutil.Call(t, h.GetDirectoryListRequest, withURLParams(newRequestAsUser(testUserID, http.MethodGet, "/", nil), "runtimeId", runtimeID, "requestId", req.ID)).Want(http.StatusNotFound)
}

type directoryProbeStore struct {
	DirectoryListStore
	slow     bool
	popCalls int
}

func (s *directoryProbeStore) HasPending(ctx context.Context, _ string) (bool, error) {
	if s.slow {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return false, nil
}
func (s *directoryProbeStore) PopPending(context.Context, string) (*RuntimeDirectoryListRequest, error) {
	s.popCalls++
	return nil, nil
}
func TestDirectoryHeartbeatProbe(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	h := *testHandler
	original := heartbeatHasPendingTimeout
	heartbeatHasPendingTimeout = 10 * time.Millisecond
	t.Cleanup(func() { heartbeatHasPendingTimeout = original })
	for _, slow := range []bool{false, true} {
		store := &directoryProbeStore{DirectoryListStore: NewInMemoryDirectoryListStore(), slow: slow}
		h.DirectoryListStore = store
		start := time.Now()
		if _, _, err := h.processHeartbeat(context.Background(), "empty-runtime", false); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("probe blocked heartbeat for %s", elapsed)
		}
		if store.popCalls != 0 {
			t.Fatal("empty or timed-out probe claimed work")
		}
	}
}

func TestDirectoryStoreFailureAndRunningTimeout(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryDirectoryListStore()
	failed, _ := s.Create(ctx, "runtime", "daemon", "/missing")
	if _, err := s.PopPending(ctx, "runtime"); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(ctx, failed.ID, nil, "permission denied"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, failed.ID)
	if got.Status != "failed" || got.Error != "permission denied" || got.Result != nil {
		t.Fatalf("failure = %+v", got)
	}
	timed, _ := s.Create(ctx, "runtime", "daemon", "/slow")
	if _, err := s.PopPending(ctx, "runtime"); err != nil {
		t.Fatal(err)
	}
	s.requests[timed.ID].UpdatedAt = time.Now().Add(-directoryRunningTimeout - time.Second)
	if err := s.Finish(ctx, timed.ID, &protocol.DirectoryListing{Path: "/slow"}, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, timed.ID)
	if got.Status != "timeout" || got.Result != nil {
		t.Fatalf("late completion = %+v", got)
	}
}

func TestRedisDirectoryClaimAcrossNodes(t *testing.T) {
	rdb := newRedisTestClient(t)
	a, b := NewRedisDirectoryListStore(rdb), NewRedisDirectoryListStore(rdb)
	ctx := context.Background()
	req, err := a.Create(ctx, "runtime", "daemon", "/directory")
	if err != nil {
		t.Fatal(err)
	}
	claimed := make(chan *RuntimeDirectoryListRequest, 2)
	errors := make(chan error, 2)
	for _, store := range []*RedisDirectoryListStore{a, b} {
		go func(store *RedisDirectoryListStore) {
			got, err := store.PopPending(ctx, "runtime")
			claimed <- got
			errors <- err
		}(store)
	}
	count := 0
	for range 2 {
		got := <-claimed
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		if got != nil {
			count++
			if got.ID != req.ID || got.DaemonID != "daemon" {
				t.Fatalf("claim = %+v", got)
			}
		}
	}
	if count != 1 {
		t.Fatalf("claimed %d times", count)
	}
	got, err := a.Get(ctx, req.ID)
	if err != nil || got.Status != "running" {
		t.Fatalf("record after claim = %+v %v", got, err)
	}
}

func TestRedisDirectoryListTimeout(t *testing.T) {
	rdb := newRedisTestClient(t)
	store := NewRedisDirectoryListStore(rdb)
	ctx := context.Background()
	for _, status := range []string{"pending", "running"} {
		t.Run(status, func(t *testing.T) {
			req, err := store.Create(ctx, "runtime", "daemon", "/directory")
			if err != nil {
				t.Fatal(err)
			}
			if status == "running" {
				req, err = store.PopPending(ctx, "runtime")
				if err != nil || req == nil {
					t.Fatalf("claim = %+v %v", req, err)
				}
				req.UpdatedAt = time.Now().Add(-directoryRunningTimeout - time.Second)
			} else {
				req.CreatedAt = time.Now().Add(-directoryPendingTimeout - time.Second)
			}
			raw, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := rdb.Set(ctx, directoryKey(req.ID), raw, directoryRetention).Err(); err != nil {
				t.Fatal(err)
			}
			got, err := store.Get(ctx, req.ID)
			if err != nil || got.Status != "timeout" {
				t.Fatalf("timeout = %+v %v", got, err)
			}
			if pending, err := store.HasPending(ctx, "runtime"); err != nil || pending {
				t.Fatalf("pending after timeout = %t %v", pending, err)
			}
			if err := store.Finish(ctx, req.ID, &protocol.DirectoryListing{Path: "/directory"}, ""); err != nil {
				t.Fatal(err)
			}
			got, err = NewRedisDirectoryListStore(rdb).Get(ctx, req.ID)
			if err != nil || got.Status != "timeout" || got.Result != nil {
				t.Fatalf("late completion = %+v %v", got, err)
			}
		})
	}
}
