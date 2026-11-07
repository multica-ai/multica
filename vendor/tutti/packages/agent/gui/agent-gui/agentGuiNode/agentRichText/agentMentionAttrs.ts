import { resolveAgentMentionFileThumbnailUrl } from "../../shared/mentionFilePresentation";
import type {
  AgentContextMentionItem,
  AgentFileMentionKind,
  AgentMentionKind,
  AgentMentionReferenceSource,
  AgentMentionSessionItem
} from "./agentFileMentionContracts";
import {
  agentSessionTargetIdFromHref,
  dirnameFromPath,
  parseMentionFileCount,
  parseMentionItemFromHref
} from "./agentMentionMarkdown";
import { resolveAgentSessionMentionIconUrl } from "./agentMentionPresentation";

export function mentionItemToAttrs(
  item: AgentContextMentionItem
): Record<string, string> {
  if (item.kind === "file") {
    return {
      name: item.name,
      kind: item.kind,
      href: item.href,
      path: item.path,
      entryKind: item.entryKind,
      directoryPath: item.directoryPath,
      thumbnailUrl: resolveAgentMentionFileThumbnailUrl(item) ?? "",
      ...(item.attachmentId ? { attachmentId: item.attachmentId } : {}),
      ...(item.attachmentStatus
        ? { attachmentStatus: item.attachmentStatus }
        : {}),
      ...(item.attachmentErrorCode
        ? { attachmentErrorCode: item.attachmentErrorCode }
        : {})
    };
  }
  if (item.kind === "session") {
    const iconUrl = resolveAgentSessionMentionIconUrl({
      agentIconUrl: item.agentIconUrl
    });
    return {
      name: item.name,
      kind: item.kind,
      href: item.href,
      ...workspaceMentionAttrs(item.workspaceId),
      targetId: item.targetId,
      ...(item.agentTargetId?.trim()
        ? { agentTargetId: item.agentTargetId.trim() }
        : {}),
      scope: item.scope,
      title: item.title,
      initiatorName: item.initiatorName,
      agentName: item.agentName,
      ...(iconUrl ? { iconUrl } : {}),
      status: item.status ?? "",
      inputPreview: item.inputPreview ?? "",
      summaryPreview: item.summaryPreview ?? ""
    };
  }
  if (item.kind === "workspace-app") {
    return {
      name: item.name,
      kind: item.kind,
      href: item.href,
      ...workspaceMentionAttrs(item.workspaceId),
      targetId: item.targetId,
      appId: item.appId,
      description: item.description ?? "",
      iconUrl: item.iconUrl ?? ""
    };
  }
  if (item.kind === "workspace-member") {
    return { kind: item.kind, href: item.href, name: item.name, workspaceId: item.workspaceId, targetId: item.targetId, iconUrl: item.iconUrl ?? "" };
  }
  if (item.kind === "agent-target") {
    return {
      name: item.name,
      kind: item.kind,
      href: item.href,
      ...workspaceMentionAttrs(item.workspaceId),
      targetId: item.targetId,
      description: item.description ?? "",
      agentProviderId: item.agentProviderId ?? "",
      iconUrl: item.iconUrl ?? ""
    };
  }
  if (item.kind === "workspace-reference") {
    return {
      name: item.name,
      kind: item.kind,
      href: item.href,
      ...workspaceMentionAttrs(item.workspaceId),
      targetId: item.targetId,
      source: item.source,
      groupId: item.groupId ?? "",
      iconUrl: item.iconUrl ?? "",
      fileCount: String(item.fileCount)
    };
  }
  if (item.kind === "workspace-app-factory") {
    return {
      name: item.name,
      kind: item.kind,
      href: item.href,
      ...workspaceMentionAttrs(item.workspaceId),
      targetId: item.targetId,
      jobId: item.jobId,
      action: item.action ?? "",
      contextPath: item.contextPath ?? ""
    };
  }
  if (item.kind === "custom") {
    return {
      name: item.name,
      sourceLabel: item.sourceLabel,
      kind: item.kind,
      customKind: item.customKind,
      href: item.href,
      ...workspaceMentionAttrs(item.workspaceId),
      targetId: item.targetId,
      preview: item.summary ?? ""
    };
  }
  return {
    name: item.name,
    kind: item.kind,
    href: item.href,
    ...workspaceMentionAttrs(item.workspaceId),
    targetId: item.targetId,
    topicId: item.topicId ?? "",
    title: item.title,
    creatorName: item.creatorName ?? "",
    iconUrl: item.iconUrl ?? "",
    status: item.status ?? "",
    contentPreview: item.contentPreview ?? ""
  };
}

function workspaceMentionAttrs(
  workspaceId: string
): Pick<AgentMentionSessionItem, "workspaceId"> {
  const normalizedWorkspaceId = workspaceId.trim();
  return {
    workspaceId: normalizedWorkspaceId
  };
}

