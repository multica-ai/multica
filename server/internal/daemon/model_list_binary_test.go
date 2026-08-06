package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// modelListFixture stands up a Daemon whose model-list report is captured and
// whose agent.ListModels call is stubbed, so a test can assert exactly which
// executable path model discovery enumerated without shelling out to a CLI.
type modelListFixture struct {
	daemon *Daemon

	mu             sync.Mutex
	listedProvider string
	listedPath     string
	listCalls      int
	report         map[string]any
}

func newModelListFixture(t *testing.T) *modelListFixture {
	t.Helper()
	withFastLocalSkillReportBackoffs(t)

	fx := &modelListFixture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fx.mu.Lock()
		fx.report = body
		fx.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.profileLaunchSpecs = make(map[string]profileLaunchSpec)
	fx.daemon = d

	orig := listModels
	listModels = func(_ context.Context, provider, execPath string) (agent.Catalog, error) {
		fx.mu.Lock()
		fx.listedProvider = provider
		fx.listedPath = execPath
		fx.listCalls++
		fx.mu.Unlock()
		return agent.Catalog{Models: []agent.Model{{ID: "m-1", Label: "M 1"}}}, nil
	}
	t.Cleanup(func() { listModels = orig })

	return fx
}

func (fx *modelListFixture) snapshot() (provider, path string, calls int, report map[string]any) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.listedProvider, fx.listedPath, fx.listCalls, fx.report
}

// fakeExecutable writes an executable file the daemon's exec.LookPath-based
// presence check accepts. Tests must never resolve a user-installed agent CLI,
// so every built-in AgentEntry.Path in this file points at one of these.
func fakeExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	return path
}

// TestHandleModelList_CustomProfileEnumeratesProfileBinary pins the core of
// MUL-5789: a profile-backed runtime must have its models discovered from the
// binary the profile pinned, not from the built-in CLI of the same protocol
// family that happens to also be installed. Enumerating the built-in advertises
// a catalog the launched binary never agreed to.
func TestHandleModelList_CustomProfileEnumeratesProfileBinary(t *testing.T) {
	fx := newModelListFixture(t)
	d := fx.daemon

	builtinPath := fakeExecutable(t, "hermes")
	d.cfg.Agents = map[string]AgentEntry{"hermes": {Path: builtinPath}}
	d.runtimeIndex["rt-custom"] = Runtime{ID: "rt-custom", Provider: "hermes", ProfileID: "prof-1"}
	d.profileLaunchSpecs["prof-1"] = profileLaunchSpec{path: "/usr/local/bin/jcode", version: "1.2.3"}

	d.handleModelList(context.Background(), d.runtimeIndex["rt-custom"], "req-1")

	provider, path, calls, report := fx.snapshot()
	if calls != 1 {
		t.Fatalf("expected exactly 1 discovery call, got %d", calls)
	}
	if provider != "hermes" {
		t.Fatalf("discovery provider = %q, want the protocol family %q", provider, "hermes")
	}
	if path != "/usr/local/bin/jcode" {
		t.Fatalf("discovery path = %q, want the profile's command path", path)
	}
	if got := report["status"]; got != "completed" {
		t.Fatalf("report status = %v, want completed (report: %v)", got, report)
	}
}

// TestHandleModelList_CustomProfileWithoutBuiltinAgent is the reported failure
// (GitHub #6466): a host that has only the custom command installed — no
// built-in CLI of the same protocol family — used to answer the model list with
// `no agent configured for provider`, leaving the picker empty even though the
// same runtime executes tasks fine.
func TestHandleModelList_CustomProfileWithoutBuiltinAgent(t *testing.T) {
	fx := newModelListFixture(t)
	d := fx.daemon

	// No built-in agents at all on this host.
	d.cfg.Agents = map[string]AgentEntry{}
	d.runtimeIndex["rt-custom"] = Runtime{ID: "rt-custom", Provider: "hermes", ProfileID: "prof-1"}
	d.profileLaunchSpecs["prof-1"] = profileLaunchSpec{path: "/usr/local/bin/jcode"}

	d.handleModelList(context.Background(), d.runtimeIndex["rt-custom"], "req-1")

	_, path, calls, report := fx.snapshot()
	if calls != 1 {
		t.Fatalf("expected discovery to run, got %d calls (report: %v)", calls, report)
	}
	if path != "/usr/local/bin/jcode" {
		t.Fatalf("discovery path = %q, want the profile's command path", path)
	}
	if got := report["status"]; got != "completed" {
		t.Fatalf("report status = %v, want completed (report: %v)", got, report)
	}
	if _, hasErr := report["error"]; hasErr {
		t.Fatalf("report carried an error: %v", report)
	}
}

