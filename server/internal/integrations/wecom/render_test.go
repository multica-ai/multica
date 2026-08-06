package wecom

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTruncateUTF8BytesPreservesRuneBoundary(t *testing.T) {
	body := strings.Repeat("你好", 6000)
	got := TruncateUTF8Bytes(body, 10)
	if len(got) > 10 {
		t.Fatalf("expected <=10 bytes, got %d", len(got))
	}
	if !strings.HasPrefix(body, got) {
		t.Fatal("truncated prefix mismatch")
	}
}

func TestRenderOutboundBindingPrompt(t *testing.T) {
	raw := []byte(`{"template":"binding_prompt"}`)
	got, err := RenderOutbound(raw, RenderInput{
		Locale:          "en",
		AppURL:          "https://app.example.com",
		BindingTokenRaw: "tok123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "https://app.example.com/wecom/bind?token=tok123") {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestRenderOutboundChatDoneTruncates(t *testing.T) {
	content := strings.Repeat("a", outboundMaxUTF8Bytes+100)
	raw, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		t.Fatal(err)
	}
	got, err := RenderOutbound(raw, RenderInput{Locale: "en", AppURL: "https://app.example.com", WorkspaceSlug: "acme", ChatSessionID: "sess"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > outboundMaxUTF8Bytes {
		t.Fatalf("expected capped body, got %d bytes", len(got))
	}
}

func TestRenderOutboundUnknownTemplateFails(t *testing.T) {
	_, err := RenderOutbound([]byte(`{"template":"nope"}`), RenderInput{Locale: "en"})
	if err == nil {
		t.Fatal("expected error")
	}
}