export function attrsToMentionItem(
  attrs: Record<string, unknown>
): AgentContextMentionItem {
  const kind = normalizeMentionKind(attrs.kind);
  const name = typeof attrs.name === "string" ? attrs.name : "";
  const href =
    typeof attrs.href === "string" && attrs.href.trim() ? attrs.href : "";
  if (kind === "session") {
    const workspaceId = workspaceIdFromMentionAttrs(attrs);
    const targetId = typeof attrs.targetId === "string" ? attrs.targetId : "";
    return {
      kind,
      href,
      workspaceId,
      targetId,
      agentTargetId:
        typeof attrs.agentTargetId === "string" && attrs.agentTargetId.trim()
          ? attrs.agentTargetId.trim()
          : agentSessionTargetIdFromHref(href),
      name,
      title:
        typeof attrs.title === "string" && attrs.title.trim()
          ? attrs.title
          : name,
      scope:
        attrs.scope === "my_sessions" || attrs.scope === "collab_sessions"
          ? attrs.scope
          : "collab_sessions",
      initiatorName:
        typeof attrs.initiatorName === "string" ? attrs.initiatorName : "",
      agentName: typeof attrs.agentName === "string" ? attrs.agentName : "",
      agentIconUrl:
        typeof attrs.iconUrl === "string" && attrs.iconUrl.trim()
          ? attrs.iconUrl.trim()
          : undefined,
      status: typeof attrs.status === "string" ? attrs.status : undefined,
      inputPreview:
        typeof attrs.inputPreview === "string" ? attrs.inputPreview : undefined,
      summaryPreview:
        typeof attrs.summaryPreview === "string"
          ? attrs.summaryPreview
          : undefined
    };
  }
  if (kind === "custom") {
    const workspaceId = workspaceIdFromMentionAttrs(attrs);
    const targetId =
      typeof attrs.targetId === "string" ? attrs.targetId.trim() : "";
    const customKind =
      typeof attrs.customKind === "string" ? attrs.customKind.trim() : "";
    const sourceLabel =
      typeof attrs.sourceLabel === "string" && attrs.sourceLabel.trim()
        ? attrs.sourceLabel.trim()
        : name;
    const summary =
      typeof attrs.preview === "string" && attrs.preview.trim()
        ? attrs.preview.trim()
        : undefined;
    return {
      kind,
      customKind,
      href,
      workspaceId,
      targetId,
      sourceLabel,
      name,
      summary
    };
  }
  if (kind === "workspace-issue") {
    const workspaceId = workspaceIdFromMentionAttrs(attrs);
    const targetId = typeof attrs.targetId === "string" ? attrs.targetId : "";
    const topicId =
      typeof attrs.topicId === "string" && attrs.topicId.trim()
        ? attrs.topicId.trim()
        : undefined;
    return {
      kind,
      href,
      workspaceId,
      targetId,
      topicId,
      name,
      title:
        typeof attrs.title === "string" && attrs.title.trim()
          ? attrs.title
          : name,
      creatorName:
        typeof attrs.creatorName === "string" ? attrs.creatorName : undefined,
      iconUrl:
        typeof attrs.iconUrl === "string" && attrs.iconUrl.trim()
          ? attrs.iconUrl.trim()
          : undefined,
      status: typeof attrs.status === "string" ? attrs.status : undefined,
      contentPreview:
        typeof attrs.contentPreview === "string"
          ? attrs.contentPreview
          : undefined
    };
  }
  if (kind === "workspace-app") {
    const workspaceId = workspaceIdFromMentionAttrs(attrs);
    const appId =
      typeof attrs.appId === "string" && attrs.appId.trim()
        ? attrs.appId.trim()
        : typeof attrs.targetId === "string"
          ? attrs.targetId.trim()
          : "";
    return {
      kind,
      href,
      workspaceId,
      targetId: appId,
      appId,
      name,
      description:
        typeof attrs.description === "string" ? attrs.description : undefined,
      iconUrl:
        typeof attrs.iconUrl === "string" && attrs.iconUrl.trim()
          ? attrs.iconUrl.trim()
          : undefined
    };
  }
  if (kind === "workspace-member") {
    return { kind, href, name, workspaceId: workspaceIdFromMentionAttrs(attrs), targetId: String(attrs.targetId ?? ""), iconUrl: String(attrs.iconUrl ?? "") || undefined };
  }
  if (kind === "agent-target") {
    const workspaceId = workspaceIdFromMentionAttrs(attrs);
    const targetId = typeof attrs.targetId === "string" ? attrs.targetId : "";
    return {
      kind,
      href,
      workspaceId,
      targetId,
      name,
      description:
        typeof attrs.description === "string" ? attrs.description : undefined,
      agentProviderId:
        typeof attrs.agentProviderId === "string" &&
        attrs.agentProviderId.trim()
          ? attrs.agentProviderId.trim()
          : undefined,
      iconUrl:
        typeof attrs.iconUrl === "string" && attrs.iconUrl.trim()
          ? attrs.iconUrl.trim()
          : undefined
    };
  }
  if (kind === "workspace-reference") {
    const workspaceId = workspaceIdFromMentionAttrs(attrs);
    const targetId =
      typeof attrs.targetId === "string" ? attrs.targetId.trim() : "";
    const source: AgentMentionReferenceSource =
      attrs.source === "task" ? "task" : "app";
    const groupId =
      typeof attrs.groupId === "string" && attrs.groupId.trim()
        ? attrs.groupId.trim()
        : undefined;
    const iconUrl =
      typeof attrs.iconUrl === "string" && attrs.iconUrl.trim()
        ? attrs.iconUrl.trim()
        : undefined;
    const fileCount = parseMentionFileCount(attrs.fileCount);
    return {
      kind,
      href,
      workspaceId,
      targetId,
      source,
      groupId,
      name,
      iconUrl,
      fileCount
    };
  }
  if (kind === "workspace-app-factory") {
    const workspaceId = workspaceIdFromMentionAttrs(attrs);
    const jobId =
      typeof attrs.jobId === "string" && attrs.jobId.trim()
        ? attrs.jobId.trim()
        : typeof attrs.targetId === "string"
          ? attrs.targetId.trim()
          : "";
    const action =
      typeof attrs.action === "string" && attrs.action.trim()
        ? attrs.action.trim()
        : undefined;
    const contextPath =
      typeof attrs.contextPath === "string" && attrs.contextPath.trim()
        ? attrs.contextPath.trim()
        : undefined;
    return {
      kind,
      href,
      workspaceId,
      targetId: jobId,
      jobId,
      name,
      action,
      contextPath
    };
  }
  const path =
    typeof attrs.path === "string" && attrs.path.trim()
      ? attrs.path
      : typeof attrs.href === "string"
        ? attrs.href
        : "";
  return {
    kind: "file",
    href: href || path,
    path,
    name,
    entryKind: normalizeEntryKind(attrs.entryKind),
    directoryPath:
      typeof attrs.directoryPath === "string" && attrs.directoryPath.trim()
        ? attrs.directoryPath
        : dirnameFromPath(path),
    attachmentId:
      typeof attrs.attachmentId === "string" && attrs.attachmentId.trim()
        ? attrs.attachmentId.trim()
        : undefined,
    attachmentStatus:
      attrs.attachmentStatus === "uploading" ||
      attrs.attachmentStatus === "error"
        ? attrs.attachmentStatus
        : attrs.attachmentId
          ? "ready"
          : undefined,
    attachmentErrorCode:
      typeof attrs.attachmentErrorCode === "string" &&
      attrs.attachmentErrorCode.trim()
        ? attrs.attachmentErrorCode.trim()
        : undefined,
    thumbnailUrl: resolveAgentMentionFileThumbnailUrl({
      entryKind: normalizeEntryKind(attrs.entryKind),
      href: href || path,
      name,
      path,
      thumbnailUrl:
        typeof attrs.thumbnailUrl === "string" ? attrs.thumbnailUrl : undefined
    })
  };
}

