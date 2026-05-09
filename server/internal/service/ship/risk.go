// Phase 5 — Ship Hub PR risk classifier.
//
// Phase 1's "touches schema / migration" badge derived from a keyword
// scan on the title and labels (deriveRiskHint in
// packages/views/ship/hooks/use-pr-state.ts). That heuristic was noisy
// — a PR titled "rename schema field" lit up the badge but a PR with a
// 200-line auth handler change did nothing. Phase 5 replaces it with a
// rule-based classifier that consults the actual changed-file paths,
// folds in the diff body for migrations, and persists the verdict on
// the row so:
//
//   - Web and desktop see the same answer (no per-render derivation).
//   - The "Why this risk?" popover renders the trigger list verbatim.
//   - Workspace-wide aggregates (sidebar widget) can SUM/COUNT against
//     the indexed column instead of decoding every row's title.
//
// The classifier is rule-based, not LLM-driven. Reasons:
//   - Determinism: a sysadmin reading the trigger list can predict
//     when a PR will be flagged.
//   - Latency: the hot path is the pull_request:opened webhook
//     dispatch. Adding an LLM round-trip there is a meaningful UX
//     regression for a feature whose value is fast feedback.
//   - Falsifiability: rule changes are reviewable in code; an LLM's
//     "high" verdict on PR #142 is opaque.
//
// Tier semantics (most-actionable wins):
//
//   critical — the change can take production down on merge. API
//              breaking changes (removed exports), Dockerfile/k8s/infra
//              edits, migrations with destructive SQL.
//   high     — change touches a "we'd hate to roll this back at 3am"
//              surface: any migration, auth handlers, payment handlers,
//              member handlers, agent runtime files, the daemon.
//   medium   — handler changes, new endpoints, frontend logic. The
//              default — a PR with no triggers stays here.
//   low      — tests-only, docs-only, lint fixes. Not zero risk in
//              theory, but the user wants the chip muted so the
//              high-risk PRs above stand out.
//
// Mixing tiers takes the highest-severity match. A PR that touches
// docs AND a migration is "high" with both reasons listed.

package ship

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
)

// RiskLevel mirrors the SQL enum; declared here so callers don't have
// to reach into the generated db package for the four constants.
type RiskLevel = db.RiskLevel

const (
	RiskLevelLow      = db.RiskLevelLow
	RiskLevelMedium   = db.RiskLevelMedium
	RiskLevelHigh     = db.RiskLevelHigh
	RiskLevelCritical = db.RiskLevelCritical
)

// RiskInputs is the slice of the PR shape the classifier needs. We
// model it as its own struct so the same function can be invoked from
// the webhook path (where the wire shape is gh.PullRequest) and from
// the manual reclassify endpoint (where it's a db.PullRequest +
// fetched files).
type RiskInputs struct {
	Title        string
	Body         string
	BaseRef      string
	HeadRef      string
	ChangedCount int
	// Files is OPTIONAL. Without it we degrade to a title-and-branch
	// heuristic that's strictly weaker than Phase 1 — the classifier
	// returns medium-with-no-reasons rather than failing.
	Files []gh.PullRequestFile
}

// ClassifyResult is the persisted verdict.
type ClassifyResult struct {
	Level   db.RiskLevel
	Reasons []string
}

// ToJSON encodes Reasons as a JSONB-ready []byte for the
// UpdatePullRequestRiskProfile call. Empty array (not null) so the
// downstream `risk_reasons[]` reads stay simple.
func (r ClassifyResult) ToJSON() []byte {
	out := r.Reasons
	if out == nil {
		out = []string{}
	}
	b, err := json.Marshal(out)
	if err != nil {
		// Should never happen for a []string. Defensive fallback so the
		// classifier never blocks the upsert.
		return []byte(`[]`)
	}
	return b
}

