import type { InboxItem } from "../types";
import { paths, type WorkspacePaths } from "../paths";

export interface ChannelInboxTarget {
  channelId: string;
  messageId: string | null;
}

function asNonEmptyString(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value : null;
}

export function getChannelInboxTarget(item: InboxItem): ChannelInboxTarget | null {
  const details = item.details;
  if (details?.source_type !== "channel_message") return null;
  const channelId = asNonEmptyString(details.channel_id);
  if (!channelId) return null;
  return {
    channelId,
    messageId: asNonEmptyString(details.message_id),
  };
}

export function getInboxItemSelectionKey(item: InboxItem): string {
  const channelTarget = getChannelInboxTarget(item);
  if (channelTarget) {
    return `channel:${channelTarget.channelId}:${channelTarget.messageId ?? item.id}`;
  }
  return item.issue_id ?? item.id;
}

export function buildChannelInboxPath(
  item: InboxItem,
  wsPaths: Pick<WorkspacePaths, "channelDetail">,
): string | null {
  const target = getChannelInboxTarget(item);
  if (!target) return null;
  const base = wsPaths.channelDetail(target.channelId);
  if (!target.messageId) return base;
  return `${base}?message=${encodeURIComponent(target.messageId)}`;
}

export function buildChannelInboxPathForSlug(
  item: InboxItem,
  slug: string,
): string | null {
  return buildChannelInboxPath(item, paths.workspace(slug));
}
