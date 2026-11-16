package util

import "encoding/json"

// JSONObjectOrEmpty decodes a JSONB column whose API contract promises an
// object is always present (issue metadata, issue custom properties) into a
// Go map. Empty, null, or unparseable blobs degrade to an empty map rather
// than nil: nil marshals to `null`, and "always present" is precisely what
// lets clients index the field without a nil guard.
func JSONObjectOrEmpty(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
