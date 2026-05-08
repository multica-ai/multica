// Cerebro extensions over upstream's channels package — net-new functionality
// that lives outside the upstream zone so the validate-cerebro-patches gate
// stays clean. See docs/cerebro-patches.md (`cerebro-channels-favorites`).
export { ChannelAgentInlineRow } from "./channel-agent-inline-row";
export { useChannelFavoritesStore, actorKey } from "./favorites-store";
export type { ActorKey } from "./favorites-store";
