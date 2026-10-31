package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDaemonEnrollmentFailsClosedWithoutAuthority(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true })
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/register", nil)
	req.Header.Set("Authorization", "Bearer mde_untrusted")
	req.Header.Set("X-Actor-Source", "human")
	req.Header.Set("X-Agent-ID", "forged-agent")
	res := httptest.NewRecorder()
	DaemonAuth(nil, nil, nil, nil)(next).ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable || nextCalled {
		t.Fatalf("unavailable enrollment authority must deny, status=%d next=%v", res.Code, nextCalled)
	}
	if req.Header.Get("X-Actor-Source") != "" || req.Header.Get("X-Agent-ID") != "" {
		t.Fatal("untrusted actor headers must be stripped before enrollment auth")
	}
}

func TestDerivedEnrollmentFailsClosedWithoutAuthority(t *testing.T) {
	called := false
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/tasks/claim", nil)
	req.Header.Set("Authorization", "Bearer mds_untrusted")
	req.Header.Set("X-User-ID", "forged-user")
	res := httptest.NewRecorder()
	DaemonAuth(nil, nil, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })).ServeHTTP(res, req)
	if called || res.Code != http.StatusServiceUnavailable {
		t.Fatalf("derived enrollment must fail closed: status %d called %v", res.Code, called)
	}
}

type enrollmentBudgetTestStarter struct {
	calls int
	fail  bool
}

func (s *enrollmentBudgetTestStarter) Begin(context.Context) (pgx.Tx, error) {
	s.calls++
	if s.fail {
		return nil, errors.New("database unavailable")
	}
	return &enrollmentBudgetTestTx{}, nil
}

type enrollmentBudgetTestTx struct{ pgx.Tx }

func (*enrollmentBudgetTestTx) Commit(context.Context) error   { return nil }
func (*enrollmentBudgetTestTx) Rollback(context.Context) error { return nil }

func TestEnrollmentBudgetLeavesConnectionsForProtectedHandlers(t *testing.T) {
	starter := &enrollmentBudgetTestStarter{}
	budget := &enrollmentTxBudget{starter: starter, slots: make(chan struct{}, 1)}
	ctx := context.Background()
	tx, err := budget.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.Begin(ctx); err == nil || starter.calls != 1 {
		t.Fatalf("exhausted authorization budget must not acquire another pool connection: %v, calls=%d", err, starter.calls)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	next, err := budget.Begin(ctx)
	if err != nil {
		t.Fatalf("completed authorization did not release capacity: %v", err)
	}
	if err := next.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentBudgetReturnsCapacityAfterDatabaseFailure(t *testing.T) {
	starter := &enrollmentBudgetTestStarter{fail: true}
	budget := &enrollmentTxBudget{starter: starter, slots: make(chan struct{}, 1)}
	if _, err := budget.Begin(context.Background()); err == nil {
		t.Fatal("expected database failure")
	}
	starter.fail = false
	tx, err := budget.Begin(context.Background())
	if err != nil {
		t.Fatalf("failed authorization leaked capacity: %v", err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
}
