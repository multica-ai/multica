package cerebra

import (
	"context"
	"strings"
	"testing"
)

func TestHeuristicClassifier_KeywordScoring(t *testing.T) {
	ctx := context.Background()
	classifier := HeuristicClassifier{}

	tests := []struct {
		name        string
		prompt      string
		meta        TaskMeta
		wantTier    Tier
		wantRuleSub string
	}{
		{
			name:        "simple prompt",
			prompt:      "Hello world, can you answer a simple question?",
			meta:        TaskMeta{},
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "debug keyword routes to standard",
			prompt:      "Please debug why the user login is failing",
			meta:        TaskMeta{},
			wantTier:    TierStandard,
			wantRuleSub: "keyword:debug",
		},
		{
			name:        "architect keyword routes to heavy",
			prompt:      "Design and architect the new distributed cache system",
			meta:        TaskMeta{},
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:architect",
		},
		{
			name:        "highest-tier keyword escalation (debug + architect)",
			prompt:      "Please debug the issue and architect a permanent refactor",
			meta:        TaskMeta{},
			wantTier:    TierHeavy, // Heavy wins over standard
			wantRuleSub: "keyword:",
		},
		{
			name:        "MCP tool floor overrides simple",
			prompt:      "Simple lookup",
			meta:        TaskMeta{WillUseMCPTools: true},
			wantTier:    TierStandard,
			wantRuleSub: "mcp_floor",
		},
		{
			name:        "substrings like prefix do not falsely trigger fix keyword",
			prompt:      "What prefix should I use for these environment variables?",
			meta:        TaskMeta{},
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "exact word fix triggers standard tier",
			prompt:      "Can you fix this typo in the readme?",
			meta:        TaskMeta{},
			wantTier:    TierStandard,
			wantRuleSub: "keyword:fix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTier, gotRule, err := classifier.Score(ctx, tt.prompt, tt.meta)
			if err != nil {
				t.Fatalf("Score() unexpected error: %v", err)
			}
			if gotTier != tt.wantTier {
				t.Errorf("Score() gotTier = %v, want %v", gotTier, tt.wantTier)
			}
			if tt.wantRuleSub != "" && !strings.Contains(gotRule, tt.wantRuleSub) {
				t.Errorf("Score() gotRule = %v, want substring %v", gotRule, tt.wantRuleSub)
			}
		})
	}
}
