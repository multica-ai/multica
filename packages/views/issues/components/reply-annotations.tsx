"use client";

import { useId, useState } from "react";
import { ChevronDown, ChevronRight, Quote, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import type { ReplyAnnotation } from "@multica/core/drafts/reply-annotation";
import { ReadonlyContent } from "../../editor";
import { useT } from "../../i18n";
import { annotationRange, findAnnotationSource } from "./comment-annotation-selection";

export function ReplyAnnotations({ draftKey, annotations, content, disabled, onSubmit, onViewSource }: {
  draftKey: CommentDraftKey;
  annotations: ReplyAnnotation[];
  content: string;
  disabled: boolean;
  onSubmit: () => void;
  onViewSource?: (sourceCommentId: string) => void;
}) {
  const { t } = useT("issues");
  const [expanded, setExpanded] = useState(false);
  const [preview, setPreview] = useState(false);
  const [unavailable, setUnavailable] = useState<string | null>(null);
  const panelId = useId();

  return <div className="mb-3 flex flex-col gap-2">
    <div className="flex flex-wrap items-center justify-between gap-1">
      <Button variant="brandSubtle" size="sm" aria-expanded={expanded} aria-controls={panelId} onClick={() => setExpanded(!expanded)}>
        <Quote />{t(($) => $.reply.annotations.count, { count: annotations.length })}
        {expanded ? <ChevronDown /> : <ChevronRight />}
      </Button>
      <span className="text-caption text-muted-foreground">{t(($) => $.reply.annotations.saved)}</span>
    </div>
    {expanded && <div id={panelId} className="flex flex-col gap-3 rounded-lg border border-border p-2.5">
      {annotations.map((a, index) => <div key={a.id} className="flex flex-col gap-2">
        <div className="flex min-w-0 items-center gap-2 text-label">
          <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-muted text-caption">{index + 1}</span>
          <span className="min-w-0 flex-1 truncate font-medium">{a.sourceActorName}</span>
          <Button variant="ghost" size="sm" onClick={(event) => {
            onViewSource?.(a.sourceCommentId);
            const card = event.currentTarget.closest<HTMLElement>("[data-annotation-thread]");
            requestAnimationFrame(() => {
              const source = card && findAnnotationSource(card, a.sourceCommentId);
              const range = source && annotationRange(source, a);
              setUnavailable(range ? null : a.id);
              if (source) source.scrollIntoView({ block: "center", behavior: "instant" });
              if (range) {
                window.getSelection()?.removeAllRanges();
                window.getSelection()?.addRange(range);
              }
            });
          }}>{t(($) => $.reply.annotations.view_source)}</Button>
          <Button variant="ghost" size="icon-sm" disabled={disabled}
            aria-label={t(($) => $.reply.annotations.remove, { number: index + 1 })}
            onClick={() => useCommentDraftStore.getState().removeAnnotation(draftKey, a.id)}><X /></Button>
        </div>
        <blockquote className="max-h-32 overflow-y-auto whitespace-pre-wrap break-words border-l-2 border-brand/50 pl-2.5 text-body text-muted-foreground">{a.quote}</blockquote>
        {unavailable === a.id && <p role="status" className="text-caption text-muted-foreground">{t(($) => $.reply.annotations.source_changed)}</p>}
        <label className="flex flex-col gap-1.5">
          <span className="text-caption text-muted-foreground">{t(($) => $.reply.annotations.note_label)}</span>
          <Textarea value={a.note} disabled={disabled}
            onChange={(event) => useCommentDraftStore.getState().updateAnnotation(draftKey, a.id, event.target.value)}
            onKeyDown={(event) => {
              if (!event.nativeEvent.isComposing && event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
                event.preventDefault(); onSubmit();
              }
            }} />
        </label>
      </div>)}
      <Button variant="ghost" size="sm" className="self-start" aria-expanded={preview} onClick={() => setPreview(!preview)}>
        {preview ? t(($) => $.reply.annotations.hide_preview) : t(($) => $.reply.annotations.preview)}
      </Button>
      {preview && <div className="min-w-0 rounded-lg bg-muted p-2.5"><ReadonlyContent content={content} /></div>}
    </div>}
  </div>;
}
