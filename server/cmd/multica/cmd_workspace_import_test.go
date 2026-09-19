package main

import (
	"strings"
	"testing"
)

func TestWorkspaceImportCmdDocumentsSecretPolicy(t *testing.T) {
	help := workspaceImportCmd.Long
	if !strings.Contains(help, "custom_env") || !strings.Contains(help, "mcp_config") {
		t.Fatalf("import help must say secrets are not copied:\n%s", help)
	}
	if workspaceImportCmd.Flag("from") == nil || workspaceImportCmd.Flag("runtime-id") == nil {
		t.Fatal("import command must expose --from and --runtime-id")
	}
}
