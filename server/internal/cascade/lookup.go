package cascade

import "regexp"

// LookupIssueIdentifier extracts the issue identifier (e.g. "PUL-102")
// from the PR title or, failing that, the branch name. Returns the
// stringified PUL-N when found, empty otherwise. The caller then
// uses GetIssueByNumber on multica's queries to map number → issue
// UUID inside the workspace.
//
// G4 amendment: title parsing is primary; branch fallback covers
// the case where a reviewer edited the title and dropped the prefix.
// Both regexes are case-insensitive on the alpha prefix so user
// typos like "pul-102" still match.
//
// The shape `[A-Z]+-[0-9]+` accepts any multica issue-prefix the
// workspace has configured (PUL, MUL, OPS, …) so this code does not
// hardcode "PUL".
func LookupIssueIdentifier(prTitle, branch string) string {
	if id := matchTitle(prTitle); id != "" {
		return id
	}
	return matchBranch(branch)
}

// titleRegex matches a leading [PREFIX-N] bracket. Anchored to the
// start so a PUL reference deeper in the title (e.g. "fix typo in
// PUL-99 ref") does not falsely match — PR titles authored by
// agents put the identifier first by convention.
var titleRegex = regexp.MustCompile(`^\[([A-Za-z]+-[0-9]+)\]`)

func matchTitle(s string) string {
	m := titleRegex.FindStringSubmatch(s)
	if len(m) != 2 {
		return ""
	}
	return normalize(m[1])
}

// branchRegex matches the cascade convention: agent-<N>/<prefix>-<N>-<slug>.
// Anchored to start. Underscores and dots inside the slug are
// allowed (branches may not include `..` per git, but isolated dots
// are valid). The branch regex deliberately does NOT match
// pre-cascade conventions like `feat/pul-1-foo` — the scope filter
// only triggers for agent-driven PRs.
var branchRegex = regexp.MustCompile(`^agent-[0-9a-zA-Z]+/([A-Za-z]+-[0-9]+)(?:[-_./].*)?$`)

func matchBranch(s string) string {
	m := branchRegex.FindStringSubmatch(s)
	if len(m) != 2 {
		return ""
	}
	return normalize(m[1])
}

// normalize uppercases the alpha prefix so look-ups against
// workspace.issue_prefix are consistent regardless of caller casing.
// "pul-99" and "PUL-99" must map to the same issue.
func normalize(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// agentBranchRegex matches the looser "any agent-driven branch"
// shape used by the scope filter. Unlike branchRegex, this does not
// require a `<PREFIX-N>` segment after the agent prefix — the scope
// filter only confirms "yes, an agent owns this branch", regardless
// of whether an identifier can be extracted. Identifier extraction
// is branchRegex's job and is intentionally stricter.
var agentBranchRegex = regexp.MustCompile(`^agent-[0-9a-zA-Z]+/`)

// InScope reports whether a PR title + branch combination satisfies
// the cascade scope filter (C4): only agent-driven PRs trigger the
// pipeline. A PR is in-scope when either the title carries a
// `[PREFIX-N]` bracket OR the branch starts with `agent-<id>/`
// (regardless of what follows the slash).
//
// Manual user PRs (no agent branch, no bracket prefix) return false
// — webhook handler logs them and skips. The filter lives here, not
// in the router, per C4 "scope filter ONE place — in handler".
func InScope(prTitle, branch string) bool {
	if titleRegex.MatchString(prTitle) {
		return true
	}
	if agentBranchRegex.MatchString(branch) {
		return true
	}
	return false
}
