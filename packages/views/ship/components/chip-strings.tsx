// Typed i18n lookups per chip action.
//
// The chip components (`ChipButton`, `PrChipRow`) accept a chip whose action
// name is a runtime string. The i18next selector API is statically typed
// against the resource bundle, which means a dynamic key lookup like
// `t($ => $.chips[action].label)` doesn't typecheck — the type system can't
// prove every branch resolves to a string. Casting via `as never` was the
// workaround and it stops working under strict mode.
//
// Instead we route every action through a switch with one static selector
// per branch. The switch is exhaustive over the chip names we surface today;
// unknown action names degrade to a generic fallback (matches CLAUDE.md
// "Enum drift downgrades, not crashes").

import type { TFunction } from "i18next";

// We don't import the `Ship` resource type directly because its key shape is
// internal to the ship.json bundle — a future bundle reshape would force a
// matching change here, but the same is true at every callsite.

type T = TFunction<"ship">;

export function chipLabel(t: T, action: string): string {
  switch (action) {
    case "merge":
      return t(($) => $.chips.merge.label);
    case "rebase_on_main":
      return t(($) => $.chips.rebase_on_main.label);
    case "diagnose_ci_failure":
      return t(($) => $.chips.diagnose_ci_failure.label);
    case "summarize_review_feedback":
      return t(($) => $.chips.summarize_review_feedback.label);
    case "nudge_author":
      return t(($) => $.chips.nudge_author.label);
    case "comment":
      return t(($) => $.chips.comment.label);
    case "dismiss_review":
      return t(($) => $.chips.dismiss_review.label);
    case "run_smoke_tests":
      return t(($) => $.chips.run_smoke_tests.label);
    case "close_as_stale":
      return t(($) => $.chips.close_as_stale.label);
    case "close_pr":
      return t(($) => $.chips.close_pr.label);
    case "talk_to_agent":
      // Caller may interpolate the agent name post-hoc; this returns the
      // base fallback label so unknown agent names still produce
      // useful copy.
      return t(($) => $.chips.talk_to_agent.label_fallback);
    case "pull_into_issue":
      return t(($) => $.chips.pull_into_issue.label);
    case "submit_review":
      return t(($) => $.chips.review.label);
    default:
      // Unknown enum drift — render the action name itself rather than a
      // blank chip. The user can still read the affordance and cancel out.
      return action;
  }
}

export function chipSuccessToast(t: T, action: string): string {
  switch (action) {
    case "merge":
      return t(($) => $.chips.merge.toast_success);
    case "rebase_on_main":
      return t(($) => $.chips.rebase_on_main.toast_success);
    case "diagnose_ci_failure":
      return t(($) => $.chips.diagnose_ci_failure.toast_success);
    case "summarize_review_feedback":
      return t(($) => $.chips.summarize_review_feedback.toast_success);
    case "nudge_author":
      return t(($) => $.chips.nudge_author.toast_success);
    case "comment":
      return t(($) => $.chips.comment.toast_success);
    case "dismiss_review":
      return t(($) => $.chips.dismiss_review.toast_success);
    case "run_smoke_tests":
      return t(($) => $.chips.run_smoke_tests.toast_success);
    case "close_as_stale":
      return t(($) => $.chips.close_as_stale.toast_success);
    case "close_pr":
      return t(($) => $.chips.close_pr.toast_success);
    case "talk_to_agent":
      return t(($) => $.chips.talk_to_agent.toast_success);
    case "pull_into_issue":
      return t(($) => $.chips.pull_into_issue.toast_success);
    case "submit_review":
      return t(($) => $.chips.review.toast_success);
    default:
      return t(($) => $.chips.toast_generic_failure);
  }
}

export function chipInProgressToast(t: T, action: string): string {
  switch (action) {
    case "merge":
      return t(($) => $.chips.merge.toast_in_progress);
    case "rebase_on_main":
      return t(($) => $.chips.rebase_on_main.toast_in_progress);
    case "diagnose_ci_failure":
      return t(($) => $.chips.diagnose_ci_failure.toast_in_progress);
    case "summarize_review_feedback":
      return t(($) => $.chips.summarize_review_feedback.toast_in_progress);
    case "nudge_author":
      return t(($) => $.chips.nudge_author.toast_in_progress);
    case "comment":
      return t(($) => $.chips.comment.toast_in_progress);
    case "dismiss_review":
      return t(($) => $.chips.dismiss_review.toast_in_progress);
    case "run_smoke_tests":
      return t(($) => $.chips.run_smoke_tests.toast_in_progress);
    case "close_as_stale":
      return t(($) => $.chips.close_as_stale.toast_in_progress);
    case "close_pr":
      return t(($) => $.chips.close_pr.toast_in_progress);
    case "talk_to_agent":
      return t(($) => $.chips.talk_to_agent.toast_in_progress);
    case "pull_into_issue":
      return t(($) => $.chips.pull_into_issue.toast_in_progress);
    case "submit_review":
      return t(($) => $.chips.review.toast_in_progress);
    default:
      return "";
  }
}

// Confirmation strings only exist for the destructive subset (merge,
// dismiss_review, run_smoke_tests, close_as_stale). Other actions return
// empty strings here — the component never renders the dialog for those
// chips, but a stray call shouldn't throw.
export function chipConfirmTitle(t: T, action: string): string {
  switch (action) {
    case "merge":
      return t(($) => $.chips.merge.confirm_title);
    case "dismiss_review":
      return t(($) => $.chips.dismiss_review.confirm_title);
    case "run_smoke_tests":
      return t(($) => $.chips.run_smoke_tests.confirm_title);
    case "close_as_stale":
      return t(($) => $.chips.close_as_stale.confirm_title);
    case "close_pr":
      return t(($) => $.chips.close_pr.confirm_title);
    default:
      return "";
  }
}

export function chipConfirmDescription(
  t: T,
  action: string,
  vars: { number: number; title: string },
): string {
  switch (action) {
    case "merge":
      return t(($) => $.chips.merge.confirm_description, vars);
    case "dismiss_review":
      return t(($) => $.chips.dismiss_review.confirm_description, vars);
    case "run_smoke_tests":
      return t(($) => $.chips.run_smoke_tests.confirm_description, vars);
    case "close_as_stale":
      return t(($) => $.chips.close_as_stale.confirm_description, vars);
    case "close_pr":
      return t(($) => $.chips.close_pr.confirm_description, vars);
    default:
      return "";
  }
}

export function chipConfirmAction(t: T, action: string): string {
  switch (action) {
    case "merge":
      return t(($) => $.chips.merge.confirm_action);
    case "dismiss_review":
      return t(($) => $.chips.dismiss_review.confirm_action);
    case "run_smoke_tests":
      return t(($) => $.chips.run_smoke_tests.confirm_action);
    case "close_as_stale":
      return t(($) => $.chips.close_as_stale.confirm_action);
    case "close_pr":
      return t(($) => $.chips.close_pr.confirm_action);
    default:
      return "";
  }
}
