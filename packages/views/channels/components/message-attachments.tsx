"use client";

import { Paperclip, FileText, Download } from "lucide-react";
import type { ChannelMessageAttachment } from "@multica/core/types";

interface MessageAttachmentsProps {
  attachments: ChannelMessageAttachment[];
}

function isImage(att: ChannelMessageAttachment): boolean {
  return att.content_type.startsWith("image/");
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

/**
 * MessageAttachments renders the attachment row under a channel message.
 * Image attachments inline as a thumbnail (clicking opens the full
 * resolution in a new tab); other types render as a download card with
 * filename + size + a download button.
 *
 * Layout intentionally unbounded — long filenames truncate with ellipsis,
 * many attachments wrap. Phase 5 ships the simplest readable rendering;
 * a Phase 6 polish could collapse-after-3 with "show more" or a lightbox
 * gallery.
 */
export function MessageAttachments({ attachments }: MessageAttachmentsProps) {
  if (attachments.length === 0) return null;
  return (
    <div className="mt-2 flex flex-wrap gap-2">
      {attachments.map((att) =>
        isImage(att) ? (
          <ImageAttachment key={att.id} att={att} />
        ) : (
          <FileAttachment key={att.id} att={att} />
        ),
      )}
    </div>
  );
}

function ImageAttachment({ att }: { att: ChannelMessageAttachment }) {
  return (
    <a
      href={att.download_url}
      target="_blank"
      rel="noopener noreferrer"
      className="block max-w-xs overflow-hidden rounded-md border border-border"
    >
      <img
        src={att.download_url}
        alt={att.filename}
        loading="lazy"
        className="block max-h-72 w-auto object-contain"
      />
    </a>
  );
}

function FileAttachment({ att }: { att: ChannelMessageAttachment }) {
  return (
    <a
      href={att.download_url}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex max-w-xs items-center gap-2 rounded-md border border-border bg-muted/40 px-3 py-2 text-sm text-foreground hover:bg-muted/60"
      aria-label={`Download ${att.filename}`}
    >
      <FileText className="h-4 w-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1 truncate" title={att.filename}>
        {att.filename}
      </span>
      <span className="shrink-0 text-xs text-muted-foreground">
        {formatBytes(att.size_bytes)}
      </span>
      <Download className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
    </a>
  );
}

/**
 * PendingAttachmentsRow is the in-composer chip row for files the user
 * has selected but not yet sent. Shows filename + spinner while the
 * upload is in flight, an X to remove a queued file before send, and
 * an error indicator if the upload failed.
 */
export interface PendingAttachment {
  /** Stable client-side id so React keys are happy across renders. */
  key: string;
  filename: string;
  contentType: string;
  /** Server attachment id once the upload completes. Null while pending. */
  serverID: string | null;
  status: "uploading" | "ready" | "error";
  error?: string;
}

interface PendingAttachmentsRowProps {
  pending: PendingAttachment[];
  onRemove: (key: string) => void;
}

export function PendingAttachmentsRow({ pending, onRemove }: PendingAttachmentsRowProps) {
  if (pending.length === 0) return null;
  return (
    <div className="mb-2 flex flex-wrap gap-2">
      {pending.map((p) => (
        <div
          key={p.key}
          className={[
            "inline-flex max-w-xs items-center gap-2 rounded-md border px-2 py-1 text-xs",
            p.status === "error"
              ? "border-destructive/50 bg-destructive/10 text-destructive"
              : "border-border bg-muted/40 text-foreground",
          ].join(" ")}
        >
          <Paperclip className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate" title={p.filename}>
            {p.filename}
          </span>
          {p.status === "uploading" ? (
            <span className="shrink-0 text-muted-foreground">…</span>
          ) : null}
          {p.status === "error" ? (
            <span className="shrink-0" title={p.error ?? "upload failed"}>
              ⚠️
            </span>
          ) : null}
          <button
            type="button"
            onClick={() => onRemove(p.key)}
            aria-label={`Remove ${p.filename}`}
            className="ml-1 shrink-0 rounded hover:bg-muted"
          >
            ×
          </button>
        </div>
      ))}
    </div>
  );
}
