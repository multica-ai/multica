package runtime

import (
	"context"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// With no connection store wired (connDeny == nil), apiEndpointSetting fails open
// to Allow and filterDeniedAPIEndpoints keeps every tool — the behaviour-safe
// default when the resolver is unavailable.
func TestAPIEndpointSettingFailOpenNilStore(t *testing.T) {
	e := &FirtalGatewayExecutor{logger: slog.Default()}
	reg := &Registry{tools: map[string]Tool{}}
	api := &APIConnectionTool{toolName: "c__get_x", connName: "c", method: "GET", path: "/x", baseURL: "http://x"}
	reg.Register(api)

	got, _ := e.apiEndpointSetting(context.Background(), gateTestUUID(1), gateTestUUID(9), reg, "c__get_x", false, GatewayRequestMeta{})
	if got != toolpolicy.SettingAllow {
		t.Fatalf("expected Allow (fail-open, nil store), got %s", got)
	}
}

// failClosed=true must never deny a tool that is NOT an API-connection tool: the
// secrets-box fail-closed path only applies once the tool is confirmed to be an
// API-connection tool, so ordinary tools stay Allow even at call time. (FIR-2166
// C review fix — guards against the fail-closed flag over-blocking.)
func TestAPIEndpointSettingFailClosedNonAPIToolStaysAllow(t *testing.T) {
	e := &FirtalGatewayExecutor{logger: slog.Default()}
	reg := &Registry{tools: map[string]Tool{}}
	reg.Register(stubTool{name: "web_fetch"})

	got, conn := e.apiEndpointSetting(context.Background(), gateTestUUID(1), gateTestUUID(9), reg, "web_fetch", true, GatewayRequestMeta{})
	if got != toolpolicy.SettingAllow || conn != "" {
		t.Fatalf("non-API tool must stay Allow/\"\" even with failClosed, got %s/%q", got, conn)
	}
}

// A tool name that is not an API-connection tool must resolve to Allow without
// any DB work, so ordinary tools are never affected by endpoint enforcement.
func TestAPIEndpointSettingNonAPITool(t *testing.T) {
	e := &FirtalGatewayExecutor{logger: slog.Default()}
	reg := &Registry{tools: map[string]Tool{}}
	reg.Register(stubTool{name: "web_fetch"})

	got, conn := e.apiEndpointSetting(context.Background(), gateTestUUID(1), gateTestUUID(9), reg, "web_fetch", false, GatewayRequestMeta{})
	if got != toolpolicy.SettingAllow || conn != "" {
		t.Fatalf("non-API tool must be Allow/\"\", got %s/%q", got, conn)
	}
}

// filterDeniedAPIEndpoints passes non-API tools through untouched and, with the
// resolver failing open, keeps API tools too.
func TestFilterDeniedAPIEndpointsPassthrough(t *testing.T) {
	e := &FirtalGatewayExecutor{logger: slog.Default()}
	reg := &Registry{tools: map[string]Tool{}}
	api := &APIConnectionTool{toolName: "c__get_x", connName: "c", method: "GET", path: "/x", baseURL: "http://x"}
	stub := stubTool{name: "web_fetch"}
	reg.Register(api)
	reg.Register(stub)

	in := []Tool{api, stub}
	out := e.filterDeniedAPIEndpoints(context.Background(), gateTestUUID(1), gateTestUUID(9), reg, in, GatewayRequestMeta{})
	if len(out) != 2 {
		t.Fatalf("expected both tools kept (fail-open), got %d", len(out))
	}
}
