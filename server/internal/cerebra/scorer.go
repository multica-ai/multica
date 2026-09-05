package cerebra

import (
	"context"
	"strconv"
	"strings"
	"unicode"
)

// Tier is the complexity tier assigned to a task.
type Tier string

const (
	TierSimple   Tier = "simple"   // low-cost / fast  (e.g. Haiku)
	TierStandard Tier = "standard" // balanced          (e.g. Sonnet)
	TierHeavy    Tier = "heavy"    // highest-capability (e.g. Opus)
)

// TaskMeta carries routing signals extracted from the task before classification.
type TaskMeta struct {
	// TaskID is the claimed task identifier, if any. Used for routing log attribution.
	TaskID string

	// WillUseMCPTools is true when the task is expected to invoke MCP/tool chains.
	// Tool tasks are floored at TierStandard regardless of keyword/token score.
	WillUseMCPTools bool

	// IssueID is the source issue, if any. Used for session-affinity lookups.
	IssueID string

	// SessionID is the chat session, if any. Used for session-affinity lookups.
	SessionID string
}

// Classifier scores a prompt and returns the appropriate routing tier plus
// the rule that drove the decision (for explainability and the routing log).
type Classifier interface {
	Score(ctx context.Context, prompt string, meta TaskMeta) (Tier, string, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// HeuristicClassifier — v1 implementation
// ─────────────────────────────────────────────────────────────────────────────

// keywordTier pairs a keyword with the tier it implies.
type keywordTier struct {
	keyword string
	tier    Tier
}

// orderedKeywords is evaluated left-to-right. Highest-tier-wins: all matching
// rules are evaluated; the highest tier among matches wins (not first-match).
//
// Mapping rationale:
//   - heavy: architecture/design/migration tasks need deep reasoning
//   - standard: coding, debugging, test tasks need reliable tool-calling ability
//   - (simple is the default when no keyword matches and token count is low)
var orderedKeywords = []keywordTier{
	{"refactor", TierHeavy},
	{"architect", TierHeavy},
	{"architecture", TierHeavy},
	{"design", TierHeavy},
	{"migrate", TierHeavy},
	{"migration", TierHeavy},
	{"debug", TierStandard},
	{"debugging", TierStandard},
	{"test", TierStandard},
	{"fix", TierStandard},
	{"add", TierStandard},
	{"update", TierStandard},
	{"implement", TierStandard},
}

// Token-count thresholds (word-split approximation — cheap and deterministic).
const (
	tokenThresholdStandard = 500  // prompts above this → at least standard
	tokenThresholdHeavy    = 2000 // prompts above this → heavy
)

// HeuristicClassifier classifies tasks using keyword detection and token
// counting. It is deterministic, inexpensive, and requires no training data —
// the right-sized MVP for the initial Cerebra deployment.
//
// Rules (applied in order):
//  1. Keyword rules run first and mark candidate tiers. Multiple keyword
//     matches use highest-tier-wins (not first-match-wins).
//  2. Token count scoring runs next and can raise the tier (but not lower it).
//  3. If MCP/tool usage is expected the result is floored at TierStandard.
type HeuristicClassifier struct{}

// Score implements Classifier.
func (h HeuristicClassifier) Score(_ context.Context, prompt string, meta TaskMeta) (Tier, string, error) {
	// Security boundary: clamp prompt inspection size to 64KB to prevent ReDoS / CPU exhaustion
	const maxClassifyLen = 64 * 1024
	if len(prompt) > maxClassifyLen {
		prompt = prompt[:maxClassifyLen]
	}

	lower := strings.ToLower(prompt)
	words := tokenize(lower)

	// Step 1 — keyword scan on natural language text (markdown code blocks stripped).
	// Keywords inside ```...``` or `...` are ignored so code examples don't falsely elevate tier.
	// Negated keywords ("do not refactor", "don't architect") are ignored.
	cleanText := stripCodeBlocks(lower)
	resultTier := TierSimple
	matchedRule := "default:simple"

	for _, kt := range orderedKeywords {
		if containsWord(cleanText, kt.keyword) {
			if tierRank(kt.tier) > tierRank(resultTier) {
				resultTier = kt.tier
				matchedRule = "keyword:" + kt.keyword
			}
		}
	}

	// Step 2 — token-count scoring (can only raise, not lower).
	tokenCount := len(words)
	switch {
	case tokenCount >= tokenThresholdHeavy:
		if tierRank(TierHeavy) > tierRank(resultTier) {
			resultTier = TierHeavy
			matchedRule = "token_count:" + itoa(tokenCount)
		}
	case tokenCount >= tokenThresholdStandard:
		if tierRank(TierStandard) > tierRank(resultTier) {
			resultTier = TierStandard
			matchedRule = "token_count:" + itoa(tokenCount)
		}
	}

	// Step 3 — MCP/tool floor: tool tasks are never sent to simple models.
	if (meta.WillUseMCPTools || hasExplicitToolExecutionIntent(cleanText)) && tierRank(resultTier) < tierRank(TierStandard) {
		resultTier = TierStandard
		if meta.WillUseMCPTools {
			matchedRule = "mcp_floor"
		} else {
			matchedRule = "tool_intent_floor"
		}
	}

	return resultTier, matchedRule, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

func tierRank(t Tier) int {
	switch t {
	case TierHeavy:
		return 2
	case TierStandard:
		return 1
	default:
		return 0
	}
}

// tokenize splits a string into words using Unicode letter/digit boundaries —
// a cheap, locale-independent word count.
func tokenize(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// containsWord reports whether word appears as a whole word in text.
func containsWord(text, word string) bool {
	offset := 0
	for {
		idx := strings.Index(text[offset:], word)
		if idx == -1 {
			return false
		}
		absIdx := offset + idx
		end := absIdx + len(word)
		beforeOK := absIdx == 0 || !unicode.IsLetter(rune(text[absIdx-1])) && !unicode.IsDigit(rune(text[absIdx-1]))
		afterOK := end == len(text) || !unicode.IsLetter(rune(text[end])) && !unicode.IsDigit(rune(text[end]))
		if beforeOK && afterOK {
			return true
		}
		offset = absIdx + 1
		if offset >= len(text) {
			return false
		}
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// stripCodeBlocks removes markdown fenced code blocks (```...```) and inline code (`...`)
// so code syntax, comments, and identifiers do not falsely trigger routing keywords.
func stripCodeBlocks(text string) string {
	var b strings.Builder
	inBlock := false
	i := 0
	n := len(text)
	for i < n {
		if i+2 < n && text[i] == '`' && text[i+1] == '`' && text[i+2] == '`' {
			inBlock = !inBlock
			i += 3
			b.WriteByte(' ')
			continue
		}
		if inBlock {
			b.WriteByte(' ')
			i++
			continue
		}
		if text[i] == '`' {
			j := i + 1
			for j < n && text[j] != '`' && text[j] != '\n' {
				j++
			}
			if j < n && text[j] == '`' {
				for k := i; k <= j; k++ {
					b.WriteByte(' ')
				}
				i = j + 1
				continue
			}
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}

// hasExplicitToolExecutionIntent detects explicit shell or CLI invocation commands
// that require tool-use capabilities, elevating them above simple models.
func hasExplicitToolExecutionIntent(text string) bool {
	commands := []string{
		"bash ", "/bin/sh", "curl ", "pytest", "npm run", "docker run",
		"git commit", "git push", "multica ", "python -m", "go test",
	}
	for _, cmd := range commands {
		if strings.Contains(text, cmd) {
			return true
		}
	}
	return false
}

