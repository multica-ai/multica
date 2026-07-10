// Package versioning is the shared foundation for cerebro's
// propose → review → approve → snapshot governance pattern (FIR-2698).
//
// The pattern was implemented twice before this package existed — once for
// skills (server/internal/handler/skill_ownership.go) and once for agent
// context (server/internal/cerebro/agentoffice) — with byte-identical semver
// helpers, LCS diff engines, and approval-transaction skeletons. This package
// holds the entity-agnostic pieces so a third versioned entity (the model
// registry) does not mean a third copy. Entity-specific parts (snapshot shape,
// apply logic, rendering) stay in the owning package.
//
// Consumers today: agentoffice (agent context) and modelregistry. Skill
// governance still carries its own copy because it lives in the upstream zone;
// migrating it here is a planned follow-up.
package versioning

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// --- Approval sentinels ---

// ErrStaleProposal is returned when the proposed version is no longer greater
// than the entity's current version (someone merged another change between
// propose and approve). Maps to 409 so the client knows to rebase.
var ErrStaleProposal = errors.New("proposed_version is no longer greater than current — rebase the change request")

// ErrNotPending is returned when a concurrent reviewer already moved the
// change request out of the pending state.
var ErrNotPending = errors.New("change request is no longer pending")

// StatusForMergeError maps an approval error to an HTTP status.
func StatusForMergeError(err error) int {
	switch {
	case errors.Is(err, ErrStaleProposal), errors.Is(err, ErrNotPending):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// --- Semver helpers ---

var semverRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// ValidSemver reports whether s is a strict X.Y.Z form.
func ValidSemver(s string) bool { return semverRe.MatchString(s) }

// SemverGT reports whether a > b under strict semver. Components are compared
// by length first, then lexically, so "10" sorts above "2".
func SemverGT(a, b string) bool {
	am := semverRe.FindStringSubmatch(a)
	bm := semverRe.FindStringSubmatch(b)
	if am == nil || bm == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		if am[i] == bm[i] {
			continue
		}
		if len(am[i]) != len(bm[i]) {
			return len(am[i]) > len(bm[i])
		}
		return am[i] > bm[i]
	}
	return false
}

// BumpPatch returns the next patch version of a valid semver, used when a
// rollback needs a fresh version number greater than current.
func BumpPatch(v string) string {
	m := semverRe.FindStringSubmatch(v)
	if m == nil {
		return "1.0.0"
	}
	patch := 0
	fmt.Sscanf(m[3], "%d", &patch)
	return fmt.Sprintf("%s.%s.%d", m[1], m[2], patch+1)
}

// --- Unified diff (LCS) ---

// UnifiedDiff returns a unified-diff-style string between two text renderings
// of an entity, or "" when they are identical. label names the entity in the
// header lines.
func UnifiedDiff(base, proposed, label string) string {
	if base == proposed {
		return ""
	}
	ops := diffLines(splitLines(base), splitLines(proposed))
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s (base)\n+++ %s (proposed)\n", label, label)
	for _, op := range ops {
		switch op.kind {
		case diffEqual:
			fmt.Fprintf(&b, " %s\n", op.line)
		case diffDel:
			fmt.Fprintf(&b, "-%s\n", op.line)
		case diffAdd:
			fmt.Fprintf(&b, "+%s\n", op.line)
		}
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, "\n")
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

const (
	diffEqual = iota
	diffAdd
	diffDel
)

type diffOp struct {
	kind int
	line string
}

// diffLines is a textbook LCS-based line diff. Sufficient for
// snapshot-rendering-sized inputs; the O(N*M) cost is bounded by the callers'
// truncation before storage.
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	if n == 0 {
		out := make([]diffOp, m)
		for i, line := range b {
			out[i] = diffOp{kind: diffAdd, line: line}
		}
		return out
	}
	if m == 0 {
		out := make([]diffOp, n)
		for i, line := range a {
			out[i] = diffOp{kind: diffDel, line: line}
		}
		return out
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	var ops []diffOp
	i, j := n, m
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			ops = append([]diffOp{{kind: diffEqual, line: a[i-1]}}, ops...)
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			ops = append([]diffOp{{kind: diffDel, line: a[i-1]}}, ops...)
			i--
		} else {
			ops = append([]diffOp{{kind: diffAdd, line: b[j-1]}}, ops...)
			j--
		}
	}
	for i > 0 {
		ops = append([]diffOp{{kind: diffDel, line: a[i-1]}}, ops...)
		i--
	}
	for j > 0 {
		ops = append([]diffOp{{kind: diffAdd, line: b[j-1]}}, ops...)
		j--
	}
	return ops
}
