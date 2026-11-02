import { maybeRenewSession } from "./session-renewal";

/**
 * The touch-observer half of activity-driven session renewal (MUL-7436).
 *
 * Kept out of the component so it can be tested where mobile's test setup
 * lives — the data layer — rather than requiring a React Native renderer for
 * what is really one decision: notice the touch, decline the gesture.
 *
 * `onStartShouldSetPanResponderCapture` runs in the capture phase, before any
 * child can claim the gesture, and returning false declines to become the
 * responder. That combination is what lets this sit on the path of every touch
 * in the app without changing how a single one behaves.
 */
export const sessionActivityResponderConfig = {
  onStartShouldSetPanResponderCapture: (): boolean => {
    maybeRenewSession();
    // Never true. Returning true here would make this wrapper the responder
    // and swallow every tap, scroll and swipe in the app.
    return false;
  },
};
