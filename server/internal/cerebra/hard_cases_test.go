package cerebra

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: The "Misleading Substring" Heuristic Trap
// ─────────────────────────────────────────────────────────────────────────────

func TestHardCase_MisleadingSubstringTrap(t *testing.T) {
	ctx := context.Background()
	classifier := HeuristicClassifier{}

	testCases := []struct {
		name        string
		prompt      string
		wantTier    Tier
		wantRuleSub string
	}{
		{
			name:        "prefix, postfix, fixture without whole keywords stay Simple",
			prompt:      "Please explain what prefix and postfix naming conventions we should follow in our documentation. Make sure to mention the sample fixture structure.",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "prefix and postfix with standalone keyword 'test' triggers keyword:test",
			prompt:      "Please explain what prefix and postfix naming conventions we should follow in our documentation. Make sure to mention the test fixture structure.",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:test",
		},
		{
			name:        "affix and suffix should not trigger fix",
			prompt:      "Check if the affix and suffix formatting are compliant with style guides.",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "debugger and untested should not trigger debug or test",
			prompt:      "We configured the debugger yesterday and found an untested code path.",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "redesign and refactorer should not trigger design or refactor",
			prompt:      "The UI redesign was reviewed by our automated refactorer.",
			wantTier:    TierSimple,
			wantRuleSub: "default:simple",
		},
		{
			name:        "real keyword 'fix' next to punctuation correctly triggers Standard",
			prompt:      "Please fix this bug immediately.",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:fix",
		},
		{
			name:        "real keyword 'debug' in parentheses triggers Standard",
			prompt:      "Can you help (debug) this issue?",
			wantTier:    TierStandard,
			wantRuleSub: "keyword:debug",
		},
		{
			name:        "real keyword 'architect' triggers Heavy",
			prompt:      "Please architect the new messaging bus.",
			wantTier:    TierHeavy,
			wantRuleSub: "keyword:architect",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tier, rule, err := classifier.Score(ctx, tc.prompt, TaskMeta{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tier != tc.wantTier {
				t.Errorf("prompt=%q\ngot tier=%v, want tier=%v (matchedRule=%q)", tc.prompt, tier, tc.wantTier, rule)
			}
			if !strings.Contains(rule, tc.wantRuleSub) {
				t.Errorf("got matchedRule=%q, want substring %q", rule, tc.wantRuleSub)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: Negation & Contradictory Intent Analysis
// ─────────────────────────────────────────────────────────────────────────────

func TestHardCase_NegationAndContradictoryIntent(t *testing.T) {
	ctx := context.Background()
	classifier := HeuristicClassifier{}

	prompt := "Do not architect any new systems and do not debug any code. Just write a 2-sentence summary of what this project does."

	tier, rule, err := classifier.Score(ctx, prompt, TaskMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// HeuristicClassifier is a keyword-presence scorer; the presence of "architect"
	// triggers TierHeavy. This is the documented deterministic behavior of v1 heuristics.
	if tier != TierHeavy {
		t.Errorf("expected HeuristicClassifier to score TierHeavy on keyword 'architect', got %v (rule=%s)", tier, rule)
	}
	if rule != "keyword:architect" {
		t.Errorf("expected rule 'keyword:architect', got %q", rule)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: Session Pinning Consistency (Multi-Turn)
// ─────────────────────────────────────────────────────────────────────────────

func TestHardCase_SessionPinningConsistency(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	classifier := HeuristicClassifier{}
	policy := &Policy{}
	session := NewSessionStore(2 * time.Hour)
	unavail := NewUnavailabilityStore(time.Hour)
	router := NewRouter(classifier, policy, session, unavail, logger, nil)

	runtimes := []RuntimeEntry{
		{
			RuntimeID: "rt-opencode-01",
			TierMap: TierMap{
				TierSimple:   "opencode/mimo-v2.5-free",
				TierStandard: "opencode/nemotron-3.5-lightning-free",
				TierHeavy:    "opencode/nemotron-3-ultra-free",
			},
		},
	}

	meta := TaskMeta{
		TaskID:    "task-turn-1",
		IssueID:   "issue-arch-event-bus",
		SessionID: "session-arch-event-bus",
	}

	// Turn 1: Heavy Architectural Prompt
	turn1Prompt := "Architect and design a high-throughput distributed event bus with partition balancing."
	res1 := router.Route(ctx, turn1Prompt, meta, runtimes, "default-model")

	if res1.Tier != TierHeavy || res1.Model != "opencode/nemotron-3-ultra-free" {
		t.Fatalf("Turn 1: expected heavy tier with nemotron-3-ultra-free, got tier=%s, model=%s", res1.Tier, res1.Model)
	}

	// Turn 2: Short 3-word follow-up comment in the same issue/session thread
	meta.TaskID = "task-turn-2"
	turn2Prompt := "Looks good, proceed."
	res2 := router.Route(ctx, turn2Prompt, meta, runtimes, "default-model")

	// Session pinning MUST retain the heavy tier model from Turn 1 to preserve architectural context
	if res2.Model != "opencode/nemotron-3-ultra-free" {
		t.Fatalf("Turn 2: expected session pin to retain 'opencode/nemotron-3-ultra-free', got %q", res2.Model)
	}
	if res2.Tier != TierHeavy {
		t.Fatalf("Turn 2: expected tier to remain Heavy due to sticky session escalation, got %s", res2.Tier)
	}
	if !strings.Contains(res2.MatchedRule, "session_pin") {
		t.Fatalf("Turn 2: expected matchedRule to indicate session_pin, got %s", res2.MatchedRule)
	}

	// Turn 3: Coding request on the same session
	meta.TaskID = "task-turn-3"
	turn3Prompt := "Implement the partition balancing worker."
	res3 := router.Route(ctx, turn3Prompt, meta, runtimes, "default-model")

	if res3.Model != "opencode/nemotron-3-ultra-free" {
		t.Fatalf("Turn 3: expected session pin retention, got %q", res3.Model)
	}

	// Turn 4: Different issue / fresh session should NOT inherit the pin
	freshMeta := TaskMeta{
		TaskID:    "task-turn-4",
		IssueID:   "issue-new-fresh",
		SessionID: "session-new-fresh",
	}
	res4 := router.Route(ctx, "What is the project structure?", freshMeta, runtimes, "default-model")
	if res4.Tier != TierSimple || res4.Model != "opencode/mimo-v2.5-free" {
		t.Fatalf("Turn 4: fresh session should route to Simple tier, got tier=%s, model=%s", res4.Tier, res4.Model)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4: Rate-Limit, Context Window & Cooldown Failover Stress Test
// ─────────────────────────────────────────────────────────────────────────────

func TestHardCase_RateLimitAndContextWindowStress(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	classifier := HeuristicClassifier{}
	policy := &Policy{}
	session := NewSessionStore(2 * time.Hour)
	unavail := NewUnavailabilityStore(time.Hour)
	router := NewRouter(classifier, policy, session, unavail, logger, nil)

	runtimes := []RuntimeEntry{
		{
			RuntimeID: "rt-1",
			TierMap: TierMap{
				TierSimple:   "claude-3-5-haiku",
				TierStandard: "claude-3-5-sonnet",
				TierHeavy:    "claude-3-opus",
			},
		},
	}

	// 4.1 Massive Input Token Scoring (>2000 words)
	var sb strings.Builder
	for i := 0; i < 2200; i++ {
		sb.WriteString("analysis ")
	}
	largePrompt := sb.String()

	tier, rule, err := classifier.Score(ctx, largePrompt, TaskMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tier != TierHeavy || !strings.Contains(rule, "token_count:") {
		t.Fatalf("expected TierHeavy from token threshold (>2000), got tier=%s, rule=%s", tier, rule)
	}

	// 4.2 Error Classification: Rate Limit vs Context Window
	rateLimitError := "HTTP 429 Too Many Requests: Rate limit exceeded for claude-3-opus"
	kind := ParseFailure(rateLimitError)
	if kind != FailureRateLimit || !ShouldMarkUnavailable(kind) {
		t.Fatalf("expected FailureRateLimit + ShouldMarkUnavailable=true, got kind=%v", kind)
	}

	contextLenError := "Error: maximum context length exceeded (32768 tokens limit reached)"
	kindCtx := ParseFailure(contextLenError)
	if kindCtx != FailureContextLength || ShouldMarkUnavailable(kindCtx) {
		t.Fatalf("expected FailureContextLength + ShouldMarkUnavailable=false, got kind=%v", kindCtx)
	}

	// 4.3 Cooldown & Failover Routing
	// Initial route for Heavy task
	heavyMeta := TaskMeta{IssueID: "issue-stress-1"}
	res1 := router.Route(ctx, "architect a new migration pipeline", heavyMeta, runtimes, "default-model")
	if res1.Model != "claude-3-opus" {
		t.Fatalf("expected claude-3-opus, got %s", res1.Model)
	}

	// Mark claude-3-opus unavailable due to 429 rate limit
	unavail.MarkUnavailable(ctx, "rt-1", "claude-3-opus", time.Hour)

	// Next task should fail over cleanly to Standard (claude-3-5-sonnet)
	res2 := router.Route(ctx, "architect a new migration pipeline", TaskMeta{IssueID: "issue-stress-2"}, runtimes, "default-model")
	if res2.Model != "claude-3-5-sonnet" || !res2.FallbackUsed || res2.Status != "fallback" {
		t.Fatalf("expected failover to claude-3-5-sonnet, got model=%s, fallback=%v, status=%s", res2.Model, res2.FallbackUsed, res2.Status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 5: Tool Floor Policy Test
// ─────────────────────────────────────────────────────────────────────────────

func TestHardCase_ToolFloorPolicy(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	classifier := HeuristicClassifier{}
	policy := &Policy{}
	session := NewSessionStore(2 * time.Hour)
	unavail := NewUnavailabilityStore(time.Hour)
	router := NewRouter(classifier, policy, session, unavail, logger, nil)

	runtimes := []RuntimeEntry{
		{
			RuntimeID: "rt-1",
			TierMap: TierMap{
				TierSimple:   "opencode/mimo-v2.5-free",
				TierStandard: "opencode/nemotron-3.5-lightning-free",
				TierHeavy:    "opencode/nemotron-3-ultra-free",
			},
		},
	}

	// 5.1 Simple prompt without tools -> Simple Tier
	simpleMeta := TaskMeta{WillUseMCPTools: false}
	resSimple := router.Route(ctx, "Say hello in 3 words.", simpleMeta, runtimes, "default-fallback")
	if resSimple.Tier != TierSimple || resSimple.Model != "opencode/mimo-v2.5-free" {
		t.Fatalf("expected Simple tier for non-tool task, got tier=%s, model=%s", resSimple.Tier, resSimple.Model)
	}

	// 5.2 Simple prompt with MCP tools -> Tool Floor elevates to Standard Tier
	toolMeta := TaskMeta{WillUseMCPTools: true}
	resTool := router.Route(ctx, "Say hello in 3 words.", toolMeta, runtimes, "default-fallback")
	if resTool.Tier != TierStandard || resTool.Model != "opencode/nemotron-3.5-lightning-free" {
		t.Fatalf("expected Tool Floor to enforce Standard tier, got tier=%s, model=%s", resTool.Tier, resTool.Model)
	}
	if !strings.Contains(resTool.MatchedRule, "mcp_floor") {
		t.Fatalf("expected matchedRule to contain mcp_floor, got %s", resTool.MatchedRule)
	}

	// 5.3 Heavy prompt with MCP tools -> Remains Heavy Tier (not lowered)
	resHeavyTool := router.Route(ctx, "architect a sharding cluster", toolMeta, runtimes, "default-fallback")
	if resHeavyTool.Tier != TierHeavy || resHeavyTool.Model != "opencode/nemotron-3-ultra-free" {
		t.Fatalf("expected Heavy tier to be preserved with tools, got tier=%s, model=%s", resHeavyTool.Tier, resHeavyTool.Model)
	}

	// 5.4 Tool task when Standard is unavailable escalates to Heavy, NOT Simple
	unavail.MarkUnavailable(ctx, "rt-1", "opencode/nemotron-3.5-lightning-free", time.Hour)
	resEscalate := router.Route(ctx, "Say hello in 3 words.", toolMeta, runtimes, "default-fallback")
	if resEscalate.Tier != TierHeavy || resEscalate.Model != "opencode/nemotron-3-ultra-free" {
		t.Fatalf("expected tool task to escalate to Heavy when Standard is down, got tier=%s, model=%s", resEscalate.Tier, resEscalate.Model)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 6: Multi-Runtime Diversity & Cross-Runtime Fallback
// ─────────────────────────────────────────────────────────────────────────────

func TestHardCase_CrossRuntimeFailover(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	classifier := HeuristicClassifier{}
	policy := &Policy{}
	session := NewSessionStore(2 * time.Hour)
	unavail := NewUnavailabilityStore(time.Hour)
	router := NewRouter(classifier, policy, session, unavail, logger, nil)

	runtimes := []RuntimeEntry{
		{
			RuntimeID: "rt-claude-primary",
			TierMap: TierMap{
				TierSimple:   "claude-3-5-haiku",
				TierStandard: "claude-3-5-sonnet",
				TierHeavy:    "claude-3-opus",
			},
		},
		{
			RuntimeID: "rt-opencode-backup",
			TierMap: TierMap{
				TierSimple:   "opencode/mimo-v2.5-free",
				TierStandard: "opencode/nemotron-3.5-lightning-free",
				TierHeavy:    "opencode/nemotron-3-ultra-free",
			},
		},
	}

	// Initial route chooses primary runtime
	res1 := router.Route(ctx, "debug the database connection", TaskMeta{}, runtimes, "default-model")
	if res1.RuntimeID != "rt-claude-primary" || res1.Model != "claude-3-5-sonnet" {
		t.Fatalf("expected primary claude-3-5-sonnet, got runtime=%s, model=%s", res1.RuntimeID, res1.Model)
	}

	// Mark primary sonnet unavailable
	unavail.MarkUnavailable(ctx, "rt-claude-primary", "claude-3-5-sonnet", time.Hour)

	// Router should seamlessly select backup runtime Standard model
	res2 := router.Route(ctx, "debug the database connection", TaskMeta{}, runtimes, "default-model")
	if res2.RuntimeID != "rt-opencode-backup" || res2.Model != "opencode/nemotron-3.5-lightning-free" {
		t.Fatalf("expected backup opencode runtime standard model, got runtime=%s, model=%s", res2.RuntimeID, res2.Model)
	}
}
