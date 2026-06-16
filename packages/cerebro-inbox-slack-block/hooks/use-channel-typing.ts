// TECH-3664 — the channel/DM typing hook moved to @multica/core/presence so
// packages/views can use it too (slack-block depends on views, so views cannot
// import from here). Re-exported to keep existing slack-block imports stable.
export {
  useChannelTyping,
  type UseChannelTypingResult,
} from "@multica/core/presence";
