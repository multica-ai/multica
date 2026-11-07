import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo, type JSX, type MouseEvent } from "react";
import type { JSONContent } from "@tiptap/core";
import { renderToReactElement } from "@tiptap/static-renderer";
import { cn } from "../app/renderer/lib/utils";
import { plainTextToAgentRichTextDoc } from "../agent-gui/agentGuiNode/agentRichText/agentRichTextDocument";
import { AGENT_RICH_TEXT_CARET_ANCHOR } from "../agent-gui/agentGuiNode/agentRichText/agentRichTextCaretAnchor";
import { createAgentRichTextReadonlyExtensions } from "../agent-gui/agentGuiNode/agentRichText/agentRichTextExtensions";
import { AgentMentionReadonlyView } from "../agent-gui/agentGuiNode/agentRichText/AgentMentionNodeView";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "./AgentMessageMarkdown";
import type { AgentGUIProviderSkillOption } from "../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import {
  useAgentTargetPresentations,
  type AgentMessageMarkdownAgentTarget
} from "./AgentTargetPresentationContext";
import { readParsedDocumentCache } from "./parsedDocumentCache";
import { observer } from "mobx-react-lite";
export const AgentRichTextReadonly = observer(AgentRichTextReadonlyRender);

const EMPTY_WORKSPACE_APP_ICONS: readonly AgentMessageMarkdownWorkspaceAppIcon[] =
  [];
const EMPTY_SKILLS: readonly AgentGUIProviderSkillOption[] = [];

interface AgentRichTextReadonlyProps {
  value: string;
  documentCacheKey?: string;
  className?: string;
  editorClassName?: string;
  onLinkClick?: (href: string) => void;
  availableSkills?: readonly AgentGUIProviderSkillOption[];
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  agentTargets?: readonly AgentMessageMarkdownAgentTarget[];
}

function AgentRichTextReadonlyRender({
  value,
  documentCacheKey,
  className,
  editorClassName,
  onLinkClick,
  availableSkills = EMPTY_SKILLS,
  workspaceAppIcons = EMPTY_WORKSPACE_APP_ICONS,
  agentTargets
}: AgentRichTextReadonlyProps): JSX.Element {
  "use memo";
  const contextAgentTargets = useAgentTargetPresentations();
  const effectiveAgentTargets = agentTargets ?? contextAgentTargets;
  const quotedPrompt = useMemo(() => {
    const match = /^(> [^\n]*(?:\n> [^\n]*)*)\n\n([\s\S]+)$/u.exec(value.replace(/\r\n?/gu, "\n"));
    return match ? { quote: match[1]!.replace(/^> /gmu, ""), prompt: match[2]! } : null;
  }, [value]);
  const promptValue = quotedPrompt?.prompt ?? value;
  const skillsCacheSource = useMemo(
    () => JSON.stringify(availableSkills),
    [availableSkills]
  );
  const parsedDoc = useMemo(
    () =>
      readParsedDocumentCache({
        namespace: "rich-text",
        identity: documentCacheKey?.trim() || "content",
        source: `${promptValue}\u0000${skillsCacheSource}`,
        create: () =>
          plainTextToAgentRichTextDoc(promptValue, { skills: availableSkills })
      }),
    [availableSkills, documentCacheKey, skillsCacheSource, promptValue]
  );
  const contentDoc = useMemo(
    () =>
      workspaceAppIcons.length > 0
        ? hydrateWorkspaceAppMentionIcons(parsedDoc, workspaceAppIcons)
        : parsedDoc,
    [parsedDoc, workspaceAppIcons]
  );
  const isMentionOnly = isMentionOnlyRichTextDoc(contentDoc);
  const extensions = useMemo(
    () =>
      createAgentRichTextReadonlyExtensions({
        skills: availableSkills
      }),
    [availableSkills]
  );
  const renderedContent = useMemo(
    () =>
      renderToReactElement({
        content: contentDoc,
        extensions,
        options: {
          nodeMapping: {
            agentFileMention: ({ node }) => (
              <AgentMentionReadonlyViewBinding1
                effectiveAgentTargets={effectiveAgentTargets}
                node={node}
              />
            )
          }
        }
      }),
    [contentDoc, effectiveAgentTargets, extensions]
  );

  const handleClick = (event: MouseEvent<HTMLDivElement>) => {
    if (!onLinkClick || !(event.target instanceof Element)) {
      return;
    }
    const mention = event.target.closest(
      '[data-agent-file-mention="true"][data-agent-mention-href]'
    );
    if (!(mention instanceof HTMLElement)) {
      return;
    }
    const href = mention.getAttribute("data-agent-mention-href") || "";
    if (!href) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    onLinkClick(href);
  };
  const handleClickCallback = usePersistCallback(handleClick);

  return (
    <div
      className={className}
      data-agent-mention-only={isMentionOnly && !quotedPrompt ? "true" : undefined}
    >
      {quotedPrompt && <blockquote className="agent-gui-submitted-quote">{quotedPrompt.quote}</blockquote>}
      <div
        className={cn(
          "tiptap ProseMirror",
          editorClassName,
          "max-w-full overflow-x-hidden overflow-y-auto whitespace-pre-wrap break-words [overflow-wrap:anywhere] [&_p]:m-0 [&_p]:min-h-[1.45em] [&_a[data-agent-file-mention=true]]:cursor-pointer [&_[data-agent-file-mention=true]]:overflow-hidden"
        )}
        onClick={handleClickCallback}
      >
        {renderedContent}
      </div>
    </div>
  );
}

