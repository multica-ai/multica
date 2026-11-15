"use client";

import { useRef, useState, type ReactNode, type RefObject } from "react";
import { ArrowUp, Loader2, Trash2 } from "@multica/ui/icons";
import { createShortcutChord } from "@multica/core/shortcuts";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { MAX_REPLY_ANNOTATIONS, type ReplyAnnotation } from "@multica/core/drafts/reply-annotation";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import { EditorOverlayOwnerContext } from "../../editor/overlay-owner";
import { ShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import {
  useComposerVoice,
  VoiceErrorRow,
  VoiceHud,
  VoiceMicButton,
  VoiceSetupDialog,
  VoiceStrip,
} from "../../voice";

/** The existing reply/comment composer owns routing, uploads and submission. */
export interface AnnotationSendControls {
  submit: () => Promise<void>;
  submitting: boolean;
  uploading: boolean;
  targetMissing?: boolean;
  restart: boolean;
  recipients: ReactNode;
  draftContent: string;
  annotations: readonly ReplyAnnotation[];
}

export interface AnnotationComposerSlot {
  pending: ReplyAnnotation;
  render: (controls?: AnnotationSendControls) => ReactNode;
}

export function annotationsWithPending(annotations: readonly ReplyAnnotation[], pending?: ReplyAnnotation): readonly ReplyAnnotation[] {
  if (!pending) return annotations;
  const saved = annotations.some((a) => a.id === pending.id);
  return saved ? annotations.map((a) => a.id === pending.id ? pending : a) : [...annotations, pending];
}

const ENTER = createShortcutChord("Enter");
const SEND = createShortcutChord("Enter", { primary: true });

export function AnnotationComposer({
  issueId, owner, annotation, existing, number, editorRef, controls,
  sourceMissing, sourceChanged, canSave, onChange, onSave, onRemove, onBusyChange,
}: {
  issueId?: string;
  owner: string;
  annotation: ReplyAnnotation;
  existing: boolean;
  number: number;
  editorRef: RefObject<ContentEditorRef | null>;
  controls?: AnnotationSendControls;
  sourceMissing: boolean;
  sourceChanged: boolean;
  canSave: boolean;
  onChange: (note: string) => void;
  onSave: (note: string, close: boolean) => boolean;
  onRemove?: () => void;
  onBusyChange: (busy: boolean) => void;
}) {
  const { t } = useT("issues");
  const [initialNote] = useState(annotation.note);
  const [sending, setSending] = useState(false);
  const inFlight = useRef(false);
  const busy = sending || controls?.submitting === true;
  const otherAnnotations = controls?.annotations.filter((a) => a.id !== annotation.id).length ?? 0;
  const hasBody = !!controls?.draftContent.trim();
  const sendAll = otherAnnotations > 0 || hasBody;
  const sendDisabled = !annotation.note.trim() || !canSave || busy || !!controls?.uploading || !!controls?.targetMissing;
  const save = () => {
    if (busy || inFlight.current || !canSave) return;
    const note = editorRef.current?.getMarkdown() ?? "";
    if (note.trim()) onSave(note, true);
  };
  const send = async () => {
    if (!controls || controls.targetMissing || busy || inFlight.current || !canSave) return;
    const note = editorRef.current?.getMarkdown() ?? "";
    if (!note.trim()) return;
    inFlight.current = true;
    onBusyChange(true);
    setSending(true);
    try {
      // Commit first: the existing composer's failure/retry path owns the note.
      if (onSave(note, false)) await controls.submit();
    } finally {
      inFlight.current = false;
      onBusyChange(false);
      setSending(false);
    }
  };
  // No voice send: Enter saves the note here, so dictation only inserts.
  const voice = useComposerVoice({
    insert: (text) => editorRef.current?.insertText(text) ?? false,
    scope: annotation.id,
    disabled: busy,
  });

  return <EditorOverlayOwnerContext.Provider value={owner}>
    <div ref={voice.rootRef} onFocusCapture={voice.onFocusCapture}
      className="relative flex w-96 max-w-[calc(100vw-26px)] flex-col gap-1" aria-busy={busy || undefined}>
      <div className={busy ? "pointer-events-none opacity-60" : undefined} inert={busy || undefined}>
        <ContentEditor ref={editorRef} defaultValue={initialNote}
          ariaLabel={t(($) => $.reply.annotations.note_label)}
          placeholder={t(($) => $.reply.annotations.note_placeholder)}
          className="min-h-8 max-h-32 overflow-y-auto px-2 py-1.5"
          currentIssueId={issueId} showBubbleMenu={false} debounceMs={100}
          onUpdate={onChange} onEnter={save} onSubmit={send} />
      </div>
      <VoiceHud voice={voice} />
      <VoiceSetupDialog voice={voice} />
      <VoiceErrorRow voice={voice} className="mx-1" />
      {(sourceMissing || sourceChanged) && <p role={sourceMissing ? "alert" : undefined} className="px-2 text-caption text-muted-foreground">
        {t(($) => $.reply.annotations.source_changed)}
      </p>}
      {controls?.targetMissing && <p role="alert" className="px-2 text-caption text-destructive">
        {t(($) => $.reply.annotations.target_deleted)}
      </p>}
      {!canSave && <p className="px-2 text-caption text-muted-foreground">{t(($) => $.reply.annotations.count_limit, { count: MAX_REPLY_ANNOTATIONS })}</p>}
      {sendAll && <p className="px-2 text-caption text-muted-foreground">
        {otherAnnotations ? t(($) => hasBody ? $.reply.annotations.send_with_annotations_and_body : $.reply.annotations.send_with_annotations, { count: otherAnnotations })
          : t(($) => $.reply.annotations.send_with_body)}
      </p>}
      {voice.showStrip ? <div className="flex items-center gap-1 px-1 pb-1">
        {controls?.recipients && <div className="min-w-0 max-w-[60%] shrink-0 overflow-hidden">{controls.recipients}</div>}
        <VoiceStrip voice={voice} />
      </div> : <div className="flex flex-wrap items-center gap-1 px-1 pb-1" inert={busy || undefined}>
        {existing && onRemove && <IconButton variant="ghost" size="icon-sm"
          className="text-muted-foreground hover:text-destructive" label={t(($) => $.reply.annotations.remove, { number })}
          disabled={busy} onClick={onRemove}><Trash2 /></IconButton>}
        <div className="min-w-0 flex-1">{controls?.recipients}</div>
        <div className="ml-auto flex items-center gap-1">
          <VoiceMicButton voice={voice} />
          {controls && (controls.restart
            // Stopping a run must stay spelled out, never an unlabeled icon.
            ? <Button size="sm" variant="destructive" disabled={sendDisabled} onClick={send} aria-busy={busy || undefined}>
              {busy && <Loader2 className="animate-spin" />}
              {t(($) => $.comment.restart_send)}
              <ShortcutKeycaps shortcut={SEND} decorative />
            </Button>
            : <IconButton label={t(($) => sendAll ? $.reply.annotations.send_all : $.comment.send_tooltip)}
              shortcut={<ShortcutKeycaps shortcut={SEND} decorative />}
              disabled={sendDisabled} onClick={send} aria-busy={busy || undefined}>
              {busy ? <Loader2 className="animate-spin" /> : <ArrowUp />}
            </IconButton>)}
          <Button size="sm" disabled={!annotation.note.trim() || !canSave || busy} onClick={save}
            aria-label={t(($) => existing ? $.reply.annotations.save : $.reply.annotations.confirm_add)}>
            {t(($) => existing ? $.reply.annotations.save_short : $.reply.annotations.confirm_add_short)}
            <ShortcutKeycaps shortcut={ENTER} decorative />
          </Button>
        </div>
      </div>}
    </div>
  </EditorOverlayOwnerContext.Provider>;
}
