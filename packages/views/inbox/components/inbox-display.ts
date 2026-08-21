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

/**
 * Which row the detail pane renders while its selection lags the list's.
 *
 * The pane's key is deferred (`useDeferredValue`) so mounting the issue
 * detail runs at transition priority instead of blocking the click, which
 * means there is a window where the list highlight has already moved and the
 * pane has not. Normally the pane should keep showing the row it is still on
 * — that is the whole point, and it is what the progress bar is covering.
 *
 * The exception is a stale key that no longer resolves: archive advances the
 * selection to the neighbour and drops the actioned row in the same commit,
 * so there is nothing left to hold. Rendering the miss would blink the empty
 * state between the two issues, so that case jumps straight to the live
 * selection instead.
 */
export function resolveDetailItem(
  visibleItems: InboxItem[],
  selectedKey: string,
  detailKey: string,
): InboxItem | null {
  const byKey = (key: string) =>
    visibleItems.find((i) => (i.issue_id ?? i.id) === key) ?? null;
  const deferred = detailKey ? byKey(detailKey) : null;
  if (deferred) return deferred;
  // No stale row to hold: either the pane is empty (nothing selected yet) or
  // the row it was on is gone. Only the second case falls forward.
  if (!detailKey || detailKey === selectedKey) return null;
  return selectedKey ? byKey(selectedKey) : null;
}
