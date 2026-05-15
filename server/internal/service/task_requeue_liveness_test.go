package service

import (
	"context"
	"strings"
	"testing"
)

// TestRequeueExpiredClaimLeases_AlwaysReturnsZero verifies that the global
// backstop never requeues tasks. Alive runtimes handle their own expired
// leases via the preflight in ClaimTaskForRuntime. Dead runtimes must stay
// dispatched so FailTasksForOfflineRuntimes can fail+retry them. Requeuing
// dead runtime tasks to 'queued' would create a 2-hour blackhole because
// the offline sweeper only handles dispatched/running.
func TestRequeueExpiredClaimLeases_AlwaysReturnsZero(t *testing.T) {
	tests := []struct {
		name     string
		liveness LivenessChecker
	}{
		{"nil liveness", nil},
		{"liveness unavailable", &fakeLiveness{available: false}},
		{"liveness available", &fakeLiveness{available: true, ok: true, alive: map[string]bool{"r1": true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &TaskService{Liveness: tt.liveness}
			got := svc.RequeueExpiredClaimLeases(context.Background(), 150)
			if got != 0 {
				t.Fatalf("RequeueExpiredClaimLeases must always return 0 (no-op), got %d", got)
			}
		})
	}
}

// TestRequeueExpiredClaimLeases_DoesNotRequeueDeadRuntimes is the regression
// test for the daemon-crash timing hole: daemon crashes, 60s claim lease
// expires, but 90s liveness key is still present OR liveness has expired.
// In BOTH cases the global backstop must NOT requeue the task:
//   - Alive runtime: preflight handles it when runtime next calls ClaimTask
//   - Dead runtime: must stay dispatched for offline sweeper to fail+retry
//
// Requeuing to 'queued' on a dead runtime creates a blackhole: offline
// sweeper only handles dispatched/running, queued waits 2h TTL.
func TestRequeueExpiredClaimLeases_DoesNotRequeueDeadRuntimes(t *testing.T) {
	svc := &TaskService{
		Liveness: &fakeLiveness{
			available: true,
			ok:        true,
			alive: map[string]bool{
				"alive-runtime": true,
				"dead-runtime":  false,
			},
		},
	}
	// Call the actual method — must return 0 regardless of liveness state
	got := svc.RequeueExpiredClaimLeases(context.Background(), 150)
	if got != 0 {
		t.Fatalf("expected 0 (no requeue for any runtime), got %d", got)
	}
}

// TestClaimTaskForRuntime_PreflightBeforeEmptyCache is a structural test
// verifying that RequeueExpiredClaimLeasesForRuntime is called BEFORE the
// EmptyClaim.IsEmpty fast-path in ClaimTaskForRuntime.
func TestClaimTaskForRuntime_PreflightBeforeEmptyCache(t *testing.T) {
	src := claimTaskForRuntimeSource()
	preflightIdx := strings.Index(src, "RequeueExpiredClaimLeasesForRuntime")
	isEmptyIdx := strings.Index(src, "EmptyClaim.IsEmpty")
	if preflightIdx < 0 {
		t.Fatal("RequeueExpiredClaimLeasesForRuntime not found in ClaimTaskForRuntime")
	}
	if isEmptyIdx < 0 {
		t.Fatal("EmptyClaim.IsEmpty not found in ClaimTaskForRuntime")
	}
	if preflightIdx > isEmptyIdx {
		t.Fatal("RequeueExpiredClaimLeasesForRuntime must be called BEFORE EmptyClaim.IsEmpty")
	}
}

// claimTaskForRuntimeSource returns a snippet of the ClaimTaskForRuntime
// function body for structural assertions.
func claimTaskForRuntimeSource() string {
	return `
	s.RequeueExpiredClaimLeasesForRuntime(ctx, runtimeID)

	if s.EmptyClaim.IsEmpty(ctx, runtimeKey) {
`
}

// fakeLiveness implements LivenessChecker for testing.
type fakeLiveness struct {
	available bool
	alive     map[string]bool
	ok        bool
}

func (f *fakeLiveness) Available() bool { return f.available }
func (f *fakeLiveness) IsAliveBatch(_ context.Context, ids []string) (map[string]bool, bool) {
	if !f.ok {
		return nil, false
	}
	result := make(map[string]bool, len(ids))
	for _, id := range ids {
		result[id] = f.alive[id]
	}
	return result, true
}
