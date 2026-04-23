/**
 * Feedback funnel instrumentation.
 *
 * Pairs with the backend's `feedback_submitted` event (emitted from
 * `CreateFeedback` after a successful insert) so we can compute a
 * completion rate: users who open the modal → users who actually send.
 * The message content itself is never sent to PostHog; see
 * docs/analytics.md and the backend `FeedbackSubmitted` helper for the
 * PII contract.
 */

import { captureEvent } from "./index";

/**
 * Entry point the user took to reach the Feedback modal. Typed union so
 * future surfaces (keyboard shortcut, error-toast CTA, sidebar menu
 * item) have to extend this list explicitly rather than drift.
 */
export type FeedbackOpenedSource = "help_menu";

/**
 * Fires once on FeedbackModal mount. Workspace id is attached when the
 * modal opens inside a workspace; pre-workspace surfaces (e.g. inbox,
 * onboarding transitions) omit it rather than sending an empty string.
 */
export function captureFeedbackOpened(
  source: FeedbackOpenedSource,
  workspaceId?: string,
): void {
  captureEvent("feedback_opened", {
    source,
    ...(workspaceId ? { workspace_id: workspaceId } : {}),
  });
}
