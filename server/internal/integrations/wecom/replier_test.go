package wecom

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeOutboundEnqueuer struct {
	calls []db.EnqueueChannelOutboundParams
}

func (f *fakeOutboundEnqueuer) EnqueueChannelOutbound(_ context.Context, arg db.EnqueueChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	f.calls = append(f.calls, arg)
	return db.ChannelOutboundQueue{}, nil
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("mustUUID(%q): %v", s, err)
	}
	return u
}

func testResolvedInstallation(t *testing.T) engine.ResolvedInstallation {
	return engine.ResolvedInstallation{
		ID:          mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		WorkspaceID: mustUUID(t, "11111111-1111-1111-1111-111111111111"),
		AgentID:     mustUUID(t, "22222222-2222-2222-2222-222222222222"),
		Active:      true,
	}
}

func TestReply_NeedsBinding_GroupEnqueuesPromptWithoutToken(t *testing.T) {
	enq := &fakeOutboundEnqueuer{}
	r := NewOutboundReplier(OutboundReplierConfig{Queries: enq})
	inst := testResolvedInstallation(t)
	msg := inbound(channel.ChatTypeGroup, "chat1", "u1", "m1")

	r.Reply(context.Background(), inst, msg, engine.Result{Outcome: engine.OutcomeNeedsBinding})

	if len(enq.calls) != 1 {
		t.Fatalf("expected one enqueue, got %d", len(enq.calls))
	}
	call := enq.calls[0]
	if call.SourceKind != "binding_prompt" {
		t.Fatalf("source_kind = %q, want binding_prompt", call.SourceKind)
	}
	assertBindingPayload(t, call.Payload, templateBindingPromptGroup)
}

func TestReply_NeedsBinding_DMEnqueuesPromptWithoutToken(t *testing.T) {
	enq := &fakeOutboundEnqueuer{}
	r := NewOutboundReplier(OutboundReplierConfig{Queries: enq})
	inst := testResolvedInstallation(t)
	msg := inbound(channel.ChatTypeP2P, "ignored", "u1", "m2")

	r.Reply(context.Background(), inst, msg, engine.Result{Outcome: engine.OutcomeNeedsBinding})

	if len(enq.calls) != 1 {
		t.Fatalf("expected one enqueue, got %d", len(enq.calls))
	}
	call := enq.calls[0]
	if call.SourceKind != "binding_prompt" {
		t.Fatalf("source_kind = %q, want binding_prompt", call.SourceKind)
	}
	assertBindingPayload(t, call.Payload, templateBindingPrompt)
}

func assertBindingPayload(t *testing.T, raw []byte, wantTemplate string) {
	t.Helper()
	payload := string(raw)
	if strings.Contains(payload, "token") {
		t.Fatalf("binding prompt payload must not contain token: %s", payload)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if got["template"] != wantTemplate {
		t.Fatalf("template = %q, want %q", got["template"], wantTemplate)
	}
}
