"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  Download,
  File,
  FileText,
  Video,
} from "lucide-react";
import type { Attachment } from "@multica/core/types";
import { issueAttachmentsOptions } from "@multica/core/issues/queries";
import { timeAgo } from "@multica/core/utils";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ActorAvatar } from "../../common/actor-avatar";
import { useActorName } from "@multica/core/workspace/hooks";

interface IssueFilesSectionProps {
  issueId: string;
}

export function IssueFilesSection({ issueId }: IssueFilesSectionProps) {
  const [open, setOpen] = useState(true);
  const { data: attachments = [], isLoading } = useQuery(
    issueAttachmentsOptions(issueId),
  );

  if (!isLoading && attachments.length === 0) return null;

  return (
    <div>
      <button
        className={cn(
          "mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors hover:bg-accent/70",
          !open && "text-muted-foreground hover:text-foreground",
        )}
        onClick={() => setOpen(!open)}
      >
        Files
        <ChevronRight
          className={cn(
            "!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
        {attachments.length > 0 && (
          <span className="ml-auto font-mono text-[11px] tabular-nums text-muted-foreground">
            {attachments.length}
          </span>
        )}
      </button>
      {open && (
        <div className="space-y-1 pl-2">
          {isLoading ? (
            <>
              <Skeleton className="h-10 w-full rounded-md" />
              <Skeleton className="h-10 w-5/6 rounded-md" />
            </>
          ) : (
            attachments.map((attachment) => (
              <AttachmentRow key={attachment.id} attachment={attachment} />
            ))
          )}
        </div>
      )}
    </div>
  );
}

function AttachmentRow({ attachment }: { attachment: Attachment }) {
  const { getActorName } = useActorName();
  const actorName = getActorName(attachment.uploader_type, attachment.uploader_id);
  const href = attachment.download_url || attachment.url;

  return (
    <div className="group flex items-center gap-2 rounded-md px-1 py-1.5 transition-colors hover:bg-accent/40">
      <FilePreview attachment={attachment} />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-xs font-medium">{attachment.filename}</span>
        </div>
        <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
          <ActorAvatar
            actorType={attachment.uploader_type}
            actorId={attachment.uploader_id}
            size={14}
            enableHoverCard
          />
          <span className="truncate">{actorName}</span>
          <span className="shrink-0">·</span>
          <span className="shrink-0">{formatFileSize(attachment.size_bytes)}</span>
          <span className="shrink-0">·</span>
          <span className="shrink-0">{timeAgo(attachment.created_at)}</span>
        </div>
      </div>
      {href && (
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-all hover:bg-secondary hover:text-foreground group-hover:opacity-100 focus:opacity-100"
                onClick={() => window.open(href, "_blank", "noopener,noreferrer")}
                aria-label={`Open ${attachment.filename}`}
              >
                <Download className="size-3.5" />
              </button>
            }
          />
          <TooltipContent>Open file</TooltipContent>
        </Tooltip>
      )}
    </div>
  );
}

function FilePreview({ attachment }: { attachment: Attachment }) {
  const kind = getAttachmentKind(attachment);

  if (kind === "image") {
    return (
      <div className="flex size-8 shrink-0 items-center justify-center overflow-hidden rounded-md border bg-muted">
        <img
          src={attachment.download_url || attachment.url}
          alt=""
          className="h-full w-full object-cover"
          loading="lazy"
        />
      </div>
    );
  }

  const Icon = kind === "video" ? Video : kind === "pdf" ? FileText : File;
  return (
    <div className="flex size-8 shrink-0 items-center justify-center rounded-md border bg-muted text-muted-foreground">
      <Icon className="size-4" />
    </div>
  );
}

function getAttachmentKind(attachment: Attachment): "image" | "video" | "pdf" | "file" {
  if (attachment.content_type.startsWith("image/")) return "image";
  if (attachment.content_type.startsWith("video/")) return "video";
  if (attachment.content_type === "application/pdf") return "pdf";
  return "file";
}

function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const precision = value >= 10 || unit === 0 ? 0 : 1;
  return `${value.toFixed(precision)} ${units[unit]}`;
}
