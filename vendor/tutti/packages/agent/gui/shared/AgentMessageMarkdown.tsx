import {
  type AnchorHTMLAttributes,
  type ComponentPropsWithoutRef,
  type JSX,
  type MouseEvent,
  useCallback,
  memo,
  useMemo,
  useState
} from "react";
import { useTranslation } from "../i18n/index";
import { cn } from "../app/renderer/lib/utils";
import ReactMarkdown from "react-markdown";
import rehypeSanitize, {
  defaultSchema,
  type Options as RehypeSanitizeOptions
} from "rehype-sanitize";
import remarkGfm from "remark-gfm";
import { workspaceFileName as basenameWorkspacePath } from "@tutti-os/workspace-file-manager/services";
import { resolveWorkspaceFileExtension } from "@tutti-os/workspace-file-preview";
import { MentionPill } from "@tutti-os/ui-system/components";
import { useResolvedRichTextMention } from "@tutti-os/ui-rich-text/editor";
import {
  resolveWorkspaceMentionLinkAction,
  resolveWorkspaceLinkAction,
  type WorkspaceLinkAction,
  type WorkspaceLinkActionSource
} from "../actions/workspaceLinkActions";
import { resolveAgentWorkspaceFileVisualKind } from "./workspaceFileVisualKind";
import { stabilizeStreamingMarkdownTail } from "./streamingMarkdownTailStabilizer";
import { useStreamingVisibleText } from "./useStreamingVisibleText";
import {
  useAgentTargetPresentations,
  type AgentMessageMarkdownAgentTarget
} from "./AgentTargetPresentationContext";
import { resolveAgentMentionTargetPresentation } from "./agentTargetPresentation";
import {
  activateMarkdownLink,
  activateMarkdownLinkFromKey,
  activateMarkdownLinkFromPointer,
  hasOpenPotentialMermaidFence,
  hashMarkdownProfilerContent,
  isLikelyLongerThanLineLimit,
  resolveMarkdownAnchorHref,
  splitStreamingMarkdownBlocks
} from "./agentMessageMarkdownRuntime";
import {
  isClickableMarkdownHref,
  isLocalAbsolutePath,
  isWindowsAbsolutePath,
  isMentionOnlyMarkdownContent,
  markdownUrlTransform,
  normalizeMentionMarkdownLinks,
  normalizeLocalPathMarkdownLinks,
  normalizePlainIssueMentionTitleContent,
  normalizePlainSessionMentionTitle,
  parseMentionLink,
  resolveMarkdownWorkspaceMediaPath,
  resolveRenderableMarkdownMediaSrc,
  type ParsedMentionLink
} from "./agentMessageMarkdownLinks";
import { MarkdownLinkContext } from "./agentMessageMarkdownContext";
import {
  MarkdownCode,
  MarkdownListItem,
  MarkdownOrderedList,
  MarkdownParagraph,
  MarkdownUnorderedList,
  textFromReactNode
} from "./AgentMessageMarkdownRenderers";
import { MarkdownTable } from "@multica/tutti-agent-gui-host/markdown-table";
import { MarkdownMedia } from "./AgentMessageMarkdownMedia";
import { MarkdownPre } from "@multica/tutti-agent-gui-host/html-preview-pre";
import { remarkLiteralAutolinkBoundary } from "./remarkLiteralAutolinkBoundary";
import { cachedMarkdownParser } from "./cachedMarkdownParser";
import { ResourceMarkdownLink, remarkMessageResourceLinks } from "@multica/tutti-agent-gui-host/resource-link";
export { resetCachedMarkdownImagesForTests } from "./AgentMessageMarkdownMedia";
export type { StreamingMarkdownBlock } from "./agentMessageMarkdownRuntime";
export { splitStreamingMarkdownBlocks } from "./agentMessageMarkdownRuntime";

const STREAMING_MARKDOWN_FRAME_MS = 40;
const STREAMING_MARKDOWN_MAX_CHARS_PER_SECOND = 6_000;
const STREAMING_MARKDOWN_TAIL_FLUSH_CHARS = 0;
const STANDARD_MARKDOWN_LINK_PROTOCOLS = [
  "http",
  "https",
  "irc",
  "ircs",
  "mailto",
  "tel",
  "xmpp"
] as const;
const WINDOWS_DRIVE_HREF_PROTOCOLS =
  "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ".split("");
