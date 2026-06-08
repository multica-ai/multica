"use client";

// Self-contained string table for the cerebro chat-session header
// (rename / archive / convert-to-issue). Mirrors the cerebro-inbox approach
// — react-i18next is used directly so this package doesn't pull a circular
// dependency on @multica/views.
import { useTranslation } from "react-i18next";

type StringTable = {
  edit_title_aria: string;
  save_title: string;
  cancel_edit: string;
  more_actions: string;
  archive: string;
  unarchive: string;
  convert_to_issue: string;
  archived_badge: string;
  archive_dialog_title: string;
  archive_dialog_description: string;
  archive_confirm: string;
  archive_confirming: string;
  unarchive_dialog_title: string;
  unarchive_dialog_description: string;
  unarchive_confirm: string;
  convert_dialog_title: string;
  convert_dialog_description: string;
  convert_confirm: string;
  convert_confirming: string;
  cancel: string;
  untitled: string;
  toast_converted: string;
  toast_convert_failed: string;
  // JEH-736 — session price chip + token breakdown tooltip.
  session_price_label: string;
  session_price_aria: string;
  session_token_breakdown_input: string;
  session_token_breakdown_output: string;
  session_token_breakdown_cache_read: string;
  session_token_breakdown_cache_write: string;
  session_token_breakdown_runs: string;
};

const en: StringTable = {
  edit_title_aria: "Edit chat session title",
  save_title: "Save",
  cancel_edit: "Cancel",
  more_actions: "Session actions",
  archive: "Archive session",
  unarchive: "Unarchive session",
  convert_to_issue: "Convert to issue",
  archived_badge: "Archived",
  archive_dialog_title: "Archive chat session",
  archive_dialog_description:
    "The session will be moved to your archive. You'll keep the conversation history but can no longer send messages until you unarchive it.",
  archive_confirm: "Archive",
  archive_confirming: "Archiving…",
  unarchive_dialog_title: "Unarchive chat session",
  unarchive_dialog_description:
    "The session will become active again so you can resume the conversation.",
  unarchive_confirm: "Unarchive",
  convert_dialog_title: "Convert to issue",
  convert_dialog_description:
    "A new issue will be created with this session's title and a transcript of the conversation. The chat session itself stays put.",
  convert_confirm: "Create issue",
  convert_confirming: "Creating…",
  cancel: "Cancel",
  untitled: "New chat",
  toast_converted: "Issue created",
  toast_convert_failed: "Failed to create issue",
  session_price_label: "Session price",
  session_price_aria: "Session price and token breakdown",
  session_token_breakdown_input: "Input",
  session_token_breakdown_output: "Output",
  session_token_breakdown_cache_read: "Cache read",
  session_token_breakdown_cache_write: "Cache write",
  session_token_breakdown_runs: "Runs",
};

const da: StringTable = {
  edit_title_aria: "Edit chat session title",
  save_title: "Save",
  cancel_edit: "Cancel",
  more_actions: "Session actions",
  archive: "Archive session",
  unarchive: "Unarchive session",
  convert_to_issue: "Convert to issue",
  archived_badge: "Archived",
  archive_dialog_title: "Archive chat session",
  archive_dialog_description:
    "The session will be moved to your archive. You'll keep the conversation history but can no longer send messages until you unarchive it.",
  archive_confirm: "Archive",
  archive_confirming: "Archiving…",
  unarchive_dialog_title: "Unarchive chat session",
  unarchive_dialog_description:
    "The session will become active again so you can resume the conversation.",
  unarchive_confirm: "Unarchive",
  convert_dialog_title: "Convert to issue",
  convert_dialog_description:
    "A new issue will be created with this session's title and a transcript of the conversation. The chat session itself stays put.",
  convert_confirm: "Create issue",
  convert_confirming: "Creating…",
  cancel: "Cancel",
  untitled: "New chat",
  toast_converted: "Issue created",
  toast_convert_failed: "Failed to create issue",
  session_price_label: "Session price",
  session_price_aria: "Session price and token breakdown",
  session_token_breakdown_input: "Input",
  session_token_breakdown_output: "Output",
  session_token_breakdown_cache_read: "Cache read",
  session_token_breakdown_cache_write: "Cache write",
  session_token_breakdown_runs: "Runs",
};

export function useCerebroChatHeaderStrings(): StringTable {
  const { i18n } = useTranslation();
  const lang = (i18n.language ?? "en").toLowerCase();
  if (lang.startsWith("da")) return da;
  return en;
}
