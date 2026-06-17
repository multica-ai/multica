// CEREBRO-PATCH(chat-upload-attachment-ids): TECH-3657 — the chat composer
// uploads a file (which creates an `attachment` row) and embeds it in the
// message body as a `!file[name](/uploads/.../<attachmentId>.<ext>)` token,
// but never passed the attachment id to the send call. So the row stayed
// orphaned (chat_message_id = null) and the Firtal Gateway runtime's
// `loadChatAttachments` (which looks up attachments by chat_message_id) found
// nothing — the model only received the unreadable `/uploads/...` URL and
// fabricated or denied the file's contents. This helper recovers the
// attachment ids from the outgoing markdown so `sendChatMessage` can link
// them to the new message. Lives in the cerebro zone; the upstream send call
// is touched with a single CEREBRO-PATCH line.

// Attachment uploads live at `/uploads/workspaces/<workspaceId>/<attachmentId>.<ext>`.
// Both path segments are UUIDs, so the capture group is anchored to the
// `.<ext>` basename to pick the attachment id, not the workspace id. Matches
// both `!file[](url)` file cards and `![](url)` inline image embeds, since
// both create linkable attachment rows.
const UPLOAD_ATTACHMENT_ID_RE =
  /\/uploads\/[^\s)]*?([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.[a-z0-9]+/gi;

/**
 * Extract the attachment ids of any uploaded files referenced in a chat
 * message body, de-duplicated and lower-cased, in first-seen order. Returns
 * an empty array when the message references no uploads — callers can pass the
 * result straight to `sendChatMessage` (which omits `attachment_ids` when
 * empty).
 */
export function extractChatUploadAttachmentIds(content: string): string[] {
  const seen = new Set<string>();
  for (const match of content.matchAll(UPLOAD_ATTACHMENT_ID_RE)) {
    const id = match[1]?.toLowerCase();
    if (id) seen.add(id);
  }
  return [...seen];
}
