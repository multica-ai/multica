package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func membershipKagentFixture(t *testing.T) revocationFixture {
	t.Helper()
	id := uuid.NewString()
	fx := setupRevocationFixture(t, "membership-kagent-"+id, "membership-daemon-"+id)
	dbfx.Exec(t, `UPDATE agent_runtime SET status='online' WHERE id=$1`, fx.RuntimeID)
	return fx
}

func membershipKagentRequest(fx revocationFixture, route string) *http.Request {
	req := withURLParam(newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID, nil), "id", fx.WorkspaceID)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	if route == "leave" {
		req.Header.Set("X-User-ID", fx.TargetUserID)
	} else {
		req = withURLParam(req, "memberId", fx.MemberID)
	}
	return req
}

func membershipKagentRevoke(ctx context.Context, h *Handler, fx revocationFixture, route string) error {
	if route == "helper" {
		_, err := h.revokeAndRemoveMember(ctx, parseUUID(fx.WorkspaceID), parseUUID(fx.TargetUserID), parseUUID(fx.MemberID), parseUUID(testUserID))
		return err
	}
	w := httptest.NewRecorder()
	req := membershipKagentRequest(fx, route).WithContext(ctx)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fx.WorkspaceID)
	rctx.URLParams.Add("memberId", fx.MemberID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	if route == "leave" {
		h.LeaveWorkspace(w, req)
	} else {
		h.DeleteMember(w, req)
	}
	if w.Code == http.StatusNoContent {
		return nil
	}
	// workspace.go maps the helper's typed conflict to 409 with
	// kagentTeardownBlockedMessage.
	if w.Code == http.StatusConflict {
		if !strings.Contains(w.Body.String(), kagentTeardownBlockedMessage) {
			return fmt.Errorf("unexpected conflict body: %s", w.Body.String())
		}
		return service.ErrRuntimeKagentEnrollment
	}
	return errors.New(w.Body.String())
}

func membershipKagentAssertIntact(t *testing.T, fx revocationFixture) {
	t.Helper()
	var intact bool
	dbfx.QueryRow(t, `SELECT
		EXISTS(SELECT 1 FROM member WHERE id=$1) AND
		EXISTS(SELECT 1 FROM agent_runtime WHERE id=$2 AND status='online') AND
		EXISTS(SELECT 1 FROM agent WHERE id=$3 AND archived_at IS NULL) AND
		EXISTS(SELECT 1 FROM agent_task_queue WHERE id=$4 AND status='queued') AND
		EXISTS(SELECT 1 FROM daemon_token WHERE token_hash=$5)`,
		fx.MemberID, fx.RuntimeID, fx.AgentID, fx.TaskID, fx.TokenHash).Scan(&intact)
	if !intact {
		t.Fatal("blocked revocation changed membership, runtime, agent, task, or token")
	}
}

