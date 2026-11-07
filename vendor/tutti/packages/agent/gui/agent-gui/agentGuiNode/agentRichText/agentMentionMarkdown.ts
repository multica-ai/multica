import {
  createRichTextLinkMarkdown,
  createRichTextMarkdownLink,
  createRichTextMentionHref,
  createRichTextMentionMarkdown,
  isRichTextMentionHref,
  parseRichTextMentionHref
} from "@tutti-os/ui-rich-text/core";
import { getAgentCustomMentionKind } from "../../../shared/agentCustomMentionKinds";
import { translate } from "../../../i18n/index";
import type {
  AgentContextMentionItem,
  AgentComposerFileMentionStatus,
  ParsedAgentMentionMarkdown
} from "./agentFileMentionContracts";

export const AGENT_COMPOSER_FILE_MENTION_KIND = "composer-file";

export interface AgentComposerFileMentionReference {
  end: number;
  errorCode?: string;
  id: string;
  name: string;
  start: number;
  status: AgentComposerFileMentionStatus;
}

export function createAgentComposerFileMentionMarkdown(input: {
  errorCode?: string;
  id: string;
  name: string;
  status: AgentComposerFileMentionStatus;
}): string {
  return createRichTextMentionMarkdown({
    providerId: AGENT_COMPOSER_FILE_MENTION_KIND,
    entityId: input.id,
    label: input.name,
    scope: {
      ...(input.errorCode?.trim() ? { errorCode: input.errorCode.trim() } : {}),
      status: input.status
    }
  });
}

export function dirnameFromPath(path: string): string {
  const normalized = normalizePathSeparators(path);
  const withoutTrailingSeparators = normalized.replace(/\/+$/, "");
  if (!withoutTrailingSeparators) {
    return normalized.startsWith("/") ? "/" : "";
  }
  if (
    /^[A-Za-z]:$/.test(withoutTrailingSeparators) &&
    /^[A-Za-z]:\/+/.test(normalized)
  ) {
    return `${withoutTrailingSeparators}/`;
  }
  const index = withoutTrailingSeparators.lastIndexOf("/");
  if (index < 0) {
    return "";
  }
  if (index === 0) {
    return "/";
  }
  const directory = withoutTrailingSeparators.slice(0, index);
  if (/^[A-Za-z]:$/.test(directory)) {
    return `${directory}/`;
  }
  if (/^[A-Za-z]:\//.test(directory)) {
    return directory;
  }
  return directory.startsWith("/") ? directory : `/${directory}`;
}

export function basenameFromPath(path: string): string {
  const normalized = normalizePathSeparators(path).replace(/\/+$/, "");
  return normalized.split("/").filter(Boolean).at(-1) ?? "";
}

export function hasPathTrailingSeparator(path: string): boolean {
  const trimmed = path.trim();
  return (
    trimmed.endsWith("/") ||
    (isWindowsLikePath(trimmed) && trimmed.endsWith("\\"))
  );
}

function normalizePathSeparators(path: string): string {
  const trimmed = path.trim();
  return isWindowsLikePath(trimmed) ? trimmed.replace(/\\/g, "/") : trimmed;
}

function isWindowsLikePath(path: string): boolean {
  return (
    !path.startsWith("/") ||
    /^[A-Za-z]:[\\/]/.test(path) ||
    /^\/[A-Za-z]:[\\/]/.test(path) ||
    path.startsWith("\\\\")
  );
}

export function normalizeAgentSessionMentionTitle(value: string): string {
  const trimmed = value.trim();
  const withoutMentionPrefix = trimmed.replace(/^@+/, "").trim();
  return withoutMentionPrefix || trimmed;
}

export function formatAgentFileMentionMarkdown(
  name: string,
  path: string
): string {
  return formatAgentMentionMarkdown({
    kind: "file",
    href: path,
    path,
    name,
    entryKind: "unknown",
    directoryPath: ""
  });
}

export function createAgentSessionMentionHref(input: {
  agentSessionId: string;
  agentTargetId?: string | null;
  label: string;
  workspaceId: string;
}): string {
  const label = normalizeAgentSessionMarkdownLabel(input.label);
  const agentTargetId = input.agentTargetId?.trim() ?? "";
  return createRichTextMentionHref({
    providerId: "agent-session",
    entityId: input.agentSessionId,
    label,
    scope: {
      ...(agentTargetId ? { agentTargetId } : {}),
      workspaceId: input.workspaceId
    }
  });
}