export const AGENT_MARKDOWN_PLAIN_TITLE_CLASSNAME =
  "[font-size:inherit] [line-height:inherit] text-inherit [&_a]:text-inherit [&_a]:font-inherit [&_a]:no-underline [&_a:hover]:no-underline [&_a:focus-visible]:no-underline [&_strong]:font-inherit [&_strong]:text-inherit";

const MARKDOWN_SANITIZE_SCHEMA: RehypeSanitizeOptions = {
  ...defaultSchema,
  protocols: {
    ...defaultSchema.protocols,
    href: [
      ...(defaultSchema.protocols?.href ?? []),
      "mention",
      ...STANDARD_MARKDOWN_LINK_PROTOCOLS,
      ...WINDOWS_DRIVE_HREF_PROTOCOLS
    ],
    src: [...(defaultSchema.protocols?.src ?? []), "file"]
  }
};

interface MarkdownHastNode {
  type?: string;
  properties?: Record<string, unknown>;
  children?: MarkdownHastNode[];
}

function normalizeWindowsMarkdownMediaPaths() {
  return (tree: MarkdownHastNode): void => {
    normalizeWindowsMarkdownMediaPathNode(tree);
  };
}

function normalizeWindowsMarkdownMediaPathNode(node: MarkdownHastNode): void {
  if (node.type === "element" && typeof node.properties?.src === "string") {
    const src = node.properties.src;
    const workspacePath = resolveMarkdownWorkspaceMediaPath(src);
    if (workspacePath && isWindowsAbsolutePath(workspacePath)) {
      node.properties.src = resolveRenderableMarkdownMediaSrc(src);
    }
  }
  for (const child of node.children ?? []) {
    normalizeWindowsMarkdownMediaPathNode(child);
  }
}

export interface AgentMessageMarkdownWorkspaceLinkContext {
  workspaceRoot?: string | null;
  basePath?: string | null;
  source: WorkspaceLinkActionSource;
}

interface AgentMessageMarkdownProps {
  content: string;
  documentCacheKey?: string;
  onLinkClick?: (href: string) => void;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  workspaceLinkContext?: AgentMessageMarkdownWorkspaceLinkContext | null;
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  agentTargets?: readonly AgentMessageMarkdownAgentTarget[];
  collapsible?: boolean;
  expandLabel?: string;
  className?: string;
  inline?: boolean;
  normalizePlainIssueMentionTitle?: boolean;
  enableImageZoom?: boolean;
  streaming?: boolean;
}

export interface AgentMessageMarkdownWorkspaceAppIcon {
  appId: string;
  iconUrl: string | null;
  workspaceId?: string | null;
}

const EMPTY_WORKSPACE_APP_ICONS: readonly AgentMessageMarkdownWorkspaceAppIcon[] =
  [];
const EMPTY_AGENT_TARGETS: readonly AgentMessageMarkdownAgentTarget[] = [];

export type MarkdownDomProps<Tag extends keyof JSX.IntrinsicElements> =
  ComponentPropsWithoutRef<Tag> & {
    node?: unknown;
  };

type ReactMarkdownComponents = ComponentPropsWithoutRef<
  typeof ReactMarkdown
>["components"];
type ReactMarkdownRemarkPlugins = NonNullable<
  ComponentPropsWithoutRef<typeof ReactMarkdown>["remarkPlugins"]
>;

