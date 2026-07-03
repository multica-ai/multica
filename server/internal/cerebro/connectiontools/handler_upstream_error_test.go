package connectiontools

// FIR-2441: an upstream failure must reach the agent as a readable error.
// The handler used to answer 502 — but Cloudflare replaces an origin's 502
// body with its generic "error code: 502" page, so the agent saw a bare 502
// and the real upstream message (e.g. Agent Vault's 403 "No access to this
// vault", or Infisical's 404 after a folder move) was lost. Two debugging
// sessions chased a phantom server crash because of it. The contract now:
// upstream failure → 424 Failed Dependency with the upstream detail in the
// JSON error body, a status Cloudflare passes through untouched.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebroruntime "github.com/multica-ai/multica/server/internal/cerebro/runtime"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestCallUpstreamErrorIs424WithDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"No access to this vault"}`))
	}))
	defer srv.Close()

	conns := flipConnLister{conns: []connections.Connection{{
		Name: "c", Type: connections.TypeAPI, URL: srv.URL, Enabled: true,
		EndpointPermissions: []connections.EndpointPermission{
			{Path: "/allow", Methods: []string{"GET"}},
		},
	}}}
	policy := flipEndpointPolicy{verdicts: map[string]toolpolicy.Setting{
		"c GET /allow": toolpolicy.SettingAllow,
	}}
	flag := flipFlag{on: true}
	api := cerebroruntime.NewAPIConnectionResolver(conns, policy, flag, nil)
	resolver := cerebroruntime.NewConnectionToolResolver(api, conns, nil, flag, nil, nil)
	res := &fakeResolver{
		wsID:    mustUUID(t, testWsID),
		runID:   mustUUID(t, testRunID),
		ownerID: mustUUID(t, testOwnerID),
	}
	h := NewHandler(res, resolver)

	tools := listTools(t, h)
	if len(tools) != 1 {
		t.Fatalf("want 1 listed tool, got %d: %v", len(tools), tools)
	}
	var name string
	for n := range tools {
		name = n
	}

	code, body := callTool(t, h, name)
	if code != http.StatusFailedDependency {
		t.Fatalf("upstream failure want 424 (NOT 502 — Cloudflare swallows 502 bodies), got %d (%s)", code, body)
	}
	msg := decodeErr(t, body)
	if !strings.Contains(msg, "403") || !strings.Contains(msg, "No access to this vault") {
		t.Fatalf("424 body must carry the upstream status and message, got %q", msg)
	}
}
