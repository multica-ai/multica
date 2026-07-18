// Classifies a manual "run now" outcome into a toast (MUL-4525). Pure so it can
// be unit-tested across every run-status class without mounting the page.
//
// The response schema deliberately accepts any status string for forward
// compatibility, so success must be a WHITELIST — never "anything that isn't
// skipped/failed". A future or anomalous-but-parseable status (e.g. "blocked",
// "deferred") must degrade to an error toast, never a false "triggered".

export type RunNowToastKind = "success" | "warning" | "error";

// Only explicit start statuses count as success. Everything else is a
// non-success outcome the user must be warned/errored about.
export function runNowToastKind(status: string | undefined): RunNowToastKind {
  switch (status) {
    case "issue_created":
    case "running":
      return "success";
    case "skipped":
      // Admission blocked / target not ready — recoverable, informational.
      return "warning";
    case "failed":
      return "error";
    default:
      // Unknown / future status: never claim success.
      return "error";
  }
}

// The i18n key (under the autopilots `detail` namespace) describing why a
// non-success run did not trigger, keyed on the stable server reason_code. An
// unknown/absent code degrades to a generic message.
export type RunNowBlockedKey =
  | "run_blocked_invocation_not_allowed"
  | "run_blocked_runtime_offline"
  | "run_blocked_target_unavailable"
  | "run_blocked_attribution"
  | "run_blocked_already_active"
  | "run_blocked_generic";

export function runNowBlockedKey(reasonCode: string | undefined): RunNowBlockedKey {
  switch (reasonCode) {
    case "invocation_not_allowed":
      return "run_blocked_invocation_not_allowed";
    case "runtime_offline":
      return "run_blocked_runtime_offline";
    case "target_unavailable":
      return "run_blocked_target_unavailable";
    case "attribution_blocked":
      return "run_blocked_attribution";
    case "already_active":
      return "run_blocked_already_active";
    default:
      return "run_blocked_generic";
  }
}
