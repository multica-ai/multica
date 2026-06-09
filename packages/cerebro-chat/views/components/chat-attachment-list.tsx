"use client";

import type { Attachment } from "@multica/core/types";
import { AttachmentList } from "@multica/cerebro-attachments/views";

// TECH-3183 — render the files linked to a chat message (attachment.chat_message_id)
// on the message bubble. Reuses the issue/comment AttachmentList so chat matches the
// rest of the app: an "open in new tab" viewer button for viewable types
// (pdf/md/text/html/json) plus a download button on every file.
//
// Unlike issue/comment bodies, we deliberately do NOT pass `content` here: the
// agent's reply markdown may embed an expiring signed URL for the same file, and
// the content-filter would then hide the stable attachment card. Chat always shows
// the card so the durable download stays reachable. Read-only — chat has no
// edit-time attachment removal.
export function ChatAttachmentList({
  attachments,
  className,
}: {
  attachments?: Attachment[];
  className?: string;
}) {
  if (!attachments?.length) return null;
  return <AttachmentList attachments={attachments} className={className} />;
}
