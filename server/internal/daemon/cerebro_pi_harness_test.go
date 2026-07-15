package daemon

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestPreparePiHarnessOwnsExtensionsAndExposesPolicy(t *testing.T) {
	env := map[string]string{}
	args, err := preparePiHarness(true, "pi", t.TempDir(), "enforce", []string{"--theme", "dark", "-e", "/tmp/rogue.ts"}, json.RawMessage(`{"mcpServers":{}}`), env)
	if err != nil {
		t.Fatalf("preparePiHarness: %v", err)
	}
	if !slices.Contains(args, "--no-extensions") {
		t.Fatalf("managed args missing --no-extensions: %#v", args)
	}
	if slices.Contains(args, "/tmp/rogue.ts") {
		t.Fatalf("unmanaged extension leaked into args: %#v", args)
	}
	if env["CEREBRO_TOOLPOLICY_STAGE"] != "enforce" {
		t.Fatalf("policy stage = %q", env["CEREBRO_TOOLPOLICY_STAGE"])
	}
	if env["MULTICA_PI_HARNESS_COMMAND"] == "" {
		t.Fatal("managed MCP command was not exposed")
	}
	if env["MULTICA_PI_HARNESS_MCP_CONFIG"] == "" {
		t.Fatal("managed MCP HTTP config was not exposed")
	}
	path := args[len(args)-1]
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("managed extension does not exist: %v", err)
	}
}

func TestPreparePiHarnessLeavesOtherProvidersUntouched(t *testing.T) {
	want := []string{"--allowedTools", "Read"}
	env := map[string]string{}
	args, err := preparePiHarness(true, "claude", t.TempDir(), "enforce", want, nil, env)
	if err != nil {
		t.Fatalf("preparePiHarness: %v", err)
	}
	if !slices.Equal(args, want) || len(env) != 0 {
		t.Fatalf("non-Pi provider changed: args=%#v env=%#v", args, env)
	}
}

func TestPreparePiHarnessKillSwitchLeavesPiUntouched(t *testing.T) {
	want := []string{"-e", "/user/extension.ts"}
	env := map[string]string{}
	args, err := preparePiHarness(false, "pi", t.TempDir(), "enforce", want, nil, env)
	if err != nil {
		t.Fatalf("preparePiHarness: %v", err)
	}
	if !slices.Equal(args, want) || len(env) != 0 {
		t.Fatalf("disabled Pi harness changed spawn: args=%#v env=%#v", args, env)
	}
}
