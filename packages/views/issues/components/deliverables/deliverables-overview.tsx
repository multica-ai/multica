"use client";

import { useEffect, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { LayoutGrid, MessageSquareText, X } from "lucide-react";
import type { DeliverableFile } from "@multica/core/attachments/deliverables";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { useLocale, useT, useTimeAgo } from "../../../i18n";
import { ActorAvatar } from "../../../common/actor-avatar";
import { formatBytes } from "../../../common/format-bytes";
import { SegmentedToggle } from "../../../common/segmented-toggle";
import { usePreviewSequence } from "../../../editor";
import { fileTypeLabel } from "../../../editor/utils/preview";
import { useImmersiveMode } from "../../../platform";
import type { DeliverableOrigin } from "./deliverable-details";
import {
  DELIVERABLE_CATEGORIES,
  deliverableCategory,
  type DeliverableCategory,
} from "./deliverable-kind";
import { DeliverableThumbnail } from "./deliverable-thumbnail";
import { VersionBadge } from "./version-badge";
import { useOpenAttachment } from "./use-open-attachment";

type Filter = "all" | DeliverableCategory;

// Same desktop window-chrome contract as the attachment viewer: the overlay
// covers the title bar's drag region, so it is `no-drag` itself, its header
// drags the window, and the header's controls opt back out.
const NO_DRAG = { WebkitAppRegion: "no-drag" } as CSSProperties;
const DRAG = { WebkitAppRegion: "drag" } as CSSProperties;

interface FileGroup {
  commentId: string;
  comment?: TimelineEntry;
  files: DeliverableFile[];
}

/**
 * Every deliverable of the issue on one dark stage (MUL-7649), opened from
 * the sidebar's "view all" and from the viewer's grid button (`G`).
 *
 * Files grouped by the comment that posted them, in page order, each group
 * one click from that comment. Files show their latest version only, so the
 * count here always equals the sidebar's.
 *
 * A full-window Dialog: the primitive owns focus (moved in on open, trapped
 * while open, returned to the opener on close) and Escape; this only restyles
 * the popup to cover the window.
 */
export function DeliverablesOverview({
  open,
  onClose,
  identifier,
  files,
  commentById,
  onLocate,
  returnKey,
}: {
  open: boolean;
  onClose: () => void;
  identifier: string;
  files: ReadonlyArray<DeliverableFile>;
  commentById: ReadonlyMap<string, TimelineEntry>;
  onLocate: (origin: DeliverableOrigin) => void;
  /** The file the viewer was showing when it opened this; `G` goes back to it. */
  returnKey: string | null;
}) {
  useImmersiveMode(open);
  const { open: openAttachment, modal } = useOpenAttachment();
  const { openAt } = usePreviewSequence();
  const onReturn = useMemo(
    () =>
      returnKey
        ? () => {
            onClose();
            openAt(returnKey);
          }
        : undefined,
    [returnKey, onClose, openAt],
  );

  useEffect(() => {
    if (!open || !onReturn) return;
    const handler = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
      if (e.key.toLowerCase() === "g") {
        e.preventDefault();
        onReturn();
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open, onReturn]);

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next) onClose();
        }}
      >
        <DialogContent
          showCloseButton={false}
          style={NO_DRAG}
          className="dark top-0 left-0 flex h-dvh max-h-none w-screen max-w-none translate-x-0 translate-y-0 flex-col gap-0 overflow-hidden rounded-none bg-black/95 p-0 text-foreground shadow-none ring-0 backdrop-blur-xl sm:max-w-none"
        >
          <OverviewBody
            identifier={identifier}
            files={files}
            commentById={commentById}
            onClose={onClose}
            onOpenFile={(file) => {
              onClose();
              openAttachment(file.latest);
            }}
            onLocate={(origin) => {
              onClose();
              onLocate(origin);
            }}
          />
        </DialogContent>
      </Dialog>
      {modal}
    </>
  );
}