// ClassifyPullRequest returns the risk verdict + reason list. Pure
// over `inputs`; no DB or network. The webhook ingest path calls
// FetchAndClassify (below) which wraps this with a GitHub round-trip.
//
// The function is exported so service tests can drive it directly.
func ClassifyPullRequest(inputs RiskInputs) ClassifyResult {
	level := db.RiskLevelMedium
	var reasons []string

	bump := func(want db.RiskLevel, reason string) {
		// Promote `level` to the higher of (current, want). Order:
		// low < medium < high < critical.
		if levelRank(want) > levelRank(level) {
			level = want
		}
		reasons = append(reasons, reason)
	}

	// Tier-shifting rules — each can promote the level. We deliberately
	// scan ALL files (not just the first match) so the reasons list
	// shows everything the user should know about.
	hasCode := false
	hasOnlyTestsOrDocs := len(inputs.Files) > 0

	for _, f := range inputs.Files {
		path := f.Filename
		lower := strings.ToLower(path)

		// Docs / tests / lint detection — used at the end if nothing
		// promoted the level, to lower it to "low".
		isDocsOrTests := isDocOrTestPath(lower)
		if !isDocsOrTests {
			hasOnlyTestsOrDocs = false
			hasCode = true
		}

		// Critical promotions.
		if isInfraPath(lower) {
			bump(db.RiskLevelCritical, fmt.Sprintf("infra/k8s/dockerfile change: %s", path))
		}
		if isMigrationPath(lower) && hasDestructiveSQL(f.Patch) {
			bump(db.RiskLevelCritical, fmt.Sprintf("migration with DROP/DELETE: %s", path))
		}
		// API-breaking heuristic: a removal in a top-level "exports"
		// barrel ("packages/*/index.ts" etc.) or a Go file with a
		// removed `func`/`type` exports is hard to spot statically;
		// we use a softer signal — a "removed" status on a file the
		// rest of the codebase imports as a library entry point.
		if f.Status == "removed" && isLibraryEntryPath(lower) {
			bump(db.RiskLevelCritical, fmt.Sprintf("removed library entry: %s", path))
		}

		// High promotions.
		if isMigrationPath(lower) {
			bump(db.RiskLevelHigh, fmt.Sprintf("migration file: %s", path))
		}
		if isAuthHandlerPath(lower) {
			bump(db.RiskLevelHigh, fmt.Sprintf("auth handler change: %s", path))
		}
		if isPaymentHandlerPath(lower) {
			bump(db.RiskLevelHigh, fmt.Sprintf("payment handler change: %s", path))
		}
		if isMemberHandlerPath(lower) {
			bump(db.RiskLevelHigh, fmt.Sprintf("member handler change: %s", path))
		}
		if isAgentRuntimePath(lower) {
			bump(db.RiskLevelHigh, fmt.Sprintf("agent runtime change: %s", path))
		}
	}

	// Title-based fallback — runs regardless of file list. A PR titled
	// "fix(auth): ..." with files we missed should still be flagged.
	titleLower := strings.ToLower(inputs.Title)
	if strings.Contains(titleLower, "[breaking]") || strings.Contains(titleLower, "breaking change") {
		bump(db.RiskLevelHigh, "title declares breaking change")
	}

	// If the file list was empty (workspace token couldn't fetch the
	// /files endpoint, e.g. fork PR without proper auth), fall back
	// to the Phase 1 keyword scan so the badge is at least useful.
	if len(inputs.Files) == 0 {
		if strings.Contains(titleLower, "migration") || strings.Contains(strings.ToLower(inputs.HeadRef), "migration") {
			bump(db.RiskLevelHigh, "title or branch mentions migration (file list unavailable)")
		}
	}

	// Low-tier downgrade — only when nothing promoted the level past
	// medium and EVERY file we did see is docs or tests.
	if level == db.RiskLevelMedium && hasOnlyTestsOrDocs && !hasCode {
		level = db.RiskLevelLow
		reasons = append(reasons, "tests / docs only")
	}

	return ClassifyResult{Level: level, Reasons: reasons}
}

// levelRank gives a numeric ordering for the enum. Used internally by
// the bump() helper.
func levelRank(l db.RiskLevel) int {
	switch l {
	case db.RiskLevelLow:
		return 1
	case db.RiskLevelMedium:
		return 2
	case db.RiskLevelHigh:
		return 3
	case db.RiskLevelCritical:
		return 4
	default:
		return 0
	}
}

