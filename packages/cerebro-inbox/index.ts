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
export { CerebroInboxTimestamp } from "./components/cerebro-inbox-timestamp";
export { useInboxKeyboardShortcuts } from "./use-inbox-keyboard-shortcuts";
export {
  useMuteInbox,
  useUnmuteInbox,
  useMarkInboxUnread,
  useUnarchiveInbox,
} from "./mutations";
export { isMuted, nextLocalEightAm, formatMutedUntilTime } from "./mute-time";
