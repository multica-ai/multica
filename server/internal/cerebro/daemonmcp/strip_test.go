package daemonmcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStripServers(t *testing.T) {
	raw := json.RawMessage(`{"mcpServers":{"customer-service":{"url":"u1"},"data-registry":{"url":"u2"}}}`)

	out := StripServers(raw, []string{"customer-service"})
	s := string(out)
	if strings.Contains(s, "customer-service") {
		t.Fatalf("expected customer-service stripped, got %s", s)
	}
	if !strings.Contains(s, "data-registry") {
		t.Fatalf("expected data-registry kept, got %s", s)
	}

	// No-op cases.
	if got := StripServers(raw, nil); string(got) != string(raw) {
		t.Fatalf("nil names should be a no-op")
	}
	if got := StripServers(raw, []string{"nonexistent"}); string(got) != string(raw) {
		t.Fatalf("unmatched name should be a no-op")
	}
	if got := StripServers(nil, []string{"x"}); got != nil {
		t.Fatalf("nil input should stay nil")
	}
}
