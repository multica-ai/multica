import {
  AlertTriangle,
  GitMerge,
  GitBranch,
  MessageSquare,
  Bot,
  Bell,
  PlayCircle,
  type LucideIcon,
} from "lucide-react";
import type { PullRequest } from "@multica/core/types";

// Phase 3 — smart action chips on a PR card.
//
// Given a PR's derived state (state, ci_status, review_decision, mergeable,
// timestamps), `derivePrChips` returns an ORDERED list of chip descriptors.
// The card renders at most the first 1–2; remaining chips overflow into a
// secondary menu. Order matters because the most actionable chip should
// always be first — the priority rules below mirror the spec in
// ROA-139.
//
// Why a derivation function rather than a Zustand selector or a server-
// supplied list?
//   - Pure: no React or Query dependencies, trivially testable.
//   - Workspace-policy free: a PR's state is the only input. The card already
//     receives the PR via TanStack Query; rendering chips off it is one less
//     coupling between the component tree and the workspace store.
//   - Server-side derivation would either require a per-PR enrichment query
//     (extra round-trip) or per-PR fields baked into the list response
//     (which already drifts via the lenient zod schema). Doing it on the
//     client keeps the contract minimal and the rules visible.

export type PrChipVariant = "primary" | "secondary" | "destructive";

export interface PrChip {
  /** Stable React key. Same as `action` today; kept distinct so a future
   *  rule that emits two chips for the same action (e.g. "Merge (squash)"
   *  vs "Merge (rebase)") can give them unique ids. */
  id: string;
  /** Action name — must match the canonical names in
   *  server/internal/service/ship/actions.go and the chip mutation hook. */
  action: string;
  /** i18n key fragment under `ship.chips.<action>.label`. The component
   *  resolves it via the t() selector form so missing translations fall
   *  back to the EN bundle rather than the raw key. */
  labelKey: string;
  /** Icon shown to the left of the label. */
  icon: LucideIcon;
  /** Visual emphasis. `primary` is filled; `secondary` is outline;
   *  `destructive` is red. Mapped to shadcn Button variants in the
   *  component. */
  variant: PrChipVariant;
  /** When true, the chip opens a confirmation dialog before firing. The
   *  dialog uses the `ship.chips.<action>.confirm_*` translation keys. */
  destructive?: boolean;
  /** Builds the request body sent to the chip mutation. Returns undefined
   *  if the chip wants to send an empty body (or a body composed entirely
   *  of server defaults). The card's ChipButton calls this lazily. */
  bodyBuilder?: (pr: PullRequest) => Record<string, unknown> | undefined;
}

const FIVE_DAYS_MS = 5 * 24 * 60 * 60 * 1000;
const ONE_DAY_MS = 24 * 60 * 60 * 1000;

/** "Diagnose CI failure" — top priority when CI is red. The chip kicks off
 *  an agent task; success surfaces a toast pointing at the task. */
const DIAGNOSE_CI_CHIP: PrChip = {
  id: "diagnose_ci_failure",
  action: "diagnose_ci_failure",
  labelKey: "diagnose_ci_failure",
  icon: Bot,
  variant: "primary",
};

/** "Rebase on main" — when the PR conflicts. Uses GitHub's update-branch
 *  endpoint server-side, which is a true rebase only when the PR's head
 *  branch is fast-forwardable; falls back to a merge from main otherwise. */
const REBASE_ON_MAIN_CHIP: PrChip = {
  id: "rebase_on_main",
  action: "rebase_on_main",
  labelKey: "rebase_on_main",
  icon: GitBranch,
  variant: "primary",
};

/** "Merge" — only shows for an approved + green PR. Destructive because it
 *  irreversibly publishes the change. */
const MERGE_CHIP: PrChip = {
  id: "merge",
  action: "merge",
  labelKey: "merge",
  icon: GitMerge,
  variant: "primary",
  destructive: true,
};

/** "Summarize feedback" — async chip. Spawns an agent task that drops a
 *  comment on the PR summarizing all CHANGES_REQUESTED reviewer feedback. */
const SUMMARIZE_FEEDBACK_CHIP: PrChip = {
  id: "summarize_review_feedback",
  action: "summarize_review_feedback",
  labelKey: "summarize_review_feedback",
  icon: MessageSquare,
  variant: "secondary",
};

/** "Nudge author" — friendly comment that pings the author with a default
 *  polite-nudge message. The destructive flag is FALSE here because the
 *  comment can be deleted on GitHub — not destructive in the irreversible
 *  sense. */
const NUDGE_AUTHOR_CHIP: PrChip = {
  id: "nudge_author",
  action: "nudge_author",
  labelKey: "nudge_author",
  icon: Bell,
  variant: "secondary",
};

/** "Run smoke tests" — for merged PRs whose head SHA isn't yet on staging.
 *  Body must include the staging environment id; the caller wires a closure
 *  with the snapshot's staging env id. */
