package channelnotify

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestParseNotificationAcceptsIssueBackedMemberItem(t *testing.T) {
	e := inboxEvent(map[string]any{
		"id":             "11111111-1111-4111-8111-111111111111",
		"workspace_id":   "22222222-2222-4222-8222-222222222222",
		"recipient_type": "member",
		"recipient_id":   "33333333-3333-4333-8333-333333333333",
		"issue_id":       "44444444-4444-4444-8444-444444444444",
		"type":           "comment_added",
		"severity":       "attention",
		"title":          "Review the deployment",
		"body":           "A new comment needs your attention.",
		"details":        json.RawMessage(`{"comment_id":"comment-1"}`),
	})

	n, ok := ParseNotification(e)
	if !ok {
		t.Fatal("ParseNotification rejected a complete member Inbox item")
	}
	if n.Type != "comment_added" || n.Title != "Review the deployment" || n.Body == "" || !n.IssueID.Valid {
		t.Fatalf("unexpected notification: %+v", n)
	}
}

func TestParseNotificationAcceptsMentionedWithoutSpecialCase(t *testing.T) {
	e := inboxEvent(map[string]any{
		"id":             "11111111-1111-4111-8111-111111111111",
		"workspace_id":   "22222222-2222-4222-8222-222222222222",
		"recipient_type": "member",
		"recipient_id":   "33333333-3333-4333-8333-333333333333",
		"issue_id":       "44444444-4444-4444-8444-444444444444",
		"type":           "mentioned",
		"severity":       "info",
		"title":          "You were mentioned",
		"body":           (*string)(nil),
	})

	n, ok := ParseNotification(e)
	if !ok || n.Type != "mentioned" || n.Body != "" {
		t.Fatalf("mentioned item was not accepted as a normal notification: ok=%v item=%+v", ok, n)
	}
}

func TestParseNotificationRejectsNonMemberOrMissingIssue(t *testing.T) {
	base := map[string]any{
		"id":           "11111111-1111-4111-8111-111111111111",
		"workspace_id": "22222222-2222-4222-8222-222222222222",
		"recipient_id": "33333333-3333-4333-8333-333333333333",
		"issue_id":     "44444444-4444-4444-8444-444444444444",
		"type":         "mentioned",
		"title":        "A notification",
	}

	for name, mutate := range map[string]func(map[string]any){
		"agent recipient": func(item map[string]any) { item["recipient_type"] = "agent" },
		"missing issue":   func(item map[string]any) { delete(item, "issue_id") },
		"nil issue":       func(item map[string]any) { item["issue_id"] = (*string)(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			item := cloneMap(base)
			item["recipient_type"] = "member"
			mutate(item)
			if _, ok := ParseNotification(inboxEvent(item)); ok {
				t.Fatal("ParseNotification accepted an ineligible Inbox item")
			}
		})
	}
}

func TestParseNotificationRejectsMalformedEnvelope(t *testing.T) {
	for name, payload := range map[string]any{
		"wrong event":     map[string]any{"item": map[string]any{}},
		"non-map payload": "not an inbox event",
		"missing item":    map[string]any{},
	} {
		t.Run(name, func(t *testing.T) {
			e := events.Event{Type: protocol.EventIssueUpdated, Payload: payload}
			if name == "wrong event" {
				e.Type = protocol.EventInboxNew
			}
			if _, ok := ParseNotification(e); ok {
				t.Fatal("ParseNotification accepted a malformed event")
			}
		})
	}
}

func inboxEvent(item map[string]any) events.Event {
	return events.Event{
		Type:    protocol.EventInboxNew,
		Payload: map[string]any{"item": item},
	}
}

func cloneMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
