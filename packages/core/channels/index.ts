// CEREBRO-PATCH(channels-index-rename-participants): expose channel rename + participant mutations (JEH-700)
// CEREBRO-PATCH(core-channels-index): JEH-699 — re-export listen-mode queries/mutations alongside the upstream channel hooks.
export {
  channelKeys,
  channelListOptions,
  channelDetailOptions,
  channelAgentSettingsOptions,
  channelPermissionsOptions, // CEREBRO-PATCH(core-channels-perms): TECH-3698
} from "./queries";
export {
  useCreateChannel,
  useMarkChannelRead,
  useUpdateChannel,
  useToggleChannelParticipant,
  useSetChannelAgentListenMode,
  useUpdateChannelPermissions, // CEREBRO-PATCH(core-channels-perms): TECH-3698
} from "./mutations";
