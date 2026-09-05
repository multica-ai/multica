package cerebra

import (
	"context"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// tierRank
// ─────────────────────────────────────────────────────────────────────────────

func TestTierRank(t *testing.T) {
	tests := []struct {
		tier Tier
		want int
	}{
		{TierSimple, 0},
		{TierStandard, 1},
		{TierHeavy, 2},
		// Unknown / empty tier falls into the default case → 0
		{Tier("unknown"), 0},
		{Tier(""), 0},
	}
	for _, tt := range tests {
		got := tierRank(tt.tier)
		if got != tt.want {
			t.Errorf("tierRank(%q) = %d, want %d", tt.tier, got, tt.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tokenize
// ─────────────────────────────────────────────────────────────────────────────

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int // expected word count
	}{
		{"empty string", "", 0},
		{"single word", "hello", 1},
		{"two words", "hello world", 2},
		{"punctuation separators", "hello, world!", 2},
		{"numbers count as tokens", "abc123 def", 2},
		{"only punctuation", "!!!", 0},
		{"mixed unicode letters", "café naïve", 2},
		{"newlines and tabs as delimiters", "a\tb\nc", 3},
		{"multiple spaces collapsed", "a   b", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := len(tokenize(tt.input))
			if got != tt.want {
				t.Errorf("tokenize(%q) wordCount = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// containsWord
// ─────────────────────────────────────────────────────────────────────────────

func TestContainsWord(t *testing.T) {
	tests := []struct {
		name string
		text string
		word string
		want bool
	}{
		// Basic positive matches
		{"exact match", "fix the bug", "fix", true},
		{"word at start", "debug the code", "debug", true},
		{"word at end", "you should debug", "debug", true},
		{"only word", "debug", "debug", true},

		// Boundary guards — substrings must NOT match
		{"prefix substring rejected", "prefix value", "fix", false},
		{"suffix substring rejected", "fixer tool", "fix", false},
		{"mid-word substring rejected", "refixing things", "fix", false},
		{"debugging does not trigger debug", "debugging the issue", "debug", false},

		// Keyword next to punctuation still matches (punctuation = boundary)
		{"word after comma", "please, fix this", "fix", true},
		{"word before period", "please fix.", "fix", true},
		{"word in parens", "(fix) it", "fix", true},
		{"word after hyphen", "auto-fix mode", "fix", true},
		{"word before hyphen", "fix-it tool", "fix", true},

		// Numbers adjacent — NOT a letter/digit word boundary so should block
		{"word adjacent to digit rejected", "fix2 things", "fix", false},

		// Empty
		{"empty text", "", "fix", false},
		// An empty word never matches on non-empty text: len(word)==0 means end==absIdx,
		// so afterOK checks text[absIdx] which is a letter → false, and the cursor
		// advances without ever satisfying both boundary conditions.
		{"empty word on non-empty text", "fix the bug", "", false},
		{"both empty", "", "", true},

		// Repeated occurrence where first fails boundary but second passes
		{"second occurrence matches", "prefix the fix", "fix", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsWord(tt.text, tt.word)
			if got != tt.want {
				t.Errorf("containsWord(%q, %q) = %v, want %v", tt.text, tt.word, got, tt.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HeuristicClassifier.Score — keyword scoring
// ─────────────────────────────────────────────────────────────────────────────

func TestHeuristicClassifier_KeywordScoring(t *testing.T) {
	ctx := context.Background()
	c := HeuristicClassifier{}

	tests := []struct {
		name        string
		prompt      string
		meta        TaskMeta
		wantTier    Tier
		wantRuleSub string
	}{
		// ── No keywords → simple default ──────────────────────────────────────
		{
			name:        "empty prompt defaults to simple",
			prompt:      "",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "no matching keywords → simple",
			prompt:      "Hello, can you explain this code?",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},

		// ── Standard-tier keywords ─────────────────────────────────────────────
		{
			name:        "debug → standard",
			prompt:      "Please debug why the user login is failing",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:debug",
		},
		{
			name:        "debugging → standard",
			prompt:      "I need help debugging this goroutine leak",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:debugging",
		},
		{
			name:        "test → standard",
			prompt:      "Can you write a test for this handler?",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:test",
		},
		{
			name:        "fix → standard",
			prompt:      "Please fix the null pointer exception",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:fix",
		},
		{
			name:        "add → standard",
			prompt:      "Add a retry mechanism to the HTTP client",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:add",
		},
		{
			name:        "update → standard",
			prompt:      "Update the README with the new endpoints",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:update",
		},
		{
			name:        "implement → standard",
			prompt:      "Implement the missing cache eviction logic",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:implement",
		},

		// ── Heavy-tier keywords ────────────────────────────────────────────────
		{
			name:        "refactor → heavy",
			prompt:      "Please refactor the auth package",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:refactor",
		},
		{
			name:        "architect → heavy",
			prompt:      "Architect the new multi-region failover system",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:architect",
		},
		{
			name:        "architecture → heavy",
			prompt:      "Review our microservices architecture",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:architecture",
		},
		{
			name:        "design → heavy",
			prompt:      "Design the new API surface for billing",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:design",
		},
		{
			name:        "migrate → heavy",
			prompt:      "Migrate the database from Postgres 13 to 16",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:migrate",
		},
		{
			name:        "migration → heavy",
			prompt:      "Write the migration script for the new schema",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:migration",
		},

		// ── Highest-tier-wins (multiple keywords) ─────────────────────────────
		{
			name:        "debug + refactor → heavy wins",
			prompt:      "Please debug the issue and refactor the code",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:refactor",
		},
		{
			name:        "fix + architect → heavy wins",
			prompt:      "Fix the bug and architect a long-term solution",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:architect",
		},
		{
			name:        "add + design → heavy wins",
			prompt:      "Add a feature and design the new data model",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:design",
		},
		{
			name:        "two standard keywords stay standard",
			prompt:      "debug the issue and add a test",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:",
		},

		// ── Case insensitivity ─────────────────────────────────────────────────
		{
			name:        "REFACTOR uppercased → heavy",
			prompt:      "REFACTOR the authentication module",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:refactor",
		},
		{
			name:        "Debug mixed case → standard",
			prompt:      "Debug the login flow",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:debug",
		},

		// ── Substring false-positive guards ───────────────────────────────────
		{
			name:        "prefix does not trigger fix",
			prompt:      "What prefix should I use for environment variables?",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "fixer does not trigger fix",
			prompt:      "Run the fixer tool on this file",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "debugger does not trigger debug",
			prompt:      "Use the debugger to inspect state",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "untested does not trigger test",
			prompt:      "This function is currently untested",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "updated does not trigger update",
			prompt:      "The value was last updated yesterday",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "implementation does not trigger implement",
			prompt:      "Review this implementation draft",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "redesign does not trigger design",
			prompt:      "Let us redesign the caching layer",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier, rule, err := c.Score(ctx, tt.prompt, tt.meta)
			if err != nil {
				t.Fatalf("Score() unexpected error: %v", err)
			}
			if tier != tt.wantTier {
				t.Errorf("tier = %q, want %q", tier, tt.wantTier)
			}
			if !strings.Contains(rule, tt.wantRuleSub) {
				t.Errorf("rule = %q, want to contain %q", rule, tt.wantRuleSub)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HeuristicClassifier.Score — token-count scoring
// ─────────────────────────────────────────────────────────────────────────────

func TestHeuristicClassifier_TokenScoring(t *testing.T) {
	ctx := context.Background()
	c := HeuristicClassifier{}

	// makePrompt generates a prompt of exactly n neutral words (no keywords).
	makePrompt := func(n int) string {
		words := make([]string, n)
		for i := range words {
			words[i] = "lorem"
		}
		return strings.Join(words, " ")
	}

	tests := []struct {
		name        string
		wordCount   int
		wantTier    Tier
		wantRuleSub string
	}{
		// Below standard threshold
		{"0 words → simple", 0, TierSimple, "default:simple"},
		{"1 word → simple", 1, TierSimple, "default:simple"},
		{"499 words → simple", 499, TierSimple, "default:simple"},

		// At and above standard threshold, below heavy
		{"500 words → standard", 500, TierStandard, "token_count:500"},
		{"501 words → standard", 501, TierStandard, "token_count:501"},
		{"1999 words → standard", 1999, TierStandard, "token_count:1999"},

		// At and above heavy threshold
		{"2000 words → heavy", 2000, TierHeavy, "token_count:2000"},
		{"2001 words → heavy", 2001, TierHeavy, "token_count:2001"},
		{"5000 words → heavy", 5000, TierHeavy, "token_count:5000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier, rule, err := c.Score(ctx, makePrompt(tt.wordCount), TaskMeta{})
			if err != nil {
				t.Fatalf("Score() unexpected error: %v", err)
			}
			if tier != tt.wantTier {
				t.Errorf("tier = %q, want %q", tier, tt.wantTier)
			}
			if !strings.Contains(rule, tt.wantRuleSub) {
				t.Errorf("rule = %q, want to contain %q", rule, tt.wantRuleSub)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HeuristicClassifier.Score — token count cannot lower a keyword-driven tier
// ─────────────────────────────────────────────────────────────────────────────

func TestHeuristicClassifier_TokenDoesNotLowerKeywordTier(t *testing.T) {
	ctx := context.Background()
	c := HeuristicClassifier{}

	// A heavy keyword in a short prompt must stay heavy (tokens can't lower).
	tier, rule, err := c.Score(ctx, "refactor this", TaskMeta{})
	if err != nil {
		t.Fatalf("Score() unexpected error: %v", err)
	}
	if tier != TierHeavy {
		t.Errorf("tier = %q, want %q", tier, TierHeavy)
	}
	if !strings.Contains(rule, "keyword:refactor") {
		t.Errorf("rule = %q, want keyword:refactor", rule)
	}

	// A standard keyword in a 2000-word prompt — heavy token threshold should
	// raise to heavy (standard keyword + heavy token = heavy).
	words := make([]string, 2000)
	words[0] = "fix"
	for i := 1; i < 2000; i++ {
		words[i] = "lorem"
	}
	tier, rule, err = c.Score(ctx, strings.Join(words, " "), TaskMeta{})
	if err != nil {
		t.Fatalf("Score() unexpected error: %v", err)
	}
	if tier != TierHeavy {
		t.Errorf("tier = %q, want heavy (token raises keyword-standard)", tier)
	}
	if !strings.Contains(rule, "token_count:") {
		t.Errorf("rule = %q, want token_count: as driver when token > keyword", rule)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HeuristicClassifier.Score — MCP tool floor
// ─────────────────────────────────────────────────────────────────────────────

func TestHeuristicClassifier_MCPFloor(t *testing.T) {
	ctx := context.Background()
	c := HeuristicClassifier{}

	t.Run("mcp floor raises simple to standard", func(t *testing.T) {
		tier, rule, err := c.Score(ctx, "say hello", TaskMeta{WillUseMCPTools: true})
		if err != nil {
			t.Fatalf("Score() unexpected error: %v", err)
		}
		if tier != TierStandard {
			t.Errorf("tier = %q, want standard", tier)
		}
		if rule != "mcp_floor" {
			t.Errorf("rule = %q, want mcp_floor", rule)
		}
	})

	t.Run("mcp floor does not lower standard to simple", func(t *testing.T) {
		tier, _, err := c.Score(ctx, "fix the bug", TaskMeta{WillUseMCPTools: true})
		if err != nil {
			t.Fatalf("Score() unexpected error: %v", err)
		}
		if tier != TierStandard {
			t.Errorf("tier = %q, want standard", tier)
		}
	})

	t.Run("mcp floor does not lower heavy to standard", func(t *testing.T) {
		tier, rule, err := c.Score(ctx, "refactor the entire service", TaskMeta{WillUseMCPTools: true})
		if err != nil {
			t.Fatalf("Score() unexpected error: %v", err)
		}
		if tier != TierHeavy {
			t.Errorf("tier = %q, want heavy", tier)
		}
		// Rule should still be the keyword, not mcp_floor.
		if rule == "mcp_floor" {
			t.Errorf("rule = mcp_floor but heavy keyword should have won")
		}
	})

	t.Run("mcp false does not floor", func(t *testing.T) {
		tier, rule, err := c.Score(ctx, "say hello", TaskMeta{WillUseMCPTools: false})
		if err != nil {
			t.Fatalf("Score() unexpected error: %v", err)
		}
		if tier != TierSimple {
			t.Errorf("tier = %q, want simple", tier)
		}
		if rule != "default:simple" {
			t.Errorf("rule = %q, want default:simple", rule)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// HeuristicClassifier.Score — interaction: token count + MCP floor
// ─────────────────────────────────────────────────────────────────────────────

func TestHeuristicClassifier_TokenAndMCPInteraction(t *testing.T) {
	ctx := context.Background()
	c := HeuristicClassifier{}

	makePrompt := func(n int) string {
		words := make([]string, n)
		for i := range words {
			words[i] = "lorem"
		}
		return strings.Join(words, " ")
	}

	t.Run("500-word prompt with MCP stays at standard (token drives, mcp does not raise further)", func(t *testing.T) {
		tier, rule, err := c.Score(ctx, makePrompt(500), TaskMeta{WillUseMCPTools: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tier != TierStandard {
			t.Errorf("tier = %q, want standard", tier)
		}
		// Rule should be token_count, not mcp_floor, because token raised it first.
		if !strings.Contains(rule, "token_count:") {
			t.Errorf("rule = %q, want token_count: (token elevated before mcp check)", rule)
		}
	})

	t.Run("heavy token prompt with MCP stays heavy", func(t *testing.T) {
		tier, _, err := c.Score(ctx, makePrompt(2000), TaskMeta{WillUseMCPTools: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tier != TierHeavy {
			t.Errorf("tier = %q, want heavy", tier)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// HeuristicClassifier.Score — Score never returns an error
// ─────────────────────────────────────────────────────────────────────────────

func TestHeuristicClassifier_NeverErrors(t *testing.T) {
	c := HeuristicClassifier{}
	inputs := []string{
		"",
		"normal prompt",
		strings.Repeat("word ", 3000),
		"refactor debug fix architect design migrate",
	}
	for _, p := range inputs {
		_, _, err := c.Score(context.Background(), p, TaskMeta{})
		if err != nil {
			t.Errorf("Score(%q) returned unexpected error: %v", p[:min(len(p), 30)], err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HeuristicClassifier — satisfies the Classifier interface at compile time
// ─────────────────────────────────────────────────────────────────────────────

func TestHeuristicClassifier_ImplementsClassifier(t *testing.T) {
	var _ Classifier = HeuristicClassifier{}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
