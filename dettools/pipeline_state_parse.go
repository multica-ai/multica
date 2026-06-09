package step

import (
	"encoding/json"
	"strings"
)

// Run parses a Multica issue description into the JSON block used by the
// coding-team pipeline.
//
// Input:
//   - description: string, the issue description.
//
// Output machine_data:
//   - state: object, the parsed pipeline state when the JSON has a stage.
//   - config: object, the parsed JSON block even when it is config-only.
//   - raw_json: string, the JSON segment that was parsed.
//   - body: string, description text after a leading JSON object, if any.
//   - is_pipeline_state: bool, true only when the parsed object has stage.
//   - has_json: bool, true when a fenced or leading JSON object was found.
//   - errors: []string, parse or shape problems.
func Run(input map[string]any) map[string]any {
	desc, _ := input["description"].(string)
	raw, body, ok := extractJSON(desc)
	data := map[string]any{
		"state":             map[string]any{},
		"config":            map[string]any{},
		"raw_json":          raw,
		"body":              body,
		"is_pipeline_state": false,
		"has_json":          ok,
		"errors":            []any{},
	}
	if !ok {
		return okResult("No JSON state or config block found", data)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		data["errors"] = []any{"invalid JSON: " + err.Error()}
		return map[string]any{
			"status":       "error",
			"error_code":   "INVALID_INPUT",
			"summary":      "Issue JSON block could not be parsed",
			"machine_data": data,
		}
	}

	data["config"] = parsed
	if _, hasStage := parsed["stage"]; hasStage {
		data["state"] = parsed
		data["is_pipeline_state"] = true
		return okResult("Parsed pipeline state", data)
	}
	return okResult("Parsed config-only JSON block", data)
}

func extractJSON(desc string) (string, string, bool) {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return "", "", false
	}

	if raw, ok := fencedJSON(desc); ok {
		return raw, "", true
	}
	if !strings.HasPrefix(desc, "{") {
		return "", desc, false
	}
	end := findJSONObjectEnd(desc)
	if end < 0 {
		return desc, "", true
	}
	raw := strings.TrimSpace(desc[:end+1])
	body := strings.TrimSpace(desc[end+1:])
	return raw, body, true
}

func fencedJSON(desc string) (string, bool) {
	start := strings.Index(desc, "```json")
	if start < 0 {
		return "", false
	}
	after := desc[start+len("```json"):]
	end := strings.Index(after, "```")
	if end < 0 {
		return strings.TrimSpace(after), true
	}
	return strings.TrimSpace(after[:end]), true
}

func findJSONObjectEnd(s string) int {
	depth := 0
	inString := false
	escaped := false
	for i, r := range s {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func okResult(summary string, data map[string]any) map[string]any {
	return map[string]any{
		"status":       "ok",
		"summary":      summary,
		"machine_data": data,
	}
}
