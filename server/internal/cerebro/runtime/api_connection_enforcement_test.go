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

	got, _ := e.apiEndpointSetting(context.Background(), gateTestUUID(1), gateTestUUID(9), reg, "c__get_x", GatewayRequestMeta{})
	if got != toolpolicy.SettingAllow {
		t.Fatalf("expected Allow (fail-open, nil store), got %s", got)
	}
}

// A tool name that is not an API-connection tool must resolve to Allow without
// any DB work, so ordinary tools are never affected by endpoint enforcement.
func TestAPIEndpointSettingNonAPITool(t *testing.T) {
	e := &FirtalGatewayExecutor{logger: slog.Default()}
	reg := &Registry{tools: map[string]Tool{}}
	reg.Register(stubTool{name: "web_fetch"})

	got, conn := e.apiEndpointSetting(context.Background(), gateTestUUID(1), gateTestUUID(9), reg, "web_fetch", GatewayRequestMeta{})
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
