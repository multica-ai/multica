package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRuntimeBindingLocksWorkspaceBeforeRuntime(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	for _, tt := range []struct {
		entry      string
		wantStatus int
	}{
		{entry: "user create", wantStatus: http.StatusCreated},
		{entry: "user rebind", wantStatus: http.StatusOK},
		{entry: "mika create", wantStatus: http.StatusCreated},
		{entry: "builder create", wantStatus: http.StatusCreated},
		{entry: "builder switch", wantStatus: http.StatusOK},
	} {
		t.Run(tt.entry, func(t *testing.T) {
			h := *testHandler
			fx, runtimeID, handler, req := runtimeBindingVisibilityRequest(t, &h, tt.entry)
			fx.Cleanup(t, `DELETE FROM agent WHERE workspace_id = $1`, fx.WorkspaceID)
			fx.Cleanup(t, `DELETE FROM chat_session WHERE workspace_id = $1`, fx.WorkspaceID)
			ctx, cancel := context.WithTimeout(req.Context(), 20*time.Second)
			defer cancel()
			req = req.WithContext(ctx)
			holder, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin workspace delete lock: %v", err)
			}
			defer holder.Rollback(context.Background())
			if _, err := db.New(holder).LockWorkspaceForDelete(ctx, parseUUID(fx.WorkspaceID)); err != nil {
				t.Fatalf("lock workspace as deletion would: %v", err)
			}
			holderPID := holderBackendPID(t, ctx, holder)
			responses := make(chan *testutil.Response, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				responses <- testutil.Call(t, handler, req)
			}()
			defer func() {
				cancel()
				_ = holder.Rollback(context.Background())
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("binding request did not exit after cancellation")
				}
			}()
			if !waitForWaiterBlockedBy(t, holderPID, 10*time.Second) {
				select {
				case response := <-responses:
					t.Fatalf("binding returned %d while workspace was locked: %s", response.Code, response.Text())
				default:
					t.Fatal("binding did not wait for the workspace lock")
				}
			}

			// While the binding waits for the workspace, another transaction
			// must still be able to lock the runtime. Otherwise deletion can
			// hold workspace -> wait runtime while binding does the reverse.
			probe, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin runtime lock probe: %v", err)
			}
			defer probe.Rollback(context.Background())
			if _, err := probe.Exec(ctx, `SELECT id FROM agent_runtime WHERE id = $1 FOR NO KEY UPDATE NOWAIT`, runtimeID); err != nil {
				t.Fatalf("runtime was locked before binding acquired workspace: %v", err)
			}
			if err := probe.Rollback(ctx); err != nil {
				t.Fatalf("release runtime lock probe: %v", err)
			}
			if err := holder.Commit(ctx); err != nil {
				t.Fatalf("release workspace lock: %v", err)
			}
			select {
			case response := <-responses:
				response.Want(tt.wantStatus)
			case <-ctx.Done():
				t.Fatal("binding did not finish after workspace lock was released")
			}
			if got := fx.Count(t, `SELECT count(*) FROM agent WHERE runtime_id = $1`, runtimeID); got != 1 {
				t.Fatalf("successful binding left %d agents on the runtime, want 1", got)
			}
		})
	}
}
