import { defaultUrlTransform } from "react-markdown";
import {
  resolveWorkspaceImageMimeType,
  resolveWorkspaceVideoMimeType
} from "@tutti-os/workspace-file-preview";
import {
  isRichTextMentionHref,
  parseRichTextMentionHref
} from "@tutti-os/ui-rich-text/core";
import { managedAgentRoundedIconUrl } from "./managedAgentIcons";
import { isDirectAgentGeneratedMediaPath } from "../actions/workspaceFilePathCandidate";
import {
  resolveAgentMentionTargetPresentation,
  type AgentMessageMarkdownAgentTarget
} from "./agentTargetPresentation";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "./AgentMessageMarkdown";

const PLAIN_SESSION_MENTION_AGENT_LABELS = [
  "Claude Code",
  "Nexight",
  "Codex"
] as const;
const STANDARD_MARKDOWN_LINK_PROTOCOLS = [
  "http",
  "https",
  "irc",
  "ircs",
  "mailto",
  "tel",
  "xmpp"
] as const;
const EMPTY_AGENT_TARGETS: readonly AgentMessageMarkdownAgentTarget[] = [];

export type MarkdownMediaKind = "image" | "video";
export type MarkdownMediaState =
  | { status: "loading" }
  | { kind: MarkdownMediaKind; status: "ready"; src: string }
  | {
      status: "error";
      reason: "unsupported" | "read-failed";
      detail?: string;
    };
interface CachedMarkdownMedia {
  kind: MarkdownMediaKind;
  objectUrl: string;
  refCount: number;
  revokeTimer: ReturnType<typeof setTimeout> | null;
}
const cachedMarkdownMedia = new Map<string, CachedMarkdownMedia>();
const CACHED_MARKDOWN_MEDIA_REVOKE_DELAY_MS = 250;

export function resetCachedMarkdownMediaForTests(): void {
  if (process.env.NODE_ENV !== "test") return;
  for (const [path, entry] of cachedMarkdownMedia) {
    if (entry.revokeTimer) clearTimeout(entry.revokeTimer);
    URL.revokeObjectURL(entry.objectUrl);
    cachedMarkdownMedia.delete(path);
  }
}

export function isLocalAbsolutePath(path: string): boolean {
  const candidate = path.trim();
  return (
    candidate.length > 1 &&
    candidate.startsWith("/") &&
    !candidate.startsWith("//") &&
    !candidate.includes("://") &&
    !/\s/.test(candidate)
  );
}

export function resolveMarkdownWorkspaceMediaPath(src: string): string | null {
  const candidate = src.trim();
  const fileUrlPath = resolveMarkdownFileUrlPath(candidate);
  const pathCandidate = fileUrlPath ?? candidate;
  try {
    const decodedPath = fileUrlPath ?? decodeURIComponent(pathCandidate);
    if (
      !isLocalAbsolutePath(pathCandidate) &&
      !isWindowsAbsolutePath(pathCandidate) &&
      !isWindowsAbsolutePath(decodedPath)
    ) {
      return null;
    }
    const isPosixAbsolutePath =
      decodedPath.length > 1 &&
      decodedPath.startsWith("/") &&
      !decodedPath.startsWith("//");
    const isWindowsAbsolutePathValue = isWindowsAbsolutePath(decodedPath);
    return (isPosixAbsolutePath || isWindowsAbsolutePathValue) &&
      !decodedPath.includes("\0")
      ? decodedPath
      : null;
  } catch {
    return null;
  }
}

export function isHomeRelativePath(path: string): boolean {
  const candidate = path.trim();
  return (
    candidate.length > 0 &&
    !/\s/.test(candidate) &&
    (candidate === "~" ||
      candidate.startsWith("~/") ||
      candidate.startsWith("~\\"))
  );
}

export function isWindowsAbsolutePath(path: string): boolean {
  const candidate = path.trim();
  return (
    /^[A-Za-z]:[\\/]/.test(candidate) &&
    !candidate.includes("://") &&
    !candidate.includes("\0")
  );
}

export function isExplicitWorkspaceFilePath(path: string): boolean {
  const candidate = path.trim();
  if (!candidate || candidate.includes("://")) {
    return false;
  }
  return (
    isLocalAbsolutePath(candidate) ||
    isHomeRelativePath(candidate) ||
    isWindowsAbsolutePath(candidate)
  );
}

