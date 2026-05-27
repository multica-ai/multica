"use client";

// react-i18next is used directly (rather than re-exported via
// @multica/views/i18n) so this package doesn't depend on @multica/views —
// which depends on us — and avoid a circular workspace edge.
import { useTranslation } from "react-i18next";
import type { InboxActionCategory } from "./action-groups";

/**
 * Localized strings for the cerebro inbox row-actions UI. Kept inside this
 * package rather than the upstream views/locales bundles to avoid adding
 * cerebro-only keys there (the per-locale JSON files have a parity test
 * guarding the key sets and don't currently carry per-key CEREBRO-PATCH
 * markers). Migrate when a cerebro locale system lands.
 */
type StringTable = {
  archive_tooltip: string;
  archive_label: string;
  unarchive_tooltip: string;
  unarchive_label: string;
  swipe_unarchive: string;
  more_actions: string;
  mark_read: string;
  mark_unread: string;
  mark_read_tooltip: string;
  mark_unread_tooltip: string;
  mute: string;
  unmute: string;
  mute_tooltip: string;
  unmute_tooltip: string;
  remind_title: string;
  remind_one_hour: string;
  remind_three_hours: string;
  remind_tomorrow_morning: string;
  remind_monday: string;
  remind_custom: string;
  remind_save: string;
  remind_cancel: string;
  swipe_archive: string;
  drawer_title: string;
  /** Prefix shown on a row in the Muted filter, e.g. "Muted til 08:00". */
  muted_until_prefix: string;
  planned_for_prefix: string;
  reminder_overdue_prefix: string;
  /** Badge label on the dedicated reminder row (FIR-2364). */
  reminder_label: string;
  // FIR-2115 — "Group by → Action" bucket headers. English in every locale by
  // product decision. Names follow the agreed product wording: Unread, Running,
  // Done, Waiting.
  action_act_now: string;
  action_watching: string;
  action_waiting: string;
  action_calm: string;
  // FIR-2385 — private-agent run-request row.
  run_request_label: string;
  run_request_by_prefix: string;
  run_request_run: string;
  run_request_running: string;
};

/** Action-bucket headers, identical across locales (see StringTable note). */
const actionLabels = {
  action_act_now: "Unread",
  action_watching: "Running",
  action_waiting: "Waiting",
  action_calm: "Done",
} as const;

const en: StringTable = {
  archive_tooltip: "Archive (e)",
  archive_label: "Archive",
  unarchive_tooltip: "Unarchive",
  unarchive_label: "Unarchive",
  swipe_unarchive: "Unarchive",
  more_actions: "More actions",
  mark_read: "Mark as read",
  mark_unread: "Mark as unread",
  mark_read_tooltip: "Mark as read",
  mark_unread_tooltip: "Mark as unread",
  mute: "Remind me",
  unmute: "Unmute",
  mute_tooltip: "Remind me",
  unmute_tooltip: "Unmute",
  remind_title: "Remind me",
  remind_one_hour: "In 1 hour",
  remind_three_hours: "In 3 hours",
  remind_tomorrow_morning: "Tomorrow morning",
  remind_monday: "Monday morning",
  remind_custom: "Choose time",
  remind_save: "Save",
  remind_cancel: "Cancel",
  swipe_archive: "Archive",
  drawer_title: "Actions",
  muted_until_prefix: "Muted until",
  planned_for_prefix: "Planned for",
  reminder_overdue_prefix: "Overdue since",
  reminder_label: "Reminder",
  run_request_label: "Run request",
  run_request_by_prefix: "Requested by",
  run_request_run: "Run",
  run_request_running: "Starting…",
  ...actionLabels,
};

