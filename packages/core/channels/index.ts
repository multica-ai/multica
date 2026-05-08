// CEREBRO-PATCH(channels-index-rename-participants): expose channel rename + participant mutations (JEH-700)
// CEREBRO-PATCH(core-channels-index): JEH-699 — re-export listen-mode queries/mutations alongside the upstream channel hooks.
export {
  channelKeys,
  channelListOptions,
  channelDetailOptions,
  channelAgentSettingsOptions,
} from "./queries";
export {
  useCreateChannel,
  useMarkChannelRead,
  useUpdateChannel,
  useToggleChannelParticipant,
  useSetChannelAgentListenMode,
} from "./mutations";
