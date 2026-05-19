"use client";

/**
 * AttachmentBlock — thin dispatcher choosing the renderer for an attachment.
 *
 * Centralizes the kind-aware routing that previously lived inside
 * AttachmentCard (and earlier still, was duplicated across three call sites).
 * Three entry points render attachments and all must agree on which kinds get
 * which visual treatment, otherwise feature work has to be repeated three
 * times — see MUL-2330, where the inline HTML preview was missed on the
 * standalone path.
 *
 * Current routing:
 *   - kind === "html" + attachmentId + !uploading → HtmlAttachmentPreview
 *     (independent iframe + hover toolbar, no file-card chrome)
 *   - everything else → AttachmentCard (icon + filename + Eye/Download row)
 *
 * Props match AttachmentCardProps exactly so callers stay as-is — only the
 * import changes.
 */

import { getPreviewKind } from "./utils/preview";
import { AttachmentCard, type AttachmentCardProps } from "./attachment-card";
import { HtmlAttachmentPreview } from "./html-attachment-preview";

export type AttachmentBlockProps = AttachmentCardProps;

export function AttachmentBlock(props: AttachmentBlockProps) {
  const { filename, contentType = "", attachmentId, uploading } = props;
  const kind = filename ? getPreviewKind(contentType, filename) : null;
  if (kind === "html" && attachmentId && !uploading) {
    return (
      <HtmlAttachmentPreview
        attachmentId={attachmentId}
        filename={filename}
        onPreview={props.onPreview}
        onDownload={props.onDownload}
      />
    );
  }
  return <AttachmentCard {...props} />;
}
