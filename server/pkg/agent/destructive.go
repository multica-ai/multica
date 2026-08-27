package agent

import "regexp"

// GAP-30 (#25): daemon-side gate for destructive commands. The daemon is
// headless, so v1 hard-denies instead of pausing for a human — the agent sees
// a denial and reports failure to the requester, who can rerun after review.
// ponytail: static blocklist; full approval-request UI when block rate measured.
var destructivePatterns = []*regexp.Regexp{
	// rm -rf / (any recursive rm aimed at filesystem root)
	regexp.MustCompile(`(?i)\brm\s+-[a-z]*r[a-z]*\s+(--\s+)?/(?:\s|$)`),
	// git push --force/-f to main or master
	regexp.MustCompile(`(?i)\bgit\b[^;&|]*\bpush\b[^;&|]*(--force(-with-lease)?\b|\s-f\b)[^;&|]*\b(main|master)\b`),
	// SQL: DROP DATABASE / DROP TABLE / TRUNCATE TABLE
	regexp.MustCompile(`(?i)\bdrop\s+(database|table)\b|\btruncate\s+table\b`),
	// bash/zsh fork bomb :(){ :|:& };:
	regexp.MustCompile(`:\(\)\s*\{`),
}

// matchDestructiveCommand returns the matched pattern if s looks like a
// destructive command, "" otherwise. False positives fail safe (denied).
func matchDestructiveCommand(s string) string {
	for _, re := range destructivePatterns {
		if re.MatchString(s) {
			return re.String()
		}
	}
	return ""
}
