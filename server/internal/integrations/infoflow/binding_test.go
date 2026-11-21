package infoflow

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func TestBindingTokenHashStable(t *testing.T) {
	if hashBindingToken("abc") != hashBindingToken("abc") {
		t.Error("hash must be deterministic")
	}
	if hashBindingToken("abc") == hashBindingToken("abd") {
		t.Error("hash must differ for different inputs")
	}
	if len(hashBindingToken("abc")) != 64 {
		t.Errorf("hash length = %d, want 64 hex chars", len(hashBindingToken("abc")))
	}
}

func TestRandomBindingTokenUnique(t *testing.T) {
	a, err := randomBindingToken(32)
	if err != nil {
		t.Fatalf("randomBindingToken: %v", err)
	}
	b, err := randomBindingToken(32)
	if err != nil {
		t.Fatalf("randomBindingToken: %v", err)
	}
	if a == b {
		t.Error("two tokens must differ")
	}
	if len(a) < 40 {
		t.Errorf("token too short: %q", a)
	}
}

func TestRedeemAndBind_RequiresTx(t *testing.T) {
	svc := NewBindingTokenService(nil, nil)
	if _, err := svc.RedeemAndBind(context.Background(), "raw", mustUUID(t, "00000000-0000-0000-0000-000000000001")); err == nil {
		t.Fatal("expected an error when no TxStarter is configured")
	}
}

func TestDroppedReplyText(t *testing.T) {
	if got := droppedReplyText(engine.Result{DropReason: engine.DropReasonNonWorkspaceMember}, channel.InboundMessage{}); got == "" {
		t.Error("non-workspace-member drop must produce a refusal")
	}
	if got := droppedReplyText(engine.Result{DropReason: engine.DropReasonDuplicate}, channel.InboundMessage{}); got != "" {
		t.Errorf("duplicate drop must stay silent, got %q", got)
	}
}

func TestIssueCreatedText(t *testing.T) {
	res := engine.Result{IssueIdentifier: "MUL-42", IssueTitle: "Fix login"}
	text := issueCreatedText(res, "https://app.example")
	if !strings.Contains(text, "MUL-42") || !strings.Contains(text, "Fix login") {
		t.Errorf("issueCreatedText = %q", text)
	}
	if got := issueCreatedText(engine.Result{}, ""); got == "" {
		t.Error("a missing identifier still needs a confirmation")
	}
}
