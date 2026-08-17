import type { InboxItem } from "@multica/core/types";

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

  if (isQuickCreateOutcome(item.type)) {
    const prompt = singleLine(details.original_prompt);
    if (prompt) return prompt;
  }

  return item.title;
}

/**
 * The two non-success quick-create outcomes. They share a row shape (original
 * prompt + recovery affordance) but must never share failure wording: the
 * unconfirmed outcome means we could not verify the result, not that it failed.
 */
export function isQuickCreateOutcome(type: InboxItem["type"]): boolean {
  return type === "quick_create_failed" || type === "quick_create_unconfirmed";
}

export function getQuickCreateOutcomeDetail(item: InboxItem): string {
  const details = item.details ?? {};
  return singleLine(details.error) || singleLine(item.body);
}
