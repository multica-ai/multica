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
		if config.Mode != mode || config.Instruction == "" || config.TimeoutMinutes <= 0 || config.MaxTurns <= 0 {
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
		EvalSkillIDs:   []string{"eval-1"},
	}
	profile := config.Profile()
	if profile.Mode != Plan || profile.Version != "3" || profile.Timeout != 25*time.Minute || profile.MaxTurns != 12 {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.Model != "gpt-5.4" || len(profile.AllowedTools) != 1 || len(profile.EvalSkillIDs) != 1 {
		t.Fatalf("profile lost configured fields: %+v", profile)
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
		{"zero timeout", func(c *Config) { c.TimeoutMinutes = 0 }},
		{"excessive timeout", func(c *Config) { c.TimeoutMinutes = 1441 }},
		{"zero max turns", func(c *Config) { c.MaxTurns = 0 }},
		{"excessive max turns", func(c *Config) { c.MaxTurns = 201 }},
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
