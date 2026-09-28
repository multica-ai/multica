package daemon

import (
	"context"
	"errors"
	"testing"
)

func TestBuiltinVersionsTracksPiManagedMCPCapability(t *testing.T) {
	state := func(capability string) string {
		return builtinVersionsFromPayload([]map[string]string{{
			"type": "pi", "version": "9.9.9", "managed_mcp": capability,
		}})["pi"]
	}
	if state("true") == state("false") {
		t.Fatal("plugin capability changes must refresh registration even when the Pi version is unchanged")
	}
}

func TestPiManagedMCPCapabilityRefreshesWithoutVersionChange(t *testing.T) {
	previous := probePiManagedMCP
	probeErr := errors.New("adapter missing")
	probePiManagedMCP = func() error { return probeErr }
	t.Cleanup(func() { probePiManagedMCP = previous })
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"pi": {Path: "/fake/pi"}}
	stubAgentProbe(t, d.cfg.Agents)
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"}, WorkspaceInfo{ID: "ws-2", Name: "two"})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	assertCapability := func(want string) {
		t.Helper()
		fx.mu.Lock()
		defer fx.mu.Unlock()
		latest := map[string]string{}
		for _, call := range fx.registered {
			latest[call.workspaceID] = call.managedMCP["pi"]
			if call.versions["pi"] != "9.9.9" {
				t.Fatalf("Pi wire version changed to %q", call.versions["pi"])
			}
		}
		for _, ws := range []string{"ws-1", "ws-2"} {
			if latest[ws] != want {
				t.Errorf("%s managed_mcp = %q, want %q", ws, latest[ws], want)
			}
		}
	}
	assertCapability("false")
	probeErr = nil
	fx.failRegister(true)
	d.refreshAgentVersions(context.Background())
	assertCapability("false")
	fx.failRegister(false)
	d.refreshAgentVersions(context.Background())
	assertCapability("true")
	calls := fx.registerCallCount()
	d.refreshAgentVersions(context.Background())
	if fx.registerCallCount() != calls {
		t.Fatal("unchanged plugin must not trigger another registration")
	}
	probeErr = errors.New("adapter removed")
	d.refreshAgentVersions(context.Background())
	assertCapability("false")
}
