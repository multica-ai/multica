package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAcceptInvitation_LegacyMemberPoolBudget(t *testing.T) {
	for _, tc := range []struct {
		name               string
		max                int32
		existing, capacity bool
		unmanaged          bool
	}{
		{"normal_managed_pool1", 1, false, true, false},
		{"existing_disabled_pool1", 1, true, false, false},
		{"existing_managed_pool2", 2, true, true, false},
		{"existing_managed_pool1", 1, true, true, false},
		{"existing_unmanaged_pool1", 1, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, workspace, member, invitation := legacyInvitationFixture(t, "admin", "member")
			if !tc.existing {
				dbfx.Exec(t, "DELETE FROM member WHERE id=$1", parseUUID(member))
			}
			cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			cfg.MaxConns = tc.max
			cfg.MinConns = 0
			pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			var mu sync.Mutex
			var actions []string
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				mu.Lock()
				actions = append(actions, action)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(seatcapacity.Decision{Managed: !tc.unmanaged, Allowed: true})
			}))
			defer cloud.Close()
			executor, err := seatcapacity.New(seatcapacity.Config{BaseURL: cloud.URL})
			if err != nil {
				t.Fatal(err)
			}
			h := *testHandler
			h.Queries = db.New(pool)
			h.TxStarter = pool
			h.SeatCapacity = nil
			if tc.capacity {
				h.SeatCapacity = executor
			}
			h.SeatCapacityLocker = seatcapacity.NewWorkspaceLocker(pool)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			req := newRequest("POST", "/api/invitations/"+invitation+"/accept", nil).WithContext(ctx)
			req.Header.Set("X-User-ID", user)
			req = withURLParam(req, "id", invitation)
			w := httptest.NewRecorder()
			start := time.Now()
			h.AcceptInvitation(w, req)
			var status string
			if err := testPool.QueryRow(context.Background(), "SELECT status FROM workspace_invitation WHERE id=$1", parseUUID(invitation)).Scan(&status); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			gotActions := append([]string(nil), actions...)
			mu.Unlock()
			t.Logf("max=%d existing=%v capacity=%v elapsed=%s HTTP=%d body=%s DBstatus=%s actions=%v", tc.max, tc.existing, tc.capacity, time.Since(start), w.Code, w.Body, status, gotActions)
			if !tc.existing {
				var got MemberWithUserResponse
				if json.Unmarshal(w.Body.Bytes(), &got) == nil && got.ID != "" {
					t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM member WHERE id=$1", parseUUID(got.ID)) })
				}
			}
			if pool.Stat().AcquiredConns() != 0 {
				t.Errorf("connection not returned: acquired=%d", pool.Stat().AcquiredConns())
			}
			wantActions := []string(nil)
			if tc.capacity {
				wantActions = []string{"release"}
				if !tc.existing {
					wantActions = []string{"consume", "confirm"}
				}
			}
			if !slices.Equal(gotActions, wantActions) {
				t.Errorf("capacity actions=%v want=%v", gotActions, wantActions)
			}
			if tc.existing {
				var got MemberWithUserResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.ID != member || got.Role != "admin" {
					t.Errorf("existing membership changed: %s", w.Body)
				}
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM seat_capacity_outbox WHERE operation_token=$1", parseUUID(invitation)); n != 0 {
				t.Error("capacity intent not settled")
			}
			// A different pool/session must be able to take the same workspace lock.
			lockCtx, lockCancel := context.WithTimeout(context.Background(), time.Second)
			defer lockCancel()
			_, unlock, err := seatcapacity.NewWorkspaceLocker(testPool).Lock(lockCtx, uuid.MustParse(workspace))
			if err != nil {
				t.Errorf("workspace lock leaked: %v", err)
			} else {
				unlock()
			}
			if w.Code != 200 || status != "accepted" {
				t.Errorf("valid acceptance must complete with one connection: HTTP=%d status=%s workspace=%s", w.Code, status, workspace)
			}
		})
	}
}

// Cancel at commit after both writes, so PostgreSQL must roll them back together.
type legacyCanceledCommit struct {
	pgx.Tx
	cancel context.CancelFunc
}

func (tx legacyCanceledCommit) Commit(ctx context.Context) error {
	tx.cancel()
	return tx.Tx.Commit(ctx)
}

