package handler

import "testing"

func TestLatestContextUsageInBatch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		messages  []TaskMessageRequest
		wantFound bool
		wantUsed  int64
		wantLimit int64
	}{
		{
			name: "newest observation wins",
			messages: []TaskMessageRequest{
				{Type: "context", Input: map[string]any{"used_tokens": float64(10_000), "limit_tokens": float64(200_000)}},
				{Type: "tool_use", Tool: "Bash"},
				{Type: "context", Input: map[string]any{"used_tokens": float64(90_000), "limit_tokens": float64(200_000)}},
			},
			wantFound: true, wantUsed: 90_000, wantLimit: 200_000,
		},
		{
			name: "occupancy without a window keeps the window unset",
			messages: []TaskMessageRequest{
				{Type: "context", Input: map[string]any{"used_tokens": float64(12_300)}},
			},
			wantFound: true, wantUsed: 12_300, wantLimit: 0,
		},
		{
			// A compaction carries no occupancy; overwriting the stored figure
			// with nothing would blank the chip for the rest of the run.
			name: "a compaction alone reports nothing",
			messages: []TaskMessageRequest{
				{Type: "context", Input: map[string]any{"phase": "compacted", "pre_tokens": float64(180_000)}},
			},
		},
		{
			name:     "no context events",
			messages: []TaskMessageRequest{{Type: "text", Content: "hi"}},
		},
		{
			// `input` is the same field a tool's arguments ride, so anything at
			// all can appear here.
			name: "unreadable payloads are ignored",
			messages: []TaskMessageRequest{
				{Type: "context", Input: map[string]any{"used_tokens": "many"}},
				{Type: "context", Input: nil},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			usage, found := latestContextUsageInBatch(tc.messages)
			if found != tc.wantFound {
				t.Fatalf("found = %t, want %t", found, tc.wantFound)
			}
			if !found {
				return
			}
			if usage.usedTokens != tc.wantUsed || usage.limitTokens != tc.wantLimit {
				t.Fatalf("usage = %+v, want %d used / %d limit", usage, tc.wantUsed, tc.wantLimit)
			}
		})
	}
}
