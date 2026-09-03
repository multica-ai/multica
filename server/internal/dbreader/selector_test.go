package dbreader

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const testBusiness Business = "test"

type recorderStub struct {
	configured bool
	status     Status
	probes     []Status
	routes     []Selection
	fallbacks  int
}

func (r *recorderStub) SetReplicaConfigured(configured bool) { r.configured = configured }
func (r *recorderStub) SetReplicaStatus(healthy bool, lagBytes int64, replayLag time.Duration) {
	r.status = Status{Healthy: healthy, LagBytes: lagBytes, ReplayLag: replayLag}
}
func (r *recorderStub) ObserveReplicaProbe(healthy bool, reason string) {
	r.probes = append(r.probes, Status{Healthy: healthy, Reason: Reason(reason)})
}
func (r *recorderStub) RecordReadRoute(_, role, reason string) {
	r.routes = append(r.routes, Selection{Role: Role(role), Reason: Reason(reason)})
}
func (r *recorderStub) RecordReadFallback(_, _ string) { r.fallbacks++ }

func testSelector(t *testing.T, snapshot probeSnapshot, probeErr error) (*Selector, *db.Queries, *db.Queries, *recorderStub) {
	t.Helper()
	primary := &db.Queries{}
	replica := &db.Queries{}
	recorder := &recorderStub{}
	selector := newSelector(primary, replica, Config{
		ProbeInterval: time.Hour,
		ProbeTimeout:  time.Second,
		MaxReplayLag:  5 * time.Second,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, func(context.Context) (probeSnapshot, error) {
		return snapshot, probeErr
	}, recorder)
	return selector, primary, replica, recorder
}

func healthySnapshot() probeSnapshot {
	return probeSnapshot{
		primaryDatabase: "multica",
		replicaDatabase: "multica",
		inRecovery:      true,
		readOnly:        true,
		replayKnown:     true,
	}
}

func TestPrimaryOnlySelectorPreservesExistingRouting(t *testing.T) {
	primary := &db.Queries{}
	selector := NewPrimaryOnly(primary)

	selection := selector.Select(testBusiness, EventualConsistency)
	if selection.Queries != primary || selection.Role != RolePrimary || selection.Reason != ReasonReplicaDisabled {
		t.Fatalf("selection = %#v, want disabled primary", selection)
	}
}

func TestSelectorRequiresSuccessfulProbeBeforeReplica(t *testing.T) {
	selector, primary, replica, recorder := testSelector(t, healthySnapshot(), nil)

	before := selector.Select(testBusiness, EventualConsistency)
	if before.Queries != primary || before.Reason != ReasonInitializing {
		t.Fatalf("before probe = %#v, want initializing primary", before)
	}

	status := selector.ProbeNow(context.Background())
	if !status.Healthy || status.Reason != ReasonHealthy {
		t.Fatalf("status = %#v, want healthy", status)
	}
	after := selector.Select(testBusiness, EventualConsistency)
	if after.Queries != replica || after.Role != RoleReplica {
		t.Fatalf("after probe = %#v, want replica", after)
	}
	if !recorder.configured || len(recorder.probes) != 1 {
		t.Fatalf("recorder = %#v, want configured probe", recorder)
	}
}

func TestSelectorStrongConsistencyAlwaysUsesPrimary(t *testing.T) {
	selector, primary, _, _ := testSelector(t, healthySnapshot(), nil)
	selector.ProbeNow(context.Background())

	selection := selector.Select(testBusiness, StrongConsistency)
	if selection.Queries != primary || selection.Reason != ReasonStrongConsistency {
		t.Fatalf("selection = %#v, want strong primary", selection)
	}
}

func TestClassifyReplica(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*probeSnapshot)
		reason  Reason
		healthy bool
	}{
		{name: "healthy", reason: ReasonHealthy, healthy: true},
		{name: "not standby", mutate: func(s *probeSnapshot) { s.inRecovery = false }, reason: ReasonNotStandby},
		{name: "not read only", mutate: func(s *probeSnapshot) { s.readOnly = false }, reason: ReasonNotReadOnly},
		{name: "replay unknown", mutate: func(s *probeSnapshot) { s.replayKnown = false }, reason: ReasonReplayUnknown},
		{name: "database mismatch", mutate: func(s *probeSnapshot) { s.replicaDatabase = "wrong" }, reason: ReasonDatabaseMismatch},
		{
			name: "lag over budget",
			mutate: func(s *probeSnapshot) {
				s.lagBytes = 1
				s.replayLag = 6 * time.Second
			},
			reason: ReasonReplayLag,
		},
		{
			name: "idle caught up ignores old replay timestamp",
			mutate: func(s *probeSnapshot) {
				s.lagBytes = 0
				s.replayLag = time.Hour
			},
			reason:  ReasonHealthy,
			healthy: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := healthySnapshot()
			if tt.mutate != nil {
				tt.mutate(&snapshot)
			}
			got := classify(snapshot, 5*time.Second)
			if got.Reason != tt.reason || got.Healthy != tt.healthy {
				t.Fatalf("classify = %#v, want reason=%s healthy=%v", got, tt.reason, tt.healthy)
			}
		})
	}
}

func TestProbeFailureKeepsPrimaryEligible(t *testing.T) {
	selector, primary, _, _ := testSelector(t, probeSnapshot{}, errors.New("replica unavailable"))
	status := selector.ProbeNow(context.Background())
	if status.Healthy || status.Reason != ReasonProbeFailed {
		t.Fatalf("status = %#v, want failed probe", status)
	}
	selection := selector.Select(testBusiness, EventualConsistency)
	if selection.Queries != primary || selection.Reason != ReasonProbeFailed {
		t.Fatalf("selection = %#v, want primary fallback", selection)
	}
}

func TestReadFallsBackOnceForRetryableReplicaError(t *testing.T) {
	selector, primary, replica, recorder := testSelector(t, healthySnapshot(), nil)
	selector.ProbeNow(context.Background())
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
	if recorder.fallbacks != 1 || len(recorder.probes) != 1 || selector.Status().Reason != ReasonQueryFailed {
		t.Fatalf("fallbacks=%d status=%#v", recorder.fallbacks, selector.Status())
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
			selector, _, _, recorder := testSelector(t, healthySnapshot(), nil)
			selector.ProbeNow(context.Background())
			ctx, cancel := tt.ctx()
			defer cancel()
			calls := 0
			_, err := Read(ctx, selector, testBusiness, EventualConsistency,
				func(context.Context, *db.Queries) (string, error) {
					calls++
					return "", tt.err
				})
			if !errors.Is(err, tt.err) || calls != 1 || recorder.fallbacks != 0 {
				t.Fatalf("err=%v calls=%d fallbacks=%d", err, calls, recorder.fallbacks)
			}
		})
	}
}
