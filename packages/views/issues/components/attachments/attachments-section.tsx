"use client";

import { useState } from "react";
import { ChevronRight, LayoutGrid } from "lucide-react";
import type { IssueAttachmentFile } from "@multica/core/attachments/issue-attachments";
import { useT } from "../../../i18n";
import { formatBytes } from "../../../common/format-bytes";
import { fileIcon } from "../../../editor/utils/file-icon";
import { getPreviewKind } from "../../../editor/utils/preview";
import { DeliverableThumbnail } from "../deliverables/deliverable-thumbnail";
import { useOpenAttachment } from "../deliverables/use-open-attachment";
import { VersionBadge } from "../deliverables/version-badge";

const RECENT_IMAGES = 3;
const RECENT_FILES = 4;

/**
 * "Attachments" in the issue sidebar: member-uploaded files from the
 * description and from comments. Hidden until there is at least one.
 */
export function AttachmentsSection({
  files,
  onOpenOverview,
}: {
  files: ReadonlyArray<IssueAttachmentFile>;
  onOpenOverview: () => void;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const { open: openAttachment, modal } = useOpenAttachment();

  if (files.length === 0) return null;

  const images: IssueAttachmentFile[] = [];
  const others: IssueAttachmentFile[] = [];
  for (const file of files) {
    const isImage =
      getPreviewKind(file.latest.content_type, file.latest.filename) === "image";
    if (isImage && images.length < RECENT_IMAGES) images.push(file);
    else if (!isImage && others.length < RECENT_FILES) others.push(file);
  }

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
        aria-expanded={open}
      >
        {t(($) => $.attachments.section_title)}{" "}
        <span className="rounded-xs bg-muted px-1 text-micro font-medium tabular-nums text-muted-foreground">
          {files.length}
        </span>
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2">
          {images.length > 0 && (
            <div className="mb-1.5 grid grid-cols-3 gap-1.5">
              {images.map((file) => (
                <button
                  key={file.key}
                  type="button"
                  className="relative aspect-[4/3] overflow-hidden rounded-md ring-1 ring-border transition-shadow hover:ring-foreground/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  title={file.latest.filename}
                  aria-label={versionedName(file, t)}
                  onClick={() => openAttachment(file.latest)}
                >
                  <DeliverableThumbnail
                    attachment={file.latest}
                    showTypeLabel={false}
                    className="size-full"
                  />
                </button>
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
          <button
            type="button"
            className="mt-0.5 flex w-[calc(100%+1rem)] -mx-2 items-center gap-2 rounded-md px-2 py-1 text-left text-caption text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground"
            onClick={onOpenOverview}
          >
            <LayoutGrid className="size-3.5 shrink-0" />
            {t(($) => $.attachments.view_all, { count: files.length })}
          </button>
        </div>
      )}
      {modal}
    </div>
  );
}

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

function versionedName(file: IssueAttachmentFile, t: IssuesT): string {
  return file.versions.length > 1
    ? t(($) => $.attachments.name_with_version, {
        name: file.latest.filename,
        version: file.versions.length,
      })
    : file.latest.filename;
}

function FileRow({ file, onOpen }: { file: IssueAttachmentFile; onOpen: () => void }) {
  const { t } = useT("issues");
  const { latest } = file;
  const Icon = fileIcon(latest.content_type, latest.filename);
  return (
    <button
      type="button"
      className="group flex w-[calc(100%+1rem)] -mx-2 items-center gap-2 rounded-md px-2 py-1 text-left text-caption transition-colors hover:bg-accent/50"
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
