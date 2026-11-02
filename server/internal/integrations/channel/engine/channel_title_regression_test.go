package engine

import (
	"context"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"testing"
)

func TestRouterTitleUsesCurrentInstruction(t *testing.T) {
	for _, tc := range []struct {
		name, body, command, want string
		start, fresh              bool
	}{
		{name: "quoted reply", body: "<quoted_message>\nOld subject\n</quoted_message>\n\nCompare alternatives", command: "Compare alternatives", want: "Compare alternatives"},
		{name: "recent context", body: "<recent_context>\nOld subject\n</recent_context>\n\nCompare alternatives", command: "Compare alternatives", want: "Compare alternatives"},
		{name: "consumed clear", body: "<quoted_message>\nOld subject\n</quoted_message>\n\nCompare alternatives", command: "/clear Compare alternatives", fresh: true, want: "Compare alternatives"},
		{name: "new chat", body: "<quoted_message>\nOld subject\n</quoted_message>\n\nCompare alternatives", command: "/new Compare alternatives", start: true, want: "Compare alternatives"},
		{name: "new body literal clear", body: "<quoted_message>\nOld subject\n</quoted_message>\n\n/clear literal", command: "/new /clear literal", start: true, want: "/clear literal"},
		{name: "missing command", body: "Compare alternatives", want: "Compare alternatives"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.binder.appendResult.InitialTitle = tc.want
			msg := p2pMessage(t)
			msg.Text = tc.body
			msg.CommandText = tc.command
			msg.ForceFresh = tc.fresh
			if err := h.router.Handle(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
			h.lifecycle.mu.Lock()
			defer h.lifecycle.mu.Unlock()
			if len(h.lifecycle.generatedSourceTexts) != 1 || h.lifecycle.generatedSourceTexts[0] != tc.want {
				t.Errorf("LLM title source = %q, want only current instruction %q", h.lifecycle.generatedSourceTexts, tc.want)
			}
		})
	}
}

func TestContextualMediaOpeningWaitsForFilenameTitle(t *testing.T) {
	f := newFake()
	session := newTestSession(f)
	body := "<quoted_message>\nHistorical subject\n</quoted_message>\n\n[Image]"
	result, err := session.AppendUserMessage(context.Background(), AppendInput{SessionID: uid(1), Sender: uid(7), Body: body, CommandText: "[Image]", MessageID: "image-current", MediaPendingSeconds: 45})
	if err != nil {
		t.Fatal(err)
	}
	if result.InitialTitle != "" {
		t.Fatalf("context initialized title before media metadata: %q", result.InitialTitle)
	}
	bound, err := session.BindMediaRefsWithResult(context.Background(), BindMediaInput{MessageID: result.MessageID, SessionID: uid(1), WorkspaceID: uid(9), Sender: uid(7), Body: body, MediaRefs: []channel.MediaRef{{Type: channel.MsgTypeImage, StorageKey: "image-key", StorageURL: "https://cdn.example.test/image-key", Filename: "incident screenshot.png", MimeType: "image/png"}}})
	if err != nil {
		t.Fatal(err)
	}
	if f.initializedMediaTitle != "incident screenshot.png" || bound.InitialTitle != "incident screenshot.png" || bound.TitleSource != "incident screenshot.png" {
		t.Fatalf("media title stored=%q result=%+v", f.initializedMediaTitle, bound)
	}
}