export function AgentMessageMarkdown({
  content,
  documentCacheKey,
  onLinkClick,
  onLinkAction,
  workspaceLinkContext = null,
  workspaceAppIcons = EMPTY_WORKSPACE_APP_ICONS,
  agentTargets,
  collapsible = false,
  expandLabel,
  className,
  inline = false,
  normalizePlainIssueMentionTitle = false,
  enableImageZoom = false,
  streaming = false
}: AgentMessageMarkdownProps): JSX.Element {
  "use memo";
  const { t } = useTranslation();
  const contextAgentTargets = useAgentTargetPresentations();
  const effectiveAgentTargets = agentTargets ?? contextAgentTargets;
  const visibleContent = useStreamingVisibleText(content, {
    enabled: streaming,
    frameMs: STREAMING_MARKDOWN_FRAME_MS,
    maxCharsPerSecond: STREAMING_MARKDOWN_MAX_CHARS_PER_SECOND,
    trailingFlushChars: STREAMING_MARKDOWN_TAIL_FLUSH_CHARS
  });
  const stabilizedContent = useMemo(
    () =>
      stabilizeStreamingMarkdownTail(visibleContent, {
        streaming
      }).content,
    [streaming, visibleContent]
  );
  const mermaidStreaming =
    streaming || hasOpenPotentialMermaidFence(visibleContent);
  const workspaceRoot = workspaceLinkContext?.workspaceRoot ?? null;
  const basePath = workspaceLinkContext?.basePath ?? null;
  const workspaceLinkSource = workspaceLinkContext?.source ?? null;
  const [isExpanded, setIsExpanded] = useState(false);
  const resolvedExpandLabel =
    expandLabel ?? t("agentHost.workspaceAgentMessageExpand");
  const shouldCollapse =
    collapsible && isLikelyLongerThanLineLimit(stabilizedContent);
  const isCollapsed = shouldCollapse && !isExpanded;
  const ContainerTag = inline ? "span" : "div";
  const normalizedContent = useMemo(
    () =>
      normalizeLocalPathMarkdownLinks(
        normalizeMentionMarkdownLinks(
          normalizePlainIssueMentionTitle
            ? normalizePlainIssueMentionTitleContent(
                normalizePlainSessionMentionTitle(stabilizedContent)
              )
            : normalizePlainSessionMentionTitle(stabilizedContent)
        )
      ),
    [normalizePlainIssueMentionTitle, stabilizedContent]
  );
  const settledRemarkPlugins = useMemo<ReactMarkdownRemarkPlugins>(
    () => [
      remarkGfm,
      remarkLiteralAutolinkBoundary,
      remarkMessageResourceLinks,
      [
        cachedMarkdownParser,
        {
          cacheKey: documentCacheKey?.trim() || "content",
          content: normalizedContent
        }
      ]
    ],
    [documentCacheKey, normalizedContent]
  );
  const isMentionOnly = isMentionOnlyMarkdownContent(normalizedContent);
  const handleLinkClick = useCallback(
    (href: string): void => {
      if (workspaceLinkSource && onLinkAction) {
        const action =
          resolveWorkspaceMentionLinkAction({
            href,
            source: workspaceLinkSource
          }) ??
          (workspaceRoot || basePath
            ? resolveWorkspaceLinkAction({
                href,
                workspaceRoot,
                basePath,
                source: workspaceLinkSource
              })
            : null);
        if (action) {
          onLinkAction(action);
          return;
        }
      }
      onLinkClick?.(href);
    },
    [basePath, onLinkAction, onLinkClick, workspaceLinkSource, workspaceRoot]
  );
  const handleAnchorClickCapture = useCallback(
    (event: MouseEvent<HTMLElement>): void => {
      const href = resolveMarkdownAnchorHref(event.target);
      if (!href) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      handleLinkClick(href);
    },
    [handleLinkClick]
  );
  const markdownComponents = useMemo(
    () => ({
      a: (props: MarkdownDomProps<"a">) => (
        <ResourceMarkdownLink href={props.href} fallback={<MarkdownLink {...props} onLinkClick={handleLinkClick} workspaceAppIcons={workspaceAppIcons} agentTargets={effectiveAgentTargets} />}>{props.children}</ResourceMarkdownLink>
      ),
      code: (props: MarkdownDomProps<"code">) => <MarkdownCode {...props} />,
      img: (props: MarkdownDomProps<"img">) => (
        <MarkdownMedia {...props} enableZoom={enableImageZoom} />
      ),
      p: (props: MarkdownDomProps<"p">) => (
        <MarkdownParagraph {...props} inline={inline} />
      ),
      table: MarkdownTable,
      ul: MarkdownUnorderedList,
      ol: MarkdownOrderedList,
      li: MarkdownListItem,
      pre: (props: MarkdownDomProps<"pre">) => (
        <MarkdownPre
          {...props}
          mermaidEnabled={!inline}
          streaming={mermaidStreaming}
        />
      )
    }),
    [
      effectiveAgentTargets,
      enableImageZoom,
      handleLinkClick,
      inline,
      mermaidStreaming,
      workspaceAppIcons
    ]
  );

  return (
    <ContainerTag
      className="flex w-full min-w-0 flex-col items-start gap-1"
      data-workspace-agent-markdown-shell="true"
    >
      <ContainerTag
        className={cn(
          "relative w-full min-w-0 overflow-x-auto text-[13px] leading-[1.5] text-[var(--text-primary)] [overflow-wrap:anywhere]",
          "[&_>table:first-child]:mt-0 [&_p]:mb-2 [&_pre]:mb-2 [&_blockquote]:mb-2",
          "[&_hr]:my-4 [&_hr]:h-0 [&_hr]:border-0 [&_hr]:border-t [&_hr]:border-t-[color-mix(in_srgb,var(--text-primary)_14%,transparent)]",
          "[&_ul]:my-2 [&_ul]:max-w-full [&_ul]:rounded-[8px] [&_ul]:border [&_ul]:border-[var(--line-2)] [&_ul]:bg-[var(--background-panel)] [&_ul]:px-4 [&_ul]:py-2",
          "[&_ol]:my-2 [&_ol]:max-w-full [&_ol]:rounded-[8px] [&_ol]:border [&_ol]:border-[var(--line-2)] [&_ol]:bg-[var(--background-panel)] [&_ol]:px-4 [&_ol]:py-2",
          "[&_li>ul]:mt-1.5 [&_li>ul]:mb-0.5 [&_li>ul]:border-0 [&_li>ul]:px-0 [&_li>ul]:py-1",
          "[&_li>ol]:mt-1.5 [&_li>ol]:mb-0.5 [&_li>ol]:border-0 [&_li>ol]:px-0 [&_li>ol]:py-1",
          "[&_table]:my-2 [&_table]:w-max [&_table]:min-w-full [&_table]:max-w-full [&_table]:border-separate [&_table]:border-spacing-0 [&_table]:overflow-hidden [&_table]:rounded-[8px] [&_table]:border [&_table]:border-[var(--line-2)] [&_table]:text-[13px] [&_table]:leading-[1.45]",
          "[&_th]:max-w-[280px] [&_th]:border-r [&_th]:border-b [&_th]:border-[var(--line-2)] [&_th]:px-2 [&_th]:py-1.5 [&_th]:align-top [&_th]:font-semibold [&_th]:text-[var(--text-primary)] [&_th]:[overflow-wrap:anywhere] [&_th]:bg-[color-mix(in_srgb,var(--background-panel)_94%,var(--text-primary))]",
          "[&_td]:max-w-[280px] [&_td]:border-r [&_td]:border-b [&_td]:border-[var(--line-2)] [&_td]:px-2 [&_td]:py-1.5 [&_td]:align-top [&_td]:[overflow-wrap:anywhere]",
          "[&_tr:last-child_th]:border-b-0 [&_tr:last-child_td]:border-b-0 [&_th:last-child]:border-r-0 [&_td:last-child]:border-r-0",
          "[&_a]:cursor-pointer [&_a]:font-semibold [&_a]:text-[var(--tutti-purple)] [&_a]:no-underline [&_a:hover]:underline [&_a:focus-visible]:underline",
          "[&_strong]:font-semibold",
          "[&_code]:inline [&_code]:rounded-[2px] [&_code]:bg-[var(--transparency-block)] [&_code]:px-1 [&_code]:py-[1px] [&_code]:font-[var(--tsh-font-mono)] [&_code]:text-[11px] [&_code]:leading-[1.35] [&_code]:text-[var(--text-primary)] [&_code]:[box-decoration-break:clone] [&_code]:[-webkit-box-decoration-break:clone] [&_code]:[overflow-wrap:anywhere] [&_code]:[word-break:break-word]",
          "[&_pre]:box-border [&_pre]:overflow-auto [&_pre]:rounded-[6px] [&_pre]:bg-[var(--transparency-block)] [&_pre]:px-2.5 [&_pre]:py-2",
          "[&_pre_code]:inline [&_pre_code]:h-auto [&_pre_code]:items-normal [&_pre_code]:rounded-none [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_pre_code]:text-[inherit] [&_pre_code]:leading-[inherit] [&_pre_code]:[white-space:pre-wrap] [&_pre_code]:[overflow-wrap:anywhere] [&_pre_code]:[word-break:break-word]",
          "[&>*:first-child]:mt-0 [&>*:last-child]:mb-0",
          inline &&
            "inline min-w-0 overflow-hidden align-baseline [&_p]:inline [&_p]:m-0",
          shouldCollapse &&
            "overflow-hidden transition-[max-height] duration-220 ease-out",
          isCollapsed &&
            "max-h-[calc(13px*1.5*8)] overflow-hidden [mask-image:linear-gradient(180deg,black_0%,black_calc(100%_-_36px),transparent_100%)] [-webkit-mask-image:linear-gradient(180deg,black_0%,black_calc(100%_-_36px),transparent_100%)] [&_pre]:overflow-hidden",
          shouldCollapse && !isCollapsed && "max-h-[72rem]",
          // Blocks that finished stop costing layout on each update once off screen; the last one is still changing.
          streaming &&
            "[&>*:not(:last-child)]:[content-visibility:auto] [&>*:not(:last-child)]:[contain-intrinsic-size:auto_24px]",
          className
        )}
        data-workspace-agent-markdown="true"
        data-agent-mention-only={isMentionOnly ? "true" : undefined}
        data-collapsed={isCollapsed ? "true" : "false"}
        onClickCapture={handleAnchorClickCapture}
      >
        {streaming ? (
          <StreamingMarkdownBlocks
            content={normalizedContent}
            components={markdownComponents}
          />
        ) : (
          <ReactMarkdown
            remarkPlugins={settledRemarkPlugins}
            rehypePlugins={[
              normalizeWindowsMarkdownMediaPaths,
              [rehypeSanitize, MARKDOWN_SANITIZE_SCHEMA]
            ]}
            urlTransform={markdownUrlTransform}
            components={markdownComponents}
          >
            {normalizedContent}
          </ReactMarkdown>
        )}
      </ContainerTag>
      {shouldCollapse && !isExpanded ? (
        <button
          type="button"
          className="m-0 border-0 bg-transparent p-0 text-[11px] leading-[1.4] font-semibold text-[var(--tutti-purple)] hover:underline focus-visible:underline focus-visible:outline-none"
          onClick={() => setIsExpanded(true)}
        >
          {resolvedExpandLabel}
        </button>
      ) : null}
    </ContainerTag>
  );
}

