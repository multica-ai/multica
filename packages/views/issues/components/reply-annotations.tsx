"use client";

import { useRef, useState } from "react";
import { MessageSquare, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import type { ReplyAnnotation } from "@multica/core/drafts/reply-annotation";
import { useT } from "../../i18n";

export function ReplyAnnotations({ draftKey, annotations, disabled, onEditAnnotation }: {
  draftKey: CommentDraftKey;
  annotations: ReplyAnnotation[];
  disabled: boolean;
  onEditAnnotation?: (id: string) => boolean;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const [unavailable, setUnavailable] = useState<string | null>(null);
  const returningToSource = useRef(false);
  const label = t(($) => $.reply.annotations.count, { count: annotations.length });

  return <div className="mb-2">
    <Popover open={open} onOpenChange={(next) => {
      if (next) returningToSource.current = false;
      setOpen(next);
    }}>
      <PopoverTrigger openOnHover delay={150} closeDelay={200}
        render={<Button variant="outline" size="sm" />}>
        <MessageSquare />{label}
      </PopoverTrigger>
      <PopoverContent side="top" align="start" aria-label={label}
        className="w-80 max-w-[calc(100vw-16px)]"
        initialFocus={false}
        finalFocus={() => !returningToSource.current}
        onClick={(event) => event.stopPropagation()}>
        <div className="flex max-h-64 flex-col gap-2 overflow-y-auto">
          {annotations.map((annotation, index) => <div key={annotation.id} className="flex items-start gap-1">
            <span aria-hidden="true" className="mt-1 flex size-6 shrink-0 items-center justify-center rounded-full bg-muted text-caption tabular-nums">{index + 1}</span>
            <div className="min-w-0 flex-1">
              <button type="button" disabled={disabled}
                aria-label={t(($) => $.reply.annotations.edit, { number: index + 1 })}
                className="w-full rounded-sm p-1 text-left text-body hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
                onClick={() => {
                  if (onEditAnnotation?.(annotation.id)) {
                    returningToSource.current = true;
                    setOpen(false); setUnavailable(null);
                  }
                  else setUnavailable(annotation.id);
                }}>
                <blockquote className="max-h-28 overflow-y-auto whitespace-pre-wrap break-words border-l-2 border-border pl-2 text-muted-foreground">
                  {annotation.quote}
                </blockquote>
                {annotation.note && <p className="mt-1 whitespace-pre-wrap break-words">{annotation.note}</p>}
              </button>
              {unavailable === annotation.id && <p role="status" className="p-1 text-caption text-muted-foreground">
                {t(($) => $.reply.annotations.source_changed)}
              </p>}
            </div>
            <Button variant="ghost" size="icon-sm" disabled={disabled}
              aria-label={t(($) => $.reply.annotations.remove, { number: index + 1 })}
              onClick={() => useCommentDraftStore.getState().removeAnnotation(draftKey, annotation.id)}><X /></Button>
          </div>)}
        </div>
      </PopoverContent>
    </Popover>
  </div>;
}
