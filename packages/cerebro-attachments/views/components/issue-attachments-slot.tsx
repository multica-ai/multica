"use client";

// FIR-2034 — the issue-level attachment slot at the top of an issue body.
//
// When `cerebro_attachments_tab` is ON, the standalone attachments move into
// the dedicated Attachments tab and the top of the page shows only a compact
// one-line hint (so the top stays clean but the files are still discoverable).
// When OFF, this renders the existing AttachmentList exactly as before — the
// chip grid (or legacy rows), preserving current behavior as the kill-switch.
//
// This slot only governs the ISSUE body surface. Comment- and chat-level
// attachment lists keep using AttachmentList directly and are unaffected.

import { Paperclip } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { Attachment } from "@multica/core/types";
import { useFlagValue } from "@multica/cerebro-feature-flags";
import { standaloneAttachments } from "@multica/cerebro-attachments/core/standalone";
import { AttachmentList } from "./attachment-list";

export function IssueAttachmentsSlot({
  attachments,
  content,
  className,
}: {
  attachments?: Attachment[];
  content?: string;
  className?: string;
}) {
  const tabEnabled = useFlagValue("cerebro_attachments_tab");

  if (!tabEnabled) {
    return <AttachmentList attachments={attachments} content={content} className={className} />;
  }

  const count = standaloneAttachments(attachments, content).length;
  if (!count) return null;

  return (
    <div
      className={cn(
        "flex items-center gap-2 rounded-md border border-dashed border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground",
        className,
      )}
    >
      <Paperclip className="size-3.5 shrink-0" />
      <span>
        {count} {count === 1 ? "attachment" : "attachments"} — see the{" "}
        <span className="font-medium text-foreground">Attachments</span> tab
      </span>
    </div>
  );
}
