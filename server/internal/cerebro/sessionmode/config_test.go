package sessionmode

import (
	"encoding/json"
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
		ExtraSkillIDs:  []string{"skill-1"},
	}
	profile := config.Profile()
	if profile.Mode != Plan || profile.Version != "3" || profile.Timeout != 25*time.Minute || profile.MaxTurns != 12 {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.Model != "gpt-5.4" || len(profile.AllowedTools) != 1 || len(profile.ExtraSkillIDs) != 1 {
		t.Fatalf("profile lost configured fields: %+v", profile)
	}
}

// FIR-4047: Plan Mode used to render "Writes are disabled" underneath its own
// instruction to save a plan, so it could not produce its one deliverable.
func TestPlanAndResearchMayWritePlansWithoutCodeWrites(t *testing.T) {
	configs := DefaultConfigs()
	for _, mode := range []Mode{Plan, Research} {
		config := configs[mode]
		if config.AllowsWrite {
			t.Fatalf("%q unexpectedly allows code writes", mode)
		}
		if !config.CanWritePlans() {
			t.Fatalf("%q cannot save a plan or note", mode)
		}
	}
	if !configs[Build].AllowsWrite || !configs[Build].CanWritePlans() {
		t.Fatalf("build lost a write scope: %+v", configs[Build])
	}
	if configs[Review].CanWritePlans() {
		t.Fatalf("review unexpectedly writes documents: %+v", configs[Review])
	}
}

// A published snapshot predating the plan-write scope carries allows_write only.
// Writing code implies writing a plan, so that snapshot must keep both.
func TestCanWritePlansFollowsCodeWriteScope(t *testing.T) {
	if !(Config{AllowsWrite: true}).CanWritePlans() {
		t.Fatal("a code-writing Mode must be able to save a plan")
	}
	if (Config{}).CanWritePlans() {
		t.Fatal("a Mode with no write scope must not save a plan")
	}
}

// Published Mode versions are immutable history, so a snapshot written before
// the field was renamed must still resolve its extra skills.
func TestConfigUnmarshalAcceptsLegacyEvalSkillIDsKey(t *testing.T) {
	var config Config
	if err := json.Unmarshal([]byte(`{"mode":"plan","eval_skill_ids":["skill-1"]}`), &config); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(config.ExtraSkillIDs) != 1 || config.ExtraSkillIDs[0] != "skill-1" {
		t.Fatalf("legacy key dropped: %+v", config)
	}

	var current Config
	if err := json.Unmarshal([]byte(`{"mode":"plan","extra_skill_ids":["skill-2"]}`), &current); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(current.ExtraSkillIDs) != 1 || current.ExtraSkillIDs[0] != "skill-2" {
		t.Fatalf("current key dropped: %+v", current)
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
