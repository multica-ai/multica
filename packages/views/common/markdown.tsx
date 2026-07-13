"use client";

import * as React from "react";
import {
  Markdown as MarkdownBase,
  type MarkdownProps as MarkdownBaseProps,
  type RenderMode,
  isIssueIdentifier,
} from "@multica/ui/markdown";
import { useConfigStore } from "@multica/core/config";
import type { Attachment as AttachmentRecord } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { IssueMentionCard } from "../issues/components/issue-mention-card";
import { useResolveIssueIdentifier } from "../issues/hooks";
import { ProjectChip } from "../projects/components/project-chip";
import { AppLink } from "../navigation";
import {
  Attachment as AttachmentRenderer,
  AttachmentDownloadProvider,
} from "../editor";

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

/**
 * Default renderMention that delegates to entity chips for issue/project mentions
 * and renders a styled span for other mention types.
 */
function ProjectMentionCard({ projectId }: { projectId: string }): React.ReactNode {
  const p = useWorkspacePaths();
  return (
    <AppLink href={p.projectDetail(projectId)} className="project-mention not-prose inline-flex">
      <ProjectChip
        projectId={projectId}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </AppLink>
  );
}

/**
 * Autolinked bare identifier (e.g. `MUL-123`) routed through
 * `mention://issue/<identifier>`. Resolves the identifier to a real issue in
 * the current workspace; renders a navigable chip on a hit, plain text on a
 * miss / while loading / cross-workspace.
 */
function AutolinkedIssueMention({ identifier }: { identifier: string }): React.ReactNode {
  const issue = useResolveIssueIdentifier(identifier);
  if (!issue) return identifier;
  return <IssueMentionCard issueId={issue.id} fallbackLabel={identifier} />;
}

function defaultRenderMention({
  type,
  id,
}: {
  type: string;
  id: string;
}): React.ReactNode {
  if (type === "issue") {
    // A bare identifier (from the autolink preprocessor) is carried as the id
    // segment; a real mention carries a UUID. Dispatch on the id shape.
    if (isIssueIdentifier(id)) {
      return <AutolinkedIssueMention identifier={id} />;
    }
    return <IssueMentionCard issueId={id} />;
  }
  if (type === "project") {
    return <ProjectMentionCard projectId={id} />;
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
        // chat / skill markdown `![]()` is structurally an image. Without
        // forceKind, empty/descriptive alt strings would route to the
        // file-card chrome via getPreviewKind autodetect.
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

/**
 * App-level Markdown wrapper. Injects:
 *   - entity chips for issue/project mentions
 *   - cdnDomain from the config store (drives fileCard preprocessing)
 *   - unified <Attachment> as the image / file-card renderer
 *   - AttachmentDownloadProvider so url → record resolution works inside
 *     the injected <Attachment> components
 */
export function Markdown(props: MarkdownProps): React.JSX.Element {
  const cdnDomain = useConfigStore((s) => s.cdnDomain);
  const { attachments, ...rest } = props;
  return (
    <AttachmentDownloadProvider attachments={attachments}>
      <MarkdownBase
        renderMention={defaultRenderMention}
        renderImage={renderImage}
        renderFileCard={renderFileCard}
        cdnDomain={cdnDomain}
        autolinkIssueIdentifiers
        {...rest}
      />
    </AttachmentDownloadProvider>
  );
}

export const MemoizedMarkdown = React.memo(Markdown);
MemoizedMarkdown.displayName = "MemoizedMarkdown";