func TestMembershipKagentRevocationPreflight(t *testing.T) {
	for _, route := range []string{"helper", "remove", "leave"} {
		for _, location := range []string{"owned", "orphan-owned", "orphan-missing-runtime", "shared", "public", "member-agent-other-private", "unbound-owner", "archived-runtime-agent"} {
			for _, state := range []struct{ desired, phase string }{
				{"present", "pending"}, {"present", "ready"}, {"present", "blocked"},
				{"absent", "draining"}, {"absent", "deleting"}, {"absent", "blocked"}, {"absent", "deleted"},
			} {
				t.Run(route+"/"+location+"/"+state.desired+"/"+state.phase, func(t *testing.T) {
					fx := membershipKagentFixture(t)
					rt, agentID := fx.RuntimeID, fx.AgentID
					switch location {
					case "orphan-owned":
						agentID = uuid.NewString()
					case "orphan-missing-runtime":
						rt, agentID = uuid.NewString(), uuid.NewString()
					case "shared", "public", "member-agent-other-private":
						rt = dbfx.Runtime(t, "Other owner's runtime", testutil.Cols{
							"workspace_id": fx.WorkspaceID, "owner_id": testUserID,
							"provider": "claude", "visibility": "private",
						})
						if location == "shared" {
							dbfx.Exec(t, `UPDATE agent_runtime SET shared_with=ARRAY[$1::uuid] WHERE id=$2`, fx.TargetUserID, rt)
						} else if location == "public" {
							dbfx.Exec(t, `UPDATE agent_runtime SET visibility='public' WHERE id=$1`, rt)
						}
						if location == "member-agent-other-private" {
							dbfx.Exec(t, `UPDATE agent SET runtime_id=$1 WHERE id=$2`, rt, agentID)
						} else {
							agentID = uuid.NewString()
						}
					case "unbound-owner":
						rt = uuid.NewString()
						dbfx.Exec(t, `UPDATE agent SET runtime_id=NULL WHERE id=$1`, agentID)
					case "archived-runtime-agent":
						// Archiving by runtime also affects somebody else's agents,
						// even when their intent points to a historical runtime.
						dbfx.Exec(t, `UPDATE agent SET owner_id=$1 WHERE id=$2`, testUserID, agentID)
						rt = uuid.NewString()
					}
					cols := testutil.Cols{"workspace_id": fx.WorkspaceID, "runtime_id": rt, "agent_id": agentID, "desired_state": state.desired, "phase": state.phase}
					if state.phase == "ready" || state.phase == "deleted" {
						cols["applied_generation"] = 1
					}
					dbfx.InsertNoID(t, "kagent_enrollment", cols, "agent_id=$1", agentID)
					if err := membershipKagentRevoke(context.Background(), testHandler, fx, route); !errors.Is(err, service.ErrRuntimeKagentEnrollment) {
						t.Fatalf("expected typed enrollment refusal, got %v", err)
					}
					membershipKagentAssertIntact(t, fx)
					var retained bool
					dbfx.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM kagent_enrollment WHERE agent_id=$1 AND desired_state=$2 AND phase=$3)`, agentID, state.desired, state.phase).Scan(&retained)
					if !retained {
						t.Fatal("enrollment intent changed")
					}
					if location == "shared" {
						var shared bool
						dbfx.QueryRow(t, `SELECT $1::uuid=ANY(shared_with) FROM agent_runtime WHERE id=$2`, fx.TargetUserID, rt).Scan(&shared)
						if !shared {
							t.Fatal("blocked revocation removed runtime grant")
						}
					}
				})
			}
		}
	}
}

func TestMembershipKagentRevocationUnrelatedIntentDoesNotBlock(t *testing.T) {
	for _, route := range []string{"helper", "remove", "leave"} {
		t.Run(route, func(t *testing.T) {
			fx := membershipKagentFixture(t)
			rt := dbfx.Runtime(t, "Unrelated private runtime", testutil.Cols{"workspace_id": fx.WorkspaceID, "owner_id": testUserID, "visibility": "private"})
			a := uuid.NewString()
			dbfx.InsertNoID(t, "kagent_enrollment", testutil.Cols{"workspace_id": fx.WorkspaceID, "runtime_id": rt, "agent_id": a}, "agent_id=$1", a)
			if err := membershipKagentRevoke(context.Background(), testHandler, fx, route); err != nil {
				t.Fatal(err)
			}
			assertRevoked(t, fx)
		})
	}
}

type membershipKagentCommitBarrier struct {
	txStarter
	entered, release chan struct{}
}

func (b membershipKagentCommitBarrier) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return membershipKagentBarrierTx{Tx: tx, entered: b.entered, release: b.release}, nil
}

type membershipKagentBarrierTx struct {
	pgx.Tx
	entered, release chan struct{}
}

func (tx membershipKagentBarrierTx) Commit(ctx context.Context) error {
	close(tx.entered)
	select {
	case <-tx.release:
		return tx.Tx.Commit(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestMembershipKagentRevocationRacingEnrollment(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	for _, route := range []string{"helper", "remove", "leave"} {
		t.Run(route, func(t *testing.T) {
			fx := membershipKagentFixture(t)
			dbfx.Exec(t, `UPDATE agent_runtime SET provider='kagent' WHERE id=$1`, fx.RuntimeID)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			creatorConn, err := testPool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer creatorConn.Release()
			revokerConn, err := testPool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer revokerConn.Release()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			creator, revoker := *testHandler, *testHandler
			creator.TxStarter = membershipKagentCommitBarrier{txStarter: creatorConn, entered: entered, release: release}
			revoker.TxStarter = revokerConn
			created := httptest.NewRecorder()
			createDone, revokeDone := make(chan struct{}), make(chan struct{})
			var revokeErr error
			defer func() { unblock(); cancel(); <-createDone; <-revokeDone }()
			go func() {
				defer close(createDone)
				req := newRequest("POST", "/api/agents", map[string]any{"name": "Racing member enrollment", "runtime_id": fx.RuntimeID, "runtime_config": map[string]string{"mode": "native-text"}})
				req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
				req.Header.Set("X-User-ID", fx.TargetUserID)
				creator.CreateAgent(created, req.WithContext(ctx))
			}()
			select {
			case <-entered:
			case <-createDone:
				close(revokeDone)
				t.Fatalf("creation failed before commit: %d %s", created.Code, created.Body.String())
			case <-ctx.Done():
				close(revokeDone)
				t.Fatal(ctx.Err())
			}
			go func() {
				defer close(revokeDone)
				revokeErr = membershipKagentRevoke(ctx, &revoker, fx, route)
			}()
			waitForBlockedWriter(t, ctx, int32(revokerConn.Conn().PgConn().PID()))
			unblock()
			<-createDone
			<-revokeDone
			if created.Code != http.StatusCreated || !errors.Is(revokeErr, service.ErrRuntimeKagentEnrollment) {
				t.Fatalf("create=%d %s revoke=%v", created.Code, created.Body.String(), revokeErr)
			}
			membershipKagentAssertIntact(t, fx)
		})
	}
}

func TestMembershipKagentRevocationHoldsRuntimeLocksThroughCommit(t *testing.T) {
	for _, route := range []string{"helper", "remove", "leave"} {
		t.Run(route, func(t *testing.T) {
			fx := membershipKagentFixture(t)
			public := dbfx.Runtime(t, "Public commit fence", testutil.Cols{"workspace_id": fx.WorkspaceID, "owner_id": testUserID, "visibility": "public"})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			conn, err := testPool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Release()
			waiter, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer waiter.Rollback(context.Background())
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			h := *testHandler
			h.TxStarter = membershipKagentCommitBarrier{txStarter: conn, entered: entered, release: release}
			done, lockDone := make(chan struct{}), make(chan struct{})
			var revokeErr, lockErr error
			defer func() { unblock(); cancel(); <-done; <-lockDone }()
			go func() {
				defer close(done)
				revokeErr = membershipKagentRevoke(ctx, &h, fx, route)
			}()
			select {
			case <-entered:
			case <-done:
				close(lockDone)
				t.Fatalf("revocation failed before commit: %v", revokeErr)
			case <-ctx.Done():
				close(lockDone)
				t.Fatal(ctx.Err())
			}
			// Probe an otherwise untouched public runtime without the workspace
			// fence, proving the preflight lock itself remains held until commit.
			go func() {
				defer close(lockDone)
				_, lockErr = waiter.Exec(ctx, `SELECT id FROM agent_runtime WHERE id=$1 FOR NO KEY UPDATE`, public)
			}()
			waitForBlockedWriter(t, ctx, int32(waiter.Conn().PgConn().PID()))
			membershipKagentAssertIntact(t, fx)
			unblock()
			<-done
			<-lockDone
			if revokeErr != nil || lockErr != nil {
				t.Fatalf("revoke=%v lock=%v", revokeErr, lockErr)
			}
			if err := waiter.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			assertRevoked(t, fx)
		})
	}
}
