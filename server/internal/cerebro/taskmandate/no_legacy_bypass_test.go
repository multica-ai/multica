package taskmandate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This contract prevents an executable server path from restoring either
// retired access store. Historical migrations are deliberately outside the
// scan because fresh databases must create old schema before migration 9147
// removes it.
func TestNoLegacyAccessStoreCanReenterServerCode(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	forbidden := []string{
		"FROM agent_tool_grant",
		"INTO agent_tool_grant",
		"UPDATE agent_tool_grant",
		"type RegistryGrant struct",
		"AppendRegistryProjection(",
		"internal/cerebro/agentpass",
		"/api/cerebro/agent-passes",
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return err
		}
		// The standalone recovery command is an operator-only, read-only source
		// reader for a separate PITR database. It is deliberately not linked into
		// the server or runtime and writes only canonical cerebro_tool_policy rows.
		if strings.Contains(filepath.ToSlash(path), "/cmd/cerebro_permission_recovery/") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, needle := range forbidden {
			if strings.Contains(string(raw), needle) {
				t.Errorf("legacy access bypass %q found in %s", needle, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
