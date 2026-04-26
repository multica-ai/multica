// Package github contains the CodeRabbit / GitHub PR-driven status automation.
//
// This file resolves a Multica issue identifier (e.g. "TIM-42") from a
// GitHub pull-request payload, falling back through three sources:
//
//	1. PR head_ref (the branch name)        — preferred; agent convention
//	2. PR body                              — covers legacy branches
//	3. PR title                             — last-chance fallback
//
// All three sources are matched against the same regex so any "<PREFIX>-<N>"
// token wins. We deliberately do NOT match anywhere else in the request — a
// stray identifier in a comment or commit message must not trigger a status
// transition for an unrelated PR.
package github

import "regexp"

// identifierRegex matches workspace-prefixed issue identifiers like TIM-42,
// MUL-1128, etc. Prefix is one uppercase letter followed by 1+ uppercase
// alphanumerics; suffix is a positive integer.
var identifierRegex = regexp.MustCompile(`\b([A-Z][A-Z0-9]+)-(\d+)\b`)

// Identifier is the parsed result of an identifier match: workspace prefix
// (e.g. "TIM") and issue number (e.g. 42).
type Identifier struct {
	Prefix string
	Number int32
}

// ExtractIdentifier returns the first matching identifier across head_ref,
// body, and title in that order, or false if none match.
//
// A bare identifier means: "an uppercase token, dash, integer" with word
// boundaries on either side. Examples that match: "TIM-42", "agent/dev/TIM-9-abc",
// "Closes TIM-42 cleanly". Examples that DO NOT match: "tim-42" (lowercase),
// "TIM_42" (underscore), "TIM-42abc" (no word boundary after digits).
func ExtractIdentifier(headRef, body, title string) (Identifier, bool) {
	for _, candidate := range []string{headRef, body, title} {
		if m := identifierRegex.FindStringSubmatch(candidate); m != nil {
			n, err := parsePositiveInt(m[2])
			if err != nil {
				continue
			}
			return Identifier{Prefix: m[1], Number: n}, true
		}
	}
	return Identifier{}, false
}

// parsePositiveInt parses an unsigned base-10 integer that fits in int32.
// Returns an error on overflow or non-digit input.
func parsePositiveInt(s string) (int32, error) {
	var out int32
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBadInt
		}
		// Overflow guard: if multiplying by 10 would overflow int32, bail.
		if out > (1<<31-1)/10 {
			return 0, errBadInt
		}
		out = out*10 + int32(c-'0')
	}
	return out, nil
}

var errBadInt = &parseError{msg: "invalid integer"}

type parseError struct{ msg string }

func (e *parseError) Error() string { return e.msg }
