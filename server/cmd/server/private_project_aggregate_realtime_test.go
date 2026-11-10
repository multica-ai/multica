package main

import (
	"sort"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const ppOutsider = "00000000-0000-0000-0000-0000000000c3"

func publishPrivateAudience(e events.Event) *fakeBroadcaster {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb, newPrivateProjectAudience(fakeAudienceQuerier{}))
	e.WorkspaceID = ppWorkspace
	bus.Publish(e)
	return fb
}

func memberScopes(userIDs ...string) []string {
	out := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		out = append(out, realtime.WorkspaceMemberScopeID(ppWorkspace, id))
	}
	sort.Strings(out)
	return out
}

func sameScopes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Pins, deleted private issues and pull requests linked to private issues
// (MUL-8472) used to keep workspace fanout, handing their ids to members
// outside the project.
func TestRegisterListeners_PrivateProjectAggregateEvents(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    events.Event
		want []string
	}{
		{"own pin of a private issue", events.Event{Type: protocol.EventPinCreated, ActorType: "member", ActorID: ppMember,
			Payload: map[string]any{"pin": map[string]any{"item_type": "issue", "item_id": ppSecretIssue, "user_id": ppMember}}}, memberScopes(ppMember)},
		{"pin delete", events.Event{Type: protocol.EventPinDeleted, ActorType: "member", ActorID: ppOutsider,
			Payload: map[string]any{"item_type": "project", "item_id": ppPublic}}, memberScopes(ppOutsider)},
		{"pin reorder", events.Event{Type: protocol.EventPinReordered, ActorType: "member", ActorID: ppMember,
			Payload: map[string]any{"items": []map[string]any{{"id": "x", "position": 1}}}}, memberScopes(ppMember)},
		{"deleted private issue", events.Event{Type: protocol.EventIssueDeleted,
			Payload: map[string]any{"issue_id": "00000000-0000-0000-0000-00000000dead", "project_id": ppPrivate}}, memberScopes(ppMember, ppAdmin)},
		{"pull request linked to a private and a public issue", events.Event{Type: protocol.EventPullRequestUpdated,
			Payload: map[string]any{"pull_request": map[string]any{"id": "pr"}, "linked_issue_ids": []string{ppPublicIssue, ppSecretIssue}}}, memberScopes(ppMember, ppAdmin)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fb := publishPrivateAudience(tc.e)
			if len(fb.workspaceCalls) != 0 {
				t.Fatal("broadcast to the whole workspace")
			}
			if got := privateAudienceScopes(fb); !sameScopes(got, tc.want) {
				t.Fatalf("delivered to %v, want %v", got, tc.want)
			}
		})
	}

	// Controls: nothing private involved keeps workspace fanout.
	for _, e := range []events.Event{
		{Type: protocol.EventIssueDeleted, Payload: map[string]any{"issue_id": "00000000-0000-0000-0000-00000000dead", "project_id": nil}},
		{Type: protocol.EventPullRequestUpdated, Payload: map[string]any{"pull_request": map[string]any{"id": "pr"}, "linked_issue_ids": []string{ppPublicIssue}}},
		{Type: protocol.EventPullRequestUpdated, Payload: map[string]any{"pull_request": map[string]any{"id": "pr"}, "linked_issue_ids": []string{}}},
	} {
		if fb := publishPrivateAudience(e); len(fb.workspaceCalls) != 1 {
			t.Errorf("%s %v: workspace broadcasts = %d, want 1", e.Type, e.Payload, len(fb.workspaceCalls))
		}
	}
}
