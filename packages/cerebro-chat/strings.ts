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
  // TECH-3489 — chat row-actions menu (inbox).
  mark_read: string;
  rename: string;
  rename_dialog_title: string;
  rename_placeholder: string;
  rename_save: string;
  delete: string;
  delete_dialog_title: string;
  delete_dialog_description: string;
  delete_confirm: string;
  delete_confirming: string;
  toast_archive_failed: string;
  toast_unarchive_failed: string;
  toast_delete_failed: string;
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
  mark_read: "Mark as read",
  rename: "Rename",
  rename_dialog_title: "Rename chat",
  rename_placeholder: "Chat title",
  rename_save: "Save",
  delete: "Delete",
  delete_dialog_title: "Delete chat session",
  delete_dialog_description:
    "The session and its conversation history will be permanently deleted. This can't be undone.",
  delete_confirm: "Delete",
  delete_confirming: "Deleting…",
  toast_archive_failed: "Couldn't archive the chat",
  toast_unarchive_failed: "Couldn't reopen the chat",
  toast_delete_failed: "Couldn't delete the chat",
  session_price_label: "Session price",
  session_price_aria: "Session price and token breakdown",
  session_token_breakdown_input: "Input",
  session_token_breakdown_output: "Output",
  session_token_breakdown_cache_read: "Cache read",
  session_token_breakdown_cache_write: "Cache write",
  session_token_breakdown_runs: "Runs",
};

const da: StringTable = {
  edit_title_aria: "Rediger sessionens titel",
  save_title: "Gem",
  cancel_edit: "Annullér",
  more_actions: "Handlinger",
  archive: "Arkivér session",
  unarchive: "Genåbn session",
  convert_to_issue: "Konvertér til issue",
  archived_badge: "Arkiveret",
  archive_dialog_title: "Arkivér chat-session",
  archive_dialog_description:
    "Sessionen flyttes til arkivet. Samtalen bevares, men du kan ikke sende nye beskeder, før du genåbner den.",
  archive_confirm: "Arkivér",
  archive_confirming: "Arkiverer…",
  unarchive_dialog_title: "Genåbn chat-session",
  unarchive_dialog_description:
    "Sessionen bliver aktiv igen, så du kan fortsætte samtalen.",
  unarchive_confirm: "Genåbn",
  convert_dialog_title: "Konvertér til issue",
  convert_dialog_description:
    "Der oprettes et nyt issue med sessionens titel og en udskrift af samtalen. Selve chat-sessionen bevares.",
  convert_confirm: "Opret issue",
  convert_confirming: "Opretter…",
  cancel: "Annullér",
  untitled: "Ny chat",
  toast_converted: "Issue oprettet",
  toast_convert_failed: "Kunne ikke oprette issue",
  mark_read: "Markér som læst",
  rename: "Omdøb",
  rename_dialog_title: "Omdøb chat",
  rename_placeholder: "Chat-titel",
  rename_save: "Gem",
  delete: "Slet",
  delete_dialog_title: "Slet chat-session",
  delete_dialog_description:
    "Sessionen og hele samtalen slettes permanent. Det kan ikke fortrydes.",
  delete_confirm: "Slet",
  delete_confirming: "Sletter…",
  toast_archive_failed: "Kunne ikke arkivere chatten",
  toast_unarchive_failed: "Kunne ikke genåbne chatten",
  toast_delete_failed: "Kunne ikke slette chatten",
  session_price_label: "Sessions-pris",
  session_price_aria: "Sessions-pris og token-fordeling",
  session_token_breakdown_input: "Input",
  session_token_breakdown_output: "Output",
  session_token_breakdown_cache_read: "Cache read",
  session_token_breakdown_cache_write: "Cache write",
  session_token_breakdown_runs: "Kørsler",
};

export function useCerebroChatHeaderStrings(): StringTable {
  const { i18n } = useTranslation();
  const lang = (i18n.language ?? "en").toLowerCase();
  if (lang.startsWith("da")) return da;
  return en;
}
