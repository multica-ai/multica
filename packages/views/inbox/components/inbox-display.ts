import type { InboxItem } from "@multica/core/types";

export type InboxReadFilter = "all" | "unread" | "read";

function singleLine(value: string | null | undefined): string {
  return (value ?? "").replace(/\s+/g, " ").trim();
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function stripQuickCreatePrefix(title: string, identifier?: string): string {
  const normalized = singleLine(title);
  if (!normalized) return "";

  if (identifier) {
    const exactPrefix = new RegExp(
      `^Created\\s+${escapeRegExp(identifier)}:\\s*`,
      "i",
    );
    const withoutExactPrefix = normalized.replace(exactPrefix, "");
    if (withoutExactPrefix !== normalized) return withoutExactPrefix.trim();
  }

  return normalized.replace(/^Created\s+[A-Z][A-Z0-9]*-\d+:\s*/i, "").trim();
}

export function getInboxDisplayTitle(item: InboxItem): string {
  const details = item.details ?? {};

  if (item.type === "quick_create_done") {
    const cleanedTitle = stripQuickCreatePrefix(item.title, details.identifier);
    if (cleanedTitle) return cleanedTitle;

    const prompt = singleLine(details.original_prompt);
    if (prompt) return prompt;
  }

  if (item.type === "quick_create_failed") {
    const prompt = singleLine(details.original_prompt);
    if (prompt) return prompt;
  }

  return item.title;
}

export function getQuickCreateFailureDetail(item: InboxItem): string {
  const details = item.details ?? {};
  return singleLine(details.error) || singleLine(item.body);
}

export function filterInboxItemsByReadState(
  items: InboxItem[],
  filter: InboxReadFilter,
): InboxItem[] {
  if (filter === "unread") return items.filter((item) => !item.read);
  if (filter === "read") return items.filter((item) => item.read);
  return items;
}

export function keepSelectedInboxItemVisible(
  items: InboxItem[],
  filteredItems: InboxItem[],
  selectedItemId: string | null | undefined,
): InboxItem[] {
  if (!selectedItemId) return filteredItems;

  const filteredIds = new Set(filteredItems.map((item) => item.id));
  if (filteredIds.has(selectedItemId)) return filteredItems;

  return items.filter(
    (item) => filteredIds.has(item.id) || item.id === selectedItemId,
  );
}
