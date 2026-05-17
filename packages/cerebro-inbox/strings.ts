"use client";

// react-i18next is used directly (rather than re-exported via
// @multica/views/i18n) so this package doesn't depend on @multica/views —
// which depends on us — and avoid a circular workspace edge.
import { useTranslation } from "react-i18next";

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
  swipe_archive: string;
  drawer_title: string;
  /** Prefix shown on a row in the Muted filter, e.g. "Muted til 08:00". */
  muted_until_prefix: string;
};

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
  mute: "Mute until 8 AM",
  unmute: "Unmute",
  mute_tooltip: "Mute until 8 AM",
  unmute_tooltip: "Unmute",
  swipe_archive: "Archive",
  drawer_title: "Actions",
  muted_until_prefix: "Muted until",
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
  mute: "Mute til kl. 08",
  unmute: "Unmute",
  mute_tooltip: "Mute til kl. 08",
  unmute_tooltip: "Unmute",
  swipe_archive: "Arkivér",
  drawer_title: "Handlinger",
  muted_until_prefix: "Muted til",
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
  mute: "静音至早上 8 点",
  unmute: "取消静音",
  mute_tooltip: "静音至早上 8 点",
  unmute_tooltip: "取消静音",
  swipe_archive: "归档",
  drawer_title: "操作",
  muted_until_prefix: "静音至",
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