function StreamingMarkdownBlocks({
  content,
  components
}: {
  content: string;
  components: ReactMarkdownComponents;
}): JSX.Element {
  const blocks = useMemo(
    () => splitStreamingMarkdownBlocks(content),
    [content]
  );
  return (
    <>
      {blocks.map((block, index) => (
        <MemoizedMarkdownBlock
          key={`${index}:${hashMarkdownProfilerContent(block.initialKeyContent)}`}
          content={block.content}
          components={components}
        />
      ))}
    </>
  );
}

const MemoizedMarkdownBlock = memo(function MemoizedMarkdownBlock({
  content,
  components
}: {
  content: string;
  components: ReactMarkdownComponents;
}): JSX.Element {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm, remarkLiteralAutolinkBoundary, remarkMessageResourceLinks]}
      rehypePlugins={[
        normalizeWindowsMarkdownMediaPaths,
        [rehypeSanitize, MARKDOWN_SANITIZE_SCHEMA]
      ]}
      urlTransform={markdownUrlTransform}
      components={components}
    >
      {content}
    </ReactMarkdown>
  );
});

function MarkdownLink({
  node: _node,
  onClick: _onClick,
  onLinkClick,
  workspaceAppIcons,
  agentTargets,
  href,
  ...props
}: MarkdownDomProps<"a"> & {
  onLinkClick?: (href: string) => void;
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  agentTargets?: readonly AgentMessageMarkdownAgentTarget[];
}): JSX.Element {
  "use memo";
  const { t } = useTranslation();
  const targetHref = href?.trim() ?? "";
  const mention = targetHref
    ? parseMentionLink(
        targetHref,
        textFromReactNode(props.children),
        workspaceAppIcons ?? [],
        agentTargets ?? EMPTY_AGENT_TARGETS,
        t("agentHost.agentGui.workspaceAppFactoryMentionFallback")
      )
    : null;
  if (mention) {
    return (
      <MentionLink
        {...props}
        agentTargets={agentTargets ?? EMPTY_AGENT_TARGETS}
        href={targetHref}
        mention={mention}
        onLinkClick={onLinkClick}
      />
    );
  }
  const fileMention = targetHref
    ? parseWorkspaceFileMentionLink(
        targetHref,
        textFromReactNode(props.children)
      )
    : null;
  if (fileMention) {
    return (
      <WorkspaceFileMentionLink
        {...props}
        href={targetHref}
        mention={fileMention}
        onLinkClick={onLinkClick}
      />
    );
  }
  if (!isClickableMarkdownHref(targetHref)) {
    return (
      <MarkdownLinkContext.Provider value={true}>
        <span className={props.className} title={props.title}>
          {props.children}
        </span>
      </MarkdownLinkContext.Provider>
    );
  }

  return (
    <MarkdownLinkContext.Provider value={true}>
      <a
        {...props}
        data-agent-link-href={targetHref}
        role="link"
        tabIndex={0}
        onClick={(event) => {
          activateMarkdownLink(event, targetHref, onLinkClick);
        }}
        onPointerDown={(event) => {
          activateMarkdownLinkFromPointer(event, targetHref, onLinkClick);
        }}
        onKeyDown={(event) => {
          activateMarkdownLinkFromKey(event, targetHref, onLinkClick);
        }}
      />
    </MarkdownLinkContext.Provider>
  );
}

