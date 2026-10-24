package execenv

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAntigravityMCPAudit(t *testing.T) {
	valid := []antigravityMCPEvent{
		{Method: "initialize"},
		{Method: "notifications/initialized", Session: "session"},
		{Method: "tools/list", Session: "session"},
		{Method: "tools/call", Session: "session", Name: "calc_cube", Argument: 7, Result: "CUBE_SUCCESS:343"},
	}
	if err := validateAntigravityMCPAudit(valid, "session", "calc_cube", 7, "CUBE_SUCCESS:343"); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"no call", "no initialize", "no notification", "no discovery", "wrong session", "missing session", "wrong tool", "wrong argument", "wrong response", "call before discovery", "invalid initialize session", "incomplete new session"} {
		t.Run(scenario, func(t *testing.T) {
			events := append([]antigravityMCPEvent(nil), valid...)
			switch scenario {
			case "no call":
				events = events[:3]
			case "no initialize":
				events = events[1:]
			case "no notification":
				events = append(events[:1], events[2:]...)
			case "no discovery":
				events = append(events[:2], events[3:]...)
			case "wrong session":
				events[2].Session = "other"
			case "missing session":
				events[1].Session = ""
			case "wrong tool":
				events[3].Name = "fabricated"
			case "wrong argument":
				events[3].Argument = 12
			case "wrong response":
				events[3].Result = "CUBE_SUCCESS:0"
			case "call before discovery":
				events[2], events[3] = events[3], events[2]
			case "invalid initialize session":
				events[0].Session = "stale"
			case "incomplete new session":
				events = append(events, antigravityMCPEvent{Method: "initialize"})
			}
			if err := validateAntigravityMCPAudit(events, "session", "calc_cube", 7, "CUBE_SUCCESS:343"); err == nil {
				t.Fatal("invalid protocol evidence accepted")
			}
		})
	}
	for index := range valid {
		valid[index].Session = ""
	}
	if err := validateAntigravityMCPAudit(valid, "", "calc_cube", 7, "CUBE_SUCCESS:343"); err != nil {
		t.Fatalf("stdio audit: %v", err)
	}
}

func TestAntigravityProbeHelper(t *testing.T) {
	switch os.Getenv("ANTIGRAVITY_PROBE_TEST_MODE") {
	case "success":
		os.Stdout.WriteString("NONE\n")
		os.Exit(0)
	case "failure":
		os.Exit(23)
	case "timeout":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
}

func TestAntigravityProbeFailures(t *testing.T) {
	for _, mode := range []string{"success", "failure", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ANTIGRAVITY_PROBE_TEST_MODE", mode)
			duration := 5 * time.Second
			if mode == "timeout" {
				duration = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), duration)
			defer cancel()
			output, err := runAntigravityProbe(ctx, os.Args[0], t.TempDir(), "", "-test.run=^TestAntigravityProbeHelper$", "--")
			if mode == "success" {
				if err != nil || output != "NONE" {
					t.Fatalf("successful probe: output=%q err=%v", output, err)
				}
			} else if err == nil {
				t.Fatalf("%s probe must fail", mode)
			}
		})
	}
}

func TestAntigravityFailedStartupLocalTeardown(t *testing.T) {
	workDir := t.TempDir()
	userFile := filepath.Join(workDir, "user-file")
	if err := os.WriteFile(userFile, []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "ws", TaskID: "failed-task", Provider: "antigravity", LocalWorkDir: workDir,
		McpConfig: json.RawMessage(`{"mcpServers":{"unused":{"command":"not-executed"}}}`),
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.Cleanup(true) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := runAntigravityProbe(ctx, filepath.Join(t.TempDir(), "missing-agy"), env.WorkDir, "unused"); err == nil {
		t.Fatal("missing CLI must fail task startup")
	}
	if err := CleanupRuntimeConfig(env.WorkDir, "antigravity"); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".agents", "plugins", "multica")); !os.IsNotExist(err) {
		t.Fatalf("failed startup left MCP sidecars: %v", err)
	}
	data, err := os.ReadFile(userFile)
	if err != nil || string(data) != "preserved" {
		t.Fatalf("user file changed: %v", err)
	}
}
