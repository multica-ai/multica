package agent

import (
	"encoding/json"
	"testing"
)

func TestACPUsageAccumulatorMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		snapshots []string
		want      TokenUsage
	}{
		{
			name: "prompt only",
			snapshots: []string{
				`{"inputTokens":100,"outputTokens":30,"cacheReadTokens":20,"cacheWriteTokens":5,"costUsdTicks":900}`,
			},
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20, CacheWriteTokens: 5, CostUSDTicks: 900},
		},
		{
			name: "stream only",
			snapshots: []string{
				`{"inputTokens":80,"outputTokens":10,"cacheReadTokens":15,"cacheWriteTokens":3,"costUsdTicks":700}`,
				`{"inputTokens":100,"outputTokens":30,"cacheReadTokens":20,"cacheWriteTokens":5,"costUsdTicks":900}`,
			},
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20, CacheWriteTokens: 5, CostUSDTicks: 900},
		},
		{
			name: "duplicate stream and prompt snapshots",
			snapshots: []string{
				`{"inputTokens":120,"outputTokens":30,"totalTokens":150,"cacheReadTokens":20,"cacheWriteTokens":5,"costUsdTicks":900}`,
				`{"inputTokens":120,"outputTokens":30,"totalTokens":150,"cacheReadTokens":20,"cacheWriteTokens":5,"costUsdTicks":900}`,
			},
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20, CacheWriteTokens: 5, CostUSDTicks: 900},
		},
		{
			name: "partial terminal preserves stream-only bucket",
			snapshots: []string{
				`{"cacheWriteTokens":500,"costUsdTicks":700}`,
				`{"inputTokens":120,"outputTokens":30,"totalTokens":150,"cacheReadTokens":20,"costUsdTicks":900}`,
			},
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20, CacheWriteTokens: 500, CostUSDTicks: 900},
		},
		{
			name: "unknown terminal bucket shape cannot erase known usage",
			snapshots: []string{
				`{"inputTokens":100,"outputTokens":30,"cacheReadTokens":20,"cacheWriteTokens":5,"costUsdTicks":900}`,
				`{"prompt_tokens":200,"completion_tokens":50,"totalTokens":250,"costUsdTicks":1000}`,
			},
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20, CacheWriteTokens: 5, CostUSDTicks: 1000},
		},
		{
			name: "cumulative stream outranks smaller normalized prompt delta",
			snapshots: []string{
				`{"inputTokens":300,"outputTokens":120,"cacheReadTokens":80,"costUsdTicks":900}`,
				`{"inputTokens":120,"outputTokens":30,"totalTokens":150,"cacheReadTokens":20,"cacheWriteTokens":7,"costUsdTicks":400}`,
			},
			want: TokenUsage{InputTokens: 300, OutputTokens: 120, CacheReadTokens: 80, CacheWriteTokens: 7, CostUSDTicks: 900},
		},
		{
			name: "late cumulative stream outranks earlier normalized prompt delta",
			snapshots: []string{
				`{"inputTokens":120,"outputTokens":30,"totalTokens":150,"cacheReadTokens":20,"cacheWriteTokens":7,"costUsdTicks":400}`,
				`{"inputTokens":300,"outputTokens":120,"cacheReadTokens":80,"costUsdTicks":900}`,
			},
			want: TokenUsage{InputTokens: 300, OutputTokens: 120, CacheReadTokens: 80, CacheWriteTokens: 7, CostUSDTicks: 900},
		},
		{
			name: "normalized cumulative prompt replaces overlapping stream input",
			snapshots: []string{
				`{"inputTokens":120,"outputTokens":30,"cacheReadTokens":20,"costUsdTicks":800}`,
				`{"inputTokens":120,"outputTokens":30,"totalTokens":150,"cacheReadTokens":20,"costUsdTicks":900}`,
				`{"inputTokens":120,"outputTokens":30,"cacheReadTokens":20,"costUsdTicks":1000}`,
			},
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20, CostUSDTicks: 1000},
		},
		{
			name: "unknown total cannot block later normalized snapshot",
			snapshots: []string{
				`{"inputTokens":120,"outputTokens":30,"cacheReadTokens":20,"costUsdTicks":800}`,
				`{"prompt_tokens":200,"completion_tokens":50,"totalTokens":250,"costUsdTicks":1000}`,
				`{"inputTokens":120,"outputTokens":30,"totalTokens":150,"cacheReadTokens":20,"cacheWriteTokens":5,"costUsdTicks":900}`,
			},
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20, CacheWriteTokens: 5, CostUSDTicks: 1000},
		},
		{
			name: "provider cost uses largest cumulative report",
			snapshots: []string{
				`{"inputTokens":100,"costUsdTicks":900}`,
				`{"outputTokens":30,"costUsdTicks":500}`,
				`{"cacheReadTokens":20,"costUsdTicks":1200}`,
			},
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20, CostUSDTicks: 1200},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var accumulator acpUsageAccumulator
			for _, raw := range test.snapshots {
				accumulator.merge(parseACPTokenUsageSnapshot(json.RawMessage(raw)))
			}
			if got := accumulator.TokenUsage; got != test.want {
				t.Fatalf("usage = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestACPPromptUsageFillsPartialTopLevelFromMeta(t *testing.T) {
	t.Parallel()

	var got hermesPromptResult
	client := &hermesClient{
		pending: make(map[int]*pendingRPC),
		onPromptDone: func(result hermesPromptResult) {
			got = result
		},
	}
	client.extractPromptResult(json.RawMessage(`{
		"stopReason":"end_turn",
		"usage":{"inputTokens":100,"outputTokens":30},
		"_meta":{
			"inputTokens":999,
			"usage":{"inputTokens":999,"cacheReadTokens":20,"cacheWriteTokens":5,"costUsdTicks":900}
		}
	}`))

	want := TokenUsage{
		InputTokens:      100,
		OutputTokens:     30,
		CacheReadTokens:  20,
		CacheWriteTokens: 5,
		CostUSDTicks:     900,
	}
	if got.usage.TokenUsage != want {
		t.Fatalf("usage = %+v, want %+v", got.usage.TokenUsage, want)
	}
}

func TestACPPromptUsageNormalizesComplementaryRepresentations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want TokenUsage
	}{
		{
			name: "top-level tokens and nested meta cache",
			raw: `{
				"stopReason":"end_turn",
				"usage":{"inputTokens":120,"outputTokens":30},
				"_meta":{"usage":{"cacheReadTokens":20,"totalTokens":150}}
			}`,
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20},
		},
		{
			name: "nested meta tokens and flat meta cache",
			raw: `{
				"stopReason":"end_turn",
				"_meta":{
					"usage":{"inputTokens":120,"outputTokens":30},
					"cacheReadTokens":20,
					"totalTokens":150
				}
			}`,
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20},
		},
		{
			name: "already normalized input is not subtracted twice",
			raw: `{
				"stopReason":"end_turn",
				"usage":{"inputTokens":120,"outputTokens":30,"cacheReadTokens":20,"totalTokens":150},
				"_meta":{"usage":{"cacheWriteTokens":5,"costUsdTicks":900}}
			}`,
			want: TokenUsage{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 20, CacheWriteTokens: 5, CostUSDTicks: 900},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var got hermesPromptResult
			client := &hermesClient{
				pending: make(map[int]*pendingRPC),
				onPromptDone: func(result hermesPromptResult) {
					got = result
				},
			}
			client.extractPromptResult(json.RawMessage(test.raw))

			if got.usage.TokenUsage != test.want {
				t.Fatalf("usage = %+v, want %+v", got.usage.TokenUsage, test.want)
			}
		})
	}
}

func TestACPUsagePresentWithProviderCostOnly(t *testing.T) {
	t.Parallel()

	if !acpUsagePresent(TokenUsage{CostUSDTicks: 42}) {
		t.Fatal("provider-reported cost must produce a Result.Usage entry even without token buckets")
	}
}