interface ParsedWorkspaceFileMentionLink {
  label: string;
  href: string;
  entryKind: "file" | "directory";
  visualKind: string;
}

interface ParsedWorkspaceFileMentionTarget {
  path: string;
  explicitKind: "file" | "directory" | null;
}

function parseWorkspaceFileMentionTarget(
  href: string
): ParsedWorkspaceFileMentionTarget | null {
  const rawTarget = href.trim();
  const target = /^\/[A-Za-z]:[\\/]/.test(rawTarget)
    ? rawTarget.slice(1)
    : rawTarget;
  if (isWindowsAbsolutePath(target)) {
    const path = target.replace(/[\\/]+$/, "");
    if (!path) {
      return null;
    }
    return {
      path,
      explicitKind: /[\\/]$/.test(target) ? "directory" : null
    };
  }
  if (!isLocalAbsolutePath(target)) {
    return null;
  }

  let url: URL;
  try {
    url = new URL(target, "https://tsh.local");
  } catch {
    return null;
  }
  const explicitKindValue =
    url.searchParams.get("kind") ??
    url.searchParams.get("refType") ??
    url.searchParams.get("entryKind") ??
    "";
  const normalizedKind = explicitKindValue.trim().toLowerCase();
  const explicitKind =
    normalizedKind === "folder" || normalizedKind === "directory"
      ? "directory"
      : normalizedKind === "file"
        ? "file"
        : null;
  const path = url.pathname.replace(/\/+$/, "");
  if (!path || path === "/") {
    return null;
  }

  return {
    path,
    explicitKind:
      explicitKind ?? (url.pathname.endsWith("/") ? "directory" : null)
  };
}

