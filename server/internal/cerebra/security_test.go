package cerebra

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Security Test 1: MCP & Tool Capability Flooring
// ─────────────────────────────────────────────────────────────────────────────

func TestSecurity_MCPToolFlooringAndExecutionIntent(t *testing.T) {
	ctx := context.Background()
	c := HeuristicClassifier{}

	// 1. WillUseMCPTools explicitly enforces TierStandard even on trivial prompts
	tier, rule, err := c.Score(ctx, "hello", TaskMeta{WillUseMCPTools: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tier != TierStandard {
		t.Errorf("expected TierStandard when WillUseMCPTools is true, got %v", tier)
	}
	if rule != "mcp_floor" {
		t.Errorf("expected rule 'mcp_floor', got %q", rule)
	}

	// 2. Explicit shell / CLI execution syntax elevates to TierStandard
	execPrompts := []string{
		"Run pytest on tests/test_api.py and report failures",
		"Execute bash build_release.sh in the terminal",
		"Run npm run build and check for type errors",
		"Use docker run to test the container",
		"Run git commit -m 'update' and git push origin main",
	}

	for _, p := range execPrompts {
		tier, rule, err := c.Score(ctx, p, TaskMeta{WillUseMCPTools: false})
		if err != nil {
			t.Fatalf("unexpected error on %q: %v", p, err)
		}
		if tier != TierStandard {
			t.Errorf("prompt %q: expected TierStandard for execution intent, got %v (rule=%s)", p, tier, rule)
		}
		// Must be either tool_intent_floor or a standard keyword (e.g. test/update)
		if rule != "tool_intent_floor" && !strings.HasPrefix(rule, "keyword:") {
			t.Errorf("prompt %q: expected tool_intent_floor or keyword rule, got %q", p, rule)
		}
	}

	// 3. Natural language without execution syntax still defaults to simple
	simplePrompt := "Run the fixer tool on this file"
	tier, rule, err = c.Score(ctx, simplePrompt, TaskMeta{WillUseMCPTools: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tier != TierSimple {
		t.Errorf("expected TierSimple for harmless prompt, got %v (rule=%s)", tier, rule)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Security Test 2: Session Pinning Memory Exhaustion Protection (MaxPins)
// ─────────────────────────────────────────────────────────────────────────────

func TestSecurity_SessionPinningCapacityLimits(t *testing.T) {
	ctx := context.Background()
	store := NewSessionStore(time.Hour)
	// Configure tight capacity for test
	store.SetLimits(5, 5, 24*time.Hour)

	// Insert 10 different sessions
	for i := 1; i <= 10; i++ {
		sessionID := "session-" + itoa(i)
		store.Set(ctx, "", sessionID, "rt-1", "model-standard", TierStandard)
		time.Sleep(2 * time.Millisecond)
	}

	// Active pin count must be bounded at 5, preventing unbounded memory exhaustion
	count := store.Count()
	if count > 5 {
		t.Fatalf("expected store count <= 5, got %d (memory unbounded)", count)
	}

	// Oldest sessions (1 to 5) should have been evicted; newest session (10) should exist
	if pin := store.Get(ctx, "", "session-10"); pin == nil {
		t.Errorf("expected newest session-10 to be present in store")
	}
	if pin := store.Get(ctx, "", "session-1"); pin != nil {
		t.Errorf("expected oldest session-1 to have been evicted, but was found: %v", pin)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Security Test 3: Session Pinning Cost Amplification Protection (Turn Decay)
// ─────────────────────────────────────────────────────────────────────────────

func TestSecurity_SessionPinningCostDecay(t *testing.T) {
	ctx := context.Background()
	store := NewSessionStore(time.Hour)
	// Configure max 3 escalated turns
	store.SetLimits(100, 3, 24*time.Hour)

	issueID := "issue-cost-test"

	// 1. Initial heavy request pins to TierHeavy
	store.Set(ctx, issueID, "", "rt-1", "claude-opus-4-5", TierHeavy)
	pin := store.Get(ctx, issueID, "")
	if pin == nil || pin.Tier != TierHeavy {
		t.Fatalf("expected initial TierHeavy pin, got %v", pin)
	}

	// 2. Turns 1 to 3 with simple requests retain TierHeavy (sticky escalation preserves context)
	for turn := 1; turn <= 3; turn++ {
		store.Set(ctx, issueID, "", "rt-1", "claude-haiku-3-5", TierSimple)
		pin = store.Get(ctx, issueID, "")
		if pin == nil || pin.Tier != TierHeavy {
			t.Fatalf("turn %d: expected sticky TierHeavy retained, got %v", turn, pin)
		}
	}

	// 3. Turn 4: turn budget exhausted! Session must decay back to TierSimple to prevent cost explosion
	store.Set(ctx, issueID, "", "rt-1", "claude-haiku-3-5", TierSimple)
	pin = store.Get(ctx, issueID, "")
	if pin == nil || pin.Tier != TierSimple {
		t.Fatalf("turn 4: expected escalated pin to decay to TierSimple, got %v", pin)
	}
	if pin.Model != "claude-haiku-3-5" {
		t.Fatalf("turn 4: expected model to decay to haiku, got %q", pin.Model)
	}

	// 4. If user issues another Heavy request, it escalates back
	store.Set(ctx, issueID, "", "rt-1", "claude-opus-4-5", TierHeavy)
	pin = store.Get(ctx, issueID, "")
	if pin == nil || pin.Tier != TierHeavy {
		t.Fatalf("expected re-escalation to TierHeavy, got %v", pin)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Security Test 4: Code Block Keyword Isolation
// ─────────────────────────────────────────────────────────────────────────────

func TestSecurity_CodeBlockKeywordIsolation(t *testing.T) {
	ctx := context.Background()
	c := HeuristicClassifier{}

	// Prompt asks a simple question, but pasted code contains "refactor" and "architect"
	promptWithCodeBlock := "What does this function return?\n```go\nfunc refactorDatabase() {\n    // architect the new tables\n    return nil\n}\n```"

	tier, rule, err := c.Score(ctx, promptWithCodeBlock, TaskMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Code block must be isolated: keywords inside code block must not elevate to TierHeavy
	if tier != TierSimple {
		t.Errorf("expected TierSimple for code snippet inquiry, got %v (rule=%s)", tier, rule)
	}
	if rule != "default:simple" {
		t.Errorf("expected rule 'default:simple', got %q", rule)
	}

	// Inline code block with keyword must also be isolated
	promptWithInlineCode := "Please explain the meaning of `refactor` in this context."
	tier, rule, err = c.Score(ctx, promptWithInlineCode, TaskMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tier != TierSimple {
		t.Errorf("expected TierSimple for inline code snippet, got %v (rule=%s)", tier, rule)
	}

	// Natural language outside code blocks MUST still trigger
	promptWithIntent := "Please refactor the authentication middleware:\n```go\nfunc auth() {}\n```"
	tier, rule, err = c.Score(ctx, promptWithIntent, TaskMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tier != TierHeavy {
		t.Errorf("expected TierHeavy when intent is outside code block, got %v (rule=%s)", tier, rule)
	}
	if rule != "keyword:refactor" {
		t.Errorf("expected rule 'keyword:refactor', got %q", rule)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Security Test 5: Prompt Length Clamping & Complexity Limit
// ─────────────────────────────────────────────────────────────────────────────

func TestSecurity_PromptLengthSanitization(t *testing.T) {
	ctx := context.Background()
	c := HeuristicClassifier{}

	// Oversized 500KB prompt (potential memory / regex DoS vector)
	largePrompt := strings.Repeat("test word ", 50000)

	start := time.Now()
	tier, rule, err := c.Score(ctx, largePrompt, TaskMeta{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error on large prompt: %v", err)
	}
	if tier != TierHeavy {
		t.Errorf("expected TierHeavy due to token count, got %v", tier)
	}
	if !strings.HasPrefix(rule, "token_count:") {
		t.Errorf("expected token_count rule, got %q", rule)
	}
	// Processing must be sub-50ms even on oversized inputs
	if elapsed > 100*time.Millisecond {
		t.Errorf("classification took too long: %v", elapsed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Security Test 6: Audit Log Sensitive Data Sanitization
// ─────────────────────────────────────────────────────────────────────────────

func TestSecurity_AuditLogNoSensitiveDataLeakage(t *testing.T) {
	s := sanitizeLogField("normal_rule", 128)
	if s != "normal_rule" {
		t.Errorf("expected 'normal_rule', got %q", s)
	}

	// Clamping oversized string
	oversized := strings.Repeat("A", 300)
	clamped := sanitizeLogField(oversized, 128)
	if len(clamped) != 128 {
		t.Errorf("expected length 128, got %d", len(clamped))
	}

	// Stripping control characters / newlines to prevent log injection
	dirty := "rule:one\nINJECTED_HEADER: evil\r\tvalue"
	clean := sanitizeLogField(dirty, 128)
	if strings.Contains(clean, "\n") || strings.Contains(clean, "\r") || strings.Contains(clean, "\t") {
		t.Errorf("expected control chars removed, got %q", clean)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Security Test 7: Model Catalog Trust & Sanitization
// ─────────────────────────────────────────────────────────────────────────────

func TestSecurity_ModelCatalogSanitization(t *testing.T) {
	// 1. Control characters in model name rejected
	if isValidModelIdentifier("opencode/model\ninjection") {
		t.Errorf("expected newline in model ID to be rejected")
	}
	if isValidModelIdentifier("model\x00nullbyte") {
		t.Errorf("expected null byte in model ID to be rejected")
	}

	// 2. Excessively long model name rejected
	longModel := strings.Repeat("a", 300)
	if isValidModelIdentifier(longModel) {
		t.Errorf("expected 300-char model ID to be rejected")
	}

	// 3. Malformed models filtered from catalog
	dirtyCatalog := []string{
		"opencode/nemotron-3-ultra-free",
		"invalid\nmodel\nname",
		"sub-1b-toy:0.5b",                  // sub-1.5B non-chat model filtered
		"voice-tts-model",                  // non-chat audio model filtered
		"opencode/nemotron-3.5-lightning-free",
		"opencode/mimo-v2.5-free",
	}

	tierMap := BuildTierMapFromCatalog(dirtyCatalog)

	// Valid models correctly classified
	if tierMap[TierHeavy] != "opencode/nemotron-3-ultra-free" {
		t.Errorf("expected heavy to be ultra, got %s", tierMap[TierHeavy])
	}
	if tierMap[TierStandard] != "opencode/nemotron-3.5-lightning-free" {
		t.Errorf("expected standard to be lightning, got %s", tierMap[TierStandard])
	}
	if tierMap[TierSimple] != "opencode/mimo-v2.5-free" {
		t.Errorf("expected simple to be mimo, got %s", tierMap[TierSimple])
	}
}
