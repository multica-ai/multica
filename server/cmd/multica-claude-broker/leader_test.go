package main

import (
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

// We rely on client-go's leaderelection package being exhaustively tested
// upstream; the broker's contribution is just wiring. These tests confirm
// (a) construction succeeds with a fake clientset and (b) IsLeader starts
// false until OnStartedLeading fires. Full election loop semantics (lease
// acquire/renew/release) are upstream's job.

func TestNewLeaderState_Wireup(t *testing.T) {
	k := fake.NewSimpleClientset()
	ls, err := NewLeaderState(k, "multica", "lease-name", "pod-A")
	if err != nil {
		t.Fatalf("NewLeaderState: %v", err)
	}
	if ls.IsLeader() {
		t.Error("IsLeader() must be false before election begins")
	}
}

func TestLeaderState_CallbacksAreOptional(t *testing.T) {
	k := fake.NewSimpleClientset()
	ls, err := NewLeaderState(k, "ns", "name", "id")
	if err != nil {
		t.Fatalf("NewLeaderState: %v", err)
	}
	// Without callbacks set, simulating an internal transition should not panic.
	ls.leader.Store(true)
	if !ls.IsLeader() {
		t.Error("leader transition not reflected")
	}
}
