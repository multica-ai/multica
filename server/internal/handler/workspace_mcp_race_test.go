package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Neither workspace_mcp_server nor agent_mcp_server carries a foreign key
// (repo rule), so nothing in the database catches a write that lands after the
// row it depends on is gone. These two tests drive the actual races with two
// concurrent transactions rather than asserting the sequential outcome, which
// is what a lock protocol has to be tested against.
//
// They reuse the fence-test helpers from workspace_delete_fence_test.go:
// waitForBlockedWriter proves the second transaction is genuinely parked on a
// lock instead of merely being slow.

// A create arriving mid-teardown must not leave a library row pointing at a
// workspace that no longer exists.
func TestWorkspaceMcpServerCreate_CannotLandAfterWorkspaceTeardownCommits(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var victimID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('MCP Race WS', 'ws-mcp-race-create', '', 'MRC')
		RETURNING id
	`).Scan(&victimID); err != nil {
		t.Fatalf("create victim workspace: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1`, victimID)
		testPool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, victimID)
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		victimID, testUserID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	teardown, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin teardown: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = teardown.Rollback(ctx)
		}
	}()
	// The order DeleteWorkspace uses: take the row exclusively, then sweep.
	if _, err := teardown.Exec(ctx, `SELECT id FROM workspace WHERE id = $1 FOR UPDATE`, victimID); err != nil {
		t.Fatalf("lock workspace: %v", err)
	}

	var wg sync.WaitGroup
	var writerCode int
	writerPID := make(chan int32, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		bg := context.Background()
		conn, err := testPool.Acquire(bg)
		if err != nil {
			close(writerPID)
			return
		}
		defer conn.Release()
		var pid int32
		if err := conn.QueryRow(bg, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			close(writerPID)
			return
		}
		writerPID <- pid

		// The handler's own transaction, on this connection, so the race waits
		// on a backend the test can observe.
		h := *testHandler
		h.TxStarter = conn
		h.Queries = testHandler.Queries

		req := newRequest(http.MethodPost, "/api/workspaces/"+victimID+"/mcp-servers", map[string]any{
			"name":   "raced",
			"config": map[string]any{"url": "https://mcp.example"},
		})
		req.Header.Set("X-Workspace-ID", victimID)
		req = withURLParam(req, "id", victimID)
		w := httptest.NewRecorder()
		h.CreateWorkspaceMcpServer(w, req)
		writerCode = w.Code
	}()

	pid, ok := <-writerPID
	if !ok {
		wg.Wait()
		t.Fatal("writer could not start")
	}
	waitForBlockedWriter(t, ctx, pid)

	if _, err := teardown.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, victimID); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}
	if err := teardown.Commit(ctx); err != nil {
		t.Fatalf("commit teardown: %v", err)
	}
	committed = true

	wg.Wait()

	if writerCode == http.StatusCreated {
		t.Fatalf("the queued create landed after teardown committed (status %d); the fence did not hold", writerCode)
	}
	var orphans int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM workspace_mcp_server WHERE workspace_id = $1`, victimID).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("teardown left %d MCP server row(s) behind for a deleted workspace", orphans)
	}
}

// An assignment arriving while a library entry is being deleted must not leave
// a binding to a server that no longer exists.
func TestAgentMcpServerAdd_CannotLandAfterServerDeleteCommits(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	serverID := createWorkspaceMcpServerForTest(t, "raced-server", `{"url":"https://mcp.example"}`)
	agentID := createHandlerTestAgent(t, "ws-mcp-race-agent", nil)

	deleter, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin deleter: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = deleter.Rollback(ctx)
		}
	}()
	// The order DeleteWorkspaceMcpServer uses: take the server row
	// exclusively, then delete and sweep its bindings.
	if _, err := deleter.Exec(ctx,
		`SELECT id FROM workspace_mcp_server WHERE id = $1 FOR UPDATE`, serverID); err != nil {
		t.Fatalf("lock server: %v", err)
	}

	var wg sync.WaitGroup
	var writerCode int
	writerPID := make(chan int32, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		bg := context.Background()
		conn, err := testPool.Acquire(bg)
		if err != nil {
			close(writerPID)
			return
		}
		defer conn.Release()
		var pid int32
		if err := conn.QueryRow(bg, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			close(writerPID)
			return
		}
		writerPID <- pid

		h := *testHandler
		h.TxStarter = conn
		h.Queries = testHandler.Queries

		req := newRequest(http.MethodPost, "/api/agents/"+agentID+"/mcp-servers",
			map[string]any{"server_id": serverID})
		req = withURLParam(req, "id", agentID)
		w := httptest.NewRecorder()
		h.AddAgentMcpServer(w, req)
		writerCode = w.Code
	}()

	pid, ok := <-writerPID
	if !ok {
		wg.Wait()
		t.Fatal("writer could not start")
	}
	waitForBlockedWriter(t, ctx, pid)

	if _, err := deleter.Exec(ctx, `DELETE FROM agent_mcp_server WHERE server_id = $1`, serverID); err != nil {
		t.Fatalf("sweep bindings: %v", err)
	}
	if _, err := deleter.Exec(ctx, `DELETE FROM workspace_mcp_server WHERE id = $1`, serverID); err != nil {
		t.Fatalf("delete server: %v", err)
	}
	if err := deleter.Commit(ctx); err != nil {
		t.Fatalf("commit delete: %v", err)
	}
	committed = true

	wg.Wait()

	if writerCode == http.StatusOK {
		t.Fatalf("the queued assignment landed after the server was deleted (status %d)", writerCode)
	}
	var orphans int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_mcp_server WHERE server_id = $1`, serverID).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("delete left %d orphaned binding(s)", orphans)
	}
}
