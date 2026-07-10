import type { Attachment } from "@multica/core/types";

/**
 * Every attachment belonging to an issue — the issue body's own uploads plus
 * every comment's — deduped by id.
 *
 * Used by the FIR-2034 Attachments tab (FIR-2710 point 3). Unlike
 * {@link standaloneAttachments} this deliberately does NOT drop attachments
 * referenced inline in the description/comment markdown: the tab is the issue's
 * complete file index, so an image pasted straight into the description or a
 * comment (an "inline tray" image) still shows up here instead of being hidden.
 */
export function allIssueAttachments(
  issueAttachments: Attachment[] | undefined,
  commentAttachments: Attachment[] | undefined,
): Attachment[] {
  const byId = new Map<string, Attachment>();
  for (const a of issueAttachments ?? []) byId.set(a.id, a);
  for (const a of commentAttachments ?? []) byId.set(a.id, a);
  return [...byId.values()];
}

/**
 * The attachments that should render as standalone cards/rows — i.e. those
 * NOT already referenced inline in the given markdown `content`.
 *
 * Extracted from AttachmentList so the issue-level surfaces (the top list and
 * the FIR-2034 Attachments tab) compute an identical set, and the tab's count
 * badge never drifts from what the tab actually shows.
 *
 * Rules:
 * - Skip an attachment whose URL appears in `content`.
 * - Skip a duplicate upload (same name/type/size) when a sibling with the
 *   same identity is already inline.
 * When `content` is undefined/empty, every attachment is standalone.
 */
export function standaloneAttachments(
  attachments: Attachment[] | undefined,
  content?: string,
): Attachment[] {
  if (!attachments?.length) return [];
  if (!content) return attachments;
  return attachments.filter((a) => {
    if (content.includes(a.url)) return false;
    const hasSiblingInContent = attachments.some(
      (other) =>
        other.id !== a.id &&
        other.filename === a.filename &&
        other.content_type === a.content_type &&
        other.size_bytes === a.size_bytes &&
        content.includes(other.url),
    );
    if (hasSiblingInContent) return false;
    return true;
  });
}
