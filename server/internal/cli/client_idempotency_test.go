package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIClientPostJSONWithHeadersSendsIdempotencyKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Idempotency-Key"); got != "claim-intake-test-key" {
			t.Fatalf("Idempotency-Key = %q, want claim-intake-test-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "", "")
	var response map[string]any
	if err := client.PostJSONWithHeaders(context.Background(), "/mutation", map[string]any{"reason": "test"}, map[string]string{
		"Idempotency-Key": "claim-intake-test-key",
	}, &response); err != nil {
		t.Fatalf("PostJSONWithHeaders: %v", err)
	}
	if response["ok"] != true {
		t.Fatalf("response = %#v", response)
	}
}
