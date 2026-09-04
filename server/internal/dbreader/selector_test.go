package dbreader

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var testBusiness = BusinessDashboard

type recorderStub struct {
	routes []selection
	labels []string
}

func (r *recorderStub) RecordReadRoute(business, role, reason string) {
	r.routes = append(r.routes, selection{role: Role(role), reason: Reason(reason)})
	r.labels = append(r.labels, business)
}

func testSelector() (*Selector, *db.Queries, *db.Queries, *recorderStub) {
	primary := &db.Queries{}
	replica := &db.Queries{}
	recorder := &recorderStub{}
	selector := newSelector(
		primary,
		replica,
		recorder,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	return selector, primary, replica, recorder
}

func TestNewKeepsProvidedQueryHandles(t *testing.T) {
	primary := &db.Queries{}
	replica := &db.Queries{}
	selector := New(primary, replica, nil)

	if selector.primary != primary || selector.replica != replica {
		t.Fatal("New replaced the provided query handles")
	}
}

func TestPrimaryOnlySelectorPreservesExistingRouting(t *testing.T) {
	primary := &db.Queries{}
	selector := NewPrimaryOnly(primary)

	got, err := Read(context.Background(), selector, testBusiness, EventualConsistency,
		func(_ context.Context, queries *db.Queries) (*db.Queries, error) {
			return queries, nil
		})
	if err != nil || got != primary {
		t.Fatalf("Read = %#v, %v; want primary", got, err)
	}
}

func TestStrongConsistencyAlwaysUsesPrimary(t *testing.T) {
	selector, primary, _, _ := testSelector()

	got, err := Read(context.Background(), selector, testBusiness, StrongConsistency,
		func(_ context.Context, queries *db.Queries) (*db.Queries, error) {
			return queries, nil
		})
	if err != nil || got != primary {
		t.Fatalf("Read = %#v, %v; want primary", got, err)
	}
}

func TestEventualConsistencyUsesConfiguredReplica(t *testing.T) {
	selector, _, replica, recorder := testSelector()

	got, err := Read(context.Background(), selector, testBusiness, EventualConsistency,
		func(_ context.Context, queries *db.Queries) (*db.Queries, error) {
			return queries, nil
		})
	if err != nil || got != replica {
		t.Fatalf("Read = %#v, %v; want replica", got, err)
	}
	if len(recorder.routes) != 1 || recorder.routes[0].role != RoleReplica {
		t.Fatalf("routes = %#v, want one replica route", recorder.routes)
	}
}

func TestUnknownBusinessUsesBoundedMetricLabel(t *testing.T) {
	selector, _, _, recorder := testSelector()

	_, err := Read(context.Background(), selector, Business{}, EventualConsistency,
		func(_ context.Context, _ *db.Queries) (string, error) {
			return "replica result", nil
		})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(recorder.labels) != 1 || recorder.labels[0] != businessUnknown.label {
		t.Fatalf("business labels = %#v, want bounded unknown label", recorder.labels)
	}
}

func TestReadFallsBackOnceForServerConnectionError(t *testing.T) {
	selector, primary, replica, recorder := testSelector()
	var calls []*db.Queries

	got, err := Read(context.Background(), selector, testBusiness, EventualConsistency,
		func(_ context.Context, queries *db.Queries) (string, error) {
			calls = append(calls, queries)
			if queries == replica {
				return "", &pgconn.PgError{Code: "08006", Message: "connection failure"}
			}
			return "primary result", nil
		})
	if err != nil || got != "primary result" {
		t.Fatalf("Read = %q, %v; want primary result", got, err)
	}
	if len(calls) != 2 || calls[0] != replica || calls[1] != primary {
		t.Fatalf("calls = %#v, want replica then primary", calls)
	}
	if len(recorder.routes) != 2 || recorder.routes[1].role != RolePrimary || recorder.routes[1].reason != ReasonConnectionFailed {
		t.Fatalf("routes = %#v, want replica then connection-failed primary", recorder.routes)
	}
}

func TestConnectErrorFallsBackToPrimary(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://multica:multica@127.0.0.1:1/multica?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("create unreachable pool: %v", err)
	}
	defer pool.Close()
	connectErr := pool.Ping(context.Background())
	if connectErr == nil {
		t.Fatal("unreachable pool unexpectedly connected")
	}
	var typedConnectErr *pgconn.ConnectError
	if !errors.As(connectErr, &typedConnectErr) {
		t.Fatalf("Ping error = %T, want *pgconn.ConnectError", connectErr)
	}
	if !shouldFallback(context.Background(), connectErr) {
		t.Fatalf("shouldFallback(%T) = false, want true", connectErr)
	}

	selector, primary, replica, _ := testSelector()
	got, err := Read(context.Background(), selector, testBusiness, EventualConsistency,
		func(_ context.Context, queries *db.Queries) (string, error) {
			if queries == replica {
				return "", connectErr
			}
			if queries != primary {
				t.Fatalf("unexpected queries handle: %#v", queries)
			}
			return "primary result", nil
		})
	if err != nil || got != "primary result" {
		t.Fatalf("Read = %q, %v; want primary result", got, err)
	}
}

