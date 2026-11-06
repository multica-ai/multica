"use client";

import { useEffect, useRef, useState, type RefObject } from "react";
import { Loader2, Trash2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { composeSelectionReply, MAX_REPLY_ANNOTATIONS, type ReplyAnnotation } from "@multica/core/drafts/reply-annotation";
import type { AgentTask } from "@multica/core/types";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import { EditorOverlayOwnerContext } from "../../editor/overlay-owner";
import { useComposerSubmit } from "../../editor/use-composer-submit";
import { useUploadGate } from "../../editor/use-upload-gate";
import { useT } from "../../i18n";
import { commentTriggerPreviewSignature, useCommentTriggerPreview } from "../hooks/use-comment-trigger-preview";
import { useRecipientActions } from "../hooks/use-recipient-actions";
import { CommentTriggerChips } from "./comment-trigger-chips";
import { useStopRunsBeforeSend } from "./use-stop-runs-before-send";

export interface AnnotationReplyOptions {
  onSubmit: (sourceId: string, content: string, suppressAgentIds?: string[], steerTaskIds?: string[]) => Promise<string | boolean>;
  onAccepted?: (commentId: string) => void;
  steerByDefault: (task: AgentTask) => boolean;
}

export function AnnotationComposer({
  issueId, owner, annotation, existing, number, parentId, editorRef, reply,
  sourceMissing, sourceChanged, canSave, isSourceAvailable,
  onChange, onSave, onRemove, onBusyChange, onAccepted,
}: {
  issueId: string;
  owner: string;
  annotation: ReplyAnnotation;
  existing: boolean;
  number: number;
  parentId?: string;
  editorRef: RefObject<ContentEditorRef | null>;
  reply?: AnnotationReplyOptions;
  sourceMissing: boolean;
  sourceChanged: boolean;
  canSave: boolean;
  isSourceAvailable: () => boolean;
  onChange: (note: string) => void;
  onSave: (note: string) => void;
  onRemove?: () => void;
  onBusyChange: (busy: boolean) => void;
  onAccepted: () => void;
}) {
  const { t } = useT("issues");
  const [initialNote] = useState(annotation.note);
  const direct = !existing && !!reply;
  const composed = composeSelectionReply(annotation, annotation.note);
  const preview = useCommentTriggerPreview({ issueId, parentId, content: direct ? composed : "" });
  const { recipients, routing, setAction } = useRecipientActions({
    issueId, agents: preview.agents, allowSteer: true,
    steerByDefault: reply?.steerByDefault ?? (() => false), resetKey: `${issueId}:${annotation.id}`,
  });
  const stopRuns = useStopRunsBeforeSend(issueId);
  const uploadGate = useUploadGate(editorRef);
  const acceptedId = useRef<string | null>(null);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  const { submitting, submit } = useComposerSubmit({
    editorRef, uploadGate,
    normalize: (raw) => direct && isSourceAvailable() ? composeSelectionReply(annotation, raw) : "",
    onSubmit: async (content) => {
      if (!reply || !isSourceAvailable()) return false;
      onBusyChange(true);
      try {
        const previewMatches = preview.isCurrent && commentTriggerPreviewSignature(content) === commentTriggerPreviewSignature(composed);
        if (!(await stopRuns(previewMatches ? routing.restartTaskIds : []))) return false;
        const result = await reply.onSubmit(annotation.sourceCommentId, content,
          routing.suppressAgentIds.length ? routing.suppressAgentIds : undefined,
          routing.steerTaskIds.length ? routing.steerTaskIds : undefined);
        acceptedId.current = typeof result === "string" ? result : null;
        return !!result;
      } finally {
        onBusyChange(false);
      }
    },
    onAccepted: () => {
      if (!mounted.current) return;
      onAccepted();
      if (acceptedId.current) reply?.onAccepted?.(acceptedId.current);
    },
  });
  const save = () => {
    if (submitting || !canSave) return;
    const note = editorRef.current?.getMarkdown() ?? "";
    if (note.trim()) onSave(note);
  };
  const saveLabel = t(($) => existing ? $.reply.annotations.save : $.reply.annotations.confirm_add);

  return <EditorOverlayOwnerContext.Provider value={owner}>
    <div className="flex w-96 max-w-[calc(100vw-26px)] flex-col gap-1" aria-busy={submitting || undefined}>
      <div className={submitting ? "pointer-events-none opacity-60" : undefined} inert={submitting || undefined}>
        <ContentEditor ref={editorRef} defaultValue={initialNote}
          ariaLabel={t(($) => $.reply.annotations.note_label)}
          placeholder={t(($) => $.reply.annotations.note_placeholder)}
          className="min-h-8 max-h-32 overflow-y-auto px-2 py-1.5"
          currentIssueId={issueId} showBubbleMenu={false} debounceMs={100}
          onUpdate={onChange} onSubmit={direct ? submit : save} />
      </div>
      {sourceMissing && <p role="alert" className="px-2 text-caption text-destructive">{t(($) => $.reply.annotations.direct_target_deleted)}</p>}
      {!sourceMissing && sourceChanged && <p className="px-2 text-caption text-muted-foreground">{t(($) => $.reply.annotations.source_changed)}</p>}
      {!canSave && <p className="px-2 text-caption text-muted-foreground">{t(($) => $.reply.annotations.count_limit, { count: MAX_REPLY_ANNOTATIONS })}</p>}
      <div className="flex flex-wrap items-center gap-1 px-1 pb-1" inert={submitting || undefined}>
        {existing && onRemove && <IconButton variant="ghost" size="icon-sm"
          className="text-muted-foreground hover:text-destructive" label={t(($) => $.reply.annotations.remove, { number })}
          disabled={submitting} onClick={onRemove}><Trash2 /></IconButton>}
        {direct && <div className="min-w-0 flex-1">
          <CommentTriggerChips recipients={recipients} blocked={preview.blocked}
            hasAllMembersMention={preview.hasAllMembersMention} draftContent={composed} onActionChange={setAction} />
        </div>}
        <div className="ml-auto flex items-center gap-1">
          <Button size="sm" variant={direct ? "ghost" : "default"} className={direct ? "text-muted-foreground" : undefined}
            disabled={!annotation.note.trim() || !canSave || submitting}
            onClick={save}>{saveLabel}</Button>
          {direct && <Button size="sm" variant={routing.restartTaskIds.length ? "destructive" : "default"}
            disabled={!composed || sourceMissing || submitting} onClick={submit} aria-busy={submitting || undefined}>
            {submitting && <Loader2 className="animate-spin" />}
            {t(($) => routing.restartTaskIds.length ? $.comment.restart_send
              : parentId ? $.reply.annotations.direct_reply : $.reply.annotations.direct_comment)}
          </Button>}
        </div>
      </div>
    </div>
  </EditorOverlayOwnerContext.Provider>;
}
