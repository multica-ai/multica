package cascade

import (
	"fmt"
	"strings"
)

// BlockParams collects everything the cascade execution block needs to
// render. Populated by PR4's worker just before spawning a cascade
// run; passed through TaskContextForEnv.CascadeMarkdown into the
// CLAUDE.md template.
type BlockParams struct {
	// IssueID is the multica issue UUID. Used by the agent to call
	// `multica issue ...` for state updates.
	IssueID string

	// PUL is the human-readable identifier like "PUL-102". Reproduced
	// in the rendered block so the agent recognises which cascade it
	// is on even when no DB call is needed.
	PUL string

	// PlanRepoCloneURL is the HTTPS URL the agent will clone /
	// fetch+reset on each wake-up (A7 plans-multica freshness).
	// Without it the agent cannot read the plan, so an empty value
	// makes the block render an error fragment that fails the agent
	// loudly rather than guessing.
	PlanRepoCloneURL string

	// PlanFilePath is the path inside the plans repo, e.g.
	// `Multica/2026-05-13-pul-102-event-driven-multi-pr-autonomy.md`.
	PlanFilePath string

	// CurrentStep / TotalPRs come from issue.cascade_progress. Zero
	// values mean "atomic init has not run yet" — the agent's first
	// step is the A4 atomic UPDATE before `gh pr create`.
	CurrentStep int
	TotalPRs    int

	// BranchName is the per-task worktree branch the agent is on,
	// e.g. `agent-2/pul-102-foo`. Used by the auto-rebase step (E5).
	BranchName string

	// MainBranch is the upstream main / default branch to rebase
	// onto. Defaults to "main" when empty.
	MainBranch string
}