func TestAcceptInvitation_LegacyMemberLockedTransactionFailures(t *testing.T) {
	for _, stage := range []string{"begin", "locked_membership_read", "commit", "cancel_commit"} {
		t.Run(stage, func(t *testing.T) {
			user, workspace, member, invitation := legacyInvitationFixture(t, "admin", "member")
			cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			cfg.MaxConns = 1
			cfg.MinConns = 0
			pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			calls := 0
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				http.Error(w, "release before commit", 500)
			}))
			defer cloud.Close()
			executor, err := seatcapacity.New(seatcapacity.Config{BaseURL: cloud.URL})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			h := *testHandler
			h.Queries = db.New(pool)
			h.SeatCapacity = executor
			h.TxStarter = legacyBeginHook(func(context.Context) (pgx.Tx, error) {
				t.Error("second pool acquisition attempted")
				return nil, errors.New("unexpected pool Begin")
			})
			h.SeatCapacityLocker = legacyWorkspaceLocker{seatcapacity.NewWorkspaceLocker(pool), func(ctx context.Context, starter txStarter) (pgx.Tx, error) {
				if stage == "begin" {
					return nil, errors.New("injected locked Begin failure")
				}
				tx, err := starter.Begin(ctx)
				if err != nil {
					return nil, err
				}
				switch stage {
				case "locked_membership_read":
					return legacyTxReadFailure{tx}, nil
				case "commit":
					return legacyCommitFailure{tx}, nil
				default:
					return legacyCanceledCommit{tx, cancel}, nil
				}
			}}
			req := newRequest("POST", "/api/invitations/"+invitation+"/accept", nil).WithContext(ctx)
			req.Header.Set("X-User-ID", user)
			req = withURLParam(req, "id", invitation)
			w := httptest.NewRecorder()
			h.AcceptInvitation(w, req)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("failure status=%d body=%s", w.Code, w.Body)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM workspace_invitation WHERE id=$1 AND status='pending'", parseUUID(invitation)); n != 1 {
				t.Error("invitation escaped rollback")
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM seat_capacity_outbox WHERE operation_token=$1", parseUUID(invitation)); n != 0 {
				t.Error("release intent escaped rollback")
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM member WHERE id=$1 AND role='admin'", parseUUID(member)); n != 1 {
				t.Error("membership changed")
			}
			if pool.Stat().AcquiredConns() != 0 {
				t.Errorf("connection leaked: %d", pool.Stat().AcquiredConns())
			}
			if calls != 0 {
				t.Error("capacity released before commit")
			}
			checkCtx, checkCancel := context.WithTimeout(context.Background(), time.Second)
			defer checkCancel()
			if err := pool.Ping(checkCtx); err != nil {
				t.Errorf("pool unusable after %s: %v", stage, err)
			}
			_, unlock, err := seatcapacity.NewWorkspaceLocker(testPool).Lock(checkCtx, uuid.MustParse(workspace))
			if err != nil {
				t.Errorf("lock leaked after %s: %v", stage, err)
			} else {
				unlock()
			}
		})
	}
}

type legacyWaitingWorkspaceLocker struct {
	seatcapacity.WorkspaceLocker
	waiting chan struct{}
}

func (l legacyWaitingWorkspaceLocker) Lock(ctx context.Context, workspaceID uuid.UUID) (db.DBTX, func(), error) {
	close(l.waiting)
	return l.WorkspaceLocker.Lock(ctx, workspaceID)
}

func TestAcceptInvitation_LegacyMemberLockedTransactionInvalidatesWorkerClaim(t *testing.T) {
	user, workspace, member, invitation := legacyInvitationFixture(t, "admin", "member")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	params := capacityIntentParams(uuid.MustParse(workspace), uuid.MustParse(invitation), seatcapacity.ActionConsumeInvitation, time.Now().Add(-time.Hour))
	params.SubjectID = parseUUID(invitation)
	params.InvitationID = parseUUID(invitation)
	params.UserID = parseUUID(user)
	if _, err := testHandler.Queries.UpsertSeatCapacityIntent(ctx, params); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var actions []string
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		actions = append(actions, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/capacity/release") {
			http.Error(w, "stale worker action", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(seatcapacity.Decision{Managed: true, Allowed: true})
	}))
	defer cloud.Close()
	executor, err := seatcapacity.New(seatcapacity.Config{BaseURL: cloud.URL})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	waiting := make(chan struct{})
	workerDone := make(chan error, 1)
	worker := seatcapacity.NewWorker(testHandler.Queries, executor, legacyWaitingWorkspaceLocker{seatcapacity.NewWorkspaceLocker(testPool), waiting}, seatcapacity.WorkerConfig{BatchSize: 1})
	h := *testHandler
	h.Queries = db.New(pool)
	h.TxStarter = pool
	h.SeatCapacity = executor
	h.SeatCapacityLocker = legacyWorkspaceLocker{seatcapacity.NewWorkspaceLocker(pool), func(ctx context.Context, starter txStarter) (pgx.Tx, error) {
		go func() { workerDone <- worker.ReconcileOnce(ctx) }()
		select {
		case <-waiting:
		case err := <-workerDone:
			return nil, errors.Join(errors.New("worker did not reach workspace lock"), err)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		claimed, err := testHandler.Queries.GetSeatCapacityIntent(ctx, parseUUID(invitation))
		if err != nil {
			return nil, err
		}
		if !claimed.LeaseToken.Valid || claimed.Action != seatcapacity.ActionConsumeInvitation {
			return nil, errors.New("worker did not claim old consume intent")
		}
		return starter.Begin(ctx)
	}}
	req := newRequest("POST", "/api/invitations/"+invitation+"/accept", nil).WithContext(ctx)
	req.Header.Set("X-User-ID", user)
	req = withURLParam(req, "id", invitation)
	w := httptest.NewRecorder()
	h.AcceptInvitation(w, req)
	select {
	case err := <-workerDone:
		if err != nil {
			t.Error(err)
		}
	case <-ctx.Done():
		t.Fatal("worker did not finish after lock release")
	}
	var got MemberWithUserResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.ID != member || got.Role != "admin" {
		t.Fatalf("accept: %d %s", w.Code, w.Body)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM workspace_invitation WHERE id=$1 AND status='accepted'", parseUUID(invitation)); n != 1 {
		t.Error("invitation not settled")
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM seat_capacity_outbox WHERE operation_token=$1", parseUUID(invitation)); n != 0 {
		t.Error("outbox not settled")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(actions) != 1 || !strings.HasSuffix(actions[0], "/capacity/release") {
		t.Fatalf("worker used stale claim: %v", actions)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Error("lock connection leaked")
	}
}
