package handler

import (
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestIssueSubtreeChangeEvents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, batch := range []bool{false, true} {
		name := "single"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			parent := dbfx.Issue(t, "subtree event parent")
			otherParent := dbfx.Issue(t, "subtree event other parent")
			child := dbfx.Issue(t, "subtree event child")
			h := *testHandler
			h.Bus = events.New()
			var mu sync.Mutex
			var payloads []map[string]any
			h.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
				payload, ok := e.Payload.(map[string]any)
				if !ok {
					return
				}
				if issue, ok := payload["issue"].(IssueResponse); ok && issue.ID == child {
					mu.Lock()
					payloads = append(payloads, payload)
					mu.Unlock()
				}
			})
			steps := []struct {
				name          string
				updates       map[string]any
				parentChanged bool
				prevParent    *string
				titleChanged  bool
			}{
				{name: "ordinary edit", updates: map[string]any{"priority": "high"}},
				{name: "attach", updates: map[string]any{"parent_issue_id": parent}, parentChanged: true},
				{name: "same parent", updates: map[string]any{"parent_issue_id": parent}, prevParent: &parent},
				{name: "move", updates: map[string]any{"parent_issue_id": otherParent}, parentChanged: true, prevParent: &parent},
				{name: "detach", updates: map[string]any{"parent_issue_id": nil}, parentChanged: true, prevParent: &otherParent},
				{name: "rename", updates: map[string]any{"title": "Renamed child"}, titleChanged: true},
				{name: "same title", updates: map[string]any{"title": "Renamed child"}},
			}
			for _, step := range steps {
				t.Run(step.name, func(t *testing.T) {
					mu.Lock()
					payloads = nil
					mu.Unlock()
					if batch {
						testutil.Call(t, h.BatchUpdateIssues, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
							"issue_ids": []string{child}, "updates": step.updates,
						})).Want(http.StatusOK)
					} else {
						testutil.Call(t, h.UpdateIssue, updateIssueRequest(child, step.updates)).Want(http.StatusOK)
					}
					mu.Lock()
					defer mu.Unlock()
					if len(payloads) != 1 {
						t.Fatalf("got %d issue:updated events, want 1", len(payloads))
					}
					payload := payloads[0]
					if payload["parent_changed"] != step.parentChanged {
						t.Errorf("parent_changed = %v, want %v", payload["parent_changed"], step.parentChanged)
					}
					previous, ok := payload["prev_parent_issue_id"].(*string)
					if !ok {
						t.Fatalf("prev_parent_issue_id has unexpected type %T", payload["prev_parent_issue_id"])
					}
					if step.prevParent == nil {
						if previous != nil {
							t.Errorf("previous parent = %s, want nil", *previous)
						}
					} else if previous == nil || *previous != *step.prevParent {
						t.Errorf("previous parent = %v, want %s", previous, *step.prevParent)
					}
					if payload["title_changed"] != step.titleChanged {
						t.Errorf("title_changed = %v, want %v", payload["title_changed"], step.titleChanged)
					}
				})
			}
		})
	}
}
