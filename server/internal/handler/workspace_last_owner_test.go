package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Two owners can each lose their owner role at the same moment: one is
// demoted or removed while the other is too. Each request alone leaves an
// owner behind, so both used to pass the last-owner check and the workspace
// was left with none, which nobody could undo because only an owner can make
// another owner. The check must hold against a demotion that commits while
// the request is in flight.
func TestLastOwnerCheckHoldsAgainstAConcurrentDemotion(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	cases := []struct {
		name string
		// call takes owner A's role away, acting as owner B.
		call func(workspaceID, ownerA, memberA, ownerB string) *http.Request
		run  http.HandlerFunc
	}{
		{
			name: "demote",
			call: func(workspaceID, _, memberA, ownerB string) *http.Request {
				return withURLParams(asUser(newRequest(http.MethodPatch, "/api/workspaces/"+workspaceID+"/members/"+memberA,
					UpdateMemberRequest{Role: "admin"}), ownerB), "id", workspaceID, "memberId", memberA)
			},
			run: testHandler.UpdateMember,
		},
		{
			name: "remove",
			call: func(workspaceID, _, memberA, ownerB string) *http.Request {
				return withURLParams(asUser(newRequest(http.MethodDelete, "/api/workspaces/"+workspaceID+"/members/"+memberA, nil),
					ownerB), "id", workspaceID, "memberId", memberA)
			},
			run: testHandler.DeleteMember,
		},
		{
			name: "leave",
			call: func(workspaceID, ownerA, _, _ string) *http.Request {
				return withURLParam(asUser(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/leave", nil),
					ownerA), "id", workspaceID)
			},
			run: testHandler.LeaveWorkspace,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			suffix := uuid.NewString()[:8]
			workspaceID := dbfx.Workspace(t, "Last owner "+suffix, "last-owner-"+suffix)
			dbfx.Cleanup(t, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
			ownerA := dbfx.User(t, "Owner A "+suffix, "last-owner-a-"+suffix+"@multica.test")
			ownerB := dbfx.User(t, "Owner B "+suffix, "last-owner-b-"+suffix+"@multica.test")
			memberA := dbfx.Member(t, workspaceID, ownerA, "owner")
			memberB := dbfx.Member(t, workspaceID, ownerB, "owner")

			// The competing request: owner B loses its role in a transaction
			// that holds the owner rows the way a concurrent role change does.
			holder, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin holder: %v", err)
			}
			defer holder.Rollback(ctx)
			var holderPID int
			if err := holder.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
				t.Fatalf("holder pid: %v", err)
			}
			if _, err := holder.Exec(ctx, `SELECT id FROM member WHERE workspace_id = $1 AND role = 'owner' ORDER BY id FOR UPDATE`, workspaceID); err != nil {
				t.Fatalf("lock owners: %v", err)
			}
			if _, err := holder.Exec(ctx, `UPDATE member SET role = 'admin' WHERE id = $1`, memberB); err != nil {
				t.Fatalf("demote owner B: %v", err)
			}

			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				tc.run(w, tc.call(workspaceID, ownerA, memberA, ownerB))
				done <- w
			}()
			waitForDemotionBlockedBy(t, holderPID, done)

			if err := holder.Commit(ctx); err != nil {
				t.Fatalf("commit holder: %v", err)
			}
			w := <-done
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s while the other owner was demoted: status %d, want 400: %s", tc.name, w.Code, w.Body.String())
			}

			var owners int
			dbfx.QueryRow(t, `SELECT count(*) FROM member WHERE workspace_id = $1 AND role = 'owner'`, workspaceID).Scan(&owners)
			if owners != 1 {
				t.Fatalf("workspace has %d owners, want 1", owners)
			}
		})
	}
}

// waitForDemotionBlockedBy returns once some backend waits on a lock held by
// holderPID, failing if the request finished without waiting.
func waitForDemotionBlockedBy(t *testing.T, holderPID int, done <-chan *httptest.ResponseRecorder) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case w := <-done:
			t.Fatalf("request finished before the competing demotion committed: %d %s", w.Code, w.Body.String())
		default:
		}
		var blocked bool
		if err := testPool.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))`, holderPID).Scan(&blocked); err != nil {
			t.Fatalf("observe blocked request: %v", err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("request did not wait on the competing demotion")
}
