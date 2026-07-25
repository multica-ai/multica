package main

import (
	"path/filepath"
	"testing"
)

func TestLifeOSMCPConfigIsRoleScopedAndUsesOriginalCodexHome(t *testing.T) {
	root := filepath.Join("tmp", "Life OS AI")
	controllerRoot := filepath.Join("tmp", "lifeos-controller")
	workbenchDB := filepath.Join("tmp", "LifeOS", "lifeos-workbench.sqlite3")
	config := lifeOSMCPConfig(root, controllerRoot, "http://127.0.0.1:8080", "/Users/test/.codex", workbenchDB, "judge")

	servers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers is missing")
	}
	lifeOS, ok := servers["lifeos"].(map[string]any)
	if !ok {
		t.Fatal("lifeos MCP server is missing")
	}
	env, ok := lifeOS["env"].(map[string]string)
	if !ok {
		t.Fatal("lifeos MCP env is missing")
	}
	if got := env["LIFEOS_ROLE"]; got != "judge" {
		t.Fatalf("LIFEOS_ROLE = %q, want judge", got)
	}
	if got := env["LIFEOS_CODEX_HOME"]; got != "/Users/test/.codex" {
		t.Fatalf("LIFEOS_CODEX_HOME = %q", got)
	}
	if got := env["LIFEOS_SERVER_URL"]; got != "http://127.0.0.1:8080" {
		t.Fatalf("LIFEOS_SERVER_URL = %q", got)
	}
	if got := env["LIFEOS_WORKBENCH_DB"]; got != workbenchDB {
		t.Fatalf("LIFEOS_WORKBENCH_DB = %q, want %q", got, workbenchDB)
	}
	args, ok := lifeOS["args"].([]string)
	if !ok || len(args) != 1 {
		t.Fatalf("args = %#v", lifeOS["args"])
	}
	if want := filepath.Join(controllerRoot, "scripts", "lifeos_mcp_server.py"); args[0] != want {
		t.Fatalf("MCP script = %q, want %q", args[0], want)
	}
}
