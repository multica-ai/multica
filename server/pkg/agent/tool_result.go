package agent

import "encoding/json"

// normalizeToolResultOutput renders a provider tool_result payload to the
// stored output string. A JSON string is decoded exactly ONE level (real
// newlines/quotes, not escape sequences); an object/array/other stays as its
// raw JSON text. Shared so Claude/CodeBuddy produce the same readable output
// Qwen already does. Never a destructive string replace.
func normalizeToolResultOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}
