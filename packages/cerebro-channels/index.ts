// Cerebro extensions over upstream's channels package — net-new functionality
// that lives outside the upstream zone so the validate-cerebro-patches gate
// stays clean. See docs/cerebro-patches.md (`cerebro-channels-favorites`).
export { ChannelAgentInlineRow } from "./channel-agent-inline-row";
export { useChannelFavoritesStore, actorKey } from "./favorites-store";
export type { ActorKey } from "./favorites-store";
export { useArchiveChannel, useUnarchiveChannel } from "./archive-mutations";
// CEREBRO-PATCH(issue-comment-cost): FIR-39 per-comment cost badge shared by
// issue comment cards and channel slack messages (channels reuse the comment
// table, so one query backs both surfaces).
export { CommentCostBadge } from "./comment-cost-badge";
// CEREBRO-PATCH(channel-cost-chip): FIR-39 channel-wide total chip mounted in
// the channel header (mirrors the chat SessionCostChip).
export { ChannelCostChip } from "./channel-cost-chip";
