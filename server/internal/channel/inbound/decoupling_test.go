package inbound

import (
	"os"
	"strings"
	"testing"
)

func TestInboundStepsDoNotBypassReplySink(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "reply_sink.go" {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(raw)
		for _, needle := range []string{
			".Gateway.SendText(",
			".Gateway.SendRich(",
			".gateway.SendText(",
			".gateway.SendRich(",
			".cfg.Gateway.SendText(",
			".cfg.Gateway.SendRich(",
		} {
			if strings.Contains(src, needle) {
				t.Fatalf("%s directly sends via gateway (%s); use ChannelReplySink", name, needle)
			}
		}
		if strings.Contains(src, "adapter/feishu") {
			t.Fatalf("%s imports Feishu adapter from inbound pipeline", name)
		}
	}
}
