package main

// CEREBRO-PATCH(mcp-cli-cmd-mcp-test): cerebro modification of upstream file

import (
	"sync"
	"testing"
)

// TestMcpSessionState_AmbientIsolation verifies the core JEH-429 fix:
// when a "subagent" tracks a different session and then completes it,
// the parent's ambient pointer must remain intact.
func TestMcpSessionState_AmbientIsolation(t *testing.T) {
	var s mcpSessionState

	// Parent attaches to its issue and becomes ambient.
	s.track("parent-ws", "parent-issue", 0)
	s.setAmbient("parent-ws", "parent-issue")

	// Subagent attaches to its own issue WITHOUT becoming ambient
	// (set_active=false in attach_session).
	s.track("child-ws", "child-issue", 0)

	// Subagent completes its session.
	s.forget("child-ws")
	if s.clearAmbientIfMatches("child-ws") {
		t.Fatalf("clearAmbientIfMatches must return false when wsID is not the ambient one")
	}

	// Parent's ambient must still be intact.
	wsID, issueID := s.ambient()
	if wsID != "parent-ws" || issueID != "parent-issue" {
		t.Fatalf("parent ambient was clobbered by subagent: got (%q,%q), want (parent-ws,parent-issue)", wsID, issueID)
	}
}

// TestMcpSessionState_PerSessionSeq verifies that two parallel sessions
// each have an independent seq counter.
func TestMcpSessionState_PerSessionSeq(t *testing.T) {
	var s mcpSessionState
	s.track("a", "issue-a", 0)
	s.track("b", "issue-b", 0)

	if got := s.nextSeq("a"); got != 1 {
		t.Fatalf("a seq #1 = %d, want 1", got)
	}
	if got := s.nextSeq("a"); got != 2 {
		t.Fatalf("a seq #2 = %d, want 2", got)
	}
	if got := s.nextSeq("b"); got != 1 {
		t.Fatalf("b seq #1 = %d, want 1 (not 3 — must be independent of a)", got)
	}
	if got := s.nextSeq("a"); got != 3 {
		t.Fatalf("a seq #3 = %d, want 3", got)
	}
	if got := s.nextSeq("b"); got != 2 {
		t.Fatalf("b seq #2 = %d, want 2", got)
	}
}

// TestMcpSessionState_PerSessionNamed verifies that each session gets its
// own first-activity auto-name event independently.
func TestMcpSessionState_PerSessionNamed(t *testing.T) {
	var s mcpSessionState
	s.track("a", "issue-a", 0)
	s.track("b", "issue-b", 0)

	if !s.markNamed("a") {
		t.Fatalf("markNamed(a) first call must return true")
	}
	if s.markNamed("a") {
		t.Fatalf("markNamed(a) second call must return false")
	}
	if !s.markNamed("b") {
		t.Fatalf("markNamed(b) first call must return true (independent of a)")
	}
}

// TestMcpSessionState_ConcurrentSessions exercises the mutex by hammering
// independent sessions from many goroutines and verifying that each session's
// seq counter ends at exactly the number of nextSeq calls it received.
func TestMcpSessionState_ConcurrentSessions(t *testing.T) {
	var s mcpSessionState

	const sessions = 8
	const callsPerSession = 200

	ids := make([]string, sessions)
	for i := 0; i < sessions; i++ {
		ids[i] = string(rune('a' + i))
		s.track(ids[i], "issue", 0)
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerSession; j++ {
				s.nextSeq(id)
			}
		}()
	}
	wg.Wait()

	for _, id := range ids {
		if got := s.nextSeq(id); got != callsPerSession+1 {
			t.Fatalf("session %s ended at seq %d, want %d", id, got, callsPerSession+1)
		}
	}
}

// TestMcpSessionState_AmbientCompleteClears verifies that completing the
// ambient session DOES clear the ambient pointer.
func TestMcpSessionState_AmbientCompleteClears(t *testing.T) {
	var s mcpSessionState
	s.track("ws", "issue", 0)
	s.setAmbient("ws", "issue")

	if !s.clearAmbientIfMatches("ws") {
		t.Fatalf("clearAmbientIfMatches must return true when wsID is the ambient one")
	}
	wsID, issueID := s.ambient()
	if wsID != "" || issueID != "" {
		t.Fatalf("ambient not cleared: got (%q,%q)", wsID, issueID)
	}
}
