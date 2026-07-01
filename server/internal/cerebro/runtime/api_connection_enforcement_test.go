package runtime

import (
	"context"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// With no connection store wired (connDeny == nil), apiEndpointSetting fails open
// to Allow at call time — the behaviour-safe default when the resolver is
// unavailable (the always-on call-time guard re-checks; the list-time filter now
// lives in the shared APIConnectionResolver).
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

