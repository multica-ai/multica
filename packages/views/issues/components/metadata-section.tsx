"use client";

import { useState } from "react";
import { Copy, Maximize2, Minimize2, X } from "@multica/ui/icons";
import { toast } from "sonner";
import type { IssueMetadata } from "@multica/core/types";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { IssueSidebarSectionHeader } from "./issue-sidebar-section";

export function MetadataSection({ metadata }: { metadata: IssueMetadata }) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const count = Object.keys(metadata).length;
  if (count === 0) return null;

  const copy = async () => {
    const ok = await copyText(JSON.stringify(metadata, null, 2));
    if (ok) toast.success(t(($) => $.comment.copied_toast));
    else toast.error(t(($) => $.detail.metadata_copy_failed));
  };
  const copyButton = () => (
    <IconButton size="icon-xs" label={t(($) => $.detail.metadata_copy)} onClick={() => void copy()}><Copy /></IconButton>
  );

  return (
    <section>
      <Dialog onOpenChange={(next) => { if (!next) setExpanded(false); }}>
        <IssueSidebarSectionHeader title={t(($) => $.detail.section_metadata)} count={count} open={open} onOpenChange={setOpen}
          actions={<>
            {copyButton()}
            <DialogTrigger render={<IconButton size="icon-xs" label={t(($) => $.detail.metadata_view)}><Maximize2 /></IconButton>} />
          </>} />
        {open && <div className="px-2">
          <div className="max-h-64 overflow-auto overscroll-contain px-2" role="region" aria-label={t(($) => $.detail.section_metadata)} tabIndex={0}>
            <MetadataEntries metadata={metadata} />
          </div>
        </div>}
        <DialogContent showCloseButton={false} className={cn("flex flex-col overflow-hidden sm:max-w-3xl", expanded && "h-[calc(100dvh-2rem)] sm:max-w-[calc(100%-2rem)]")}>
          <DialogHeader className="shrink-0">
            <div className="flex min-w-0 items-center justify-between gap-2">
              <DialogTitle>{t(($) => $.detail.section_metadata)}</DialogTitle>
              <div className="flex shrink-0 items-center gap-1">
                {copyButton()}
                <IconButton size="icon-xs" label={expanded ? t(($) => $.detail.metadata_restore) : t(($) => $.detail.metadata_expand)} onClick={() => setExpanded(!expanded)}>
                  {expanded ? <Minimize2 /> : <Maximize2 />}
                </IconButton>
                <DialogClose render={<IconButton size="icon-xs" label={t(($) => $.detail.metadata_close)}><X /></IconButton>} />
              </div>
            </div>
            <DialogDescription>{t(($) => $.detail.metadata_description)}</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 overflow-auto overscroll-contain rounded-lg border border-border px-3" role="region" aria-label={t(($) => $.detail.section_metadata)} tabIndex={0}>
            <MetadataEntries metadata={metadata} wide />
          </div>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function MetadataEntries({ metadata, wide = false }: { metadata: IssueMetadata; wide?: boolean }) {
  return <dl>
    {Object.entries(metadata).map(([key, value]) => (
      <div key={key} className={cn("grid min-w-0 gap-1 py-2", wide && "sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)] sm:gap-4")}>
        <dt className="min-w-0 break-words font-mono text-micro text-muted-foreground [overflow-wrap:anywhere]">{key}</dt>
        <dd className="min-w-0 whitespace-pre-wrap break-words text-caption [overflow-wrap:anywhere]">
          {typeof value === "string" ? value : <span className="font-mono tabular-nums">{JSON.stringify(value)}</span>}
        </dd>
      </div>
    ))}
  </dl>;
}
