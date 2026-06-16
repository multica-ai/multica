// Cerebro extensions over upstream's channels package — net-new functionality
// that lives outside the upstream zone so the validate-cerebro-patches gate
// stays clean. See docs/cerebro-patches.md (`cerebro-channels-favorites`).
export { ChannelAgentInlineRow } from "./channel-agent-inline-row";
// TECH-3664 — in-conversation "is typing…" line for channels/DMs.
export { ChannelTypingIndicator } from "./channel-typing-indicator";
export type { ChannelTypingIndicatorProps } from "./channel-typing-indicator";
export { useChannelFavoritesStore, actorKey, channelKey } from "./favorites-store";
export type { ActorKey, ChannelKey, FavoriteKey } from "./favorites-store";
export { useArchiveChannel, useUnarchiveChannel } from "./archive-mutations";
// TECH-3352 — open-time auto-mark-read for channels/DMs (replaces the inline
// focus-guard effect that left the inbox row stuck as unread when focus
// flickered on open).
export { useChannelAutoMarkRead } from "./use-channel-auto-mark-read";
// TECH-3352 — per-user "remind me" (snooze) + "mark as unread" for channels/DMs.
export { useMuteChannel, useUnmuteChannel, useMarkChannelUnread } from "./state-mutations";
export { CerebroChannelRowActions } from "./channel-row-actions";
// CEREBRO-PATCH(issue-comment-cost): FIR-39 per-comment cost badge shared by
// issue comment cards and channel slack messages (channels reuse the comment
// table, so one query backs both surfaces).
export { CommentCostBadge } from "./comment-cost-badge";
// CEREBRO-PATCH(channel-cost-chip): FIR-39 channel-wide total chip mounted in
// the channel header (mirrors the chat SessionCostChip).
export { ChannelCostChip } from "./channel-cost-chip";
