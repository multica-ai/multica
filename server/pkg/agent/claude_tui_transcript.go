//go:build !windows

package agent

import (
	"bufio"
	"encoding/json"
	"os"
)

// readTranscriptUsage scans the Claude Code transcript JSONL at path and
// returns aggregated token usage keyed by model name. Each `assistant` line's
// message.usage is added to the model's running total. Lines are deduped by
// message.id because claude appends the same assistant message multiple times
// (once per content block) and they share usage counters — naive summation
// would double-count.
//
// Errors are non-fatal: a missing or unreadable transcript returns an empty
// map, on the principle that usage telemetry must never block task completion.
func readTranscriptUsage(path string) map[string]TokenUsage {
	out := map[string]TokenUsage{}
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()

	seen := map[string]bool{}

	type usageRaw struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	}
	type messageRaw struct {
		ID    string    `json:"id"`
		Model string    `json:"model"`
		Usage *usageRaw `json:"usage"`
	}
	type rowRaw struct {
		Type    string      `json:"type"`
		Message *messageRaw `json:"message"`
	}

	// Claude's transcript can grow to MB for long sessions; bufio's default
	// 64KB line buffer is too small for occasional embedded tool outputs.
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var row rowRaw
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			continue
		}
		if row.Type != "assistant" || row.Message == nil || row.Message.Usage == nil {
			continue
		}
		if row.Message.ID == "" || seen[row.Message.ID] {
			continue
		}
		seen[row.Message.ID] = true

		model := row.Message.Model
		if model == "" {
			model = "unknown"
		}
		u := out[model]
		u.InputTokens += row.Message.Usage.InputTokens
		u.OutputTokens += row.Message.Usage.OutputTokens
		u.CacheReadTokens += row.Message.Usage.CacheReadInputTokens
		u.CacheWriteTokens += row.Message.Usage.CacheCreationInputTokens
		out[model] = u
	}
	return out
}