export function isClickableMarkdownHref(href: string): boolean {
  const target = href.trim();
  return Boolean(
    target &&
    (isStandardMarkdownLinkHref(target) ||
      isRichTextMentionHref(target) ||
      isExplicitWorkspaceFilePath(target))
  );
}

export function isStandardMarkdownLinkHref(href: string): boolean {
  const target = href.trim();
  if (!target || isExplicitWorkspaceFilePath(target)) {
    return false;
  }
  if (target.startsWith("#")) {
    return target.length > 1;
  }
  let url: URL;
  try {
    url = new URL(target);
  } catch {
    return false;
  }
  const protocol = url.protocol.replace(/:$/, "").toLowerCase();
  return STANDARD_MARKDOWN_LINK_PROTOCOLS.includes(
    protocol as (typeof STANDARD_MARKDOWN_LINK_PROTOCOLS)[number]
  );
}

export function resolveRenderableMarkdownMediaSrc(src: string): string {
  const trimmed = src.trim();
  if (!trimmed) {
    return src;
  }
  const workspacePath = resolveMarkdownWorkspaceMediaPath(trimmed);
  if (workspacePath && isWindowsAbsolutePath(workspacePath)) {
    return windowsPathToFileUrl(workspacePath);
  }
  if (!isLocalAbsolutePath(trimmed) || trimmed.startsWith("/workspace/")) {
    return src;
  }
  return new URL(trimmed, "file://").toString();
}

export function canRenderMarkdownVideoFallback(src: unknown): boolean {
  if (typeof src !== "string") {
    return false;
  }
  const trimmed = src.trim();
  if (!isLocalAbsolutePath(trimmed) || trimmed.startsWith("/workspace/")) {
    return true;
  }
  return isDirectAgentGeneratedMediaPath(trimmed);
}

export function resolveMarkdownMediaKind(
  pathOrName: string
): MarkdownMediaKind | null {
  return resolveMarkdownMediaType(pathOrName)?.kind ?? null;
}

export function resolveMarkdownMediaType(
  pathOrName: string
): { kind: MarkdownMediaKind; mimeType: string } | null {
  const imageMimeType = resolveWorkspaceImageMimeType(pathOrName);
  if (imageMimeType) {
    return { kind: "image", mimeType: imageMimeType };
  }
  const videoMimeType = resolveWorkspaceVideoMimeType(pathOrName);
  if (videoMimeType) {
    return { kind: "video", mimeType: videoMimeType };
  }
  return null;
}

export function peekCachedMarkdownMediaState(
  path: string
): MarkdownMediaState | null {
  const entry = cachedMarkdownMedia.get(path);
  return entry
    ? { kind: entry.kind, status: "ready", src: entry.objectUrl }
    : null;
}

export function retainCachedMarkdownMedia(
  path: string
): { kind: MarkdownMediaKind; src: string } | null {
  const entry = cachedMarkdownMedia.get(path);
  if (!entry) {
    return null;
  }
  entry.refCount += 1;
  if (entry.revokeTimer) {
    clearTimeout(entry.revokeTimer);
    entry.revokeTimer = null;
  }
  return { kind: entry.kind, src: entry.objectUrl };
}

export function cacheMarkdownMedia(
  path: string,
  kind: MarkdownMediaKind,
  blob: Blob
): string {
  const entry = cachedMarkdownMedia.get(path);
  if (entry) {
    entry.refCount += 1;
    if (entry.revokeTimer) {
      clearTimeout(entry.revokeTimer);
      entry.revokeTimer = null;
    }
    return entry.objectUrl;
  }
  const objectUrl = URL.createObjectURL(blob);
  cachedMarkdownMedia.set(path, {
    kind,
    objectUrl,
    refCount: 1,
    revokeTimer: null
  });
  return objectUrl;
}

export function releaseCachedMarkdownMedia(
  path: string,
  objectUrl: string
): void {
  const entry = cachedMarkdownMedia.get(path);
  if (!entry || entry.objectUrl !== objectUrl) {
    URL.revokeObjectURL(objectUrl);
    return;
  }
  entry.refCount = Math.max(0, entry.refCount - 1);
  if (entry.refCount > 0 || entry.revokeTimer) {
    return;
  }
  // timing: grace period before revoking, so a quick remount can reuse the object URL
  entry.revokeTimer = setTimeout(() => {
    const current = cachedMarkdownMedia.get(path);
    if (!current || current.objectUrl !== objectUrl || current.refCount > 0) {
      return;
    }
    cachedMarkdownMedia.delete(path);
    URL.revokeObjectURL(objectUrl);
  }, CACHED_MARKDOWN_MEDIA_REVOKE_DELAY_MS);
}

