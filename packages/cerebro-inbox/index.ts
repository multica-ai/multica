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
} from "./components/cerebro-inbox-row-actions";
export { CerebroUnarchiveToolbarButton } from "./components/cerebro-unarchive-toolbar-button";
export { CerebroInboxTimestamp } from "./components/cerebro-inbox-timestamp";
export { CerebroInboxReminderRow } from "./components/cerebro-inbox-reminder-row";
export { CerebroInboxRunRequestRow } from "./components/cerebro-inbox-run-request-row";
export { useInboxKeyboardShortcuts } from "./use-inbox-keyboard-shortcuts";
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
