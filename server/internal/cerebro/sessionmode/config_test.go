package sessionmode

import (
	"testing"
	"time"
)

func TestDefaultConfigsCoverFixedModes(t *testing.T) {
	configs := DefaultConfigs()
	if len(configs) != 4 {
		t.Fatalf("default configs = %d, want 4", len(configs))
	}
	for _, mode := range []Mode{Plan, Build, Research, Review} {
		config, ok := configs[mode]
		if !ok {
			t.Fatalf("default config missing %q", mode)
		}
		if config.Mode != mode || config.Instruction == "" || config.TimeoutMinutes < 0 || config.MaxTurns < 0 {
			t.Fatalf("invalid default config for %q: %+v", mode, config)
		}
	}
}

func TestConfigProfileConvertsRuntimeLimits(t *testing.T) {
	config := Config{
		Mode:           Plan,
		Version:        "3",
		Instruction:    "Plan this safely.",
		Model:          "gpt-5.4",
		ThinkingLevel:  "medium",
		TimeoutMinutes: 25,
		MaxTurns:       12,
		AllowsWrite:    false,
		AllowedTools:   []string{"read_file"},
		EvalIDs:        []string{"7e767171-6a19-41d5-b3e6-edfdc97eedf7"},
	}
	profile := config.Profile()
	if profile.Mode != Plan || profile.Version != "3" || profile.Timeout != 25*time.Minute || profile.MaxTurns != 12 {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.Model != "gpt-5.4" || len(profile.AllowedTools) != 1 || len(profile.EvalIDs) != 1 {
		t.Fatalf("profile lost configured fields: %+v", profile)
	}
}

// FIR-4047: a Mode's evaluations are rows in the workspace eval catalog, so an
// ID that cannot name a row must never reach a published version.
func TestValidateConfigRejectsNonUUIDEvalIDs(t *testing.T) {
	config := DefaultConfigs()[Plan]
	config.EvalIDs = []string{"skill-1"}
	if err := ValidateConfig(config); err == nil {
		t.Fatal("expected a non-UUID eval id to be rejected")
	}
	config.EvalIDs = []string{"7e767171-6a19-41d5-b3e6-edfdc97eedf7"}
	if err := ValidateConfig(config); err != nil {
		t.Fatalf("a catalog eval id must be accepted: %v", err)
	}
}

// No default Mode ships with evaluations: which checks matter is a workspace
// decision, and a default that ran something would run it for every workspace.
func TestDefaultModesShipWithoutEvaluations(t *testing.T) {
	for mode, config := range DefaultConfigs() {
		if len(config.EvalIDs) != 0 {
			t.Fatalf("%q ships with evaluations: %+v", mode, config.EvalIDs)
		}
	}
}

func TestValidateConfigRejectsUnsafeOrIncompleteValues(t *testing.T) {
	valid := DefaultConfigs()[Build]
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing instruction", func(c *Config) { c.Instruction = "" }},
		{"invalid thinking", func(c *Config) { c.ThinkingLevel = "turbo" }},
		{"negative timeout", func(c *Config) { c.TimeoutMinutes = -1 }},
		{"negative max turns", func(c *Config) { c.MaxTurns = -1 }},
		{"invalid approval policy", func(c *Config) { c.ApprovalPolicy = "sometimes" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := valid
			tc.mutate(&config)
			if err := ValidateConfig(config); err == nil {
				t.Fatal("ValidateConfig returned nil")
			}
		})
	}
	if err := ValidateConfig(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidateConfigAllowsUnlimitedOrLargeRuntimeLimits(t *testing.T) {
	config := DefaultConfigs()[Build]
	config.TimeoutMinutes = 0
	config.MaxTurns = 0
	if err := ValidateConfig(config); err != nil {
		t.Fatalf("unlimited config rejected: %v", err)
	}

	config.TimeoutMinutes = 100_000
	config.MaxTurns = 100_000
	if err := ValidateConfig(config); err != nil {
		t.Fatalf("large config rejected: %v", err)
	}
}
