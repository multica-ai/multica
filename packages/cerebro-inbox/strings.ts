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

/**
 * Returns the active StringTable for the user's current i18n language.
 * Falls back to English for any non-Danish locale; "da" / "da-DK" both
 * select the Danish table.
 */
export function useCerebroInboxStrings(): StringTable {
  const { i18n } = useTranslation();
  const lang = (i18n.language ?? "en").toLowerCase();
  if (lang.startsWith("da")) return da;
  return en;
}
