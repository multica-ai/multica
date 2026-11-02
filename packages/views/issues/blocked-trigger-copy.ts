import type { useT } from "../i18n";

// Shared blocked-trigger labels for preview and post-send feedback.
// Do not infer more than reason_code guarantees: invocation_not_allowed covers
// both an inaccessible target and one that does not resolve. Labels disclose no
// target identity beyond the caller's existing mention markup.
//
// Keep remedies distinct for an unbound agent, an offline runtime, an unusable CLI,
// denied runtime access, and a missing runtime profile.
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
    case "runtime_unusable":
      return t(($) => $.comment.trigger_blocked_runtime_unusable);
    case "runtime_profile_missing":
      return t(($) => $.comment.trigger_blocked_runtime_profile_missing);
    case "agent_runtime_required":
      return t(($) => $.comment.trigger_blocked_agent_runtime_required);
    case "runtime_access_denied":
      return t(($) => $.comment.trigger_blocked_runtime_access_denied);
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
    case "runtime_unusable":
      return t(($) => $.comment.trigger_blocked_short_runtime_unusable);
    case "runtime_profile_missing":
      return t(($) => $.comment.trigger_blocked_short_runtime_profile_missing);
    case "agent_runtime_required":
      return t(($) => $.comment.trigger_blocked_short_agent_runtime_required);
    case "runtime_access_denied":
      return t(($) => $.comment.trigger_blocked_short_runtime_access_denied);
    default:
      return t(($) => $.comment.trigger_blocked_short_generic);
  }
}
