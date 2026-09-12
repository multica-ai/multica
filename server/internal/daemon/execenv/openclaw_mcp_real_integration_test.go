//go:build agentintegration

package execenv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Real-CLI coverage for the managed-MCP include chain. Opt-in twice over: the
// agentintegration build tag, and MULTICA_RUN_REAL_AGENT_SMOKE, because this
// executes the openclaw binary installed on the host.
//
// The unit tests assert the JSON this package writes. That is not enough for a
// design whose correctness lives in OpenClaw's include-merge semantics — ordered
// array includes, a null source replacing an object, and sibling keys on the
// includer winning over the include result. Only the real loader can confirm
// that, which is why this test asks the CLI to resolve the chain we generated
// rather than inspecting our own files.
//
// Those three behaviours and the exact managed-server isolation were measured on
// 2026-08-26 on Windows 10.0.19045.6466 (PowerShell 7.6.4, Go 1.26.6
// windows/amd64) against the npm extended-stable (2026.6.34), latest (2026.7.1-2)
// and beta (2026.8.1-beta.3) channels. Windows on purpose — that is the platform
// where the npm shim puts `cmd.exe → node → node` between the daemon and the CLI,
// so it is the least forgiving place to assert include resolution.
//
// The control that makes a pass mean something was run by hand rather than
// asserted here: dropping the reset stage from the chain leaks `user-only` back
// into the resolved server map on all three channels. Without that, a green test
// could equally mean "the wrapper's own block happened to win".
//
// The OpenClaw config compatibility smoke workflow keeps those three moving
// channels under scheduled and manually dispatched coverage.
//
// realOpenclawBin — the opt-in gate and binary lookup — is shared with
// openclaw_real_integration_test.go.

// mcpSibling is a non-server setting under `mcp` that must survive the reset
// untouched, together with the value the loader should report back.
//
// A list of candidates rather than one hard-coded key, because the `mcp` schema
// is not the same across the channels this runs against: 2026.8.1-beta.3 declares
// `mcp` as exactly `servers` + `apps` with additionalProperties:false, while
// `sessionIdleTtlMs` is accepted on the older channels. Hard-coding either one
// turns a schema difference into a red smoke run that says nothing about this
// change. So the fixture asks the CLI which shape it accepts.
type mcpSibling struct {
	key      string
	fragment string
	want     any
}

var mcpSiblingCandidates = []mcpSibling{
	{key: "sessionIdleTtlMs", fragment: `"sessionIdleTtlMs": 300000`, want: float64(300000)},
	{key: "apps", fragment: `"apps": {}`, want: map[string]any{}},
}

// realOpenclawConfig points the CLI at an isolated HOME holding a user config
// with a user-only MCP server (which must not survive the reset), a same-name
// server whose definition the managed set must replace, and a non-server `mcp`
// sibling this host's schema accepts (which must survive untouched).
func realOpenclawConfig(t *testing.T) (bin, activeConfig string, sibling mcpSibling) {
	t.Helper()
	bin = realOpenclawBin(t)

	home := t.TempDir()
	stateDir := filepath.Join(home, ".openclaw")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create isolated OpenClaw state: %v", err)
	}
	activeConfig = filepath.Join(stateDir, "openclaw.json")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OPENCLAW_HOME", home)
	t.Setenv("OPENCLAW_STATE_DIR", stateDir)
	t.Setenv("OPENCLAW_CONFIG_PATH", activeConfig)

	for _, candidate := range mcpSiblingCandidates {
		writeRealOpenclawUserConfig(t, activeConfig, candidate)
		if realOpenclawConfigValid(t, bin) {
			return bin, activeConfig, candidate
		}
		t.Logf("channel rejects mcp.%s; trying the next sibling shape", candidate.key)
	}
	t.Fatalf("no candidate mcp sibling is accepted by this channel; the fixture needs a shape from its schema")
	return "", "", mcpSibling{}
}

func writeRealOpenclawUserConfig(t *testing.T, path string, sibling mcpSibling) {
	t.Helper()
	config := fmt.Sprintf(`{
		"gateway": {"mode": "local"},
		"logging": {"level": "debug"},
		"mcp": {
			%s,
			"servers": {
				"user-only": {"command": "user-only"},
				"shared": {"command": "user-shared"}
			}
		}
	}`, sibling.fragment)
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write isolated OpenClaw config: %v", err)
	}
}