// FetchAndClassify is the webhook path's helper. It calls the GitHub
// /pulls/{n}/files endpoint, builds the RiskInputs, runs the
// classifier, and persists the result.
//
// Best-effort: a failure to fetch the file list logs and falls through
// to a degraded title/branch-only classification rather than blocking
// the rest of the webhook dispatch. The risk_classified_at column is
// stamped either way so the backfill job knows it has run.
func (s *Service) FetchAndClassify(ctx context.Context, repoURL string, prRow db.PullRequest, prInfo gh.PullRequest) error {
	owner, repo, err := gh.ParseRepoURL(repoURL)
	if err != nil {
		return fmt.Errorf("ship risk: parse repo url: %w", err)
	}

	inputs := RiskInputs{
		Title:        prRow.Title,
		Body:         textValue(prRow.Body),
		BaseRef:      prRow.BaseRef,
		HeadRef:      prRow.HeadRef,
		ChangedCount: int(prRow.ChangedFiles),
	}

	if s.Github != nil {
		files, err := s.listPullRequestFiles(ctx, owner, repo, int(prRow.PrNumber))
		if err != nil {
			slog.Warn("ship risk: list files failed (degrading to heuristic)",
				"pr_id", uuidString(prRow.ID), "error", err)
		} else {
			inputs.Files = files
		}
	}
	_ = prInfo // reserved for future use; kept in the signature so callers don't need to refactor.

	verdict := ClassifyPullRequest(inputs)
	if _, err := s.Q.UpdatePullRequestRiskProfile(ctx, db.UpdatePullRequestRiskProfileParams{
		ID:          prRow.ID,
		RiskLevel:   verdict.Level,
		RiskReasons: verdict.ToJSON(),
	}); err != nil {
		return fmt.Errorf("ship risk: persist verdict: %w", err)
	}
	return nil
}

// listPullRequestFiles delegates to the GH client. Wrapped so a
// future-cached / batched implementation can drop in here without
// touching FetchAndClassify.
func (s *Service) listPullRequestFiles(ctx context.Context, owner, repo string, prNumber int) ([]gh.PullRequestFile, error) {
	return s.Github.ListPullRequestFiles(ctx, owner, repo, prNumber)
}

// --- Path predicates ----------------------------------------------------

// isMigrationPath catches both Go-style ("server/migrations/...sql") and
// generic Rails/Django/Sequelize folder names. Lowercased input.
func isMigrationPath(p string) bool {
	if strings.HasSuffix(p, ".sql") &&
		(strings.Contains(p, "/migrations/") || strings.Contains(p, "/migrate/")) {
		return true
	}
	// db/migrate/, prisma/migrations/, alembic versions/
	if strings.Contains(p, "db/migrate/") ||
		strings.Contains(p, "prisma/migrations/") ||
		strings.Contains(p, "alembic/versions/") {
		return true
	}
	return false
}

