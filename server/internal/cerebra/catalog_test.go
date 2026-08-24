package cerebra

import (
	"testing"
)

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

	if tierMap[TierSimple] == "" {
		t.Errorf("expected Simple tier to be populated, got empty")
	}
	if tierMap[TierStandard] == "" {
		t.Errorf("expected Standard tier to be populated, got empty")
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
