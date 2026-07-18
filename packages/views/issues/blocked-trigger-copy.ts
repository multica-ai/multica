import type { useT } from "../i18n";

// Localized copy for a blocked @agent / @squad trigger outcome (MUL-4525 §2),
// shared by the composer preview chip and the post-send toast so both name the
// same reason the same way. `reason_code` is the enumeration-safe wire code; the
// label it maps to never reveals the target's identity — the caller supplies the
// name it already has from the user's own mention markup.
type IssuesT = ReturnType<typeof useT<"issues">>["t"];

// Full sentence — for tooltips and other surfaces with room to explain.
export function blockedReasonLabel(reasonCode: string, t: IssuesT): string {
  switch (reasonCode) {
    case "invocation_not_allowed":
      return t(($) => $.comment.trigger_blocked_invocation_not_allowed);
    case "target_unavailable":
      return t(($) => $.comment.trigger_blocked_target_unavailable);
    case "runtime_offline":
      return t(($) => $.comment.trigger_blocked_runtime_offline);
    default:
      return t(($) => $.comment.trigger_blocked_generic);
  }
}

// Short badge — for the inline chip and toast where the target name carries the
// "who" and the reason only needs to say why in a couple of words.
export function blockedShortReasonLabel(reasonCode: string, t: IssuesT): string {
  switch (reasonCode) {
    case "invocation_not_allowed":
      return t(($) => $.comment.trigger_blocked_short_invocation_not_allowed);
    case "target_unavailable":
      return t(($) => $.comment.trigger_blocked_short_target_unavailable);
    case "runtime_offline":
      return t(($) => $.comment.trigger_blocked_short_runtime_offline);
    default:
      return t(($) => $.comment.trigger_blocked_short_generic);
  }
}