// hasDestructiveSQL flags a migration patch that includes an
// unmistakably destructive statement. We check on the diff fragment,
// not the file body, so an existing destructive statement that the PR
// only modifies adjacent context for doesn't false-flag.
func hasDestructiveSQL(patch string) bool {
	// Diff lines start with "+"; we look at added lines only. A removed
	// "DROP TABLE" is the migration being safer, not less safe.
	upper := strings.ToUpper(patch)
	keywords := []string{
		"+\nDROP TABLE", "+ DROP TABLE", "+DROP TABLE",
		"+\nDROP COLUMN", "+ DROP COLUMN", "+DROP COLUMN",
		"+\nDELETE FROM", "+ DELETE FROM", "+DELETE FROM",
		"+\nTRUNCATE", "+ TRUNCATE", "+TRUNCATE",
	}
	for _, kw := range keywords {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	// Fallback: any added line with DROP/DELETE/TRUNCATE keywords.
	for _, line := range strings.Split(patch, "\n") {
		if !strings.HasPrefix(line, "+") {
			continue
		}
		u := strings.ToUpper(line)
		if strings.Contains(u, "DROP TABLE") ||
			strings.Contains(u, "DROP COLUMN") ||
			strings.Contains(u, "DELETE FROM") ||
			strings.Contains(u, "TRUNCATE") {
			return true
		}
	}
	return false
}

// isInfraPath matches Dockerfile / k8s / terraform / GitHub Actions
// workflow files. These are surfaces where a wrong line genuinely takes
// production down.
func isInfraPath(p string) bool {
	base := filenameBase(p)
	if base == "dockerfile" || strings.HasSuffix(base, ".dockerfile") {
		return true
	}
	if strings.Contains(p, ".github/workflows/") {
		return true
	}
	if strings.Contains(p, "/k8s/") || strings.Contains(p, "/kubernetes/") ||
		strings.Contains(p, "/helm/") || strings.HasSuffix(p, ".tf") ||
		strings.HasSuffix(p, ".tfvars") {
		return true
	}
	return false
}

// isAuthHandlerPath matches the specific auth surface listed in the
// Phase 5 spec. We use a wildcard prefix because handler files in this
// repo follow `auth.go`, `auth_*.go`, `auth/`.
func isAuthHandlerPath(p string) bool {
	return strings.Contains(p, "/handler/auth") ||
		strings.Contains(p, "/internal/auth/") ||
		strings.Contains(p, "/auth_middleware") ||
		strings.HasSuffix(p, "/auth.go")
}

// isPaymentHandlerPath catches anything under the (future) payment
// surface. Today the codebase has no /payment/ folder; included so the
// classifier matches the spec verbatim and stays correct when one
// lands.
func isPaymentHandlerPath(p string) bool {
	return strings.Contains(p, "/payment") ||
		strings.Contains(p, "/billing") ||
		strings.Contains(p, "/stripe")
}

// isMemberHandlerPath matches workspace member / role / permission
// changes — the surface where a bug can lock a workspace owner out.
func isMemberHandlerPath(p string) bool {
	return strings.Contains(p, "/handler/member") ||
		strings.Contains(p, "/handler/membership") ||
		strings.Contains(p, "/handler/invitation") ||
		strings.Contains(p, "/handler/workspace.go")
}

// isAgentRuntimePath catches changes to the agent runtime surfaces —
// daemon, runtime sweeper, agent_task_queue handler. Bugs here mean
// agents stop running for everyone, not just the workspace whose PR
// landed.
func isAgentRuntimePath(p string) bool {
	return strings.Contains(p, "/daemon/") ||
		strings.Contains(p, "/handler/daemon") ||
		strings.Contains(p, "/runtime_sweeper") ||
		strings.Contains(p, "/handler/runtime") ||
		strings.Contains(p, "/handler/agent.go") ||
		strings.Contains(p, "/handler/task")
}

// isLibraryEntryPath flags the "barrel" files whose removal is a
// breaking change for downstream consumers. We're conservative — the
// list is small enough that the classifier doesn't hallucinate.
func isLibraryEntryPath(p string) bool {
	base := filenameBase(p)
	return base == "index.ts" || base == "index.tsx" ||
		base == "mod.go" || base == "exports.ts"
}

// isDocOrTestPath returns true for files we'd downgrade a PR for. The
// classifier requires EVERY file to match this for the downgrade to
// happen — a single non-doc file disqualifies.
func isDocOrTestPath(p string) bool {
	if strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".mdx") {
		return true
	}
	if strings.HasSuffix(p, "_test.go") ||
		strings.HasSuffix(p, ".test.ts") || strings.HasSuffix(p, ".test.tsx") ||
		strings.HasSuffix(p, ".spec.ts") || strings.HasSuffix(p, ".spec.tsx") {
		return true
	}
	if strings.Contains(p, "/__tests__/") || strings.Contains(p, "/e2e/") ||
		strings.Contains(p, "/docs/") || strings.HasPrefix(p, "docs/") {
		return true
	}
	// Lint/format-only files.
	if strings.HasSuffix(p, ".eslintrc") || strings.HasSuffix(p, ".prettierrc") ||
		strings.HasSuffix(p, ".editorconfig") {
		return true
	}
	return false
}

// filenameBase returns the lowercase basename of a slash-separated
// path. We don't pull in `path/filepath` because it expects native
// separators; GitHub paths are always forward-slashed.
func filenameBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// emptyUUID is used by the webhook callback so an empty pgtype.UUID
// doesn't accidentally match any real row.
func emptyUUID() pgtype.UUID { return pgtype.UUID{} }