// RenderBlock produces the markdown that gets injected into the
// agent's CLAUDE.md / AGENTS.md when the task is part of an active
// cascade. The block is fully self-contained — it does not reference
// any state outside the rendered text — so providers (claude, codex,
// gemini, …) all see identical instructions even though their host
// docs differ in shape.
//
// Empty input → empty output. PR4's worker should never call this
// without a populated BlockParams, but if it does, the template
// suppresses the section entirely so the agent falls back to the
// legacy per-PR approval workflow rather than acting on a half-baked
// cascade context.
func RenderBlock(p BlockParams) string {
	if p.IssueID == "" || p.PlanRepoCloneURL == "" || p.PlanFilePath == "" {
		// Caller bug: refuse to render an incomplete block. The
		// agent treats no-block as "no active cascade", which is
		// strictly safer than a partially-rendered cascade where
		// e.g. the plan path is missing.
		return ""
	}
	if p.MainBranch == "" {
		p.MainBranch = "main"
	}

	var b strings.Builder
	b.WriteString("## Cascade Execution (PUL-102)\n\n")
	fmt.Fprintf(&b, "This task is part of an **active multi-PR cascade**. The user pre-approved the entire plan at cascade-start time — do NOT pause for per-PR confirmation. Drive each PR to merge under the plan, fix CI when it fails, and stop only when the plan is exhausted or you hit a hard blocker.\n\n")

	if p.PUL != "" {
		fmt.Fprintf(&b, "- **Issue:** %s (`%s`)\n", p.PUL, p.IssueID)
	} else {
		fmt.Fprintf(&b, "- **Issue:** `%s`\n", p.IssueID)
	}
	fmt.Fprintf(&b, "- **Plan repo:** `%s`\n", p.PlanRepoCloneURL)
	fmt.Fprintf(&b, "- **Plan path:** `%s`\n", p.PlanFilePath)
	if p.TotalPRs > 0 {
		fmt.Fprintf(&b, "- **Progress:** step %d of %d\n", max(p.CurrentStep, 1), p.TotalPRs)
	} else {
		fmt.Fprintf(&b, "- **Progress:** not yet initialized (run the atomic init below before opening PR1)\n")
	}
	if p.BranchName != "" {
		fmt.Fprintf(&b, "- **Worktree branch:** `%s`\n", p.BranchName)
	}
	b.WriteString("\n")

	b.WriteString("### On every wake-up\n\n")
	b.WriteString("Run this sequence first, in order, every time the daemon spawns you for this cascade. Skipping a step risks acting on a stale plan or branch.\n\n")
	b.WriteString("1. **Refresh the plan** (A7 freshness — the plan can change mid-cascade via /amend-plan):\n\n")
	fmt.Fprintf(&b, "   ```bash\n   cd $PLAN_REPO_CLONE && git fetch origin && git reset --hard origin/main\n   ```\n\n")
	fmt.Fprintf(&b, "   The plan repo clone lives at `$PLAN_REPO_CLONE` (the multica daemon clones `%s` per task). If `$PLAN_REPO_CLONE` is empty, clone it now: `git clone %s $PLAN_REPO_CLONE`.\n\n", p.PlanRepoCloneURL, p.PlanRepoCloneURL)
	fmt.Fprintf(&b, "2. **Read the plan** at `$PLAN_REPO_CLONE/%s`. Parse the YAML frontmatter; you need `total_prs`, `pr_steps[]`, and `amended_at`.\n\n", p.PlanFilePath)
	b.WriteString("3. **Check for plan amendments (G3).** Compare `plan.amended_at` against the cascade's `cascade_started_at` (which you can read via `multica issue get <id> --output json` → look at the cascade fields). If `amended_at > cascade_started_at`, the plan changed mid-cascade — STOP, post a comment explaining, and ask the user to re-approve. Do not proceed on an amended plan.\n\n")
	fmt.Fprintf(&b, "4. **Read cascade progress.** `multica issue get %s --output json` includes `cascade_progress` JSONB. Decode it into `{total_prs, current_step, last_pr_number, last_pr_merged_at, last_event_type}`. Decide your next action from `current_step` and the most recent event.\n\n", p.IssueID)
	b.WriteString("5. **Auto-rebase the worktree branch (E5)** before writing any code:\n\n")
	fmt.Fprintf(&b, "   ```bash\n   git fetch origin\n   git rebase origin/%s\n   ```\n\n", p.MainBranch)
	fmt.Fprintf(&b, "   On rebase conflict: do NOT attempt to resolve mid-cascade — post a comment explaining the conflict and stop. The cascade enters `paused` and waits for human intervention.\n\n")

	b.WriteString("### On the first wake-up (atomic init — A4)\n\n")
	b.WriteString("When `cascade_progress` is still NULL — meaning you are the first run after `/plan-and-implement` flipped `cascade_state='approved'` — you MUST atomically initialize progress BEFORE creating PR1:\n\n")
	b.WriteString("```sql\nUPDATE issue\nSET cascade_progress = $1::jsonb\nWHERE id = $2\n  AND cascade_progress IS NULL\n```\n\n")
	if p.TotalPRs > 0 {
		fmt.Fprintf(&b, "The JSON payload is `{\"total_prs\": %d, \"current_step\": 1, \"last_event_type\": \"cascade_init\"}` (total_prs comes from the plan frontmatter, this cascade has %d).\n\n", p.TotalPRs, p.TotalPRs)
	} else {
		b.WriteString("The JSON payload is `{\"total_prs\": <from plan>, \"current_step\": 1, \"last_event_type\": \"cascade_init\"}` where `total_prs` is read from the plan frontmatter.\n\n")
	}
	b.WriteString("The `WHERE cascade_progress IS NULL` clause guarantees exactly one writer wins under any concurrent-spawn race. If the UPDATE reports 0 rows changed, another run already initialized progress — re-read it via `multica issue get` and continue from the recorded `current_step`.\n\n")

	b.WriteString("### After each successful PR\n\n")
	b.WriteString("When you push the current PR branch and CI passes (or you merge), advance the cascade:\n\n")
	b.WriteString("```sql\nUPDATE issue\nSET cascade_progress = jsonb_set(\n        jsonb_set(\n            jsonb_set(cascade_progress, '{current_step}', to_jsonb(current_step + 1)),\n            '{last_pr_number}', to_jsonb($1::int)\n        ),\n        '{last_event_type}', to_jsonb('pr_merged'::text)\n    ),\n    cascade_last_event_at = now()\nWHERE id = $2\n```\n\n")
	b.WriteString("Then exit the run. The cascade webhook subsystem will wake you again when the next event arrives — either `pr_merged` (for the PR you just pushed once it lands) or `ci_failure` (if CI flunked).\n\n")

	b.WriteString("### When the cascade completes\n\n")
	b.WriteString("If `current_step == total_prs` AND the last PR is merged (you see `pr_merged` with `last_pr_number == final PR's number`), transition the cascade to `completed` and the issue to `deployed`:\n\n")
	b.WriteString("```sql\nUPDATE issue SET cascade_state = 'completed' WHERE id = $1;\n```\n\n")
	b.WriteString("This update is run via admin SQL (Forge / direct DB). A dedicated `multica issue cascade complete` CLI subcommand is planned in PR8 — until then, surface the completion to the user in a comment and let an operator flip `cascade_state` if you cannot reach admin SQL.\n\n")
	fmt.Fprintf(&b, "After the state update, run `multica issue status %s deployed` and post a final summary comment listing all PR links.\n\n", p.IssueID)

	b.WriteString("### Hard stops\n\n")
	b.WriteString("Exit the run immediately (post a comment first) when:\n\n")
	b.WriteString("- Plan was amended mid-cascade (G3).\n")
	b.WriteString("- Auto-rebase fails with conflicts.\n")
	b.WriteString("- Loop guard has fired (`cascade_state = 'loop_guarded'` — you cannot self-recover from this; only the user can clear it).\n")
	b.WriteString("- `cascade_progress.total_prs` disagrees with the count of merged PRs in the plan (anomaly — escalate).\n")
	b.WriteString("- `gh pr create` fails for reasons other than \"PR already exists for this branch\" (which is benign — read the existing PR's state and continue).\n\n")
	return b.String()
}

