package agentoffice

import (
	"encoding/json"
	"fmt"

	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
)

// Versioned per-agent run settings (FIR-4000).
//
// These values live in runtime_config alongside system_prompt_mode and the
// brief-layer modes. That keeps one governed source for the browser, API, MCP,
// approval history and daemon claim path without introducing parallel columns.
const (
	SpeedModeKey      = "speed_mode"
	MaxTurnsKey       = "max_turns"
	TimeoutMinutesKey = "timeout_minutes"
)

const (
	SpeedModeDefault  = ""
	SpeedModeStandard = "standard"
	SpeedModeFast     = "fast"
)

// RuntimeSettings is the normalised subset of runtime_config that controls a
// run. Zero values mean "inherit the selected session Mode profile".
type RuntimeSettings struct {
	SpeedMode      string
	MaxTurns       int
	TimeoutMinutes int
}

func RuntimeSettingsOf(snap ContextSnapshot) RuntimeSettings {
	settings := RuntimeSettings{}
	if len(snap.RuntimeConfig) == 0 {
		return settings
	}
	var cfg map[string]json.RawMessage
	if json.Unmarshal(snap.RuntimeConfig, &cfg) != nil {
		return settings
	}
	if raw, ok := cfg[SpeedModeKey]; ok {
		var value string
		if json.Unmarshal(raw, &value) == nil && value == SpeedModeFast {
			settings.SpeedMode = value
		}
	}
	if raw, ok := cfg[MaxTurnsKey]; ok {
		var value int
		if json.Unmarshal(raw, &value) == nil && value > 0 {
			settings.MaxTurns = value
		}
	}
	if raw, ok := cfg[TimeoutMinutesKey]; ok {
		var value int
		if json.Unmarshal(raw, &value) == nil && value > 0 {
			settings.TimeoutMinutes = value
		}
	}
	return settings
}

// RuntimeConfigRest returns the free-form runtime_config keys that do not have
// their own labelled Agent Office field. Review still shows the complete
// document without duplicating the governed controls inside an opaque blob.
func RuntimeConfigRest(snap ContextSnapshot) json.RawMessage {
	if len(snap.RuntimeConfig) == 0 {
		return json.RawMessage(`{}`)
	}
	var cfg map[string]json.RawMessage
	if json.Unmarshal(snap.RuntimeConfig, &cfg) != nil {
		return snap.RuntimeConfig
	}
	for _, key := range []string{
		SystemPromptModeKey,
		WorkspaceBriefModeKey,
		ToolsBriefModeKey,
		SpeedModeKey,
		MaxTurnsKey,
		TimeoutMinutesKey,
	} {
		delete(cfg, key)
	}
	blob, err := json.Marshal(cfg)
	if err != nil {
		return snap.RuntimeConfig
	}
	return blob
}

func WithSpeedMode(snap ContextSnapshot, value string) (ContextSnapshot, error) {
	switch value {
	case SpeedModeDefault, SpeedModeStandard:
		return withRuntimeSetting(snap, SpeedModeKey, nil)
	case SpeedModeFast:
		return withRuntimeSetting(snap, SpeedModeKey, value)
	default:
		return snap, fmt.Errorf("unknown speed_mode %q: want fast, standard, or empty for the runtime default", value)
	}
}

func WithMaxTurns(snap ContextSnapshot, value int) (ContextSnapshot, error) {
	if value < 0 {
		return snap, fmt.Errorf("max_turns must be zero or positive")
	}
	if value == 0 {
		return withRuntimeSetting(snap, MaxTurnsKey, nil)
	}
	return withRuntimeSetting(snap, MaxTurnsKey, value)
}

func WithTimeoutMinutes(snap ContextSnapshot, value int) (ContextSnapshot, error) {
	if value < 0 {
		return snap, fmt.Errorf("timeout_minutes must be zero or positive")
	}
	if value == 0 {
		return withRuntimeSetting(snap, TimeoutMinutesKey, nil)
	}
	return withRuntimeSetting(snap, TimeoutMinutesKey, value)
}

func withRuntimeSetting(snap ContextSnapshot, key string, value any) (ContextSnapshot, error) {
	cfg := map[string]json.RawMessage{}
	if len(snap.RuntimeConfig) > 0 {
		_ = json.Unmarshal(snap.RuntimeConfig, &cfg)
	}
	if value == nil {
		delete(cfg, key)
	} else {
		encoded, err := json.Marshal(value)
		if err != nil {
			return snap, fmt.Errorf("encode %s: %w", key, err)
		}
		cfg[key] = encoded
	}
	blob, err := json.Marshal(cfg)
	if err != nil {
		return snap, fmt.Errorf("encode runtime_config: %w", err)
	}
	snap.RuntimeConfig = blob
	return snap, nil
}

// ValidateSnapshotRuntimeSettings is the storage chokepoint for typed fields,
// full proposed_snapshot payloads and raw runtime_config replacements.
func ValidateSnapshotRuntimeSettings(provider string, snap ContextSnapshot) error {
	if len(snap.RuntimeConfig) == 0 {
		return validateThinkingLevelForProvider(provider, snap.ThinkingLevel)
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(snap.RuntimeConfig, &cfg); err != nil {
		// runtime_config remains a shared free-form document. Existing malformed
		// blobs are tolerated exactly as the other settings readers tolerate them.
		return validateThinkingLevelForProvider(provider, snap.ThinkingLevel)
	}

	if raw, ok := cfg[SpeedModeKey]; ok {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("speed_mode must be a string")
		}
		if value != SpeedModeFast && value != SpeedModeStandard && value != SpeedModeDefault {
			return fmt.Errorf("unknown speed_mode %q: want fast, standard, or empty for the runtime default", value)
		}
		if value == SpeedModeFast {
			if handling, known := agentpkg.ExecOptionsHandling(provider, agentpkg.FieldSpeedMode); known && !handling.Effective() {
				return fmt.Errorf("runtime %q does not support speed_mode %q", provider, value)
			}
		}
	}
	for _, field := range []struct {
		key string
		raw json.RawMessage
	}{
		{MaxTurnsKey, cfg[MaxTurnsKey]},
		{TimeoutMinutesKey, cfg[TimeoutMinutesKey]},
	} {
		if len(field.raw) == 0 {
			continue
		}
		var value int
		if err := json.Unmarshal(field.raw, &value); err != nil || value < 0 {
			return fmt.Errorf("%s must be a zero or positive integer", field.key)
		}
	}
	return validateThinkingLevelForProvider(provider, snap.ThinkingLevel)
}

func validateThinkingLevelForProvider(provider, value string) error {
	if value == "" {
		return nil
	}
	if _, known := agentpkg.ExecOptionsSupportFor(provider); !known {
		return nil
	}
	if !agentpkg.IsKnownThinkingValue(provider, value) {
		return fmt.Errorf("runtime %q does not support thinking_level %q", provider, value)
	}
	return nil
}