// TestHandleModelList_BuiltinRuntimeUnaffected pins that the built-in path is
// untouched, including when unrelated profile launch specs exist on the same
// daemon. A built-in runtime has no ProfileID, so it must keep resolving
// through the provider's agent entry.
func TestHandleModelList_BuiltinRuntimeUnaffected(t *testing.T) {
	fx := newModelListFixture(t)
	d := fx.daemon

	builtinPath := fakeExecutable(t, "codex")
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: builtinPath}}
	d.runtimeIndex["rt-builtin"] = Runtime{ID: "rt-builtin", Provider: "codex"}
	// A custom profile for a different runtime must not leak into this lookup.
	d.runtimeIndex["rt-custom"] = Runtime{ID: "rt-custom", Provider: "codex", ProfileID: "prof-1"}
	d.profileLaunchSpecs["prof-1"] = profileLaunchSpec{path: "/opt/bin/company-codex"}

	d.handleModelList(context.Background(), d.runtimeIndex["rt-builtin"], "req-1")

	_, path, calls, report := fx.snapshot()
	if calls != 1 {
		t.Fatalf("expected exactly 1 discovery call, got %d", calls)
	}
	if path != builtinPath {
		t.Fatalf("discovery path = %q, want the built-in entry path %q", path, builtinPath)
	}
	if got := report["status"]; got != "completed" {
		t.Fatalf("report status = %v, want completed (report: %v)", got, report)
	}
}

// TestHandleModelList_NoProfileAndNoBuiltinStillFails pins that the failure
// message survives for the case it was actually written for: a built-in runtime
// whose provider has no agent entry on this host.
func TestHandleModelList_NoProfileAndNoBuiltinStillFails(t *testing.T) {
	fx := newModelListFixture(t)
	d := fx.daemon

	d.cfg.Agents = map[string]AgentEntry{}
	d.runtimeIndex["rt-builtin"] = Runtime{ID: "rt-builtin", Provider: "codex"}

	d.handleModelList(context.Background(), d.runtimeIndex["rt-builtin"], "req-1")

	_, _, calls, report := fx.snapshot()
	if calls != 0 {
		t.Fatalf("expected no discovery call, got %d", calls)
	}
	if got := report["status"]; got != "failed" {
		t.Fatalf("report status = %v, want failed (report: %v)", got, report)
	}
	if got, want := report["error"], `no agent configured for provider "codex"`; got != want {
		t.Fatalf("report error = %v, want %q", got, want)
	}
}

// TestHandleModelList_UnresolvedProfileFallsBackToBuiltin covers the profile
// whose command never resolved on this host (registration skipped it, so there
// is no launch spec). Discovery must not invent a path — it falls through to
// the built-in entry, matching customProfileLaunchForRuntime's contract.
func TestHandleModelList_UnresolvedProfileFallsBackToBuiltin(t *testing.T) {
	fx := newModelListFixture(t)
	d := fx.daemon

	builtinPath := fakeExecutable(t, "hermes")
	d.cfg.Agents = map[string]AgentEntry{"hermes": {Path: builtinPath}}
	d.runtimeIndex["rt-custom"] = Runtime{ID: "rt-custom", Provider: "hermes", ProfileID: "prof-missing"}

	d.handleModelList(context.Background(), d.runtimeIndex["rt-custom"], "req-1")

	_, path, calls, report := fx.snapshot()
	if calls != 1 {
		t.Fatalf("expected exactly 1 discovery call, got %d", calls)
	}
	if path != builtinPath {
		t.Fatalf("discovery path = %q, want the built-in fallback %q", path, builtinPath)
	}
	if got := report["status"]; got != "completed" {
		t.Fatalf("report status = %v, want completed (report: %v)", got, report)
	}
}
