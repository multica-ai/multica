import type { Issue, IssueStatusCategory, UpdateIssueRequest } from "@multica/core/types";
import { issueStatusCategory } from "@multica/core/issues";
import { isIssueStatusCategory, type IssueStatusCatalog } from "@multica/core/issue-statuses";

/** The issue fields the gate reads. */
export type GateIssue = Pick<
  Issue,
  "id" | "status" | "status_category" | "assignee_type" | "assignee_id"
>;

/** Payload for the `issue-run-confirm` modal, or null when nothing to confirm. */
export type RunConfirmIntent =
  | {
      issueIds: [string];
      mode: "assign";
      assigneeType: "agent" | "squad";
      assigneeId: string;
    }
  | {
      issueIds: [string];
      mode: "promote";
      status: string;
      assigneeType: "agent" | "squad";
      assigneeId: string;
    };

/**
 * The category a status KEY belongs to — or `null` when nothing can answer.
 *
 * Three states, not two. `catalog.categoryOf` collapses "unknown custom key"
 * into `todo`, which is indistinguishable from a real `todo` and is exactly
 * the guess this gate must not make: it decides whether a write may start an
 * agent, so an unresolvable key has to stay unresolved and let the caller fail
 * safe. (MUL-6463)
 *
 * Resolution order mirrors the server (`issuestatus.Effective`): a category the
 * payload already carries wins, a BUILT-IN key is its own category, and only a
 * custom key needs the workspace catalog.
 */
export function resolveStatusCategory(
  statusKey: string,
  carriedCategory: IssueStatusCategory | undefined,
  catalog: Pick<IssueStatusCatalog, "entryOf">,
): IssueStatusCategory | null {
  const carried = issueStatusCategory({ status: statusKey, status_category: carriedCategory });
  if (carried) return carried;
  const category = catalog.entryOf(statusKey)?.category;
  return category && isIssueStatusCategory(category) ? category : null;
}

/** Categories a promotion can land in without starting a run. */
const NEVER_STARTS = ["backlog", "done", "cancelled"];

/**
 * Whether a status change out of a parking lot starts a run — the client-side
 * mirror of `startsRunOnStatusChange` in
 * `server/internal/service/issue_trigger.go`. Keep the two in step: this one
 * only decides whether to confirm, the server one decides what actually runs.
 *
 * `blocked` releases to `todo` only. An unresolvable origin (`null`) takes the
 * backlog rule, which starts on more targets, because failing toward the dialog
 * is the cheaper mistake — as does an unresolvable target, handled by the
 * caller: only THIS client cannot name its category, and the server may well
 * resolve it to `todo` through the catalog and start the run.
 */
function startsRun(from: IssueStatusCategory | null, target: IssueStatusCategory): boolean {
  if (from === "blocked") return target === "todo";
  return !NEVER_STARTS.includes(target);
}

/**
 * Which confirmation, if any, an issue write needs before it is applied.
 *
 * Both writes that can hand work to an agent confirm; everything else applies
 * directly. Pure so every entry point — issue detail, context menu, table row —
 * routes on one answer instead of re-deriving it (MUL-6463).
 *
 * - **assign**: giving the issue an agent/squad owner. Skipped only when the
 *   issue is KNOWN to be parked, because assigning into the backlog category
 *   never starts a run (`server/internal/service/issue_trigger.go`) and the
 *   dialog would promise something that cannot happen.
 * - **promote**: moving an already-owned issue out of a parking lot — out of
 *   the backlog category, or out of `blocked` and back to `todo` (AERIS-284).
 *   That status change alone starts the run (`RunSourceStatus`), so it earns
 *   the same dialog — for built-in `todo` and every custom Todo-category
 *   status alike.
 *
 * Unresolvable categories fail toward confirming: a dialog the user dismisses
 * costs a click, a silent start costs an unwanted agent run.
 */
export function runConfirmIntent(
  issue: GateIssue,
  updates: Partial<UpdateIssueRequest>,
  catalog: Pick<IssueStatusCatalog, "entryOf">,
): RunConfirmIntent | null {
  const issueCategory = resolveStatusCategory(issue.status, issue.status_category, catalog);
  const parked = issueCategory === "backlog";

  if (
    (updates.assignee_type === "agent" || updates.assignee_type === "squad") &&
    updates.assignee_id &&
    !parked
  ) {
    return {
      issueIds: [issue.id],
      mode: "assign",
      assigneeType: updates.assignee_type,
      assigneeId: updates.assignee_id,
    };
  }

  const owner = issue.assignee_type;
  if (
    updates.status &&
    updates.status !== issue.status &&
    // Unknown counts as possibly-parked: the write may promote, so confirm.
    (parked || issueCategory === "blocked" || issueCategory === null) &&
    (owner === "agent" || owner === "squad") &&
    issue.assignee_id
  ) {
    const target = resolveStatusCategory(updates.status, undefined, catalog);
    // An unresolvable TARGET is possibly-active for the same reason.
    if (target === null || startsRun(issueCategory, target)) {
      return {
        issueIds: [issue.id],
        mode: "promote",
        status: updates.status,
        assigneeType: owner,
        assigneeId: issue.assignee_id,
      };
    }
  }

  return null;
}