func TestCircuitSkipsReplicaThenClosesAfterHalfOpenSuccess(t *testing.T) {
	selector, primary, replica, recorder := testSelector()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	selector.now = func() time.Time { return now }
	replicaFailures := 1
	var calls []*db.Queries
	query := func(_ context.Context, queries *db.Queries) (string, error) {
		calls = append(calls, queries)
		if queries == replica && replicaFailures > 0 {
			replicaFailures--
			return "", &pgconn.PgError{Code: "08006", Message: "connection failure"}
		}
		if queries == replica {
			return "replica result", nil
		}
		return "primary result", nil
	}

	got, err := Read(context.Background(), selector, testBusiness, EventualConsistency, query)
	if err != nil || got != "primary result" {
		t.Fatalf("failed replica read = %q, %v", got, err)
	}
	calls = nil
	got, err = Read(context.Background(), selector, testBusiness, EventualConsistency, query)
	if err != nil || got != "primary result" || len(calls) != 1 || calls[0] != primary {
		t.Fatalf("open-circuit read = %q, %v, calls=%#v; want direct primary", got, err, calls)
	}

	now = now.Add(defaultReplicaCircuitCooldown)
	calls = nil
	got, err = Read(context.Background(), selector, testBusiness, EventualConsistency, query)
	if err != nil || got != "replica result" || len(calls) != 1 || calls[0] != replica {
		t.Fatalf("half-open read = %q, %v, calls=%#v; want replica", got, err, calls)
	}
	calls = nil
	got, err = Read(context.Background(), selector, testBusiness, EventualConsistency, query)
	if err != nil || got != "replica result" || len(calls) != 1 || calls[0] != replica {
		t.Fatalf("closed-circuit read = %q, %v, calls=%#v; want replica", got, err, calls)
	}

	foundCircuitRoute := false
	for _, route := range recorder.routes {
		if route.role == RolePrimary && route.reason == ReasonCircuitOpen {
			foundCircuitRoute = true
		}
	}
	if !foundCircuitRoute {
		t.Fatalf("routes = %#v, want circuit-open primary route", recorder.routes)
	}
}

func TestCircuitAllowsOnlyOneHalfOpenTrial(t *testing.T) {
	var circuit replicaCircuit
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	allowed, generation, halfOpen := circuit.allow(now)
	if !allowed || halfOpen {
		t.Fatalf("initial allow = %v, halfOpen=%v; want normal replica read", allowed, halfOpen)
	}
	if !circuit.fail(generation, now, defaultReplicaCircuitCooldown) {
		t.Fatal("first availability failure did not open circuit")
	}
	if allowed, _, _ := circuit.allow(now); allowed {
		t.Fatal("open circuit allowed replica before cooldown")
	}

	now = now.Add(defaultReplicaCircuitCooldown)
	allowed, _, halfOpen = circuit.allow(now)
	if !allowed || !halfOpen {
		t.Fatalf("first post-cooldown allow = %v, halfOpen=%v; want one trial", allowed, halfOpen)
	}
	if allowed, _, _ := circuit.allow(now); allowed {
		t.Fatal("circuit allowed a second concurrent half-open trial")
	}
}

