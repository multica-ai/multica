package inbound_test

// TC-adapt-6 (PRD E6): when the dispatch step receives a recalled
// message event, it must:
//
//   1. Reply in the originating chat with the "上游消息已撤回" template
//      (so the conversation thread shows the recall happened).
//   2. NOT call IssueFacade.CreateIssue/AddComment/SetIssueStatus, NOT
//      call CommentFacade.AddComment, and NOT mutate any persistent
//      state. The recall semantics are "annotate, don't delete".
//
// The MessageID carried on the event must surface in the reply so an
// operator scanning the chat can correlate the recall back to the
// original ingest log.

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

func TestDispatchStep_RecallEvent_AnnotatesWithoutMutating(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, commentSvc, recCh := buildDispatchConfig()
	step := inbound.NewDispatchStep(cfg)

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-recall-1",
		Type:        port.EventTypeMessageRecalled,
		ChatID:      "chat-1",
		SenderID:    "ou_sender1",
		MessageID:   "om_msg_999",
		// Intent is intentionally zero — recall events bypass intent
		// recognition entirely.
	}

	out, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
	if out.EventID != "evt-recall-1" {
		t.Error("event was not returned unchanged")
	}

	// Persistent-state assertions: nothing must be created or mutated.
	if len(issueSvc.created) != 0 {
		t.Errorf("CreateIssue called %d times on a recall event; want 0", len(issueSvc.created))
	}
	if len(issueSvc.gotByID) != 0 {
		t.Errorf("GetIssueByIdentifier called %d times on a recall event; want 0", len(issueSvc.gotByID))
	}
	if len(issueSvc.setStatus) != 0 {
		t.Errorf("SetIssueStatus called %d times on a recall event; want 0", len(issueSvc.setStatus))
	}
	if len(issueSvc.listTodos) != 0 {
		t.Errorf("ListMyTodos called %d times on a recall event; want 0", len(issueSvc.listTodos))
	}
	if len(commentSvc.added) != 0 {
		t.Errorf("AddComment called %d times on a recall event; want 0", len(commentSvc.added))
	}

	// The chat thread must receive a recall annotation.
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(recCh.sends))
	}
	reply := recCh.sends[0]
	if reply.Target != port.TargetChat("chat-1") {
		t.Errorf("reply Target = %+v, want chat-1", reply.Target)
	}
	if !strings.Contains(reply.Text, "MESSAGE_RECALLED") {
		t.Errorf("reply missing MESSAGE_RECALLED key: %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "上游消息已撤回") {
		t.Errorf("reply missing recall template body: %q", reply.Text)
	}
	// MessageID surfaces so operators can correlate without diving
	// into the raw event log.
	if !strings.Contains(reply.Text, "om_msg_999") {
		t.Errorf("reply should reference the recalled message_id: %q", reply.Text)
	}
}

// Defensive guard: a non-recall event must not accidentally bypass intent
// dispatch even if MessageID is set (i.e. the special-casing is keyed on
// Type, not MessageID).
func TestDispatchStep_NonRecallEventWithMessageID_StillDispatchesIntent(t *testing.T) {
	t.Parallel()

	cfg, issueSvc, _, _ := buildDispatchConfig()
	issueSvc.createReturn = facade.Issue{
		ID:          uuid(0xAA),
		WorkspaceID: uuid(0x01),
		Identifier:  "STA-99",
		Title:       "placeholder",
		Status:      "todo",
	}
	step := inbound.NewDispatchStep(cfg)

	evt := port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-msg-1",
		Type:        port.EventTypeMessageReceived,
		ChatID:      "chat-1",
		SenderID:    "ou_sender1",
		MessageID:   "om_msg_111",
		Intent: port.InboundIntent{
			Kind:       port.IntentCreateIssue,
			Confidence: 1,
			Params:     map[string]string{"title": "still works"},
			Source:     port.SourceRule,
		},
	}

	if _, _, err := step.Run(context.Background(), evt); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(issueSvc.created) != 1 {
		t.Errorf("CreateIssue called %d times on a normal message_received event; want 1", len(issueSvc.created))
	}
}
