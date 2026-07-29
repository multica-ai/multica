package agentoffice

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeSettingsOfReadsVersionedRunSettings(t *testing.T) {
	snap := ContextSnapshot{RuntimeConfig: json.RawMessage(`{
		"speed_mode":"fast",
		"max_turns":12,
		"timeout_minutes":25
	}`)}
	got := RuntimeSettingsOf(snap)
	if got.SpeedMode != "fast" || got.MaxTurns != 12 || got.TimeoutMinutes != 25 {
		t.Fatalf("settings = %+v", got)
	}
}

func TestRuntimeSettingWritersPreserveUnrelatedKeysAndRemoveDefaults(t *testing.T) {
	snap := ContextSnapshot{RuntimeConfig: json.RawMessage(`{
		"system_prompt_mode":"replace",
		"speed_mode":"fast",
		"max_turns":10,
		"timeout_minutes":30
	}`)}
	var err error
	if snap, err = WithSpeedMode(snap, "standard"); err != nil {
		t.Fatal(err)
	}
	if snap, err = WithMaxTurns(snap, 0); err != nil {
		t.Fatal(err)
	}
	if snap, err = WithTimeoutMinutes(snap, 45); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(snap.RuntimeConfig, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["system_prompt_mode"] != "replace" || cfg["timeout_minutes"] != float64(45) {
		t.Fatalf("settings were not preserved and updated: %v", cfg)
	}
	if _, ok := cfg["speed_mode"]; ok {
		t.Fatalf("standard speed must inherit by removing the key: %v", cfg)
	}
	if _, ok := cfg["max_turns"]; ok {
		t.Fatalf("zero max_turns must inherit by removing the key: %v", cfg)
	}
}

func TestValidateSnapshotRuntimeSettingsRejectsInvalidStoredValues(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown speed":        `{"speed_mode":"turbo"}`,
		"non-string speed":     `{"speed_mode":7}`,
		"negative max turns":   `{"max_turns":-1}`,
		"fractional max turns": `{"max_turns":1.5}`,
		"negative timeout":     `{"timeout_minutes":-2}`,
	} {
		err := ValidateSnapshotRuntimeSettings("claude", ContextSnapshot{RuntimeConfig: json.RawMessage(raw)})
		if err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestValidateSnapshotRuntimeSettingsUsesProviderSupport(t *testing.T) {
	fast := ContextSnapshot{RuntimeConfig: json.RawMessage(`{"speed_mode":"fast"}`)}
	if err := ValidateSnapshotRuntimeSettings("claude", fast); err != nil {
		t.Fatalf("Claude fast must be allowed: %v", err)
	}
	if err := ValidateSnapshotRuntimeSettings("kiro", fast); err == nil {
		t.Fatal("Kiro fast must be rejected because the engine drops it")
	}
	if err := ValidateSnapshotRuntimeSettings("future-provider", fast); err != nil {
		t.Fatalf("unknown provider must stay unknown, not unsupported: %v", err)
	}

	if err := ValidateSnapshotRuntimeSettings("claude", ContextSnapshot{ThinkingLevel: "turbo"}); err == nil {
		t.Fatal("unknown Claude thinking level must be rejected")
	}
	if err := ValidateSnapshotRuntimeSettings("claude", ContextSnapshot{ThinkingLevel: "high"}); err != nil {
		t.Fatalf("known Claude thinking level must be accepted: %v", err)
	}
}

func TestRenderSnapshotLabelsRuntimeSettings(t *testing.T) {
	out := RenderSnapshot(ContextSnapshot{RuntimeConfig: json.RawMessage(`{
		"speed_mode":"fast",
		"max_turns":14,
		"timeout_minutes":35
	}`)})
	for _, line := range []string{
		"speed_mode: fast",
		"max_turns: 14",
		"timeout_minutes: 35",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("render missing %q:\n%s", line, out)
		}
	}
}
