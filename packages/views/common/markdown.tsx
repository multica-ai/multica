"use client";

import * as React from "react";
import {
  Markdown as MarkdownBase,
  type MarkdownProps as MarkdownBaseProps,
  type RenderMode,
} from "@multica/ui/markdown";
import { useConfigStore } from "@multica/core/config";
import type { Attachment as AttachmentRecord } from "@multica/core/types";
import { IssueMentionCard } from "../issues/components/issue-mention-card";
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
 * Default renderMention that delegates to IssueMentionCard for issue mentions
 * and renders a styled span for other mention types.
 */
function defaultRenderMention({
  type,
  id,
}: {
  type: string;
  id: string;
}): React.ReactNode {
  if (type === "issue") {
    return <IssueMentionCard issueId={id} />;
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
 *   - IssueMentionCard for issue mentions
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
        {...rest}
      />
    </AttachmentDownloadProvider>
  );
}

export const MemoizedMarkdown = React.memo(Markdown);
MemoizedMarkdown.displayName = "MemoizedMarkdown";
