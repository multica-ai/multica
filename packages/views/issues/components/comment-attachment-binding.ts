// CEREBRO-PATCH(comment-edit-attachment-binding): FIR-2394 — pure helpers that
// decide which attachments stay bound to a comment when it is edited.
//
// On save the client sends the surviving attachment IDs and the server replaces
// the comment's binding set with exactly that list (UpdateComment →
// ReplaceCommentAttachments). So a still-embedded image dropped from this set is
// silently unbound and orphaned — data loss.
//
// The exact-URL substring match alone is fragile: if the markdown URL and
// `attachment.url` ever diverge in form (CDN rewrite, percent-encoding, a
// signed query string, or a markdown-serializer change) a still-referenced
// image would be dropped. The upload storage key — the last path segment, e.g.
// `<uuidv7>.png` — is globally unique and invariant across those rewrites, so
// we fall back to matching on it. This only adds recall (never a false drop)
// and leaves the common exact-match path unchanged.
import type { Attachment } from "@multica/core/types";

/** Last path segment of a URL, without query/hash — the invariant storage key. */
export function attachmentStorageKey(url: string): string {
  const path = url.split(/[?#]/, 1)[0] ?? url;
  const segment = path.split("/").pop() ?? "";
  return segment.trim();
}

/** True when the attachment is still referenced by the (edited) markdown. */
export function attachmentReferencedInContent(content: string, attachment: Attachment): boolean {
  if (attachment.url && content.includes(attachment.url)) return true;
  const key = attachmentStorageKey(attachment.url);
  return key.length > 0 && content.includes(key);
}

/**
 * The attachment IDs that must stay bound after an edit: every attachment still
 * embedded in `content`, plus the standalone (non-embedded) attachments the
 * edit UI chose to retain.
 */
export function collectActiveAttachmentIds(
  content: string,
  attachments: Attachment[],
  retainedStandaloneIds?: Set<string> | null,
): string[] {
  const ids = new Set<string>();
  for (const attachment of attachments) {
    if (attachmentReferencedInContent(content, attachment)) ids.add(attachment.id);
  }
  for (const id of retainedStandaloneIds ?? []) ids.add(id);
  return [...ids];
}
