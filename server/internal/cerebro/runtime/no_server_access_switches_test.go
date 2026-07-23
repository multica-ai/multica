package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessRolloutIsNotControlledByServerEnvironmentSwitches(t *testing.T) {
	serverRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	repoRoot := filepath.Join(serverRoot, "..")
	for _, rel := range []string{
		"server/internal/cerebro/runtime/firtal_gateway_inproc_bridge.go",
		"server/internal/cerebro/runtime/approval_gate.go",
		"server/cmd/server/main.go",
		".env.example",
	} {
		body, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			"MULTICA_SERVER_FIRTAL_GATEWAY_INPROCESS_BRIDGE",
			"CEREBRO_APPROVAL_GATE_ENABLED",
			"CEREBRO_APPROVAL_GATE_MODE",
			"CEREBRO_APPROVAL_GATE_AGENTS",
		} {
			if strings.Contains(string(body), forbidden) {
				t.Fatalf("legacy server access switch %s exists in %s", forbidden, rel)
			}
		}
	}
}