export interface CreateAgentSessionMarkdownLinkInput {
  agentSessionId: string;
  agentTargetId?: string | null;
  label: string;
  workspaceId: string;
  withAtPrefix: boolean;
}

export function createAgentSessionMarkdownLink(
  input: CreateAgentSessionMarkdownLinkInput
): string {
  const label = normalizeAgentSessionMarkdownLabel(input.label);
  const href = createAgentSessionMentionHref({
    agentSessionId: input.agentSessionId,
    agentTargetId: input.agentTargetId,
    label,
    workspaceId: input.workspaceId
  });
  return createRichTextMarkdownLink({
    href,
    label: input.withAtPrefix ? `@${label}` : label
  });
}

export interface CreateAgentSessionHandoffPromptInput {
  agentSessionId: string;
  agentTargetId?: string | null;
  label: string;
  workspaceId: string;
}

/**
 * Builds the canonical composer draft for handing a complete Agent session to
 * another Agent. The trailing space is the editable caret anchor used by the
 * rich-text composer after the atomic session mention.
 */
export function createAgentSessionHandoffPrompt(
  input: CreateAgentSessionHandoffPromptInput
): string {
  return `${createAgentSessionMarkdownLink({
    ...input,
    withAtPrefix: true
  })} `;
}

export function formatAgentMentionMarkdown(
  item: AgentContextMentionItem
): string {
  // 所有 entity(含 workspace-reference)统一序列化成单条 mention 链接。
  // workspace-reference 不再展开成文件路径——agent 拿到 URI 后经 skill+CLI 按需解析。
  if (item.kind === "file") {
    if (item.attachmentId) {
      return createAgentComposerFileMentionMarkdown({
        errorCode: item.attachmentErrorCode,
        id: item.attachmentId,
        name: item.name,
        status: item.attachmentStatus ?? "ready"
      });
    }
    const label = item.name.trim().startsWith("@")
      ? item.name
      : `@${item.name}`;
    return createRichTextLinkMarkdown({
      kind: "file",
      name: label,
      path: item.href
    });
  }
  if (item.kind === "workspace-reference") {
    return createRichTextMentionMarkdown({
      providerId: "workspace-reference",
      entityId: item.targetId,
      label: item.name,
      scope: {
        workspaceId: item.workspaceId,
        source: item.source,
        ...(item.groupId?.trim() ? { groupId: item.groupId.trim() } : {}),
        ...(item.fileCount > 0 ? { count: String(item.fileCount) } : {})
      }
    });
  }
  if (item.kind === "session") {
    return createAgentSessionMarkdownLink({
      agentSessionId: item.targetId,
      agentTargetId:
        item.agentTargetId ?? agentSessionTargetIdFromHref(item.href),
      label: item.title.trim() || item.name,
      workspaceId: item.workspaceId,
      withAtPrefix: true
    });
  }
  if (item.kind === "agent-target") {
    return createRichTextMentionMarkdown({
      providerId: "agent-target",
      entityId: item.targetId,
      label: item.name,
      scope: { workspaceId: item.workspaceId }
    });
  }
  if (item.kind === "custom") {
    const identity = parseRichTextMentionHref(item.href, item.sourceLabel);
    return identity ? createRichTextMentionMarkdown(identity) : "";
  }
  const identity = parseRichTextMentionHref(item.href, item.name);
  return identity ? createRichTextMentionMarkdown(identity) : "";
}

export function materializeAgentCustomMentionPromptText(value: string): string {
  let cursor = 0;
  let result = "";
  while (cursor < value.length) {
    const mentionStart = value.indexOf("[", cursor);
    if (mentionStart < 0) {
      result += value.slice(cursor);
      break;
    }
    const parsed = parseAgentMentionMarkdown(value, mentionStart);
    if (!parsed || parsed.item.kind !== "custom") {
      result += value.slice(cursor, mentionStart + 1);
      cursor = mentionStart + 1;
      continue;
    }
    const identity = parseRichTextMentionHref(
      parsed.item.href,
      parsed.item.sourceLabel
    );
    const definition = identity
      ? getAgentCustomMentionKind(parsed.item.customKind)
      : undefined;
    const materialized =
      identity && definition?.materializePromptText
        ? definition.materializePromptText(identity, parsed.item.href)
        : "";
    result += value.slice(cursor, mentionStart);
    result += materialized?.trim()
      ? materialized
      : value.slice(mentionStart, parsed.end);
    cursor = parsed.end;
  }
  return result;
}