function OverviewBody({
  identifier,
  files,
  commentById,
  onClose,
  onOpenFile,
  onLocate,
}: {
  identifier: string;
  files: ReadonlyArray<DeliverableFile>;
  commentById: ReadonlyMap<string, TimelineEntry>;
  onClose: () => void;
  onOpenFile: (file: DeliverableFile) => void;
  onLocate: (origin: DeliverableOrigin) => void;
}) {
  const { t } = useT("issues");
  const [filter, setFilter] = useState<Filter>("all");
  const count = files.length;

  const categoryByKey = useMemo(
    () =>
      new Map(
        files.map((f) => [f.key, deliverableCategory(f.latest.content_type, f.latest.filename)]),
      ),
    [files],
  );
  const counts = useMemo(() => {
    const c: Record<DeliverableCategory, number> = { image: 0, document: 0, video: 0, other: 0 };
    for (const category of categoryByKey.values()) c[category] += 1;
    return c;
  }, [categoryByKey]);

  // Groups in page order: by when the posting comment was written.
  const groups = useMemo(() => {
    const byComment = new Map<string, FileGroup>();
    for (const file of files) {
      if (filter !== "all" && categoryByKey.get(file.key) !== filter) continue;
      const commentId = file.latest.comment_id ?? "";
      let group = byComment.get(commentId);
      if (!group) {
        group = { commentId, comment: commentById.get(commentId), files: [] };
        byComment.set(commentId, group);
      }
      group.files.push(file);
    }
    const startedAt = (g: FileGroup) =>
      Date.parse(g.comment?.created_at ?? g.files[g.files.length - 1]!.latest.created_at);
    const list = [...byComment.values()].sort((a, b) => startedAt(a) - startedAt(b));
    // `files` is newest first; inside a group read them in upload order.
    for (const group of list) group.files.reverse();
    return list;
  }, [files, filter, categoryByKey, commentById]);

  const totalBytes = files.reduce((sum, f) => sum + Math.max(0, f.latest.size_bytes), 0);

  const categoryLabels: Record<DeliverableCategory, string> = {
    image: t(($) => $.deliverables.filter_image),
    document: t(($) => $.deliverables.filter_document),
    video: t(($) => $.deliverables.filter_video),
    other: t(($) => $.deliverables.filter_misc),
  };
  const filterOptions: ReadonlyArray<readonly [Filter, ReactNode]> = [
    ["all", <FilterLabel key="all" label={t(($) => $.deliverables.filter_all)} count={count} />],
    ...DELIVERABLE_CATEGORIES.map(
      (category) =>
        [
          category,
          <FilterLabel key={category} label={categoryLabels[category]} count={counts[category]} />,
        ] as const,
    ),
  ];

  return (
    <>
      {/* Three columns keep the filters centered on the window; on a phone
          they drop to a row of their own under the title. */}
      <header
        className="grid shrink-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-2 py-2 pl-3 pr-2 sm:h-14 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] sm:py-0"
        style={DRAG}
      >
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-secondary text-muted-foreground">
            <LayoutGrid className="size-4" />
          </span>
          <div className="min-w-0">
            <DialogTitle className="truncate text-body leading-(--text-body--line-height) font-medium">
              {t(($) => $.deliverables.overview_title, { identifier })}
            </DialogTitle>
            <p className="truncate text-caption text-muted-foreground tabular-nums">
              {totalBytes > 0
                ? t(($) => $.deliverables.overview_summary_with_size, {
                    count,
                    size: formatBytes(totalBytes),
                  })
                : t(($) => $.deliverables.overview_summary, { count })}
            </p>
          </div>
        </div>
        <div
          className="col-span-2 row-start-2 justify-self-center sm:col-span-1 sm:row-start-auto"
          style={NO_DRAG}
        >
          <SegmentedToggle value={filter} options={filterOptions} onChange={setFilter} />
        </div>
        <div className="flex items-center justify-self-end" style={NO_DRAG}>
          <button
            type="button"
            className="flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            title={t(($) => $.deliverables.overview_close)}
            aria-label={t(($) => $.deliverables.overview_close)}
            onClick={onClose}
          >
            <X className="size-4" />
          </button>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-10 pt-4 sm:px-16">
        {groups.length === 0 ? (
          <p className="pt-24 text-center text-body text-muted-foreground">
            {t(($) => $.deliverables.overview_empty)}
          </p>
        ) : (
          <div className="space-y-10">
            {groups.map((group) => (
              <CommentGroup
                key={group.commentId}
                group={group}
                onOpenFile={onOpenFile}
                onLocate={
                  group.comment
                    ? () => onLocate({ kind: "comment", commentId: group.commentId })
                    : undefined
                }
              />
            ))}
          </div>
        )}
      </div>
    </>
  );
}

