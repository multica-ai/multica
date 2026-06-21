package runtime

// FIR-1449 step 2 — unit coverage for the bridge's wiring/gating logic. The
// end-to-end bridge mechanics (a CLI tool callable through the gateway, agent
// identity reaching the server, admin tools denied) are proven separately in
// firtal_gateway_bridge_test.go; here we only assert the executor-side
// enable/guard behavior that must never touch the DB or panic.

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func dummyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
}

// pgUUID returns a valid pgtype.UUID for guard tests.
func pgUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan("11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	return u
}

func TestSetInProcessBridge_EnableState(t *testing.T) {
	e := &FirtalGatewayExecutor{}
	if e.inProcessBridgeEnabled() {
		t.Fatal("bridge should be disabled on a zero executor")
	}
	e.SetInProcessBridge(dummyHandler())
	if !e.inProcessBridgeEnabled() {
		t.Fatal("bridge should be enabled after SetInProcessBridge")
	}
	e.SetInProcessBridge(nil)
	if e.inProcessBridgeEnabled() {
		t.Fatal("passing nil should disable the bridge")
	}
}

func TestMaybeEnableInProcessBridge_EnvGate(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"nonsense", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
	}
	for _, tc := range cases {
		t.Run("val="+tc.val, func(t *testing.T) {
			t.Setenv(inProcBridgeEnvFlag, tc.val)
			e := &FirtalGatewayExecutor{}
			MaybeEnableInProcessBridge(e, dummyHandler())
			if got := e.inProcessBridgeEnabled(); got != tc.want {
				t.Fatalf("env=%q: enabled=%v, want %v", tc.val, got, tc.want)
			}
		})
	}
}

func TestMaybeEnableInProcessBridge_NilHandler(t *testing.T) {
	t.Setenv(inProcBridgeEnvFlag, "1")
	e := &FirtalGatewayExecutor{}
	MaybeEnableInProcessBridge(e, nil)
	if e.inProcessBridgeEnabled() {
		t.Fatal("a nil handler must leave the bridge disabled even with the flag on")
	}
}

// bridgeInProcessTools must be a safe no-op (no panic, no registry mutation,
// no DB call) when the bridge is disabled or inputs are missing — the guards
// run before any *db.Queries access, so a nil queries field is fine here.
func TestBridgeInProcessTools_DisabledNoop(t *testing.T) {
	reg := NewRegistry(nil)
	before := len(reg.tools)

	// Disabled executor: returns before touching reg or queries.
	(&FirtalGatewayExecutor{}).bridgeInProcessTools(context.Background(), reg, pgUUID(t), pgUUID(t), pgUUID(t), "task-1")
	if len(reg.tools) != before {
		t.Fatal("disabled bridge must not register tools")
	}

	// Enabled but nil registry: must not panic.
	e := &FirtalGatewayExecutor{}
	e.SetInProcessBridge(dummyHandler())
	e.bridgeInProcessTools(context.Background(), nil, pgUUID(t), pgUUID(t), pgUUID(t), "task-1")

	// Enabled, valid registry, but empty taskID: returns before CreateTaskToken
	// (e.queries is nil here, so reaching it would panic — proving the guard).
	e.bridgeInProcessTools(context.Background(), reg, pgUUID(t), pgUUID(t), pgUUID(t), "")
	if len(reg.tools) != before {
		t.Fatal("empty taskID must short-circuit before registering tools")
	}
}
