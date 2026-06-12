// TECH-3422: Slack-block left rail for the dynamic inbox — people with live
// presence dots, DMs, and channels. Plus the two live hooks (member presence,
// channel typing) so other surfaces can reuse the presence/typing wiring.
export { SlackBlock } from "./components/slack-block";
export type { SlackBlockProps } from "./components/slack-block";
export { useMemberPresence } from "./hooks/use-member-presence";
export type { UseMemberPresenceResult } from "./hooks/use-member-presence";
export { useChannelTyping } from "./hooks/use-channel-typing";
export type { UseChannelTypingResult } from "./hooks/use-channel-typing";