function isMentionOnlyRichTextDoc(doc: JSONContent): boolean {
  if (doc.type !== "doc") {
    return false;
  }
  const blocks = doc.content ?? [];
  if (blocks.length !== 1) {
    return false;
  }
  const paragraph = blocks[0];
  if (paragraph?.type !== "paragraph") {
    return false;
  }
  const inlineContent = (paragraph.content ?? []).filter(
    (node) =>
      !(
        node.type === "text" &&
        (node.text ?? "").replaceAll(AGENT_RICH_TEXT_CARET_ANCHOR, "")
          .length === 0
      )
  );
  return (
    inlineContent.length === 1 && inlineContent[0]?.type === "agentFileMention"
  );
}

function hydrateWorkspaceAppMentionIcons(
  node: JSONContent,
  workspaceAppIcons: readonly AgentMessageMarkdownWorkspaceAppIcon[]
): JSONContent {
  const nextContent = node.content?.map((child) =>
    hydrateWorkspaceAppMentionIcons(child, workspaceAppIcons)
  );
  if (node.type !== "agentFileMention") {
    return nextContent ? { ...node, content: nextContent } : node;
  }
  const attrs = node.attrs ?? {};
  const kind = typeof attrs.kind === "string" ? attrs.kind : "";
  const isWorkspaceAppMention = kind === "workspace-app";
  const isAppWorkspaceReferenceMention =
    kind === "workspace-reference" && attrs.source === "app";
  if (!isWorkspaceAppMention && !isAppWorkspaceReferenceMention) {
    return nextContent ? { ...node, content: nextContent } : node;
  }
  const workspaceId =
    typeof attrs.workspaceId === "string" ? attrs.workspaceId.trim() : "";
  const appId =
    isWorkspaceAppMention && typeof attrs.appId === "string"
      ? attrs.appId.trim()
      : typeof attrs.targetId === "string"
        ? attrs.targetId.trim()
        : "";
  const iconUrl = resolveWorkspaceAppIconUrl({
    appId,
    workspaceId,
    workspaceAppIcons
  });
  if (!iconUrl) {
    return nextContent ? { ...node, content: nextContent } : node;
  }
  return {
    ...node,
    attrs: {
      ...node.attrs,
      iconUrl
    },
    ...(nextContent ? { content: nextContent } : {})
  };
}

function resolveWorkspaceAppIconUrl(input: {
  appId: string;
  workspaceId: string;
  workspaceAppIcons: readonly AgentMessageMarkdownWorkspaceAppIcon[];
}): string | undefined {
  if (!input.appId) {
    return undefined;
  }
  const exactMatch = input.workspaceAppIcons.find(
    (icon) =>
      icon.appId.trim() === input.appId &&
      (icon.workspaceId?.trim() ?? "") === input.workspaceId &&
      icon.iconUrl?.trim()
  );
  const fallbackMatch = input.workspaceAppIcons.find(
    (icon) => icon.appId.trim() === input.appId && icon.iconUrl?.trim()
  );
  return (
    exactMatch?.iconUrl?.trim() || fallbackMatch?.iconUrl?.trim() || undefined
  );
}

const AgentMentionReadonlyViewBinding1 = observer(
  function AgentMentionReadonlyViewBinding1({
    effectiveAgentTargets,
    node
  }: AgentMentionReadonlyViewBinding1Props) {
    const attrsProp1 = useMemo<
      NonNullable<
        React.ComponentProps<typeof AgentMentionReadonlyView>["attrs"]
      >
    >(() => ({}), []);
    return (
      <AgentMentionReadonlyView
        agentTargets={effectiveAgentTargets}
        attrs={node.attrs ?? attrsProp1}
      />
    );
  }
);

type AgentMentionReadonlyViewBinding1Props = {
  effectiveAgentTargets: readonly AgentMessageMarkdownAgentTarget[];
  node: import("@tiptap/pm/model").Node;
};
