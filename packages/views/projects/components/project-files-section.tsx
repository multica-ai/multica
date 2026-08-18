"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  Download,
  Eye,
  EyeOff,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { fileIconFor, formatBytes } from "../../common/attachment-file";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { isPreviewable, useAttachmentPreview } from "../../editor";
import {
  projectFilesOptions,
  useHideProjectFile,
  useUnhideProjectFile,
} from "@multica/core/projects";
import type { ProjectFile } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { useFormatRelativeDate } from "./labels";



/**
 * Project "files" sidebar section — every artifact a member or agent produced
 * or uploaded across the project's issues, comments, and chat sessions. Files
 * can be viewed (preview route), downloaded, and individually hidden. Hiding is
 * personal: it only changes THIS member's list, and hidden files stay reachable
 * through the "show hidden" toggle.
 */
export function ProjectFilesSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const formatRelativeDate = useFormatRelativeDate();
  const { getActorName } = useActorName();
  const preview = useAttachmentPreview();
  const [open, setOpen] = useState(true);
  const [showHidden, setShowHidden] = useState(false);

  const { data: files = [] } = useQuery(projectFilesOptions(wsId, projectId));
  const hideFile = useHideProjectFile(wsId, projectId);
  const unhideFile = useUnhideProjectFile(wsId, projectId);

  const visible = useMemo(() => files.filter((f) => !f.hidden), [files]);
  const hidden = useMemo(() => files.filter((f) => f.hidden), [files]);

  const openPreview = (file: ProjectFile) => {
    // Inline modal preview (same as conversation surfaces), not a URL jump.
    // Falls back to download when the type is not previewable.
    if (!preview.tryOpen({ kind: "full", attachment: file })) {
      openDownload(file);
    }
  };

  const openDownload = (file: ProjectFile) => {
    if (!file.download_url) return;
    window.open(file.download_url, "_blank", "noopener,noreferrer");
  };

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.files.section_header)}
        <span className="text-micro text-muted-foreground tabular-nums">
          {visible.length}
        </span>
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ml-auto ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div className="pl-2 space-y-1.5">
          {files.length === 0 && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.files.empty)}
            </p>
          )}
          {files.length > 0 && visible.length === 0 && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.files.all_hidden)}
            </p>
          )}
          <div className="max-h-64 space-y-0.5 overflow-y-auto pr-1">
            {visible.map((file) => (
              <FileRow
                key={file.id}
                file={file}
                actorName={getActorName(file.uploader_type, file.uploader_id)}
                relativeDate={formatRelativeDate(file.created_at)}
                onView={() => openPreview(file)}
                onDownload={() => openDownload(file)}
                onHide={() => hideFile.mutate(file.id)}
                hidden={false}
              />
            ))}
            {hidden.length > 0 && (
              <>
                <button
                  type="button"
                  onClick={() => setShowHidden((v) => !v)}
                  className="flex w-full items-center gap-1 px-2 py-0.5 text-micro text-muted-foreground hover:text-foreground transition-colors"
                >
                  <EyeOff className="size-3" />
                  {showHidden
                    ? t(($) => $.files.hide_hidden)
                    : t(($) => $.files.show_hidden_count, { count: hidden.length })}
                </button>
                {showHidden &&
                  hidden.map((file) => (
                    <FileRow
                      key={file.id}
                      file={file}
                      actorName={getActorName(file.uploader_type, file.uploader_id)}
                      relativeDate={formatRelativeDate(file.created_at)}
                      onView={() => openPreview(file)}
                      onDownload={() => openDownload(file)}
                      onHide={() => unhideFile.mutate(file.id)}
                      hidden
                    />
                  ))}
              </>
            )}
          </div>
        </div>
      )}
      {preview.modal}
    </div>
  );
}

interface FileRowProps {
  file: ProjectFile;
  actorName: string;
  relativeDate: string;
  onView: () => void;
  onDownload: () => void;
  onHide: () => void;
  hidden: boolean;
}

function FileRow({
  file,
  actorName,
  relativeDate,
  onView,
  onDownload,
  onHide,
  hidden,
}: FileRowProps) {
  const { t } = useT("projects");
  const Icon = fileIconFor(file.content_type, file.filename);
  return (
    <div
      className={cn(
        "group flex items-center gap-2 text-caption",
        hidden && "opacity-50",
      )}
    >
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      <ActorAvatar actorType={file.uploader_type} actorId={file.uploader_id} size="sm" />
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
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              className="size-6 shrink-0 text-muted-foreground"
              aria-label={
                hidden
                  ? t(($) => $.files.unhide_tooltip)
                  : t(($) => $.files.hide_tooltip)
              }
              onClick={onHide}
            >
              {hidden ? <Eye className="size-3.5" /> : <EyeOff className="size-3.5" />}
            </Button>
          }
        />
        <TooltipContent side="top">
          {hidden
            ? t(($) => $.files.unhide_tooltip)
            : t(($) => $.files.hide_tooltip)}
        </TooltipContent>
      </Tooltip>
    </div>
  );
}