// realOpenclawConfigValid reports whether the CLI accepts the config it is
// currently pointed at. `config validate --json` carries `valid` on every branch
// since 2026.5.5 and exits non-zero for an invalid config, so the exit status is
// deliberately ignored in favour of the payload.
func realOpenclawConfigValid(t *testing.T, bin string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), openclawCLITimeout)
	defer cancel()
	out, _ := openclawExec(ctx, nil, bin, "config", "validate", "--json")
	var payload struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse `config validate --json` output %q: %v", out, err)
	}
	return payload.Valid
}

func realOpenclawConfigGetJSON(t *testing.T, bin, keyPath string) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), openclawCLITimeout)
	defer cancel()
	out, err := openclawExec(ctx, nil, bin, "config", "get", keyPath, "--json")
	if err != nil {
		t.Fatalf("resolve %s through the real CLI: %v", keyPath, annotateOpenclawJSONError(err, out))
	}
	var value any
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		t.Fatalf("parse resolved %s JSON %q: %v", keyPath, out, err)
	}
	return value
}

func TestPrepareOpenclawConfigRealCLI(t *testing.T) {
	bin, activeConfig, sibling := realOpenclawConfig(t)

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: bin,
		McpConfig: json.RawMessage(`{"mcpServers":{
			"managed-only":{"command":"managed-only"},
			"shared":{"command":"managed-shared"}
		}}`),
	})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig against the real CLI: %v", err)
	}

	// The chain we generated, before asking the loader what it makes of it.
	wrapper := mustReadJSON(t, result.ConfigPath)
	resetPath := filepath.Join(envRoot, openclawMcpResetFile)
	include, ok := wrapper["$include"].([]any)
	if !ok || len(include) != 2 || include[0] != activeConfig || include[1] != resetPath {
		t.Fatalf("wrapper $include = %#v, want [%q, %q]", wrapper["$include"], activeConfig, resetPath)
	}
	if result.IncludeRoot != filepath.Dir(activeConfig) {
		t.Fatalf("include root = %q, want %q", result.IncludeRoot, filepath.Dir(activeConfig))
	}

	// Ask the real CLI to resolve it. This is what verifies the reset rather than
	// merely inspecting the JSON we wrote.
	t.Setenv("OPENCLAW_CONFIG_PATH", result.ConfigPath)
	t.Setenv("OPENCLAW_INCLUDE_ROOTS", result.IncludeRoot)

	// The generated chain has to load at all: a `mcp.servers: null` that survived
	// to the resolved root would be rejected here, which is the failure
	// TestPrepareOpenclawConfigResetStagePairsWithWrapperMcp guards in unit form.
	if !realOpenclawConfigValid(t, bin) {
		t.Fatalf("the generated config does not validate; the reset stage is not paired with a wrapper mcp.servers block")
	}

	// A field that exists only in the live user config, so seeing it proves the
	// wrapper's include followed through to the user's file rather than merely
	// leaving its own managed block intact.
	if level := realOpenclawConfigGetJSON(t, bin, "logging.level"); level != "debug" {
		t.Fatalf("resolved logging.level = %#v, want debug through the include", level)
	}

	resolvedMcp, ok := realOpenclawConfigGetJSON(t, bin, "mcp").(map[string]any)
	if !ok {
		t.Fatalf("resolved mcp is not an object")
	}

	// Half one: the server map is exactly the managed set. `user-only` must be
	// gone and `shared` must carry the managed definition, not the user's.
	wantServers := map[string]any{
		"managed-only": map[string]any{"command": "managed-only"},
		"shared":       map[string]any{"command": "managed-shared"},
	}
	servers, ok := resolvedMcp["servers"].(map[string]any)
	if !ok || !reflect.DeepEqual(servers, wantServers) {
		t.Fatalf("resolved mcp.servers = %#v, want exactly %#v", resolvedMcp["servers"], wantServers)
	}

	// Half two, and the reason the reset names one key instead of the whole
	// object: the user's non-server MCP setting has to arrive unchanged, straight
	// from their own file, without this package having read or re-emitted it.
	got, present := resolvedMcp[sibling.key]
	if !present {
		t.Fatalf("resolved mcp lost the user's %s; the reset replaced more than the server map: %#v",
			sibling.key, resolvedMcp)
	}
	if !reflect.DeepEqual(got, sibling.want) {
		t.Fatalf("resolved mcp.%s = %#v, want the user's own %#v", sibling.key, got, sibling.want)
	}
}
