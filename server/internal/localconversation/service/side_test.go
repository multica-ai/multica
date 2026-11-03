package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestSideConversationStartsFromItsSourceBoundary(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	f.runTurn(t, "r1", "u1")
	f.runTurn(t, "r2", "u2")

	side, err := f.svc.CreateSide(ctx, SideCommand{ScopeID: f.scopeID, SourceID: "conv-1", TurnID: "turn-r1", Quote: "answer r1", RequestID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if side.SideSourceID != "conv-1" || !strings.HasSuffix(side.Title, "· Side") || side.TitleMode != "manual" || side.Cwd != "/repo" {
		t.Fatalf("side = %+v", side)
	}
	again, err := f.svc.CreateSide(ctx, SideCommand{ScopeID: f.scopeID, SourceID: "conv-1", TurnID: "turn-r1", Quote: "answer r1", RequestID: "s1"})
	if err != nil || again.ID != side.ID {
		t.Fatalf("retry = %v, %v", again.ID, err)
	}
	if _, err := f.svc.CreateSide(ctx, SideCommand{ScopeID: f.scopeID, SourceID: "conv-1", RequestID: "s1"}); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("reused request id = %v", err)
	}

	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: side.ID, RequestID: "side-1", Text: "and then?"}); err != nil {
		t.Fatal(err)
	}
	sends := f.sends(t)
	prompt := sends[len(sends)-1]
	if !strings.HasPrefix(prompt, "You are in an independent Multica side conversation.") || !strings.Contains(prompt, "ask r1") || strings.Contains(prompt, "ask r2") ||
		!strings.Contains(prompt, `"selected_text":"answer r1"`) || !strings.HasSuffix(prompt, "Current user request:\nand then?") {
		t.Fatalf("side prompt = %q", prompt)
	}
	messages, _ := f.svc.Store().Messages(ctx, side.ID)
	if len(messages) != 1 || messages[0].Content != "and then?" {
		t.Fatalf("side messages = %+v", messages)
	}
}

func TestQueuedMessageMovesToASide(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r0", Text: "busy"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "do this aside"), false, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	side, err := f.svc.QueueToSide(ctx, f.scopeID, "conv-1", "q1")
	if err != nil {
		t.Fatal(err)
	}
	if view, _ := f.svc.Queue(ctx, f.scopeID, "conv-1"); len(view.Items) != 0 {
		t.Fatalf("source queue = %+v", view)
	}
	eventually(t, "the moved message to run in the Side", func() bool {
		for _, s := range f.sends(t) {
			if strings.HasSuffix(s, "Current user request:\ndo this aside") {
				return true
			}
		}
		return false
	})
	again, err := f.svc.QueueToSide(ctx, f.scopeID, "conv-1", "q1")
	if err != nil || again.ID != side.ID {
		t.Fatalf("retry = %v, %v", again.ID, err)
	}
}
