package step

import (
	"encoding/json"
	"strings"
)

// Run extracts coding-team markers and structured artifacts from a task issue's
// comments. It does not call Multica or mutate state.
//
// Input:
//   - comments: []object, oldest to newest. Each comment may use content, body,
//     or text for the markdown body.
//
// Output machine_data:
//   - markers: latest marker index for each known pipeline marker.
//   - artifacts: latest parsed coding-team artifact keyed by artifact_type.
//   - artifact_indices: comment index where each artifact was found.
//   - current_round: booleans downstream roles can use for idempotency.
//   - errors: malformed artifact parse errors.
func Run(input map[string]any) map[string]any {
	comments := array(input["comments"])
	markers := map[string]any{
		"planning_blocked":        float64(-1),
		"implementation_plan":     float64(-1),
		"implementation_complete": float64(-1),
		"tests_written":           float64(-1),
		"review_pass":             float64(-1),
		"review_fail":             float64(-1),
	}
	artifacts := map[string]any{}
	artifactIndices := map[string]any{}
	errors := []any{}

	for i, raw := range comments {
		body := commentBody(raw)
		updateMarkers(markers, body, i)
		for _, rawArtifact := range extractArtifactJSON(body) {
			var artifact map[string]any
			if err := json.Unmarshal([]byte(rawArtifact), &artifact); err != nil {
				errors = append(errors, map[string]any{"comment_index": i, "error": err.Error()})
				continue
			}
			artifactType := str(artifact["artifact_type"])
			if artifactType == "" {
				errors = append(errors, map[string]any{"comment_index": i, "error": "artifact_type is required"})
				continue
			}
			artifacts[artifactType] = artifact
			artifactIndices[artifactType] = float64(i)
		}
	}

	lastImpl := intValue(markers["implementation_complete"])
	lastTests := intValue(markers["tests_written"])
	lastPass := intValue(markers["review_pass"])
	lastFail := intValue(markers["review_fail"])
	lastVerdict := max(lastPass, lastFail)
	currentRound := map[string]any{
		"implementation_needed": lastImpl < 0 || lastFail > lastImpl,
		"tests_needed":          lastTests < 0 || lastFail > lastTests,
		"review_needed":         lastVerdict <= lastImpl,
		"latest_verdict":        latestVerdict(lastPass, lastFail, lastImpl),
	}

	return map[string]any{
		"status":  "ok",
		"summary": "Extracted coding-team comments",
		"machine_data": map[string]any{
			"markers":          markers,
			"artifacts":        artifacts,
			"artifact_indices": artifactIndices,
			"current_round":    currentRound,
			"errors":           errors,
		},
	}
}

func updateMarkers(markers map[string]any, body string, idx int) {
	checks := []struct {
		key    string
		marker string
	}{
		{"planning_blocked", "## Planning Blocked: Clarification Needed"},
		{"implementation_plan", "## Implementation Plan"},
		{"implementation_complete", "## Implementation Complete"},
		{"tests_written", "## Tests Written"},
		{"review_pass", "## Review: PASS"},
		{"review_fail", "## Review: FAIL"},
	}
	for _, check := range checks {
		if strings.Contains(body, check.marker) {
			markers[check.key] = float64(idx)
		}
	}
}

func extractArtifactJSON(body string) []string {
	out := []string{}
	search := body
	for {
		start := strings.Index(search, "```")
		if start < 0 {
			return out
		}
		afterFence := search[start+3:]
		lineEnd := strings.Index(afterFence, "\n")
		if lineEnd < 0 {
			return out
		}
		info := strings.TrimSpace(afterFence[:lineEnd])
		rest := afterFence[lineEnd+1:]
		end := strings.Index(rest, "```")
		if end < 0 {
			return out
		}
		if isArtifactFence(info) {
			out = append(out, strings.TrimSpace(rest[:end]))
		}
		search = rest[end+3:]
	}
}

func isArtifactFence(info string) bool {
	fields := strings.Fields(strings.ToLower(info))
	hasJSON := false
	hasArtifact := false
	for _, field := range fields {
		if field == "json" {
			hasJSON = true
		}
		if field == "coding-team-artifact" {
			hasArtifact = true
		}
	}
	return hasJSON && hasArtifact
}

func latestVerdict(lastPass, lastFail, lastImpl int) string {
	if max(lastPass, lastFail) <= lastImpl {
		return "none"
	}
	if lastPass > lastFail {
		return "pass"
	}
	return "fail"
}

func commentBody(raw any) string {
	comment := object(raw)
	for _, key := range []string{"content", "body", "text"} {
		if s := str(comment[key]); s != "" {
			return s
		}
	}
	return ""
}

func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return []any{}
}

func object(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func intValue(v any) int {
	if n, ok := v.(float64); ok {
		return int(n)
	}
	return -1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
