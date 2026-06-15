// CEREBRO-PATCH(member-presence-core): the presence/typing REST client moved to
// @multica/core/presence (TECH-3583). Re-exported here so existing imports
// (use-channel-typing's `postTyping`, tests) keep resolving unchanged.
export {
  fetchPresenceSnapshot,
  pingPresence,
  postTyping,
  type PresenceResponse,
} from "@multica/core/presence";
