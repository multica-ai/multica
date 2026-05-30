package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// LeaderState wraps client-go's leaderelection.LeaderElector with a small
// public surface: Run, IsLeader, and optional callbacks. The chart pins
// replicas: 1 + strategy: Recreate so usually only one process bids for the
// lease — but the lease eliminates correctness bugs (clock skew, stale
// RenewTime, lost RELEASE during partitions) that hand-rolled coordination
// routinely gets wrong, and survives accidental scale-up.
type LeaderState struct {
	elector *leaderelection.LeaderElector

	leader atomic.Bool

	mu               sync.RWMutex
	OnStartedLeading func()
	OnStoppedLeading func()
}

// NewLeaderState configures an elector against a Lease named `name` in
// namespace `ns`, with this pod's identity. Durations follow the
// kubernetes-author defaults for control-plane components (Lease 30s,
// renew 20s, retry 4s).
func NewLeaderState(k kubernetes.Interface, ns, name, identity string) (*LeaderState, error) {
	lock := &resourcelock.LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Name: name, Namespace: ns},
		Client:     k.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: identity},
	}
	ls := &LeaderState{}
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:            lock,
		LeaseDuration:   30 * time.Second,
		RenewDeadline:   20 * time.Second,
		RetryPeriod:     4 * time.Second,
		ReleaseOnCancel: true, // SIGTERM → tidy handoff
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(context.Context) {
				ls.leader.Store(true)
				ls.mu.RLock()
				cb := ls.OnStartedLeading
				ls.mu.RUnlock()
				if cb != nil {
					cb()
				}
			},
			OnStoppedLeading: func() {
				ls.leader.Store(false)
				ls.mu.RLock()
				cb := ls.OnStoppedLeading
				ls.mu.RUnlock()
				if cb != nil {
					cb()
				}
			},
		},
		Name: "multica-claude-broker",
	})
	if err != nil {
		return nil, fmt.Errorf("build elector: %w", err)
	}
	ls.elector = elector
	return ls, nil
}

// Run blocks until ctx is cancelled. The election loop renews the lease
// while we hold it and bids for it when we don't.
func (l *LeaderState) Run(ctx context.Context) { l.elector.Run(ctx) }

// IsLeader is safe to call from any goroutine.
func (l *LeaderState) IsLeader() bool { return l.leader.Load() }