export function normalizeLocalPathMarkdownLinks(content: string): string {
  let out = "";
  for (let index = 0; index < content.length; ) {
    const codeSpanEnd = codeSpanEndIndex(content, index);
    if (codeSpanEnd > index) {
      out += content.slice(index, codeSpanEnd);
      index = codeSpanEnd;
      continue;
    }
    const normalized = normalizeLocalPathMarkdownLinkAt(content, index);
    if (normalized) {
      out += normalized.markdown;
      index = normalized.end;
      continue;
    }
    out += content[index];
    index += 1;
  }
  return out;
}

function normalizeLocalPathMarkdownLinkAt(
  content: string,
  index: number
): { markdown: string; end: number } | null {
  if (content[index] !== "[") return null;
  const labelEnd = content.indexOf("]", index + 1);
  if (labelEnd < 0 || content[labelEnd + 1] !== "(") return null;
  const hrefStart = labelEnd + 2;
  if (content[hrefStart] === "<") return null;
  let hrefEnd = hrefStart;
  while (hrefEnd < content.length) {
    const current = content[hrefEnd];
    if (current === "\\" && hrefEnd + 1 < content.length) {
      hrefEnd += 2;
      continue;
    }
    if (current === "\n" || current === "\r") return null;
    if (current === ")") break;
    hrefEnd += 1;
  }
  if (content[hrefEnd] !== ")") return null;
  const target = content.slice(hrefStart, hrefEnd).trim();
  if (
    !target ||
    /[<>]/.test(target) ||
    !isPotentialLocalMarkdownPathHref(target)
  ) {
    return null;
  }
  const normalizedTarget = /^[A-Za-z]:\\/.test(target)
    ? target.replaceAll("\\", "/")
    : target;
  if (normalizedTarget === target && !/\s/.test(target)) return null;
  return {
    markdown: `${content.slice(index, hrefStart)}${
      /\s/.test(normalizedTarget) ? `<${normalizedTarget}>` : normalizedTarget
    })`,
    end: hrefEnd + 1
  };
}

function isPotentialLocalMarkdownPathHref(href: string): boolean {
  const target = href.trim();
  if (!target || target.includes("://")) return false;
  return (
    (target.startsWith("/") && !target.startsWith("//")) ||
    target.startsWith("~/") ||
    target.startsWith("~\\") ||
    /^[A-Za-z]:[\\/]/.test(target)
  );
}

export function normalizePlainIssueMentionTitleContent(
  content: string
): string {
  const trimmed = content.trim();
  if (
    trimmed !== content ||
    !trimmed.startsWith("@") ||
    trimmed.includes("\n") ||
    markdownLinkEndIndex(trimmed, 0) === trimmed.length
  ) {
    return content;
  }

  const label = trimmed.replace(/^@+/, "").trim();
  if (!label) {
    return content;
  }

  return content;
}

export function normalizeMentionMarkdownLinks(content: string): string {
  return content
    .replace(/\]([\t ]*\r?\n[\t ]*)+\((mention:\/\/)/g, "]($2")
    .replace(/\]\((mention:\/\/[A-Za-z0-9.-]+)\)\?([^\s)]+)/g, "]($1?$2)");
}

export function isMentionOnlyMarkdownContent(content: string): boolean {
  const trimmed = content.trim();
  if (trimmed.length === 0) {
    return false;
  }
  if (markdownLinkEndIndex(trimmed, 0) !== trimmed.length) {
    return false;
  }
  const labelEnd = trimmed.indexOf("]");
  return isRichTextMentionHref(trimmed.slice(labelEnd + 2));
}

