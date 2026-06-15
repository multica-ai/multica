"use client";

// CEREBRO-PATCH(member-presence-core): the member-presence backbone moved to
// @multica/core/presence (TECH-3583) so @multica/views can read it too — this
// package depends on @multica/views, so it can't be the home of a hook the
// shared ActorAvatar needs. Re-exported here to keep existing imports stable.
// The context-aware hook now reads the single workspace-wide heartbeat mounted
// in the app shell instead of opening a second subscription.
export {
  useMemberPresence,
  type UseMemberPresenceResult,
} from "@multica/core/presence";
