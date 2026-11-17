"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { domToBlob } from "modern-screenshot";
import { toast } from "sonner";
import { Copy, Download, Image, Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { copyImage } from "@multica/ui/lib/clipboard";
import { useActorName } from "@multica/core/workspace/hooks";
import { isDeletedComment } from "@multica/core/issues/comment-deletion";
import { useCommentShareStore } from "@multica/core/issues/stores";
import type { TimelineEntry } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { ReadonlyContent } from "../../editor";
import { useLocale, useT } from "../../i18n";

export interface ShareThread {
  root: TimelineEntry;
  replies: readonly TimelineEntry[];
}

// A transparent pixel stands in for an image the capture cannot read (a
// storage host without CORS), so one unreadable avatar does not fail the card.
const PLACEHOLDER_IMAGE = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=";

/** Visible comments of a thread in reading order, root first. */
export function shareThreadEntries(thread: ShareThread): TimelineEntry[] {
  return [thread.root, ...thread.replies]
    .filter((entry) => entry.type === "comment" && !isDeletedComment(entry))
    .sort((a, b) => a.created_at.localeCompare(b.created_at));
}

/**
 * Renders the picked threads as one card and turns it into a PNG to copy or
 * download. The preview is the captured node itself, so what is shown is
 * what is shared.
 */
export function CommentShareImageDialog({
  open,
  onOpenChange,
  identifier,
  title,
  threads,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  identifier: string;
  title: string;
  threads: readonly ShareThread[];
}) {
  const { t } = useT("issues");
  const cardRef = useRef<HTMLDivElement>(null);
  const [image, setImage] = useState<{ blob: Blob; url: string } | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    let url: string | null = null;
    setImage(null);
    setFailed(false);
    // Wait for the dialog's enter animation and the content's images and
    // fonts, so the capture does not freeze a half-scaled or half-loaded card.
    const timer = setTimeout(async () => {
      const node = cardRef.current;
      if (!node) return;
      try {
        await Promise.all([
          document.fonts?.ready,
          ...Array.from(node.querySelectorAll("img")).map((img) => img.decode().catch(() => undefined)),
        ]);
        const blob = await domToBlob(node, {
          scale: 2,
          type: "image/png",
          backgroundColor: getComputedStyle(node).backgroundColor,
          fetch: { placeholderImage: PLACEHOLDER_IMAGE },
        });
        if (cancelled) return;
        url = URL.createObjectURL(blob);
        setImage({ blob, url });
      } catch {
        if (!cancelled) setFailed(true);
      }
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(timer);
      if (url) URL.revokeObjectURL(url);
    };
  }, [open, threads]);

  const fileName = `${identifier}-discussion.png`;
  const copy = () => {
    if (!image) return;
    void copyImage(image.url).then((ok) => {
      if (ok) toast.success(t(($) => $.share_image.copied));
      else toast.error(t(($) => $.share_image.copy_failed));
    });
  };
  const download = () => {
    if (!image) return;
    const link = document.createElement("a");
    link.href = image.url;
    link.download = fileName;
    link.click();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[720px]">
        <DialogHeader>
          <DialogTitle>{t(($) => $.share_image.dialog_title)}</DialogTitle>
        </DialogHeader>
        <div className="max-h-[60vh] overflow-auto rounded-md border bg-muted/40 p-3">
          <ShareCard ref={cardRef} identifier={identifier} title={title} threads={threads} />
        </div>
        {failed && <p className="text-label text-destructive">{t(($) => $.share_image.failed)}</p>}
        <DialogFooter>
          <Button variant="outline" disabled={!image} onClick={download}>
            {image ? <Download className="h-3.5 w-3.5" aria-hidden /> : <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />}
            {t(($) => $.share_image.download)}
          </Button>
          <Button disabled={!image} onClick={copy}>
            <Copy className="h-3.5 w-3.5" aria-hidden />
            {t(($) => $.share_image.copy)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ShareCard({
  ref,
  identifier,
  title,
  threads,
}: {
  ref: React.Ref<HTMLDivElement>;
  identifier: string;
  title: string;
  threads: readonly ShareThread[];
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const { getActorName } = useActorName();
  const formatTime = (iso: string) => new Date(iso).toLocaleString(locale, {
    year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit",
  });
  return (
    <div ref={ref} data-share-card className="mx-auto flex w-[640px] flex-col gap-4 rounded-lg bg-background p-6 text-foreground">
      <div className="flex flex-col gap-1">
        <span className="text-label text-muted-foreground">{identifier}</span>
        <span className="text-title font-semibold">{title}</span>
      </div>
      {threads.map((thread) => (
        <div key={thread.root.id} className="flex flex-col rounded-lg border border-border">
          {shareThreadEntries(thread).map((entry, i) => (
            <div key={entry.id} className={i > 0 ? "border-t border-border/50 px-4 py-3" : "px-4 py-3"}>
              <div className="mb-1.5 flex items-center gap-2">
                <ActorAvatar
                  actorType={entry.actor_type}
                  actorId={entry.actor_id}
                  name={entry.actor_name}
                  avatarUrl={entry.actor_avatar_url}
                  size="sm"
                />
                <span className="text-body font-medium">
                  {entry.actor_name || getActorName(entry.actor_type, entry.actor_id)}
                </span>
                <span className="text-label text-muted-foreground">{formatTime(entry.created_at)}</span>
              </div>
              <ReadonlyContent content={entry.content ?? ""} attachments={entry.attachments} />
            </div>
          ))}
        </div>
      ))}
      <span className="text-caption text-muted-foreground">
        {t(($) => $.share_image.footer, { time: formatTime(new Date().toISOString()) })}
      </span>
    </div>
  );
}

/**
 * Shown above the composer while threads are being picked on this issue:
 * how many are picked, and the way to the share image.
 */
export function CommentShareBar({
  issueId,
  identifier,
  title,
  timeline,
  threadReplies,
}: {
  issueId: string;
  identifier: string;
  title: string;
  timeline: readonly TimelineEntry[];
  threadReplies: ReadonlyMap<string, readonly TimelineEntry[]>;
}) {
  const { t } = useT("issues");
  const active = useCommentShareStore((s) => s.issueId === issueId);
  const selected = useCommentShareStore((s) => (s.issueId === issueId ? s.selected : EMPTY_SELECTION));
  const clear = useCommentShareStore((s) => s.clear);
  const [dialogOpen, setDialogOpen] = useState(false);

  // Selection belongs to the issue on screen; navigating away ends it.
  useEffect(() => () => {
    if (useCommentShareStore.getState().issueId === issueId) useCommentShareStore.getState().clear();
  }, [issueId]);

  const threads = useMemo(() => {
    const picked = new Set(selected);
    return timeline
      .filter((entry) => entry.type === "comment" && !entry.parent_id && picked.has(entry.id))
      .map((root) => ({ root, replies: threadReplies.get(root.id) ?? EMPTY_REPLIES }));
  }, [selected, timeline, threadReplies]);

  if (!active) return null;
  return (
    <>
      <div className="mb-2 flex items-center gap-2 rounded-lg border bg-popover px-3 py-2 shadow-sm">
        <Image className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
        <span className="text-body">{t(($) => $.share_image.selected, { count: threads.length })}</span>
        <div className="ml-auto flex items-center gap-1.5">
          <Button variant="ghost" size="sm" onClick={clear}>{t(($) => $.share_image.cancel)}</Button>
          <Button size="sm" disabled={threads.length === 0} onClick={() => setDialogOpen(true)}>
            {t(($) => $.share_image.create)}
          </Button>
        </div>
      </div>
      <CommentShareImageDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        identifier={identifier}
        title={title}
        threads={threads}
      />
    </>
  );
}

const EMPTY_SELECTION: string[] = [];
const EMPTY_REPLIES: TimelineEntry[] = [];