function workspaceIdFromMentionAttrs(attrs: Record<string, unknown>): string {
  return typeof attrs.workspaceId === "string" ? attrs.workspaceId.trim() : "";
}

export function parseAgentMentionHTMLElementAttrs(
  node: HTMLElement | string
): Record<string, string> | false {
  if (typeof node === "string") {
    return false;
  }
  const href =
    node.getAttribute("data-agent-mention-href") ||
    node.getAttribute("data-agent-link-href") ||
    node.getAttribute("href") ||
    "";
  const name =
    node.getAttribute("name") ||
    (node.textContent ?? "").replace(/^@+/, "").trim();
  const parsedItem = href ? parseMentionItemFromHref({ name, href }) : null;
  if (
    node.getAttribute("data-agent-mention-kind") === "custom" &&
    parsedItem?.kind !== "custom"
  ) {
    return false;
  }
  const parsedAttrs = parsedItem ? mentionItemToAttrs(parsedItem) : {};
  const iconUrl = node.getAttribute("data-agent-mention-icon-url") || "";
  const fileCount = node.getAttribute("data-agent-mention-file-count") || "";
  return {
    ...parsedAttrs,
    name,
    kind:
      node.getAttribute("data-agent-mention-kind") ||
      node.getAttribute("kind") ||
      parsedAttrs.kind ||
      "file",
    href,
    ...(iconUrl ? { iconUrl } : {}),
    ...(fileCount ? { fileCount } : {})
  };
}

function normalizeMentionKind(value: unknown): AgentMentionKind {
  if (value === "session" || value === "agent-session") {
    return "session";
  }
  if (value === "workspace-issue") {
    return "workspace-issue";
  }
  if (value === "workspace-app") {
    return "workspace-app";
  }
  if (value === "workspace-member") return "workspace-member";
  if (value === "agent-target") {
    return "agent-target";
  }
  if (value === "workspace-reference") {
    return "workspace-reference";
  }
  if (value === "workspace-app-factory") {
    return "workspace-app-factory";
  }
  if (value === "custom") {
    return "custom";
  }
  return "file";
}

function normalizeEntryKind(value: unknown): AgentFileMentionKind {
  return value === "directory" || value === "file" ? value : "unknown";
}