function normalizeAgentSessionMarkdownLabel(value: string): string {
  return value.trim().replace(/^@+/, "").trim();
}

export function agentSessionTargetIdFromHref(href: string): string | undefined {
  const mention = parseRichTextMentionHref(href, "");
  const agentTargetId = mention?.scope?.agentTargetId?.trim() ?? "";
  return agentTargetId || undefined;
}

export function parseMentionFileCount(value: unknown): number {
  const parsed =
    typeof value === "number"
      ? value
      : typeof value === "string"
        ? Number.parseInt(value.trim(), 10)
        : Number.NaN;
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

export function parseAgentMentionMarkdown(
  value: string,
  start = 0
): ParsedAgentMentionMarkdown | null {
  if (!value.startsWith("[", start)) {
    return null;
  }

  let index = start + 1;
  const prefixedMention = value[index] === "@";
  if (prefixedMention) {
    index += 1;
  }
  let name = "";
  while (index < value.length) {
    const current = value[index];
    if (current === "\\") {
      const escaped = value[index + 1];
      if (escaped === "\\" || escaped === "[" || escaped === "]") {
        name += escaped;
        index += 2;
        continue;
      }
      name += current;
      index += 1;
      continue;
    }
    if (current === "]") {
      break;
    }
    name += current;
    index += 1;
  }

  if (value[index] !== "]" || value[index + 1] !== "(") {
    return null;
  }

  index += 2;
  let href = "";
  while (index < value.length) {
    const current = value[index];
    if (current === "\\") {
      const escaped = value[index + 1];
      if (escaped === "\\" || escaped === ")") {
        href += escaped;
        index += 2;
        continue;
      }
      href += current;
      index += 1;
      continue;
    }
    if (current === ")") {
      const item = parseMentionItemFromHref({ name, href });
      if (!item) {
        return null;
      }
      if (
        item.kind !== "file" &&
        !prefixedMention &&
        !isRichTextMentionHref(href)
      ) {
        return null;
      }
      return {
        item,
        end: index + 1
      };
    }
    href += current;
    index += 1;
  }

  return null;
}

export function parseMentionItemFromHref(input: {
  name: string;
  href: string;
}): AgentContextMentionItem | null {
  const href = input.href.trim();
  if (!href) {
    return null;
  }
  const mention = parseRichTextMentionHref(href, input.name);
  if (!mention && !isRichTextMentionHref(href)) {
    return {
      kind: "file",
      href,
      path: href,
      name: input.name,
      entryKind: isLocalDirectoryMentionHref(href) ? "directory" : "unknown",
      directoryPath: dirnameFromPath(href)
    };
  }
  if (!mention) {
    return null;
  }

  const resource = mention.providerId.trim().toLowerCase();
  const targetId = mention.entityId.trim();
  const workspaceId = mention.scope?.workspaceId?.trim() ?? "";
  const name = mention.label;
  if (resource === AGENT_COMPOSER_FILE_MENTION_KIND) {
    if (!targetId) {
      return null;
    }
    const status = normalizeAgentComposerFileMentionStatus(
      mention.scope?.status
    );
    return {
      kind: "file",
      href,
      path: "",
      name,
      entryKind: "file",
      directoryPath: "",
      attachmentId: targetId,
      attachmentStatus: status,
      attachmentErrorCode: mention.scope?.errorCode?.trim() || undefined
    };
  }
  if (resource === "agent-session") {
    if (!workspaceId) {
      return null;
    }
    return {
      kind: "session",
      href,
      workspaceId,
      targetId,
      agentTargetId: mention.scope?.agentTargetId?.trim() || undefined,
      name,
      title: name,
      scope: "collab_sessions",
      initiatorName: "",
      agentName: ""
    };
  }
  if (resource === "workspace-issue") {
    if (!workspaceId) {
      return null;
    }
    return {
      kind: "workspace-issue",
      href,
      workspaceId,
      targetId,
      topicId: mention.scope?.topicId?.trim() || undefined,
      name,
      title: name
    };
  }
  if (resource === "workspace-app") {
    if (!workspaceId) {
      return null;
    }
    return {
      kind: "workspace-app",
      href,
      workspaceId,
      targetId,
      appId: targetId,
      name,
      iconUrl: mention.scope?.icon?.trim() || undefined
    };
  }
  if (resource === "workspace-member") {
    const workspaceId = mention.scope?.workspaceId?.trim() ?? "";
    if (!workspaceId || mention.scope?.userId !== targetId) return null;
    return { kind: "workspace-member", href, workspaceId, targetId, name };
  }
  if (resource === "agent-target") {
    if (!workspaceId) {
      return null;
    }
    return {
      kind: "agent-target",
      href,
      workspaceId,
      targetId,
      name
    };
  }
  if (resource === "workspace-reference") {
    if (!workspaceId) {
      return null;
    }
    const source = mention.scope?.source?.trim();
    if (source !== "app" && source !== "task") {
      return null;
    }
    return {
      kind: "workspace-reference",
      href,
      workspaceId,
      targetId,
      source,
      groupId: mention.scope?.groupId?.trim() || undefined,
      name,
      iconUrl: mention.scope?.icon?.trim() || undefined,
      fileCount: parseMentionFileCount(mention.scope?.count)
    };
  }
  if (resource === "workspace-app-factory") {
    return {
      kind: "workspace-app-factory",
      href,
      workspaceId,
      targetId,
      jobId: targetId === "create" ? "" : targetId,
      name:
        name ||
        translate("agentHost.agentGui.workspaceAppFactoryMentionFallback"),
      action: mention.scope?.action?.trim() || undefined,
      contextPath: mention.scope?.contextPath?.trim() || undefined
    };
  }
  const customDefinition = getAgentCustomMentionKind(resource);
  if (customDefinition) {
    const presentation = customDefinition.present(mention, href);
    if (!presentation) {
      return null;
    }
    return {
      kind: "custom",
      customKind: resource,
      href,
      workspaceId: presentation.workspaceId?.trim() || workspaceId,
      targetId,
      sourceLabel: mention.label,
      name: presentation.name.trim() || name,
      summary: presentation.summary?.trim() || undefined
    };
  }
  return null;
}

export function agentComposerFileMentionReferences(
  value: string
): AgentComposerFileMentionReference[] {
  const references: AgentComposerFileMentionReference[] = [];
  let cursor = 0;
  while (cursor < value.length) {
    const start = value.indexOf("[", cursor);
    if (start < 0) break;
    const parsed = parseAgentMentionMarkdown(value, start);
    if (parsed?.item.kind === "file" && parsed.item.attachmentId) {
      references.push({
        start,
        end: parsed.end,
        errorCode: parsed.item.attachmentErrorCode,
        id: parsed.item.attachmentId,
        name: parsed.item.name,
        status: parsed.item.attachmentStatus ?? "ready"
      });
      cursor = parsed.end;
      continue;
    }
    cursor = start + 1;
  }
  return references;
}

export interface AgentComposerFileMentionUpdate {
  errorCode?: string;
  status: AgentComposerFileMentionStatus;
}

export function updateAgentComposerFileMentions(
  value: string,
  updates: ReadonlyMap<string, AgentComposerFileMentionUpdate>
): string {
  const references = agentComposerFileMentionReferences(value);
  if (references.length === 0) return value;
  let cursor = 0;
  let result = "";
  for (const reference of references) {
    const update = updates.get(reference.id);
    const status = update?.status ?? reference.status;
    result += value.slice(cursor, reference.start);
    result += createAgentComposerFileMentionMarkdown({
      ...(status === "error"
        ? { errorCode: update?.errorCode ?? reference.errorCode }
        : {}),
      id: reference.id,
      name: reference.name,
      status
    });
    cursor = reference.end;
  }
  return result + value.slice(cursor);
}

function normalizeAgentComposerFileMentionStatus(
  value: unknown
): AgentComposerFileMentionStatus {
  return value === "uploading" || value === "error" ? value : "ready";
}

function isLocalDirectoryMentionHref(href: string): boolean {
  const isWindowsAbsolutePath = /^[A-Za-z]:[\\/]/.test(href);
  const hasScheme = /^[a-zA-Z][a-zA-Z\d+.-]*:/.test(href);
  return (
    hasPathTrailingSeparator(href) && (!hasScheme || isWindowsAbsolutePath)
  );
}