export function normalizePlainSessionMentionTitle(content: string): string {
  const trimmed = content.trim();
  if (
    trimmed !== content ||
    !trimmed.startsWith("@") ||
    trimmed.includes("\n")
  ) {
    return content;
  }

  for (const agentLabel of PLAIN_SESSION_MENTION_AGENT_LABELS) {
    const separator = ` & ${agentLabel}`;
    const separatorIndex = trimmed.indexOf(separator);
    if (separatorIndex <= 1) {
      continue;
    }

    const userLabel = trimmed.slice(1, separatorIndex).trim();
    if (!userLabel) {
      continue;
    }

    return content;
  }

  return content;
}

export function markdownUrlTransform(value: string, key = ""): string {
  const target = value.trim();
  if (key === "src") {
    const workspacePath = resolveMarkdownWorkspaceMediaPath(target);
    if (workspacePath && isWindowsAbsolutePath(workspacePath)) {
      return windowsPathToFileUrl(workspacePath);
    }
  }
  return isRichTextMentionHref(target) ||
    isExplicitWorkspaceFilePath(target) ||
    isStandardMarkdownLinkHref(target)
    ? target
    : defaultUrlTransform(value);
}

function resolveMarkdownFileUrlPath(value: string): string | null {
  if (!value.toLowerCase().startsWith("file:")) {
    return null;
  }

  try {
    const url = new URL(value);
    if (url.protocol !== "file:" || url.hostname) {
      return null;
    }
    const decodedPath = decodeURIComponent(url.pathname);
    return /^[A-Za-z]:[\\/]/.test(decodedPath.slice(1))
      ? decodedPath.slice(1)
      : decodedPath;
  } catch {
    return null;
  }
}

function windowsPathToFileUrl(path: string): string {
  const normalizedPath = path.replaceAll("\\", "/");
  const drive = normalizedPath.slice(0, 2);
  const segments = normalizedPath.slice(2).split("/");
  return `file:///${drive}${segments.map((segment) => encodeURIComponent(segment)).join("/")}`;
}

type MentionKind =
  | "session"
  | "workspace-member"
  | "agent-target"
  | "workspace-app"
  | "workspace-reference"
  | "workspace-app-factory"
  | "workspace-issue"
  | "pasted-text";

export interface ParsedMentionLink {
  agentProviderId?: string;
  appId?: string;
  entityId: string;
  kind: MentionKind;
  label: string;
  iconUrl?: string;
  providerId: string;
  referenceSource?: string;
  scope?: Readonly<Record<string, string>>;
  /** 引用文件数量(workspace-reference 专用,来自 href 的 count 参数)。 */
  fileCount?: number;
}

