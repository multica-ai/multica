package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

func TestBuildQuickCreateContext_ChannelSession(t *testing.T) {
	got := buildQuickCreateContext(quickCreateEnqueueInput{
		WorkspaceID:   testUUID(1),
		RequesterID:   testUUID(2),
		ChatSessionID: testUUID(3),
		ChatMessageID: testUUID(4),
		Prompt:        "analyze the branch",
	})
	if got.Type != QuickCreateContextType {
		t.Fatalf("type = %q, want %q", got.Type, QuickCreateContextType)
	}
	if got.ChatSessionID != util.UUIDToString(testUUID(3)) {
		t.Fatalf("chat_session_id = %q", got.ChatSessionID)
	}
	if got.ChatMessageID != util.UUIDToString(testUUID(4)) {
		t.Fatalf("chat_message_id = %q", got.ChatMessageID)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"chat_session_id"`) {
		t.Fatalf("marshaled context missing chat_session_id: %s", raw)
	}
	if !strings.Contains(string(raw), `"chat_message_id"`) {
		t.Fatalf("marshaled context missing chat_message_id: %s", raw)
	}
}

func TestBuildQuickCreateContext_WebOmitsChannelSession(t *testing.T) {
	got := buildQuickCreateContext(quickCreateEnqueueInput{
		WorkspaceID: testUUID(1),
		RequesterID: testUUID(2),
		Prompt:      "analyze the branch",
	})
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "chat_session_id") {
		t.Fatalf("web context must omit chat_session_id: %s", raw)
	}
	if strings.Contains(string(raw), "chat_message_id") {
		t.Fatalf("web context must omit chat_message_id: %s", raw)
	}
}

func TestBuildQuickCreateDoneReply(t *testing.T) {
	got := buildQuickCreateDoneReply("MUL-42", "Fix login", "https://app.example.com/acme/issues/MUL-42")
	want := "✅ MUL-42 — Fix login\n\nhttps://app.example.com/acme/issues/MUL-42"
	if got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

func TestBuildQuickCreateFailedReply(t *testing.T) {
	got := buildQuickCreateFailedReply("login broken\nsteps to reproduce", "An active issue already exists: MUL-42 — Fix login")
	want := quickCreateChatFailedReasonText +
		"\n\n> login broken\n> steps to reproduce" +
		"\n\nAn active issue already exists: MUL-42 — Fix login"
	if got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}
