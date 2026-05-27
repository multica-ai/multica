// Package codexlimit detects short standalone Codex provider-quota-exhaustion
// messages so the codex backend can downgrade them from "completed" to
// "failed" before the auto-pause path runs.
//
// Codex emits "You've hit your usage limit. Try again at HH:MM" as a normal
// assistant-text turn and exits the turn cleanly — no JSON-RPC error event,
// no non-zero exit code. Without this classifier the daemon posts the
// limit-message as a successful agent reply and the runtime keeps burning
// credits on the next queued task until the limit clears.
//
// Lives in the cerebro zone so it can be reused (codex.go + a future codex
// chat backend, plus tests) without inline-patching upstream more than once.
// Mirrors the in-file pattern used for claude (`isClaudeProviderLimitOutput`
// in server/pkg/agent/claude.go); kept as its own package because codex is
// the second runtime hitting this and the next provider will be the third.
package codexlimit

import "strings"

// maxLimitOutputLen bounds the assistant-text payload that we are willing
// to misinterpret as a provider-limit reply. A bare limit-message from
// Codex is well under 200 chars; if the agent produced ~500+ chars of
// real work that merely mentions "usage limit" we leave the task as
// "completed" and trust the work output.
const maxLimitOutputLen = 500

// limitPhrases are the high-confidence shapes Codex uses when it has
// nothing else to return. All are scanned case-insensitively. The list
// stays narrow on purpose — broader matches (`limit`, `usage`) would
// false-positive on real assistant replies that discuss limits.
var limitPhrases = []string{
	"you've hit your usage limit",
	"you have hit your usage limit",
	"hit your usage limit",
	"usage limit reached",
	"usage limit has been reached",
	"you've reached your usage limit",
	"monthly usage limit",
	"org's monthly usage",
}

// IsProviderLimitOutput reports whether out looks like a short standalone
// Codex provider-limit reply.
//
// True when, after trimming, the payload is non-empty, no longer than
// maxLimitOutputLen, and contains one of the known limit phrases. A short
// "task done — by the way we are near the rate limit" still returns true
// because the cost of one false-positive auto-pause (a 5-minute backoff
// cycle, then resume) is far below the cost of a false-negative (the
// runtime keeps draining credits replying with the same limit message).
func IsProviderLimitOutput(out string) bool {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" || len(trimmed) > maxLimitOutputLen {
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, p := range limitPhrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
