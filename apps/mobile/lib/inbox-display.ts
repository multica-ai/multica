/**
 * Inbox title display helpers.
 *
 * Mirrors packages/views/inbox/components/inbox-display.ts. Keeping behavior
 * identical is required by apps/mobile/CLAUDE.md "Behavioral parity":
 * the title a user sees in the mobile inbox MUST match what they see on
 * web for the same item. When the web version changes, sync this file.
 */
import type { InboxItem, TimelineEntry } from "@multica/core/types";
import { groupInboxItemsByIssue } from "@multica/core/inbox/group";
import { i18n } from "@/lib/i18n/singleton";

function formatResetAt(value: string | undefined): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(i18n.resolvedLanguage ?? i18n.language, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function getAutopilotQuotaBody(item: InboxItem): string | null {
  if (item.type !== "autopilot_quota_exceeded") return item.body;
  const details = item.details ?? {};
  const resetAt = formatResetAt(details.reset_at);
  if (!details.limit || !resetAt) return item.body;
  const t = i18n.t.bind(i18n);
  if (details.autopilot_title) {
    return t("inbox:body.autopilot_title_limit", {
      title: details.autopilot_title,
      limit: details.limit,
      resetAt,
    });
  }
  return t("inbox:body.autopilot_limit", {
    limit: details.limit,
    resetAt,
  });
}

/**
 * Body of an issue-less notice, in the UI locale where the client has the
 * facts to write it and as the server sent it otherwise.
 */
export function getInboxNoticeBody(item: InboxItem): string | null {
  if (item.type === "project_owner_joined" && item.details?.joined_by) {
    return i18n.t("inbox:body.project_owner_joined", {
      name: item.details.joined_by,
    });
  }
  if (item.type === "app_tool_calls_quota_exceeded") {
    const resetAt = formatResetAt(item.details?.reset_at);
    if (!item.details?.limit || !resetAt) return item.body;
    return i18n.t("inbox:body.app_tool_calls_limit", { limit: item.details.limit, resetAt });
  }
  return getAutopilotQuotaBody(item);
}

function singleLine(value: string | null | undefined): string {
  return (value ?? "").replace(/\s+/g, " ").trim();
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function stripQuickCreatePrefix(
  title: string,
  identifier?: string,
): string {
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
  // Resolve system-notice titles through the selected UI locale. Mirror
  // rather than exposing backend fallback copy that can include raw counts.
  switch (item.type) {
    case "autopilot_quota_exceeded":
      return i18n.t("inbox:type.autopilot_quota_exceeded");
    case "app_tool_calls_quota_exceeded":
      return i18n.t("inbox:type.app_tool_calls_quota_exceeded");
  }
  if (item.type === "quick_create_done") {
    const cleanedTitle = stripQuickCreatePrefix(item.title, details.identifier);
    if (cleanedTitle) return cleanedTitle;
    const prompt = singleLine(details.original_prompt);
    if (prompt) return prompt;
  }
  // Both non-success quick-create outcomes surface the user's original input
  // as the row title. Mirrors isQuickCreateOutcome in
  // packages/views/inbox/components/inbox-display.ts.
  if (item.type === "quick_create_failed" || item.type === "quick_create_unconfirmed") {
    const prompt = singleLine(details.original_prompt);
    if (prompt) return prompt;
  }
  return item.title;
}

/** Issue-less notices that open in the inbox's own detail sheet. */
export function isInboxNotice(type: InboxItem["type"]): boolean {
  return (
    type === "autopilot_quota_exceeded" ||
    type === "app_tool_calls_quota_exceeded" ||
    type === "autopilot_paused" ||
    type === "project_owner_joined"
  );
}

export function getInboxNavigationTarget(
  item: InboxItem,
  workspace: string | null,
  historyToken: string,
) {
  if (!workspace) return null;
  if (item.issue_id) {
    return {
      pathname: "/[workspace]/issue/[id]" as const,
      params: {
        workspace,
        id: item.issue_id,
        highlight: item.details?.comment_id,
        h: historyToken,
      },
    };
  }
  if (isInboxNotice(item.type)) {
    return {
      pathname: "/[workspace]/inbox/[id]" as const,
      params: { workspace, id: item.id },
    };
  }
  return null;
}

/**
 * The timeline row a deep-link `highlight` lands on. A failed run's
 * notification names its task; mobile renders no run rows, so the run shows
 * only as the failure notice it posted, and the link lands there. Null
 * while the timeline has no such row — a cached timeline may not have it
 * yet, and a run that posted nothing never will.
 */
export function resolveTimelineHighlight(
  entries: readonly TimelineEntry[],
  highlight: string,
): string | null {
  if (entries.some((entry) => entry.id === highlight)) return highlight;
  const posted = entries.filter(
    (entry) => entry.type === "comment" && entry.source_task_id === highlight,
  );
  return (
    posted.find((entry) => entry.comment_type === "system") ?? posted.at(-1)
  )?.id ?? null;
}

/**
 * The inbox list: archived rows dropped, one row per issue, each opening at
 * the timeline entry web opens it at. The grouping and anchor rule is core's
 * `groupInboxItemsByIssue`, the one web runs. Mobile renders no app
 * requests, so an app request row anchors like any other row instead of
 * keeping its request.
 */
export function deduplicateInboxItems(items: InboxItem[]): InboxItem[] {
  return groupInboxItemsByIssue(
    items.filter((i) => !i.archived),
    { keepRequestTarget: false },
  );
}
