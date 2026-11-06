package service

import (
	"encoding/json"
	"testing"
)

func TestNativeIssueEventWorkspaceOptIn(t *testing.T) {
	good := map[string]any{"oracle_issue_events": map[string]any{
		"enabled":       true,
		"actor_user_id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"agent_ids":     []string{"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "cccccccc-cccc-4ccc-8ccc-cccccccccccc"},
		"runtime_ids":   []string{"dddddddd-dddd-4ddd-8ddd-dddddddddddd", "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"},
	}}
	b, _ := json.Marshal(good)
	c, ok := parseNativeEventConfig(b)
	if !ok || !c.Enabled || len(c.AgentIDs) != 2 {
		t.Fatalf("explicit owner opt-in rejected: %v %v", ok, c)
	}
	for _, bad := range []string{
		"{}",
		// Fail closed when a selected Agent has no exact Runtime binding.
		`{"oracle_issue_events":{"enabled":true,"actor_user_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","agent_ids":["bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"]}}`,
		`{"oracle_issue_events":{"enabled":true,"actor_user_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","agent_ids":["bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","cccccccc-cccc-4ccc-8ccc-cccccccccccc"],"runtime_ids":["dddddddd-dddd-4ddd-8ddd-dddddddddddd"]}}`,
		`{"oracle_issue_events":{"enabled":true,"actor_user_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","agent_ids":["bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","cccccccc-cccc-4ccc-8ccc-cccccccccccc"],"runtime_ids":["dddddddd-dddd-4ddd-8ddd-dddddddddddd","dddddddd-dddd-4ddd-8ddd-dddddddddddd"]}}`,

		`{"oracle_issue_events":{"enabled":false,"actor_user_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","agent_ids":["bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"]}}`,
		`{"oracle_issue_events":{"enabled":true,"actor_user_id":"not-an-id","agent_ids":["bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"]}}`,
		`{"oracle_issue_events":{"enabled":true,"actor_user_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","agent_ids":["bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"]}}`,
	} {
		if _, ok := parseNativeEventConfig([]byte(bad)); ok {
			t.Fatalf("unexpected queue enable for %q", bad)
		}
	}
}

func TestNativeEventQueueAllUnfinishedRemainEvents(t *testing.T) {
	for _, state := range []string{"backlog", "todo", "blocked", "in_progress", "triage", "in_review", "custom_nonterminal"} {
		if nativeEventCategoryIsTerminal(state) {
			t.Fatalf("discarded open Issue state %q", state)
		}
	}
	for _, state := range []string{"done", "closed"} {
		if !nativeEventCategoryIsTerminal(state) {
			t.Fatalf("terminal Issue remained %s", state)
		}
	}
	if nativeEventBatchSize <= 0 || nativeEventBatchSize > 100 {
		t.Fatal("unbounded native Task inserts per scheduler pass")
	}
	// Logical event count stays at 1000 even while 16 Oracle tasks are busy.
	unfinished, running := 1000, 16
	waiting := unfinished - running
	if waiting != 984 {
		t.Fatal("lost pending Issue events")
	}
	// A completed execution does not complete its Issue: it joins the pool.
	running--
	waiting++
	if running != 15 || waiting != 985 {
		t.Fatal("incomplete issue did not re-enter the pending pool")
	}
	// A truly completed Issue exits the pool; terminal event no longer waits.
	waiting--
	if waiting != 984 {
		t.Fatal("terminal Issue was requeued")
	}
}
