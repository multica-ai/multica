package agent

// Cerebro-only companion to the claude backend's `result` handling (FIR-4013).
//
// The Claude Code CLI ends a run it could not finish with a `result` message
// carrying `is_error: true`. For the terminal conditions that are not an API
// failure — hitting `--max-turns`, or an internal abort — that message has NO
// `result` text: the only field naming the cause is `subtype`. claude.go read
// `subtype` into the struct but never used it on the failure path, so
// Result.Error came back empty.
//
// An empty Result.Error is not harmless. The daemon composes a generic
// "<provider> execution <status>" string for it (daemon.go's default branch),
// which yields the infamous "claude execution failed" — a message that names
// nothing, matches no rule in taskfailure.Classify, and therefore lands in
// agent_error.unknown, the one bucket failrouter never retries. A run that did
// 15 minutes of real work died permanently with an error text that told the
// user nothing. Verified in production: 5 runs carried exactly that string,
// and 21 runs in 30 days hit the empty-error state behind it.
//
// Keeping the subtype does not by itself make the failure retryable — that is
// a separate taxonomy change — but it turns an unnameable failure into a named
// one for both the human reading the issue and any future classifier rule.

import "strings"

// claudeResultErrorFromSubtype renders a failed `result` message's subtype as
// an error string. Returns "" when the subtype carries no information, which
// leaves the caller's existing empty-error handling untouched.
func claudeResultErrorFromSubtype(subtype string) string {
	trimmed := strings.TrimSpace(subtype)
	switch trimmed {
	case "", "success":
		// "success" cannot legitimately appear with is_error set; treat it as
		// no signal rather than reporting a contradiction to the user.
		return ""
	case "error_max_turns":
		return "claude stopped: the run reached its maximum number of turns (error_max_turns)"
	case "error_during_execution":
		return "claude stopped: the run aborted during execution (error_during_execution)"
	default:
		return "claude stopped: " + trimmed
	}
}
