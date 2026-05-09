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
};

export function useCerebroChatHeaderStrings(): StringTable {
  const { i18n } = useTranslation();
  const lang = (i18n.language ?? "en").toLowerCase();
  if (lang.startsWith("da")) return da;
  return en;
}
