"use client";

import * as React from "react";
import {
  Markdown as MarkdownBase,
  type MarkdownProps as MarkdownBaseProps,
  type RenderMode,
} from "@multica/ui/markdown";
import { useConfigStore } from "@multica/core/config";
import type { Attachment as AttachmentRecord } from "@multica/core/types";
import { useWorkspacePaths, useWorkspaceSlug } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { IssueMentionCard } from "../issues/components/issue-mention-card";
import { ProjectChip } from "../projects/components/project-chip";
import {
  Attachment as AttachmentRenderer,
  AttachmentDownloadProvider,
} from "../editor";
import { MermaidDiagram } from "../editor/mermaid-diagram";
import { HtmlBlockPreview } from "../editor/html-block-preview";
import { useLinkHover, LinkHoverCard } from "../editor/link-hover-card";
import { openLink } from "../editor/utils/link-handler";
import { preprocessJsonLiterals } from "../editor/utils/preprocess-json";
import { highlightToHtml } from "../editor/utils/highlight-markdown";
import "../editor/styles/index.css";

export type { RenderMode };

export interface MarkdownProps extends MarkdownBaseProps {
  /**
   * Attachments associated with the surrounding entity (chat message, skill
   * file). When passed, the renderer resolves inline image / file-card URLs
   * to full attachment records via AttachmentDownloadProvider, unlocking the
   * unified hover toolbar / lightbox / preview-modal behavior used in
   * editor surfaces.
   */
  attachments?: AttachmentRecord[];
}

// ---------------------------------------------------------------------------
// Mention rendering — shared between all modes
// ---------------------------------------------------------------------------

function ProjectMentionLink({ projectId, label }: { projectId: string; label?: string }) {
  const { push, openInNewTab } = useNavigation();
  const p = useWorkspacePaths();
  const path = p.projectDetail(projectId);
  return (
    <span
      className="inline align-middle"
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        if (e.metaKey || e.ctrlKey || e.shiftKey) {
          if (openInNewTab) {
            openInNewTab(path, label);
          }
          return;
        }
        push(path);
      }}
    >
      <ProjectChip projectId={projectId} fallbackLabel={label} className="cursor-pointer hover:bg-accent transition-colors" />
    </span>
  );
}

/**
 * Default renderMention that delegates to entity chips for issue/project mentions
 * and renders a styled span for other mention types.
 */
function defaultRenderMention({
  type,
  id,
  label,
}: {
  type: string;
  id: string;
  label?: string;
}): React.ReactNode {
  if (type === "issue") {
    return <IssueMentionCard issueId={id} fallbackLabel={label} />;
  }
  if (type === "project") {
    return <ProjectMentionLink projectId={id} label={label} />;
  }
  return null;
}

function renderImage({ src, alt }: { src: string; alt: string }): React.ReactNode {
  return (
    <AttachmentRenderer
      attachment={{
        kind: "url",
        url: src,
        filename: alt,
        forceKind: "image",
      }}
    />
  );
}

function renderFileCard({
  href,
  filename,
}: {
  href: string;
  filename: string;
}): React.ReactNode {
  return (
    <AttachmentRenderer
      attachment={{ kind: "url", url: href, filename }}
    />
  );
}

// ---------------------------------------------------------------------------
// editor-parity mode: Mermaid / HTML block renderers
// ---------------------------------------------------------------------------

function renderMermaid({ chart }: { chart: string }): React.ReactNode {
  return <MermaidDiagram chart={chart} />;
}

function renderHtmlBlock({ html }: { html: string }): React.ReactNode {
  return <HtmlBlockPreview html={html} />;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

/**
 * App-level Markdown wrapper. Injects:
 *   - entity chips for issue/project mentions
 *   - cdnDomain from the config store (drives fileCard preprocessing)
 *   - unified <Attachment> as the image / file-card renderer
 *   - AttachmentDownloadProvider so url → record resolution works inside
 *     the injected <Attachment> components
 *   - editor-parity mode: MermaidDiagram, HtmlBlockPreview, LinkHoverCard,
 *     and .rich-text-editor.readonly CSS scope
 */
export function Markdown(props: MarkdownProps): React.JSX.Element {
  const cdnDomain = useConfigStore((s) => s.cdnDomain);
  const slug = useWorkspaceSlug();
  const { attachments, ...rest } = props;
  const isEditorParity = props.mode === "editor-parity";

  // Link hover card — only mounted in editor-parity mode
  const wrapperRef = React.useRef<HTMLDivElement | null>(null);
  const hover = useLinkHover(wrapperRef, !isEditorParity);

  // Callback to receive the wrapper ref from MarkdownBase for link-hover
  const handleLinkHover = React.useCallback(
    (ref: React.RefObject<HTMLDivElement | null>) => {
      // Copy the ref so useLinkHover can attach listeners
      (wrapperRef as React.MutableRefObject<HTMLDivElement | null>).current = ref.current;
    },
    [],
  );

  // onUrlClick — uses openLink for client-side navigation of internal links
  // (matching original ReadonlyContent's ReadonlyLink behavior)
  const handleUrlClick = React.useCallback(
    (href: string) => openLink(href, slug),
    [slug],
  );

  const editorParityProps = isEditorParity
    ? {
        renderMermaid,
        renderHtmlBlock,
        onLinkHover: handleLinkHover,
        onUrlClick: handleUrlClick,
        postprocess: (c: string) => highlightToHtml(preprocessJsonLiterals(c)),
      }
    : {};

  return (
    <AttachmentDownloadProvider attachments={attachments}>
      <MarkdownBase
        renderMention={defaultRenderMention}
        renderImage={renderImage}
        renderFileCard={renderFileCard}
        cdnDomain={cdnDomain}
        {...editorParityProps}
        {...rest}
      />
      {isEditorParity && <LinkHoverCard {...hover} />}
    </AttachmentDownloadProvider>
  );
}

export const MemoizedMarkdown = React.memo(Markdown);
MemoizedMarkdown.displayName = "MemoizedMarkdown";
