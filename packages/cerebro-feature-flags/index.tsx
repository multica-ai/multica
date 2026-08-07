export {
  useFeatureFlag,
  useFeatureFlagsQuery,
  useWorkspaceEffectiveFlag,
  useSetFeatureFlagMutation,
  useSetWorkspaceFeatureFlagMutation,
} from "./api";
export {
  useCerebroFeatureFlagsStore,
  useFlagValue,
  useFlagLocked,
  useWorkspaceFlagValue,
  resolveWorkspaceFlag,
} from "./store";
export {
  CEREBRO_FLAG_DEFAULTS,
  CEREBRO_FLAGS,
  CEREBRO_FLAG_GROUPS,
  CEREBRO_FLAG_SUBGROUPS,
  CEREBRO_FLAG_SUBGROUP_OF,
  flagsForGroup,
  subgroupsForGroup,
  flagsForSubgroup,
  ungroupedFlags,
} from "./registry";
export type {
  CerebroFlagKey,
  CerebroFlagDefinition,
  CerebroFlagGroup,
  CerebroFlagGroupKey,
  CerebroFlagSubgroup,
} from "./registry";
export { CerebroFeatureFlagsTab, FlagRow as CerebroFlagRow } from "./settings-tab";
export { cerebroFeatureFlagTabs } from "./settings-tabs";
export {
  useSecretaryCriteria,
  useSetSecretaryCriteria,
  readSecretaryCriteria,
  DEFAULT_SECRETARY_CRITERIA,
  SECRETARY_CRITERIA_KEY,
} from "./secretary-criteria";
export type { SecretaryCriteria } from "./secretary-criteria";
export {
  useDictationSettings,
  useSetDictationSettings,
  readDictationSettings,
  glossaryToHint,
  DEFAULT_DICTATION_SETTINGS,
  DICTATION_SETTINGS_KEY,
} from "./dictation-settings";
export type { DictationSettings } from "./dictation-settings";

// FIR-4350 — Chat placement: where each conversation type is shown. Lives here
// because both the inbox packages and views need the hiding predicate, and both
// already depend on this package (a home in cerebro-chat-page would close a
// package cycle through views).
export {
  CHAT_PLACEMENT_KEY,
  CONVERSATION_KINDS,
  DEFAULT_PLACEMENT,
  canTurnOff,
  conversationKindOfChannel,
  hiddenFromInbox,
  readPlacement,
  setPlace,
  showsInChat,
  showsInInbox,
  type ChatPlacement,
  type ConversationKind,
  type PlacementEntry,
} from "./chat-placement";
export { useChatPlacement } from "./use-chat-placement";
export { isConversationHidden, useHiddenConversationKinds } from "./chat-inbox-hiding";
export { countUnreadConversations, useChatUnreadCount } from "./use-chat-unread-count";
export {
  channelHasChatUnread,
  channelChatUnreadBadge,
} from "./channel-chat-unread";
export {
  buildUnreadChannelThreads,
  type UnreadChannelThread,
} from "./channel-thread-entries";
export { ChatPlacementSettings } from "./chat-placement-settings";
export {
  CHAT_PAGE_SETTINGS_KEY,
  DEFAULT_CHAT_PAGE_SETTINGS,
  readChatPageSettings,
  type ChatPageSettings,
  type ChatPageSort,
  type ChatPageGroupBy,
} from "./chat-page-settings";
export { useChatPageSettings } from "./use-chat-page-settings";
