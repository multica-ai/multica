package agentvault

import (
	"context"
	"testing"
)

// FIR-2210: the broker injects no agent-side transport — PrepareSpawnEnv always
// returns a nil env (credential access is via Multica connections). These tests
// reuse fakeGrantSource / fakeAccessStore from mirror_test.go.

func TestPrepareSpawnEnvNoMirrorReturnsNilEnv(t *testing.T) {
	s := NewService()
	env, err := s.PrepareSpawnEnv(context.Background(), "ws", "ag", "sara-agent")
	if err != nil || env != nil {
		t.Fatalf("expected (nil,nil) with no mirror wired, got (%v,%v)", env, err)
	}
}

func TestPrepareSpawnEnvReconcilesGrantsAndReturnsNilEnv(t *testing.T) {
	src := fakeGrantSource{boxes: []BoxGrant{{Vault: "multica", Role: "read-only"}}}
	rec := newFakeAccessStore(map[string]string{"stale": "member"})
	s := NewService()
	s.SetGrantMirror(src, rec)

	env, err := s.PrepareSpawnEnv(context.Background(), "ws", "ag", "sara-agent")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if env != nil {
		t.Fatalf("expected nil env (no agent-side transport), got %v", env)
	}
	// The granted box is written and the box no longer granted is removed.
	if rec.setCalls != 1 || rec.rows["multica"] != "read-only" {
		t.Fatalf("expected multica to be set, got rows=%v setCalls=%d", rec.rows, rec.setCalls)
	}
	if _, stillThere := rec.rows["stale"]; stillThere {
		t.Fatalf("expected stale box to be deleted, got rows=%v", rec.rows)
	}
}

func TestPrepareSpawnEnvGrantSourceErrorFailsClosed(t *testing.T) {
	src := fakeGrantSource{err: context.DeadlineExceeded}
	rec := newFakeAccessStore(nil)
	s := NewService()
	s.SetGrantMirror(src, rec)

	if _, err := s.PrepareSpawnEnv(context.Background(), "ws", "ag", "sara-agent"); err == nil {
		t.Fatalf("expected error to fail the claim closed")
	}
}