export function parseMentionLink(
  href: string,
  rawLabel: string,
  workspaceAppIcons: readonly AgentMessageMarkdownWorkspaceAppIcon[] = [],
  agentTargets: readonly AgentMessageMarkdownAgentTarget[] = EMPTY_AGENT_TARGETS,
  appFactoryFallbackLabel = "Create app"
): ParsedMentionLink | null {
  const mention = parseRichTextMentionHref(href, rawLabel);
  if (!mention) {
    return null;
  }
  const resource = mention.providerId.trim().toLowerCase();
  const kind =
    resource === "agent-session"
      ? "session"
      : resource === "workspace-app"
        ? "workspace-app"
        : resource === "workspace-reference"
          ? "workspace-reference"
          : resource === "workspace-app-factory"
            ? "workspace-app-factory"
            : resource === "workspace-issue"
              ? "workspace-issue"
              : resource === "agent-target"
                ? "agent-target"
                : resource === "pasted-text"
                  ? "pasted-text"
                  : resource;
  if (
    kind !== "session" &&
    kind !== "workspace-member" &&
    kind !== "agent-target" &&
    kind !== "workspace-app" &&
    kind !== "workspace-reference" &&
    kind !== "workspace-app-factory" &&
    kind !== "workspace-issue" &&
    kind !== "pasted-text"
  ) {
    return null;
  }
  const entityId = mention.entityId.trim();
  if (!entityId) {
    return null;
  }
  const label =
    rawLabel.trim().replace(/^@+/, "").trim() ||
    (kind === "workspace-app-factory" ? appFactoryFallbackLabel : "");
  const identity = {
    entityId,
    providerId: mention.providerId,
    ...(mention.scope ? { scope: mention.scope } : {})
  };
  if (kind === "workspace-member") {
    if (!mention.scope?.workspaceId || mention.scope?.userId !== entityId) return null;
    return { ...identity, kind, label };
  }
  if (kind === "pasted-text") {
    return { ...identity, kind, label };
  }
  if (kind === "workspace-app" || kind === "workspace-app-factory") {
    const appId = kind === "workspace-app" ? entityId : "";
    const workspaceId = mention.scope?.workspaceId?.trim() || "";
    return {
      ...identity,
      kind,
      ...(kind === "workspace-app" ? { appId } : {}),
      label,
      ...(kind === "workspace-app"
        ? {
            iconUrl: resolveWorkspaceAppMentionIconUrl({
              appId,
              workspaceAppIcons,
              workspaceId
            })
          }
        : {})
    };
  }
  if (kind === "agent-target") {
    const workspaceId = mention.scope?.workspaceId?.trim() || "";
    const scopedProvider = mention.scope?.agentProviderId?.trim() || "";
    const presentation = resolveAgentMentionTargetPresentation({
      agentTargetId: entityId,
      agentTargets,
      fallbackIconUrl: managedAgentRoundedIconUrl(scopedProvider || undefined),
      fallbackName: label,
      fallbackProvider: scopedProvider,
      workspaceId
    });
    return {
      ...identity,
      agentProviderId: presentation.provider,
      kind,
      label: presentation.name ?? label,
      iconUrl: presentation.iconUrl
    };
  }
  if (kind === "session") {
    const workspaceId = mention.scope?.workspaceId?.trim() || "";
    const agentTargetId = mention.scope?.agentTargetId?.trim() || "";
    const presentation = resolveAgentMentionTargetPresentation({
      agentTargetId,
      agentTargets,
      workspaceId
    });
    return {
      ...identity,
      kind,
      label,
      iconUrl: presentation.iconUrl
    };
  }
  if (kind === "workspace-reference") {
    const source = mention.scope?.source?.trim() ?? "";
    const workspaceId = mention.scope?.workspaceId?.trim() || "";
    const appIconUrl =
      source === "app"
        ? resolveWorkspaceAppMentionIconUrl({
            appId: entityId,
            workspaceAppIcons,
            workspaceId
          })
        : undefined;
    return {
      ...identity,
      kind,
      label,
      iconUrl: mention.scope?.icon?.trim() || appIconUrl,
      fileCount: referenceFileCountFromParam(mention.scope?.count ?? null),
      referenceSource: source || undefined
    };
  }
  if (kind === "workspace-issue") {
    return {
      ...identity,
      kind,
      label
    };
  }
  return {
    ...identity,
    kind,
    label
  };
}

export function referenceFileCountFromParam(
  value: string | null
): number | undefined {
  if (!value) {
    return undefined;
  }
  const parsed = Number.parseInt(value.trim(), 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

export function resolveWorkspaceAppMentionIconUrl(input: {
  appId: string;
  workspaceId: string;
  workspaceAppIcons: readonly AgentMessageMarkdownWorkspaceAppIcon[];
}): string | undefined {
  const appId = input.appId.trim();
  if (!appId) {
    return undefined;
  }
  const workspaceId = input.workspaceId.trim();
  const exactMatch = input.workspaceAppIcons.find(
    (icon) =>
      icon.appId.trim() === appId &&
      (icon.workspaceId?.trim() ?? "") === workspaceId &&
      icon.iconUrl?.trim()
  );
  const fallbackMatch = input.workspaceAppIcons.find(
    (icon) => icon.appId.trim() === appId && icon.iconUrl?.trim()
  );
  return (
    exactMatch?.iconUrl?.trim() || fallbackMatch?.iconUrl?.trim() || undefined
  );
}

export function markdownLinkEndIndex(content: string, index: number): number {
  if (content[index] !== "[") {
    return -1;
  }
  const labelEnd = content.indexOf("]", index + 1);
  if (labelEnd < 0 || content[labelEnd + 1] !== "(") {
    return -1;
  }
  const hrefEnd = content.indexOf(")", labelEnd + 2);
  return hrefEnd < 0 ? -1 : hrefEnd + 1;
}

export function codeSpanEndIndex(content: string, index: number): number {
  if (content[index] !== "`") {
    return -1;
  }
  let tickCount = 1;
  while (content[index + tickCount] === "`") {
    tickCount += 1;
  }
  const fence = "`".repeat(tickCount);
  const end = content.indexOf(fence, index + tickCount);
  return end < 0 ? -1 : end + tickCount;
}