function resolveWorkspaceFileMentionEntryKind(
  path: string,
  label: string,
  explicitKind: "file" | "directory" | null
): "file" | "directory" {
  if (explicitKind) {
    return explicitKind;
  }
  if (resolveWorkspaceFileExtension(path)) {
    return "file";
  }

  const basename = basenameWorkspacePath(path);
  return basename === label ? "directory" : "file";
}

function parseWorkspaceFileMentionLink(
  href: string,
  rawLabel: string
): ParsedWorkspaceFileMentionLink | null {
  const label = rawLabel.trim();
  const target = parseWorkspaceFileMentionTarget(href);
  if (!target || !label.startsWith("@")) {
    return null;
  }
  const fileLabel = label.replace(/^@+/, "").trim();
  if (!fileLabel) {
    return null;
  }
  const entryKind = resolveWorkspaceFileMentionEntryKind(
    target.path,
    fileLabel,
    target.explicitKind
  );
  return {
    label: fileLabel,
    href: target.path,
    entryKind,
    visualKind: resolveAgentWorkspaceFileVisualKind(target.path, {
      refType: entryKind === "directory" ? "folder" : "file"
    })
  };
}

function WorkspaceFileMentionLink({
  onClick: _onClick,
  onLinkClick,
  href: _href,
  mention,
  ...props
}: AnchorHTMLAttributes<HTMLAnchorElement> & {
  href: string;
  mention: ParsedWorkspaceFileMentionLink;
  onLinkClick?: (href: string) => void;
}): JSX.Element {
  "use memo";
  return (
    <a
      {...props}
      className={cn(
        "tsh-workspace-file-link tsh-agent-object-token tsh-agent-object-token--file",
        props.className
      )}
      data-agent-file-mention="true"
      data-agent-mention-kind="file"
      data-agent-file-entry-kind={mention.entryKind}
      data-agent-file-visual-kind={mention.visualKind}
      data-agent-link-href={mention.href}
      data-agent-mention-href={mention.href}
      aria-label={mention.label}
      role="link"
      tabIndex={0}
      onClick={(event) => {
        activateMarkdownLink(event, mention.href, onLinkClick);
      }}
      onPointerDown={(event) => {
        activateMarkdownLinkFromPointer(event, mention.href, onLinkClick);
      }}
      onKeyDown={(event) => {
        activateMarkdownLinkFromKey(event, mention.href, onLinkClick);
      }}
    >
      <span className="tsh-agent-object-token__icon" aria-hidden="true" />
      <span className="tsh-agent-object-token__main">{mention.label}</span>
    </a>
  );
}

