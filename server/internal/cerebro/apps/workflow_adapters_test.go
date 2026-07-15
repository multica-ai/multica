package apps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/apps/tokens"
	"github.com/multica-ai/multica/server/internal/cerebro/apps/workflowexec"
)

type recordingTokenIssuer struct {
	forgotten int
	issued    int
}

func (r *recordingTokenIssuer) Forget(tokens.Identity) int { r.forgotten++; return 1 }
func (r *recordingTokenIssuer) PersonalKey(context.Context, tokens.Identity) (tokens.Token, error) {
	r.issued++
	return tokens.Token{Key: "sk_real"}, nil
}

func TestWorkflowTokenSourceForcesAFreshPersonalKeyForEveryStep(t *testing.T) {
	issuer := &recordingTokenIssuer{}
	source := workflowTokenSource{issuer: issuer, identity: tokens.Identity{MemberID: "member"}}
	for range 2 {
		key, err := source.Key(context.Background())
		if err != nil || key != "sk_real" {
			t.Fatalf("key=%q err=%v", key, err)
		}
	}
	if issuer.forgotten != 2 || issuer.issued != 2 {
		t.Fatalf("fresh exchange per step: forgotten=%d issued=%d", issuer.forgotten, issuer.issued)
	}
}

func TestRegistryAdapterCallsRealV1RoutesWithRunTrace(t *testing.T) {
	requests := make([]map[string]any, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk_real" || r.Header.Get("X-Trace-ID") != "run-123" {
			t.Errorf("headers=%v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		body["path"] = r.URL.EscapedPath()
		requests = append(requests, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	adapter := newRegistryAdapter(server.URL, "run-123", server.Client())
	if _, err := adapter.Execute(context.Background(), "sk_real", workflowexec.RegistryCall{Kind: "read", ResourceID: "source id", Config: map[string]any{"parameters": map[string]any{"sku": "1"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Execute(context.Background(), "sk_real", workflowexec.RegistryCall{Kind: "write", ResourceID: "destination", Input: map[string]any{"sku": "1"}, Config: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if requests[0]["path"] != "/api/registry/v1/data-sources/source%20id/execute" {
		t.Fatalf("read request=%+v", requests[0])
	}
	if requests[1]["path"] != "/api/registry/v1/data-destinations/destination/execute" || requests[1]["input"] == nil {
		t.Fatalf("write request=%+v", requests[1])
	}
}
