"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Download, Eye } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useActorName } from "@multica/core/workspace/hooks";
import { issueFilesOptions } from "@multica/core/issues/queries";
import { isPreviewable, useAttachmentPreview } from "../../editor";
import type { Attachment } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { fileIconFor, formatBytes } from "../../common/attachment-file";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { useTimeAgo } from "../../i18n/use-time-ago";

/**
 * Issue "files" sidebar section — every artifact produced for THIS task: the
 * issue's own attachments plus attachments on its comments, aggregated by
 * GET /api/issues/{id}/files. Read-only surface: view (preview route) and
 * download. Hiding lives at the project level, not here.
 */
export function IssueFilesSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const preview = useAttachmentPreview();
  const [open, setOpen] = useState(true);

  const { data: files = [] } = useQuery(issueFilesOptions(issueId));

  const openPreview = (file: Attachment) => {
    // Inline modal preview (same as conversation surfaces), not a URL jump.
    // Falls back to download when the type is not previewable.
    if (!preview.tryOpen({ kind: "full", attachment: file })) {
      openDownload(file);
    }
  };

  const openDownload = (file: Attachment) => {
    if (!file.download_url) return;
    window.open(file.download_url, "_blank", "noopener,noreferrer");
  };

  return (
    <div>
      <button
        type="button"
        className={cn(
          "flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70",
          open ? "" : "text-muted-foreground hover:text-foreground",
        )}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.files.section_header)}
        {files.length > 0 && (
          <span className="text-micro text-muted-foreground tabular-nums">
            {files.length}
          </span>
        )}
        <ChevronRight
          className={cn(
            "!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ml-auto",
            open ? "rotate-90" : "",
          )}
        />
      </button>
      {open && (
        <div className="pl-2 space-y-0.5">
          {files.length === 0 && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.files.empty)}
            </p>
          )}
          <div className="max-h-64 space-y-0.5 overflow-y-auto pr-1">
            {files.map((file) => (
              <FileRow
                key={file.id}
                file={file}
                actorName={getActorName(file.uploader_type, file.uploader_id)}
                relativeDate={timeAgo(file.created_at)}
                onView={() => openPreview(file)}
                onDownload={() => openDownload(file)}
              />
            ))}
          </div>
        </div>
      )}
      {preview.modal}
    </div>
  );
}

function FileRow({
  file,
  actorName,
  relativeDate,
  onView,
  onDownload,
}: {
  file: Attachment;
  actorName: string;
  relativeDate: string;
  onView: () => void;
  onDownload: () => void;
}) {
  const { t } = useT("issues");
  const Icon = fileIconFor(file.content_type, file.filename);
  return (
    <div className="group flex items-center gap-2 text-caption">
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      <ActorAvatar
        actorType={file.uploader_type}
        actorId={file.uploader_id}
        size="sm"
      />
      <div className="min-w-0 flex-1">
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={onView}
                className="block w-full truncate text-left hover:underline"
              >
                {file.filename}
              </button>
            }
          />
          <TooltipContent side="top">{file.filename}</TooltipContent>
        </Tooltip>
        <p className="truncate text-micro text-muted-foreground">
          {actorName} · {formatBytes(file.size_bytes)} · {relativeDate}
        </p>
      </div>
      {isPreviewable(file.content_type, file.filename) && (
        <Button
          variant="ghost"
          size="icon-sm"
          className="size-6 shrink-0 text-muted-foreground"
          title={t(($) => $.files.view_tooltip)}
          aria-label={t(($) => $.files.view_tooltip)}
          onClick={onView}
        >
          <Eye className="size-3.5" />
        </Button>
      )}
      <Button
        variant="ghost"
        size="icon-sm"
        className="size-6 shrink-0 text-muted-foreground"
        title={t(($) => $.files.download_tooltip)}
        aria-label={t(($) => $.files.download_tooltip)}
        onClick={onDownload}
      >
        <Download className="size-3.5" />
      </Button>
    </div>
  );
}
