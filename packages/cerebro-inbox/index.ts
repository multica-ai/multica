// Cerebro extensions over upstream's inbox.
//
// History: this package previously held inbox-folders + a cerebro-flavored
// InboxPage; both were retired during the upstream channel-first inbox merge
// (JEH-650, migration 9012). The package is now home to the cerebro-only
// row-actions surface (mute, mark-unread, hover menu, swipe gestures, long-
// press) and its supporting hooks.
export {
  CerebroInboxRowActions,
  CerebroSwipeArchive,
  CerebroUnarchiveAction,
  // Exported for the desktop reminder-picker interaction test (FIR-2546).
  CustomReminderPicker,
} from "./components/cerebro-inbox-row-actions";
export { CerebroUnarchiveToolbarButton } from "./components/cerebro-unarchive-toolbar-button";
export { CerebroInboxTimestamp } from "./components/cerebro-inbox-timestamp";
export { CerebroInboxReminderRow } from "./components/cerebro-inbox-reminder-row";
export { CerebroInboxRunRequestRow } from "./components/cerebro-inbox-run-request-row";
// FIR-2643 — "Remind me" on a specific comment, hooked into the shared comment menu.
export { useCommentReminder } from "./use-comment-reminder";
export { useInboxKeyboardShortcuts } from "./use-inbox-keyboard-shortcuts";
// FIR-2684 — always refetch the opened message (timeline + detail) on select.
export { useInboxMessageRefresh, refreshInboxMessageQueries } from "./use-inbox-message-refresh";
export {
  useMuteInbox,
  useUnmuteInbox,
  useMarkInboxUnread,
  useUnarchiveInbox,
  useCreateInboxReminder,
  useRunPrivateAgentRequest,
} from "./mutations";
export {
  isMuted,
  addHours,
  nextLocalEightAm,
  nextLocalNineAm,
  nextBusinessDayNineAm,
  nextMondayNineAm,
  toDateTimeLocalValue,
  formatMutedUntilTime,
  formatPlannedDateTime,
  isReminderOverdue,
} from "./mute-time";
// FIR-2115 — "Group by → Action" inbox grouping.
export {
  INBOX_ACTION_GROUP_BY_OPTION,
  INBOX_ACTION_ORDER,
  inboxActionOrderIndex,
  classifyInboxAction,
  bucketizeInboxAction,
  type InboxActionCategory,
  type InboxActionContext,
  type InboxActionEntry,
} from "./action-groups";
export { useInboxActionGroupLabels } from "./strings";
// FIR-2474 — pin the open inbox row in place until the message is closed.
export { sortInboxEntriesPinned, type PinnedSelection } from "./pin-selected";
export { pinnedBucketizer, type PinnedGroup, type InboxBucket } from "./pin-selected-group";
