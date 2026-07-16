import type { Attachment } from "@multica/core/types";
import { contentReferencesAttachment } from "@multica/core/types";

/**
 * The attachments to render as standalone cards below a message / comment
 * body: the ones NOT already referenced inline in `content`, minus duplicate
 * uploads whose same-identity sibling is already inline.
 *
 * "Referenced inline" is decided by the core `contentReferencesAttachment`
 * helper, so it matches every real URL form the server emits — the stable
 * `/api/attachments/<id>/download` path, the raw storage `url`, the signed
 * `download_url`, and the durable `markdown_url` that agents actually paste
 * into a reply. Checking only raw `url` (the old behaviour) missed the
 * `markdown_url` case and rendered the same image twice.
 *
 * Pass `content === undefined` (not "") to render every attachment — used when
 * the body has no markdown that could reference them.
 *
 * Mirrors web's `AttachmentList` filter in
 * `packages/views/issues/components/comment-card.tsx`.
 */
export function standaloneAttachments(
  attachments: Attachment[] | undefined,
  content: string | undefined,
): Attachment[] {
  if (!attachments || attachments.length === 0) return [];
  if (!content) return attachments;
  return attachments.filter((a) => {
    if (contentReferencesAttachment(content, a)) return false;
    // Dedup: if another attachment with the same file identity (name, type,
    // size) is already inline, this one is a duplicate upload — skip it.
    const hasSiblingInContent = attachments.some(
      (other) =>
        other.id !== a.id &&
        other.filename === a.filename &&
        other.content_type === a.content_type &&
        other.size_bytes === a.size_bytes &&
        contentReferencesAttachment(content, other),
    );
    return !hasSiblingInContent;
  });
}
