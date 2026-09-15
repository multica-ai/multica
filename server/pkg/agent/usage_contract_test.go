package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Provider fields do not share an input-token convention, but their normalized
// usage must share the TokenUsage contract. Reasoning is already in output.
// https://developers.openai.com/api/docs/guides/reasoning#managing-the-context-window
// https://platform.claude.com/docs/en/build-with-claude/prompt-caching#tracking-cache-performance
func TestProviderTokenUsageContract(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     int64 // Codex's inclusive input count.
		reasoning int64 // A subset of output, not an additional charge.
		want      TokenUsage
	}{
		{"mixed_cache_and_reasoning", 100, 5, TokenUsage{InputTokens: 20, OutputTokens: 10, CacheReadTokens: 30, CacheWriteTokens: 50}},
		{"uncached_without_reasoning", 100, 0, TokenUsage{InputTokens: 100, OutputTokens: 10}},
		{"all_output_is_reasoning", 100, 10, TokenUsage{OutputTokens: 10, CacheReadTokens: 100}},
		{"cache_only", 100, 0, TokenUsage{CacheReadTokens: 40, CacheWriteTokens: 60}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, source := range []string{"codex_event", "codex_rollout", "claude_model_totals", "claude_main_total"} {
				t.Run(source, func(t *testing.T) {
					var got TokenUsage
					switch source {
					case "codex_event":
						c := &codexClient{}
						c.setActiveTurnID("turn-current")
						usage := map[string]any{
							"inputTokens": float64(tc.input), "cachedInputTokens": float64(tc.want.CacheReadTokens),
							"cacheWriteInputTokens": float64(tc.want.CacheWriteTokens), "outputTokens": float64(tc.want.OutputTokens),
							"reasoningOutputTokens": float64(tc.reasoning), "totalTokens": float64(tc.input + tc.want.OutputTokens),
						}
						c.updateThreadTokenUsage(codexThreadTokenUsageParams("thread-1", "turn-current", usage, usage))
						got = c.usage
					case "codex_rollout":
						data := mustMarshal(t, map[string]any{
							"type": "event_msg", "payload": map[string]any{
								"type": "token_count", "info": map[string]any{
									"total_token_usage": map[string]any{
										"input_tokens": tc.input, "cached_input_tokens": tc.want.CacheReadTokens,
										"cache_write_input_tokens": tc.want.CacheWriteTokens, "output_tokens": tc.want.OutputTokens,
										"reasoning_output_tokens": tc.reasoning, "total_tokens": tc.input + tc.want.OutputTokens,
									},
								},
							},
						})
						path := filepath.Join(t.TempDir(), "rollout.jsonl")
						if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
							t.Fatal(err)
						}
						usage := parseCodexSessionFile(path)
						if usage == nil {
							t.Fatal("missing rollout usage")
						}
						got = usage.usage
					default:
						// Claude's input already excludes both cache buckets. Its
						// final output total already includes thinking.
						msg := map[string]any{"type": "result", "model": "claude-sonnet-4-6"}
						if source == "claude_model_totals" {
							msg["modelUsage"] = map[string]any{"claude-sonnet-4-6": map[string]any{
								"inputTokens": tc.want.InputTokens, "outputTokens": tc.want.OutputTokens,
								"cacheReadInputTokens": tc.want.CacheReadTokens, "cacheCreationInputTokens": tc.want.CacheWriteTokens,
							}}
						} else {
							msg["usage"] = map[string]any{
								"input_tokens": tc.want.InputTokens, "output_tokens": tc.want.OutputTokens,
								"cache_read_input_tokens": tc.want.CacheReadTokens, "cache_creation_input_tokens": tc.want.CacheWriteTokens,
							}
						}
						var decoded claudeSDKMessage
						if err := json.Unmarshal(mustMarshal(t, msg), &decoded); err != nil {
							t.Fatal(err)
						}
						got = claudeResultUsage(decoded, "claude-sonnet-4-6")["claude-sonnet-4-6"]
					}
					if got != tc.want {
						t.Fatalf("normalized usage = %+v, want %+v (reasoning is included in output)", got, tc.want)
					}
				})
			}
		})
	}
}
