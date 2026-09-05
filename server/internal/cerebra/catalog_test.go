package cerebra

import (
	"testing"
)

func TestClassifyModelTier(t *testing.T) {
	tests := []struct {
		modelID  string
		wantTier Tier
	}{
		// OpenCode models
		{"opencode/mimo-v2.5-free", TierSimple},
		{"opencode/hy3-free", TierSimple},
		{"opencode/muse-spark-1.2-contributor-free", TierSimple},
		{"opencode/x-preview-f-free", TierSimple},
		{"opencode/nemotron-3.5-lightning-free", TierStandard},
		{"opencode/nemotron-3-ultra-free", TierHeavy},
		{"opencode/big-pickle", TierHeavy},

		// Claude models
		{"claude-3-5-haiku", TierSimple},
		{"claude-3-5-sonnet", TierStandard},
		{"claude-3-opus", TierHeavy},
		{"anthropic/claude-3-5-haiku-20241022", TierSimple},
		{"anthropic/claude-3-5-sonnet-20241022", TierStandard},

		// OpenAI / Codex models
		{"gpt-4o-mini", TierSimple},
		{"gpt-4o", TierStandard},
		{"o1-mini", TierSimple},
		{"o1", TierHeavy},
		{"o1-preview", TierHeavy},
		{"o3-mini", TierSimple},
		{"deepseek-r1", TierHeavy},

		// Gemini / other models
		{"gemini-1.5-flash", TierSimple},
		{"gemini-1.5-pro", TierHeavy},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := ClassifyModelTier(tt.modelID)
			if got != tt.wantTier {
				t.Errorf("ClassifyModelTier(%q) = %v, want %v", tt.modelID, got, tt.wantTier)
			}
		})
	}
}

func TestBuildTierMapFromCatalog(t *testing.T) {
	// Test 1: OpenCode runtime discovered models
	openCodeModels := []string{
		"opencode/big-pickle",
		"opencode/hy3-free",
		"opencode/mimo-v2.5-free",
		"opencode/muse-spark-1.2-contributor-free",
		"opencode/nemotron-3-ultra-free",
		"opencode/nemotron-3.5-lightning-free",
		"opencode/x-preview-f-free",
	}

	tierMap := BuildTierMapFromCatalog(openCodeModels)

	if tierMap[TierSimple] != "opencode/mimo-v2.5-free" && tierMap[TierSimple] != "opencode/hy3-free" && tierMap[TierSimple] != "opencode/muse-spark-1.2-contributor-free" {
		t.Errorf("expected Simple tier to pick a lightweight model, got %s", tierMap[TierSimple])
	}
	if tierMap[TierStandard] != "opencode/nemotron-3.5-lightning-free" {
		t.Errorf("expected Standard tier to be lightning, got %s", tierMap[TierStandard])
	}
	if tierMap[TierHeavy] != "opencode/nemotron-3-ultra-free" && tierMap[TierHeavy] != "opencode/big-pickle" {
		t.Errorf("expected Heavy tier to pick ultra or big-pickle, got %s", tierMap[TierHeavy])
	}

	// Test 2: Claude models
	claudeModels := []string{
		"claude-3-5-haiku",
		"claude-3-5-sonnet",
		"claude-3-opus",
	}
	claudeMap := BuildTierMapFromCatalog(claudeModels)
	if claudeMap[TierSimple] != "claude-3-5-haiku" {
		t.Errorf("expected Simple to be haiku, got %s", claudeMap[TierSimple])
	}
	if claudeMap[TierStandard] != "claude-3-5-sonnet" {
		t.Errorf("expected Standard to be sonnet, got %s", claudeMap[TierStandard])
	}
	if claudeMap[TierHeavy] != "claude-3-opus" {
		t.Errorf("expected Heavy to be opus, got %s", claudeMap[TierHeavy])
	}
}