function MentionLink({
  agentTargets,
  onClick: _onClick,
  onLinkClick,
  href,
  mention,
  ...props
}: AnchorHTMLAttributes<HTMLAnchorElement> & {
  agentTargets: readonly AgentMessageMarkdownAgentTarget[];
  href: string;
  mention: ParsedMentionLink;
  onLinkClick?: (href: string) => void;
}): JSX.Element {
  "use memo";
  const snapshot = useResolvedRichTextMention({
    providerId: mention.providerId,
    entityId: mention.entityId,
    label: mention.label,
    scope: mention.scope
  });
  const resolved = snapshot.state === "ready" ? snapshot.resolved : undefined;
  const presentation = resolved?.presentation;
  const fallbackIconUrl =
    presentation?.iconUrl?.trim() ||
    presentation?.thumbnailUrl?.trim() ||
    presentation?.agentIconUrl?.trim() ||
    mention.iconUrl;
  const fallbackLabel = resolved?.label?.trim() || mention.label;
  const agentTargetPresentation =
    mention.kind === "agent-target" || mention.kind === "session"
      ? resolveAgentMentionTargetPresentation({
          agentTargetId:
            mention.kind === "agent-target"
              ? mention.entityId
              : mention.scope?.agentTargetId,
          agentTargets,
          fallbackIconUrl,
          fallbackName: fallbackLabel,
          fallbackProvider:
            presentation?.agentProviderId?.trim() || mention.agentProviderId,
          workspaceId: mention.scope?.workspaceId
        })
      : null;
  const label =
    mention.kind === "agent-target"
      ? (agentTargetPresentation?.name ?? fallbackLabel)
      : fallbackLabel;
  const iconUrl = agentTargetPresentation?.iconUrl ?? fallbackIconUrl;
  const pillKind =
    mention.kind === "workspace-issue" || mention.referenceSource === "task"
      ? "issue"
      : mention.kind === "session" || mention.kind === "agent-target"
        ? "session"
        : mention.kind === "pasted-text"
          ? "file"
          : "app";
  const usesSessionIcon = mention.kind === "session";

  return (
    <a
      {...props}
      className={cn(
        "tsh-agent-object-token tsh-agent-object-token--entity",
        props.className
      )}
      data-agent-file-mention="true"
      data-agent-link-href={href}
      data-agent-mention-icon-url={iconUrl}
      data-agent-mention-href={href}
      data-agent-mention-kind={mention.kind}
      data-agent-reference-source={mention.referenceSource}
      aria-label={label}
      role="link"
      tabIndex={0}
      onClick={(event) => {
        activateMarkdownLink(event, href, onLinkClick);
      }}
      onPointerDown={(event) => {
        activateMarkdownLinkFromPointer(event, href, onLinkClick);
      }}
      onKeyDown={(event) => {
        activateMarkdownLinkFromKey(event, href, onLinkClick);
      }}
    >
      <MentionPill
        className="top-0 max-w-full"
        iconUrl={iconUrl}
        iconContainerProps={{
          className: "h-4 w-4",
          "data-agent-mention-app-icon": usesSessionIcon ? undefined : "true",
          "data-agent-mention-session-icon": usesSessionIcon
            ? "true"
            : undefined,
          "data-workspace-app-icon": usesSessionIcon ? undefined : "true"
        }}
        fallbackIconProps={{
          "data-agent-mention-fallback-icon": "true"
        }}
        kind={pillKind}
        label={<span className="tsh-agent-object-token__main">{label}</span>}
        removable={false}
        tooltipEnabled
        withTooltipProvider
      />
    </a>
  );
}
