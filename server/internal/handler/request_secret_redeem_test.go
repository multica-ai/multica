package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/requestsecret"
)

// These tests exercise the request-secret redeem handler end-to-end: the
// handler is now mounted under /api/daemon and authorizes by DAEMON identity
// (not the mat_ task token), binds the supplied task_id to the daemon's
// workspace, and derives the Consume principal from the task's server-side
// originator_user_id. They therefore need a real task row and run only when
// the handler package's TestMain finds a reachable Postgres (it os.Exit(0)-
// skips the whole package otherwise).

// seedRedeemTask creates an issue + task in the test workspace with
// originator_user_id = testUserID and returns the task id, plus a cleanup.
func seedRedeemTask(t *testing.T) (taskID string) {
	t.Helper()
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type)
		VALUES ($1, 'request-secret-redeem-test', 'todo', 'medium', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, runtime_id, originator_user_id)
		VALUES ($1, $2, 'queued', $3, $4)
		RETURNING id
	`, agentID, issueID, runtimeID, testUserID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// withStore installs a store on the shared testHandler.TaskService for the
// duration of the test and restores the prior value afterwards.
func withStore(t *testing.T, store *requestsecret.Store) {
	t.Helper()
	prev := testHandler.TaskService.RequestSecrets
	testHandler.TaskService.RequestSecrets = store
	t.Cleanup(func() { testHandler.TaskService.RequestSecrets = prev })
}

// callRedeem posts {handle, task_id} with a daemon context for workspaceID.
// A blank workspaceID means "no daemon identity" (simulating a non-daemon
// token that fell through to this endpoint).
func callRedeem(workspaceID, daemonID, taskID, handle string) *httptest.ResponseRecorder {
	body := map[string]string{"handle": handle, "task_id": taskID}
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/request-secret/redeem", body, workspaceID, daemonID)
	if workspaceID == "" {
		// newDaemonTokenRequest always stamps a daemon context; rebuild a bare
		// request so DaemonWorkspaceIDFromContext is empty.
		buf, _ := json.Marshal(body)
		req = httptest.NewRequest(http.MethodPost, "/api/daemon/tasks/request-secret/redeem", bytes.NewReader(buf))
		req.Header.Set("Content-Type", "application/json")
	}
	rctx := chi.NewRouteContext()
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	testHandler.RedeemRequestSecret(w, req)
	return w
}

func TestRedeemRequestSecret_HappyPath_Mint_Redeem(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedRedeemTask(t)
	store := requestsecret.New(requestsecret.DefaultTTL)
	withStore(t, store)

	handle, err := store.Issue(testUserID, taskID, "jwt-secret-xyz")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	w := callRedeem(testWorkspaceID, "legit-daemon", taskID, handle)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Secret != "jwt-secret-xyz" {
		t.Fatalf("expected minted secret, got %q", resp.Secret)
	}
	if store.Len() != 0 {
		t.Fatalf("expected store emptied after consume, len=%d", store.Len())
	}
}

func TestRedeemRequestSecret_ReplayRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedRedeemTask(t)
	store := requestsecret.New(requestsecret.DefaultTTL)
	withStore(t, store)
	handle, _ := store.Issue(testUserID, taskID, "jwt-secret-xyz")

	if w := callRedeem(testWorkspaceID, "legit-daemon", taskID, handle); w.Code != http.StatusOK {
		t.Fatalf("first redeem should succeed, got %d (%s)", w.Code, w.Body.String())
	}
	if w := callRedeem(testWorkspaceID, "legit-daemon", taskID, handle); w.Code != http.StatusNotFound {
		t.Fatalf("replay should be 404, got %d", w.Code)
	}
}

func TestRedeemRequestSecret_ExpiryRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedRedeemTask(t)
	store := requestsecret.New(50 * time.Millisecond)
	clock := time.Now()
	store.SetClock(func() time.Time { return clock })
	withStore(t, store)
	handle, _ := store.Issue(testUserID, taskID, "jwt-secret-xyz")

	clock = clock.Add(time.Second)
	if w := callRedeem(testWorkspaceID, "legit-daemon", taskID, handle); w.Code != http.StatusNotFound {
		t.Fatalf("expired handle should be 404, got %d", w.Code)
	}
}

func TestRedeemRequestSecret_WrongPrincipalNotBurned(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedRedeemTask(t)
	store := requestsecret.New(requestsecret.DefaultTTL)
	withStore(t, store)
	// Mint the handle against a DIFFERENT principal than the task's originator.
	otherPrincipal := "99999999-9999-9999-9999-999999999999"
	handle, _ := store.Issue(otherPrincipal, taskID, "jwt-secret-xyz")

	// Redeem derives the principal from the task originator (testUserID), which
	// does not match — 404, and the handle must NOT be burned.
	if w := callRedeem(testWorkspaceID, "legit-daemon", taskID, handle); w.Code != http.StatusNotFound {
		t.Fatalf("principal mismatch should be 404, got %d", w.Code)
	}
	if store.Len() != 1 {
		t.Fatalf("principal-mismatch probe must not consume the handle, len=%d", store.Len())
	}
}

func TestRedeemRequestSecret_CrossWorkspaceDaemonRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedRedeemTask(t)
	store := requestsecret.New(requestsecret.DefaultTTL)
	withStore(t, store)
	handle, _ := store.Issue(testUserID, taskID, "jwt-secret-xyz")

	// A daemon from a different workspace must not redeem this task's handle —
	// task↔workspace binding collapses to 404 before the store is touched.
	if w := callRedeem("00000000-0000-0000-0000-000000000000", "attacker-daemon", taskID, handle); w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace daemon should be 404, got %d", w.Code)
	}
	if store.Len() != 1 {
		t.Fatalf("cross-workspace probe must not consume the handle, len=%d", store.Len())
	}
	// The legitimate daemon can still redeem it.
	if w := callRedeem(testWorkspaceID, "legit-daemon", taskID, handle); w.Code != http.StatusOK {
		t.Fatalf("legitimate redeem after cross-workspace probe should succeed, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestRedeemRequestSecret_NonDaemonIdentityForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedRedeemTask(t)
	store := requestsecret.New(requestsecret.DefaultTTL)
	withStore(t, store)
	handle, _ := store.Issue(testUserID, taskID, "jwt-secret-xyz")

	// No daemon workspace context (a non-daemon token that reached here) is
	// refused before the store is touched.
	if w := callRedeem("", "", taskID, handle); w.Code != http.StatusForbidden {
		t.Fatalf("non-daemon identity should be 403, got %d", w.Code)
	}
	if store.Len() != 1 {
		t.Fatalf("forbidden request must not consume the handle, len=%d", store.Len())
	}
}

func TestRedeemRequestSecret_NilStoreFailsClosed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedRedeemTask(t)
	withStore(t, nil) // no store wired → 503, never 200 with a secret.
	if w := callRedeem(testWorkspaceID, "legit-daemon", taskID, "any-handle"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store should be 503, got %d", w.Code)
	}
}

func TestRedeemRequestSecret_MissingFieldsBadRequest(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := requestsecret.New(requestsecret.DefaultTTL)
	withStore(t, store)
	if w := callRedeem(testWorkspaceID, "legit-daemon", "some-task", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("missing handle should be 400, got %d", w.Code)
	}
	if w := callRedeem(testWorkspaceID, "legit-daemon", "", "some-handle"); w.Code != http.StatusBadRequest {
		t.Fatalf("missing task id should be 400, got %d", w.Code)
	}
}
