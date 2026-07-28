package agentroute

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Runtime-config keys that describe durable identity, routing authority, or
// admission policy. A per-task execution override may not replace them.
var protectedRuntimeConfigKeys = map[string]struct{}{
	"adaptive_routing":            {},
	"provider":                    {},
	"provider_failover_protected": {},
	"provider_failover_target":    {},
	"runtime_name":                {},
}

// ValidateRuntimeConfigOverride verifies that a candidate's per-task
// runtime_config is an object containing execution knobs only.
func ValidateRuntimeConfigOverride(raw []byte) error {
	if !HasRuntimeConfigOverride(raw) {
		return nil
	}
	override, err := decodeRuntimeConfigObject(raw, false)
	if err != nil {
		return err
	}
	for key := range protectedRuntimeConfigKeys {
		if _, exists := override[key]; exists {
			return fmt.Errorf("runtime_config override cannot replace protected key %q", key)
		}
	}
	return nil
}

// HasRuntimeConfigOverride distinguishes an omitted/null optional JSON field
// from a real per-task object.
func HasRuntimeConfigOverride(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

// MergeRuntimeConfig applies a validated per-task execution override to the
// source agent's current runtime_config. Objects merge recursively so a
// provider-specific override such as gateway.endpoint does not erase a sibling
// secret such as gateway.token. Arrays and scalar leaves replace their source
// value. Durable identity and routing-policy keys are rejected.
func MergeRuntimeConfig(source, override []byte) ([]byte, error) {
	if err := ValidateRuntimeConfigOverride(override); err != nil {
		return nil, err
	}
	base, err := decodeRuntimeConfigObject(source, true)
	if err != nil {
		return nil, fmt.Errorf("decode source runtime_config: %w", err)
	}
	if !HasRuntimeConfigOverride(override) {
		return json.Marshal(base)
	}
	overlay, err := decodeRuntimeConfigObject(override, false)
	if err != nil {
		return nil, err
	}
	mergeRuntimeConfigObjects(base, overlay)
	return json.Marshal(base)
}

func decodeRuntimeConfigObject(raw []byte, emptyAllowed bool) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		if emptyAllowed {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("runtime_config override must be a JSON object")
	}
	var object map[string]any
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, fmt.Errorf("decode runtime_config override: %w", err)
	}
	if object == nil {
		if emptyAllowed && bytes.Equal(trimmed, []byte("null")) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("runtime_config override must be a JSON object")
	}
	return object, nil
}

func mergeRuntimeConfigObjects(base, overlay map[string]any) {
	for key, value := range overlay {
		overlayObject, overlayIsObject := value.(map[string]any)
		baseObject, baseIsObject := base[key].(map[string]any)
		if overlayIsObject && baseIsObject {
			mergeRuntimeConfigObjects(baseObject, overlayObject)
			continue
		}
		base[key] = value
	}
}
