//go:build agentintegration

package agent

import (
	"context"
	"testing"
)

// TestQwenRealModelCatalogSmoke verifies that the installed Qwen Code account
// configuration exposes its selectable headless model IDs without reading or
// logging credentials and without running a prompt.
func TestQwenRealModelCatalogSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	models, err := discoverQwenModels(context.Background(), Command{})
	if err != nil {
		t.Fatalf("discover Qwen models: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("Qwen settings returned an empty model catalog")
	}

	defaultCount := 0
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
		if model.Default {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		t.Fatalf("Qwen configured default model count = %d, want 1", defaultCount)
	}
	t.Logf("Qwen model catalog OK: count=%d ids=%v", len(models), ids)
}
