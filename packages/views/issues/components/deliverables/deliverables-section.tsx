"use client";

import { IssueSidebarSectionHeader } from "../issue-sidebar-section";

import { useState } from "react";
import { LayoutGrid } from "@multica/ui/icons";
import type { DeliverableFile } from "@multica/core/attachments/deliverables";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { useT } from "../../../i18n";
import { formatBytes } from "../../../common/format-bytes";
import { fileIcon } from "../../../editor/utils/file-icon";
import { getPreviewKind } from "../../../editor/utils/preview";
import { DeliverableThumbnail } from "./deliverable-thumbnail";
import { useOpenAttachment } from "./use-open-attachment";
import { VersionBadge } from "./version-badge";

// The sidebar is a summary, not the list: the newest few of each, and the
// overview for the rest.
const RECENT_IMAGES = 3;
const RECENT_FILES = 4;

/**
 * "Deliverables" in the issue sidebar (MUL-7649): the files this issue's
 * comments delivered, as a whole — not grouped by run; each comment still
 * shows its own files. Pull requests keep their own section above: linking
 * one and PR auto-complete are issue workflow, not output. Renders nothing
 * until a comment delivers a file.
 */
export function DeliverablesSection({
  files,
  onOpenOverview,
}: {
  files: ReadonlyArray<DeliverableFile>;
  onOpenOverview: () => void;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const { open: openAttachment, modal } = useOpenAttachment();

  if (files.length === 0) return null;

  const images: DeliverableFile[] = [];
  const others: DeliverableFile[] = [];
  for (const file of files) {
    const isImage =
      getPreviewKind(file.latest.content_type, file.latest.filename) === "image";
    if (isImage && images.length < RECENT_IMAGES) images.push(file);
    else if (!isImage && others.length < RECENT_FILES) others.push(file);
  }

  return (
    <div>
      <IssueSidebarSectionHeader title={t(($) => $.deliverables.section_title)} open={open} onOpenChange={setOpen} count={files.length} actions={
        <IconButton size="icon-xs" label={t(($) => $.deliverables.view_all, { count: files.length })} onClick={onOpenOverview}><LayoutGrid /></IconButton>
      } />
      {open && (
        <div className="px-2">
          {images.length > 0 && (
            <div className="mx-2 mb-1.5 grid grid-cols-3 gap-1.5">
              {images.map((file) => (
                <TooltipHint key={file.key} label={file.latest.filename}>
                  <button
                    type="button"
                    className="relative aspect-[4/3] overflow-hidden rounded-md ring-1 ring-border transition-shadow hover:ring-foreground/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    aria-label={versionedName(file, t)}
                    onClick={() => openAttachment(file.latest)}
                  >
                    <DeliverableThumbnail
                      attachment={file.latest}
                      showTypeLabel={false}
                      className="size-full"
                    />
                  </button>
                </TooltipHint>
              ))}
            </div>
          )}
          {others.map((file) => (
            <FileRow
              key={file.key}
              file={file}
              onOpen={() => openAttachment(file.latest)}
            />
          ))}

        </div>
      )}
      {modal}
    </div>
  );
}

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

function versionedName(file: DeliverableFile, t: IssuesT): string {
  return file.versions.length > 1
    ? t(($) => $.deliverables.name_with_version, {
        name: file.latest.filename,
        version: file.versions.length,
      })
    : file.latest.filename;
}

function FileRow({ file, onOpen }: { file: DeliverableFile; onOpen: () => void }) {
  const { t } = useT("issues");
  const { latest } = file;
  const Icon = fileIcon(latest.content_type, latest.filename);
  return (
    <button
      type="button"
      className="group flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-caption transition-colors hover:bg-accent/50"
      title={latest.filename}
      aria-label={versionedName(file, t)}
      onClick={onOpen}
    >
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate">{latest.filename}</span>
      {file.versions.length > 1 && <VersionBadge version={file.versions.length} />}
      <span className="ml-auto shrink-0 pl-2 text-micro tabular-nums text-muted-foreground">
        {latest.size_bytes > 0 ? formatBytes(latest.size_bytes) : null}
      </span>
    </button>
  );
}