function FilterLabel({ label, count }: { label: string; count: number }) {
  return (
    <span className="inline-flex items-center gap-1.5 px-1">
      {label}
      <span className="tabular-nums text-muted-foreground">{count}</span>
    </span>
  );
}

function CommentGroup({
  group,
  onOpenFile,
  onLocate,
}: {
  group: FileGroup;
  onOpenFile: (file: DeliverableFile) => void;
  onLocate?: () => void;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const first = group.files[0]!.latest;
  // The comment names its author; without it (not loaded) the uploader stands in.
  const actorType = group.comment?.actor_type ?? first.uploader_type;
  const actorId = group.comment?.actor_id ?? first.uploader_id;
  const name = group.comment?.actor_name ?? getActorName(actorType, actorId);
  const at = group.comment?.created_at ?? first.created_at;

  return (
    <section aria-label={t(($) => $.deliverables.group_comment_by, { name })}>
      <div className="mb-3 flex items-center gap-2">
        <ActorAvatar
          actorType={actorType}
          actorId={actorId}
          name={group.comment?.actor_name}
          avatarUrl={group.comment?.actor_avatar_url}
          size="sm"
          profileLink={false}
        />
        <h2 className="min-w-0 truncate text-body font-medium">
          {t(($) => $.deliverables.group_comment_by, { name })}
        </h2>
        <span
          className="shrink-0 text-caption text-muted-foreground"
          title={new Date(at).toLocaleString(locale)}
        >
          · {timeAgo(at)}
        </span>
        {onLocate && (
          <button
            type="button"
            className="ml-auto inline-flex shrink-0 items-center gap-1.5 rounded-md px-2 py-1 text-caption text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            onClick={onLocate}
          >
            <MessageSquareText className="size-3.5" />
            {t(($) => $.deliverables.locate_comment)}
          </button>
        )}
      </div>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-x-4 gap-y-5">
        {group.files.map((file) => (
          <FileTile key={file.key} file={file} onOpen={() => onOpenFile(file)} />
        ))}
      </div>
    </section>
  );
}

function FileTile({ file, onOpen }: { file: DeliverableFile; onOpen: () => void }) {
  const { latest } = file;
  const meta = [
    fileTypeLabel(latest.filename),
    latest.size_bytes > 0 ? formatBytes(latest.size_bytes) : "",
  ].filter(Boolean);
  return (
    <button type="button" className="group min-w-0 text-left" onClick={onOpen} title={latest.filename}>
      <span className="block aspect-[16/10] overflow-hidden rounded-lg ring-1 ring-border transition-shadow group-hover:ring-foreground/40 group-focus-visible:ring-2 group-focus-visible:ring-ring">
        <DeliverableThumbnail attachment={latest} className="size-full" />
      </span>
      <span className="mt-2 flex min-w-0 items-center gap-1.5">
        <span className="truncate text-body">{latest.filename}</span>
        {file.versions.length > 1 && (
          <VersionBadge version={file.versions.length} className="bg-secondary" />
        )}
      </span>
      {meta.length > 0 && (
        <span className="block text-caption text-muted-foreground tabular-nums">
          {meta.join(" · ")}
        </span>
      )}
    </button>
  );
}
