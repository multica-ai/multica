package agent

import (
	"context"
	"testing"
	"time"
)

// TestParseZcodeModels verifies the happy path: a realistic config.json (the
// shape zcode-cli writes) yields a catalog with the right IDs, labels,
// providers, and the model.main entry flagged Default.
func TestParseZcodeModels(t *testing.T) {
	config := []byte(`{
  "provider": {
    "zai": {
      "kind": "anthropic",
      "name": "Z.AI Coding Plan",
      "models": {
        "glm-5.2": {"name": "GLM-5.2"},
        "glm-5.1": {"name": "GLM-5.1"}
      }
    },
    "bigmodel": {
      "kind": "anthropic",
      "name": "BigModel Coding Plan",
      "models": {
        "glm-5.1": {"name": "GLM-5.1"},
        "glm-4.7": {"name": "GLM-4.7"}
      }
    }
  },
  "model": {
    "main": "bigmodel/glm-5.1"
  }
}`)
	models, err := parseZcodeModels(config)
	if err != nil {
		t.Fatalf("parseZcodeModels: unexpected error: %v", err)
	}
	if got, want := len(models), 4; got != want {
		t.Fatalf("expected %d models, got %d (%+v)", want, got, models)
	}

	// The catalog is sorted by provider label then ID, so the order is
	// deterministic: BigModel... entries before Z.AI... entries.
	byID := map[string]Model{}
	for _, m := range models {
		byID[m.ID] = m
	}

	got, ok := byID["bigmodel/glm-5.1"]
	if !ok {
		t.Fatalf("expected bigmodel/glm-5.1 in catalog, got %v", byID)
	}
	if got.Label != "GLM-5.1" {
		t.Errorf("bigmodel/glm-5.1 Label = %q, want %q", got.Label, "GLM-5.1")
	}
	if got.Provider != "BigModel Coding Plan" {
		t.Errorf("bigmodel/glm-5.1 Provider = %q, want %q", got.Provider, "BigModel Coding Plan")
	}
	if !got.Default {
		t.Errorf("bigmodel/glm-5.1 should be Default (matches model.main), got false")
	}

	// Every other entry must NOT be Default — only model.main is.
	for _, m := range models {
		if m.ID == "bigmodel/glm-5.1" {
			continue
		}
		if m.Default {
			t.Errorf("%s should not be Default, only bigmodel/glm-5.1 is", m.ID)
		}
	}

	// Sorting check: BigModel Coding Plan < Z.AI Coding Plan lexicographically,
	// so the first model belongs to BigModel.
	if models[0].Provider != "BigModel Coding Plan" {
		t.Errorf("first model Provider = %q, want %q (sorted)", models[0].Provider, "BigModel Coding Plan")
	}
}

// TestParseZcodeModelsMissingMain verifies that a config without a model.main
// still parses — no entry is marked Default, and the catalog is returned.
func TestParseZcodeModelsMissingMain(t *testing.T) {
	config := []byte(`{
  "provider": {
    "zai": {
      "name": "Z.AI",
      "models": {
        "glm-5.1": {"name": "GLM-5.1"}
      }
    }
  }
}`)
	models, err := parseZcodeModels(config)
	if err != nil {
		t.Fatalf("parseZcodeModels: unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].Default {
		t.Errorf("no model.main → no entry should be Default")
	}
}

// TestParseZcodeModelsInvalidJSON verifies that a malformed config degrades to
// an empty catalog (manual entry in the UI) rather than propagating an error —
// matching the contract documented on discoverZcodeModels.
func TestParseZcodeModelsInvalidJSON(t *testing.T) {
	models, err := parseZcodeModels([]byte("{not valid json"))
	if err != nil {
		t.Fatalf("parseZcodeModels should swallow parse errors, got: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models for invalid JSON, got %d", len(models))
	}
}

// TestParseZcodeModelsEmptyConfig verifies the empty/zero-provider config path.
func TestParseZcodeModelsEmptyConfig(t *testing.T) {
	models, err := parseZcodeModels([]byte(`{}`))
	if err != nil {
		t.Fatalf("parseZcodeModels: unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models for empty config, got %d", len(models))
	}
}

// TestDiscoverZcodeModelsMissingConfig verifies that a missing config file
// (discovered via os.UserHomeDir, which won't have .zcode/cli/config.json in
// the test runner) degrades to an empty catalog, not an error. This mirrors
// the discoverOmpModels missing-binary test.
func TestDiscoverZcodeModelsMissingConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := discoverZcodeModels(ctx, "zcode")
	if err != nil {
		t.Fatalf("discoverZcodeModels: unexpected error: %v", err)
	}
	// The test runner's home may or may not have a real config — assert only
	// that the call is safe and returns a non-nil slice. A non-empty result is
	// fine (the developer's own config is visible); a nil/empty result is also
	// fine.
	if models == nil {
		t.Fatalf("discoverZcodeModels returned nil slice")
	}
}
