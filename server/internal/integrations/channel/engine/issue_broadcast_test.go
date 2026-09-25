package engine

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A chat-created issue must broadcast the same full issue payload the HTTP
// path does. cmd/server's extractIssueFields drops any issue:created event
// whose payload lacks id or creator_id, and dropping it means the creator is
// never auto-subscribed to the issue they just opened from chat.
func TestRouter_IssueCommand_BroadcastPayloadCarriesIdentityFields(t *testing.T) {
	h := newHarness(t)
	h.binder.appendResult = AppendResult{
		DedupMarked:  true,
		IssueCommand: &IssueCommand{Title: "Fix login", Description: "details"},
	}

	issueID := uuidFromString(t, "77777777-7777-7777-7777-777777777777")
	creatorID := h.ident.id.UserID
	workspaceID := h.inst.inst.WorkspaceID
	created := db.Issue{
		ID:          issueID,
		WorkspaceID: workspaceID,
		Number:      42,
		Title:       "Fix login",
		CreatorType: "member",
		CreatorID:   creatorID,
	}
	h.issues.result = service.IssueCreateResult{Issue: created}

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.issues.called {
		t.Fatal("expected issue create")
	}

	build := h.issues.opts.BroadcastPayload
	if build == nil {
		t.Fatal("engine passed no BroadcastPayload, so the service emits its " +
			"{\"issue_id\": ...} stub; extractIssueFields finds no \"issue\" key " +
			"and short-circuits, leaving the creator unsubscribed")
	}

	payload := build(created, nil, nil)
	raw, ok := payload["issue"]
	if !ok {
		t.Fatalf("payload has no \"issue\" key; got keys %v", keysOf(payload))
	}
	issue, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("payload[\"issue\"] is %T; want map[string]any", raw)
	}

	// The three fields extractIssueFields reads before it decides whether to
	// keep the event.
	for _, tc := range []struct {
		field string
		want  string
	}{
		{"id", "77777777-7777-7777-7777-777777777777"},
		{"creator_id", "44444444-4444-4444-4444-444444444444"},
		{"workspace_id", "22222222-2222-2222-2222-222222222222"},
	} {
		got, _ := issue[tc.field].(string)
		if got != tc.want {
			t.Errorf("issue[%q] = %q; want %q", tc.field, got, tc.want)
		}
	}
	if got, _ := issue["creator_type"].(string); got != "member" {
		t.Errorf("issue[\"creator_type\"] = %q; want member", got)
	}
	// The workspace issue prefix must come from the workspace row, matching the
	// identifier the chat reply already shows.
	if got, _ := issue["identifier"].(string); got != "MUL-42" {
		t.Errorf("issue[\"identifier\"] = %q; want MUL-42", got)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