func TestPanicReleasesHalfOpenTrial(t *testing.T) {
	selector, _, replica, _ := testSelector()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	selector.now = func() time.Time { return now }

	_, err := Read(context.Background(), selector, testBusiness, EventualConsistency,
		func(_ context.Context, queries *db.Queries) (string, error) {
			if queries == replica {
				return "", &pgconn.PgError{Code: "08006", Message: "connection failure"}
			}
			return "primary result", nil
		})
	if err != nil {
		t.Fatalf("open circuit: %v", err)
	}
	now = now.Add(defaultReplicaCircuitCooldown)

	func() {
		defer func() {
			if recovered := recover(); recovered != "boom" {
				t.Fatalf("recovered = %#v, want boom", recovered)
			}
		}()
		_, _ = Read(context.Background(), selector, testBusiness, EventualConsistency,
			func(_ context.Context, queries *db.Queries) (string, error) {
				if queries != replica {
					t.Fatalf("half-open trial used %#v, want replica", queries)
				}
				panic("boom")
			})
	}()

	got, err := Read(context.Background(), selector, testBusiness, EventualConsistency,
		func(_ context.Context, queries *db.Queries) (*db.Queries, error) {
			return queries, nil
		})
	if err != nil || got != replica {
		t.Fatalf("read after panic = %#v, %v; want a new replica trial", got, err)
	}
}

func TestRecoveryConflictFallsBackWithoutOpeningCircuit(t *testing.T) {
	selector, primary, replica, _ := testSelector()
	replicaCalls := 0
	query := func(_ context.Context, queries *db.Queries) (string, error) {
		if queries == replica {
			replicaCalls++
			if replicaCalls == 1 {
				return "", &pgconn.PgError{Code: "40001", Message: "conflict with recovery"}
			}
			return "replica result", nil
		}
		if queries != primary {
			t.Fatalf("unexpected queries handle: %#v", queries)
		}
		return "primary result", nil
	}

	got, err := Read(context.Background(), selector, testBusiness, EventualConsistency, query)
	if err != nil || got != "primary result" {
		t.Fatalf("conflicted read = %q, %v; want primary result", got, err)
	}
	got, err = Read(context.Background(), selector, testBusiness, EventualConsistency, query)
	if err != nil || got != "replica result" || replicaCalls != 2 {
		t.Fatalf("next read = %q, %v, replicaCalls=%d; want replica", got, err, replicaCalls)
	}
}

func TestReadDoesNotHideApplicationOrCancellationErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		err  error
	}{
		{
			name: "application error",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			err: &pgconn.PgError{Code: "42703", Message: "undefined column"},
		},
		{
			name: "caller cancellation",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			err: context.Canceled,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			selector, _, _, recorder := testSelector()
			ctx, cancel := tt.ctx()
			defer cancel()
			calls := 0
			_, err := Read(ctx, selector, testBusiness, EventualConsistency,
				func(context.Context, *db.Queries) (string, error) {
					calls++
					return "", tt.err
				})
			if !errors.Is(err, tt.err) || calls != 1 || len(recorder.routes) != 1 {
				t.Fatalf("err=%v calls=%d routes=%#v", err, calls, recorder.routes)
			}
		})
	}
}

func TestBareContextDeadlineDoesNotFallback(t *testing.T) {
	if shouldFallback(context.Background(), context.DeadlineExceeded) {
		t.Fatal("bare operation deadline should not fall back")
	}
}