const da: StringTable = {
  archive_tooltip: "Arkivér (e)",
  archive_label: "Arkivér",
  unarchive_tooltip: "Gendan",
  unarchive_label: "Gendan",
  swipe_unarchive: "Gendan",
  more_actions: "Flere handlinger",
  mark_read: "Marker som læst",
  mark_unread: "Marker som ulæst",
  mark_read_tooltip: "Marker som læst",
  mark_unread_tooltip: "Marker som ulæst",
  mute: "Påmind mig",
  unmute: "Unmute",
  mute_tooltip: "Påmind mig",
  unmute_tooltip: "Unmute",
  remind_title: "Påmind mig",
  remind_one_hour: "Om 1 time",
  remind_three_hours: "Om 3 timer",
  remind_tomorrow_morning: "I morgen tidlig",
  remind_monday: "Mandag morgen",
  remind_custom: "Vælg tidspunkt",
  remind_save: "Gem",
  remind_cancel: "Annuller",
  swipe_archive: "Arkivér",
  drawer_title: "Handlinger",
  muted_until_prefix: "Muted til",
  planned_for_prefix: "Planlagt til",
  reminder_overdue_prefix: "Forfalden siden",
  reminder_label: "Påmindelse",
  run_request_label: "Kør-anmodning",
  run_request_by_prefix: "Anmodet af",
  run_request_run: "Kør",
  run_request_running: "Starter…",
  ...actionLabels,
};

// JEH-1322 — zh-Hans table for cerebro inbox row actions. Without this the
// `useCerebroInboxStrings` hook silently fell back to English under zh-Hans,
// surfacing an "Unarchive" aria-label on archived rows. Terms align with the
// upstream zh-Hans inbox.json glossary (归档 / 取消归档 / 标为已读).
const zhHans: StringTable = {
  archive_tooltip: "归档 (e)",
  archive_label: "归档",
  unarchive_tooltip: "取消归档",
  unarchive_label: "取消归档",
  swipe_unarchive: "取消归档",
  more_actions: "更多操作",
  mark_read: "标为已读",
  mark_unread: "标为未读",
  mark_read_tooltip: "标为已读",
  mark_unread_tooltip: "标为未读",
  mute: "提醒我",
  unmute: "取消静音",
  mute_tooltip: "提醒我",
  unmute_tooltip: "取消静音",
  remind_title: "提醒我",
  remind_one_hour: "1 小时后",
  remind_three_hours: "3 小时后",
  remind_tomorrow_morning: "明早",
  remind_monday: "周一早上",
  remind_custom: "选择时间",
  remind_save: "保存",
  remind_cancel: "取消",
  swipe_archive: "归档",
  drawer_title: "操作",
  muted_until_prefix: "静音至",
  planned_for_prefix: "计划于",
  reminder_overdue_prefix: "已逾期",
  reminder_label: "提醒",
  run_request_label: "运行请求",
  run_request_by_prefix: "请求者",
  run_request_run: "运行",
  run_request_running: "正在启动…",
  ...actionLabels,
};

/**
 * Pure language-tag → StringTable picker. Exported separately from the hook
 * so it can be unit-tested without a React renderer.
 */
export function pickCerebroInboxStrings(language: string | undefined): StringTable {
  const lang = (language ?? "en").toLowerCase();
  if (lang.startsWith("da")) return da;
  if (lang.startsWith("zh")) return zhHans;
  return en;
}

/**
 * Returns the active StringTable for the user's current i18n language.
 * Falls back to English for any unsupported locale; "da" / "da-DK" select
 * the Danish table, and "zh" / "zh-Hans" / "zh-CN" select the simplified
 * Chinese table.
 */
export function useCerebroInboxStrings(): StringTable {
  const { i18n } = useTranslation();
  return pickCerebroInboxStrings(i18n.language);
}

/**
 * FIR-2115 — "Group by → Action" bucket headers keyed by category, ready to
 * hand to `bucketizeInboxAction`. Identical across locales today, but routed
 * through the string table so a future localization lands in one place.
 */
export function useInboxActionGroupLabels(): Record<InboxActionCategory, string> {
  const s = useCerebroInboxStrings();
  return {
    act_now: s.action_act_now,
    watching: s.action_watching,
    waiting: s.action_waiting,
    calm: s.action_calm,
  };
}