function makeRunSmokeTestsChip(stagingEnvId: string): PrChip {
  return {
    id: "run_smoke_tests",
    action: "run_smoke_tests",
    labelKey: "run_smoke_tests",
    icon: PlayCircle,
    variant: "secondary",
    destructive: true,
    bodyBuilder: () => ({ environment_id: stagingEnvId }),
  };
}

/** Inputs the derivation function needs that aren't on the PullRequest row.
 *  - `stagingEnv`: snapshot of the project's staging environment, used to
 *    decide whether the PR's head SHA is already deployed there.
 *  - `now`: injected for tests; defaults to `new Date()`. */
export interface PrChipInputs {
  stagingEnv?: { id: string; current_sha: string | null } | null;
  now?: Date;
}

/**
 * Derive the ordered chip list for a PR. The first match in priority order
 * wins for each "category" — once a primary chip is picked we still allow
 * lower-priority secondary chips to follow, but the same chip never appears
 * twice. The card consumer renders the first 1–2; everything else overflows.
 *
 * Priority (per ROA-139):
 *   1. CI failure → Diagnose CI failure
 *   2. Merge conflict → Rebase on main
 *   3. Approved + green CI + open + non-draft + mergeable → Merge
 *   4. CHANGES_REQUESTED → Summarize feedback
 *   5. Open & not updated in 5+ days → Nudge author
 *   6. Merged & head_sha !== staging.current_sha & merged >24h ago → Run smoke tests
 */
export function derivePrChips(
  pr: PullRequest,
  inputs: PrChipInputs = {},
): PrChip[] {
  const chips: PrChip[] = [];
  const now = inputs.now ?? new Date();

  // Closed PRs (non-merged) get no chips. The user can still click through
  // to GitHub; we don't surface "reopen" yet.
  if (pr.state === "closed") return chips;

  const isOpen = pr.state === "open";
  const isMerged = pr.state === "merged";

  if (isOpen) {
    // Rule 1 — CI failing trumps everything because the user can't merge
    // until it's green. Skip when the PR is a draft (failing CI on a draft
    // is normal background noise; the chip would be needless).
    if (!pr.is_draft && pr.ci_status === "failure") {
      chips.push(DIAGNOSE_CI_CHIP);
    }

    // Rule 2 — Merge conflict. Even draft PRs benefit from this chip; the
    // author wants to know before they request review.
    if (pr.mergeable === "CONFLICTING") {
      chips.push(REBASE_ON_MAIN_CHIP);
    }

    // Rule 3 — Ready to land. Strict ALL conditions: approved, green CI,
    // open, not draft, mergeable. If any of those is missing the merge
    // chip would be misleading at best and dangerous at worst.
    if (
      !pr.is_draft &&
      pr.review_decision === "APPROVED" &&
      pr.ci_status === "success" &&
      pr.mergeable === "MERGEABLE"
    ) {
      chips.push(MERGE_CHIP);
    }

    // Rule 4 — Reviewer wants changes. Surface a chip that asks the agent
    // to summarize the feedback so the author can act on it without reading
    // every comment.
    if (pr.review_decision === "CHANGES_REQUESTED") {
      chips.push(SUMMARIZE_FEEDBACK_CHIP);
    }

    // Rule 6 — Stale. 5-day threshold; same window as the inbox digest's
    // "needs attention" filter so the two surfaces agree on what counts as
    // forgotten. Skip when the PR is a draft (drafts age intentionally).
    if (!pr.is_draft && pr.pr_updated_at) {
      const updatedAt = new Date(pr.pr_updated_at).getTime();
      if (
        Number.isFinite(updatedAt) &&
        now.getTime() - updatedAt >= FIVE_DAYS_MS
      ) {
        chips.push(NUDGE_AUTHOR_CHIP);
      }
    }
  }

  if (isMerged) {
    // Rule 7 — A merged PR whose head_sha is NOT what's currently on
    // staging, AND that landed more than 24h ago. The 24h delay is to
    // avoid offering the chip while the staging deploy is still rolling
    // out; the default deploy cycle in this codebase is sub-hour, so
    // 24h is a comfortable margin.
    const staging = inputs.stagingEnv;
    if (
      staging?.id &&
      pr.head_sha &&
      staging.current_sha !== pr.head_sha &&
      pr.pr_merged_at
    ) {
      const mergedAt = new Date(pr.pr_merged_at).getTime();
      if (
        Number.isFinite(mergedAt) &&
        now.getTime() - mergedAt >= ONE_DAY_MS
      ) {
        chips.push(makeRunSmokeTestsChip(staging.id));
      }
    }
  }

  return chips;
}

// Re-export the internal chip constants so tests can assert on them by
// reference. Not exposed via the package barrel — view consumers should
// only depend on the public hook + types.
export const __testing__ = {
  DIAGNOSE_CI_CHIP,
  REBASE_ON_MAIN_CHIP,
  MERGE_CHIP,
  SUMMARIZE_FEEDBACK_CHIP,
  NUDGE_AUTHOR_CHIP,
  AlertTriangleIcon: AlertTriangle,
};
