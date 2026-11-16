package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// publishIssueUpdated fires the same event the autopilot listener subscribes
// to. issue is either handler.IssueResponse (HTTP UpdateIssue) or the map
// broadcastIssueUpdated sends.
func publishIssueUpdated(bus *events.Bus, issue any, statusChanged bool) {
	bus.Publish(events.Event{
		Type: protocol.EventIssueUpdated,
		Payload: map[string]any{
			"issue":          issue,
			"status_changed": statusChanged,
		},
	})
}

// watchAutopilotIssueSync replaces the load-and-sync step for one test and
// records every issue id that passed the listener's status gate.
func watchAutopilotIssueSync(t *testing.T) *[]string {
	t.Helper()
	var seen []string
	original := autopilotIssueSync
	autopilotIssueSync = func(_ context.Context, _ *service.AutopilotService, issueID string) {
		seen = append(seen, issueID)
	}
	t.Cleanup(func() { autopilotIssueSync = original })
	return &seen
}

func TestAutopilotIssueListenerMapPayloadReachesSync(t *testing.T) {
	seen := watchAutopilotIssueSync(t)
	bus := events.New()
	registerAutopilotListeners(bus, nil)

	// broadcastIssueUpdated publishes IssueToMap. The old listener returned at
	// the IssueResponse assertion, so a map never reached the sync even when
	// the status is one the gate already lets through.
	for _, status := range []string{"done", "in_review", "cancelled", "awaiting_vendor"} {
		publishIssueUpdated(bus, map[string]any{
			"id":     "map-" + status,
			"status": status,
		}, true)
	}

	want := []string{"map-done", "map-in_review", "map-cancelled", "map-awaiting_vendor"}
	if len(*seen) != len(want) {
		t.Fatalf("map payload sync calls = %v, want %v", *seen, want)
	}
	for i, id := range want {
		if (*seen)[i] != id {
			t.Fatalf("map payload sync calls = %v, want %v", *seen, want)
		}
	}
}

func TestAutopilotIssueListenerStructPayloadStillReachesSync(t *testing.T) {
	seen := watchAutopilotIssueSync(t)
	bus := events.New()
	registerAutopilotListeners(bus, nil)

	publishIssueUpdated(bus, handler.IssueResponse{
		ID:     "issue-struct",
		Status: "done",
	}, true)

	if len(*seen) != 1 || (*seen)[0] != "issue-struct" {
		t.Fatalf("struct payload sync calls = %v, want [issue-struct]", *seen)
	}
}

func TestAutopilotIssueListenerSkipsUnchangedAndNonTerminal(t *testing.T) {
	seen := watchAutopilotIssueSync(t)
	bus := events.New()
	registerAutopilotListeners(bus, nil)

	publishIssueUpdated(bus, map[string]any{
		"id":     "unchanged",
		"status": "done",
	}, false)
	// Built-in keys that are not done, in_review, or cancelled stay filtered,
	// for both payload shapes. todo is the sweeper's reset target and is one
	// of those keys.
	for _, status := range []string{"todo", "in_progress", "backlog", "blocked"} {
		publishIssueUpdated(bus, map[string]any{
			"id":     "map-" + status,
			"status": status,
		}, true)
		publishIssueUpdated(bus, handler.IssueResponse{
			ID:     "struct-" + status,
			Status: status,
		}, true)
	}
	publishIssueUpdated(bus, map[string]any{"status": "done"}, true)
	publishIssueUpdated(bus, "not-an-issue", true)

	if len(*seen) != 0 {
		t.Fatalf("sync calls = %v, want none", *seen)
	}
}
