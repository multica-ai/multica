"use client";

import { useRef, useState, useCallback, useEffect } from "react";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay, useLazyEditor, useUploadGate, useEditorUpload } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import type { Attachment } from "@multica/core/types";
import { contentReferencesAttachment } from "@multica/core/types";
import { formatShortcut, useShortcut } from "@multica/core/shortcuts";
import { useCommentComposerStore, useCommentDraftStore } from "@multica/core/issues/stores";
import { useT } from "../../i18n";
import { CommentTriggerChips } from "./comment-trigger-chips";
import { useCommentTriggerPreview } from "../hooks/use-comment-trigger-preview";

interface CommentInputProps {
  issueId: string;
  /** Resolves true on success, false on failure. The composer keeps the text
   *  (editor locked + button spinning) until this settles, then clears only on
   *  success — a failed send must not silently discard the user's draft. */
  onSubmit: (content: string, attachmentIds?: string[], suppressAgentIds?: string[]) => Promise<boolean>;
}

function CommentInput({ issueId, onSubmit }: CommentInputProps) {
  const { t } = useT("issues");
  const { t: tEditor } = useT("editor");
  const sendShortcut = useShortcut("send");
  const editorRef = useRef<ContentEditorRef>(null);
  // Sending mid-upload would strip the pending image's blob URL out of the
  // markdown and bind no attachment id — the comment posts without the file.
  const uploadGate = useUploadGate(editorRef);
  // Read the persisted draft once on mount. ContentEditor only honors
  // `defaultValue` at mount time, so this snapshot drives both the editor's
  // initial content and the submit-button enable state — without this the
  // button would be disabled even though the editor visibly contains text.
  const draftKey = `new:${issueId}` as const;
  const [initialDraft] = useState(() =>
    useCommentDraftStore.getState().getDraft(draftKey),
  );
  const [content, setContent] = useState(initialDraft ?? "");
  const [isEmpty, setIsEmpty] = useState(() => !initialDraft?.trim());
  const [submitting, setSubmitting] = useState(false);
  const [suppressedAgentIds, setSuppressedAgentIds] = useState<Set<string>>(() => new Set());
  const triggerPreview = useCommentTriggerPreview({ issueId, content });
  // Attachments uploaded in this composer session. Drives both:
  //  - submit-time `attachment_ids` payload (filtered to URLs still in markdown)
  //  - the editor's AttachmentDownloadProvider, so file-card Eye buttons can
  //    resolve text/code/markdown previews that require the attachment id.
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const { uploadWithToast } = useEditorUpload();

  // Readonly-first: the composer renders as a same-looking static shell until
  // the user shows intent (click / keyboard / file drop). An unsent draft is
  // standing intent — mount the real editor immediately so the draft is
  // visible and editable, exactly like the pre-lazy behavior.
  const lazy = useLazyEditor({
    initialActive: !!initialDraft?.trim(),
    editorRef,
  });
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: lazy.uploadOrQueue,
  });
  // Sticky preference (Settings → Preferences): issue-detail pins this
  // composer to the bottom of the scroll viewport when enabled.
  const sticky = useCommentComposerStore((s) => s.sticky);

  // Draft persistence. Hydrate from store on mount via `defaultValue` above
  // (ContentEditorRef has no setContent, so this is the only injection point).
  // Flush on every onUpdate (debounced upstream) + visibilitychange/pagehide
  // so tab close / mobile background doesn't lose work. Cleared on submit.
  const setDraft = useCommentDraftStore((s) => s.setDraft);
  const clearDraft = useCommentDraftStore((s) => s.clearDraft);
  useEffect(() => {
    const flush = () => {
      const md = editorRef.current?.getMarkdown();
      if (md && md.trim().length > 0) setDraft(draftKey, md);
    };
    const onVis = () => { if (document.visibilityState === "hidden") flush(); };
    document.addEventListener("visibilitychange", onVis);
    window.addEventListener("pagehide", flush);
    return () => {
      document.removeEventListener("visibilitychange", onVis);
      window.removeEventListener("pagehide", flush);
    };
  }, [draftKey, setDraft]);

  const handleUpload = useCallback(async (file: File) => {
    const result = await uploadWithToast(file, { issueId });
    if (result) {
      setPendingAttachments((prev) => [...prev, result]);
    }
    return result;
  }, [uploadWithToast, issueId]);

  useEffect(() => {
    setSuppressedAgentIds(new Set());
  }, [issueId]);

  useEffect(() => {
    const visible = new Set(triggerPreview.agents.map((agent) => agent.id));
    setSuppressedAgentIds((prev) => {
      const next = new Set([...prev].filter((id) => visible.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [triggerPreview.agents]);

  const toggleSuppressedAgent = useCallback((agentId: string) => {
    setSuppressedAgentIds((prev) => {
      const next = new Set(prev);
      if (next.has(agentId)) next.delete(agentId);
      else next.add(agentId);
      return next;
    });
  }, []);

  const handleSubmit = async () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || submitting) return;
    // Re-read the queue here rather than trusting the button's disabled prop:
    // Cmd+Enter never touches the button, and a click can land in the same
    // tick an upload starts.
    if (uploadGate.isBlocked()) return;
    // Track every attachment whose stable download URL OR legacy
    // storage URL is referenced in the markdown body. Both shapes
    // can appear in the same comment during the MUL-3130 rollout —
    // see contentReferencesAttachment for the rationale.
    const activeIds = pendingAttachments
      .filter((a) => contentReferencesAttachment(content, a))
      .map((a) => a.id);
    const suppressAgentIds = triggerPreview.agents
      .filter((agent) => suppressedAgentIds.has(agent.id))
      .map((agent) => agent.id);
    // Pessimistic submit: keep the text in place (the editor is locked and the
    // button spins via `submitting`) until the server actually accepts it, then
    // clear. Clearing only on success means a slow send no longer looks like
    // "comment posted but the box is still full", and a failed send keeps the
    // draft instead of silently dropping it.
    setSubmitting(true);
    try {
      const ok = await onSubmit(
        content,
        activeIds.length > 0 ? activeIds : undefined,
        suppressAgentIds.length > 0 ? suppressAgentIds : undefined,
      );
      if (ok) {
        editorRef.current?.clearContent();
        setContent("");
        setIsEmpty(true);
        setSuppressedAgentIds(new Set());
        setPendingAttachments([]);
        clearDraft(draftKey);
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      {...dropZoneProps}
      className="relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border"
    >
      {/* Lock the editor while the send is in flight. ContentEditor can't
          toggle Tiptap's `editable` post-mount (see its docstring), so the
          documented way to make it non-interactive is a pointer-events-none +
          dimmed wrapper. */}
      {lazy.active && (
      <div
        className={cn(
          "flex-1 min-h-0 overflow-y-auto px-3 py-2",
          // Pinned to the viewport bottom the composer grows upward; cap it
          // so a long draft can't swallow the whole timeline (the editor
          // area scrolls internally instead).
          sticky && "max-h-[40vh]",
          submitting && "pointer-events-none opacity-60",
          !lazy.ready && "hidden",
        )}
        aria-busy={submitting || undefined}
      >
        <ContentEditor
          ref={editorRef}
          defaultValue={initialDraft}
          onReady={lazy.onReady}
          placeholder={t(($) => $.comment.leave_comment_placeholder)}
          onUpdate={(md) => {
            setContent(md);
            setIsEmpty(!md.trim());
            // Debounced upstream (debounceMs=100). Persist on every tick so a
            // reload or scroll-out-of-viewport restores work to the keystroke.
            if (md.trim().length > 0) setDraft(draftKey, md);
            else clearDraft(draftKey);
          }}
          onSubmit={handleSubmit}
          onUploadFile={handleUpload}
          onUploadingChange={uploadGate.onUploadingChange}
          debounceMs={100}
          currentIssueId={issueId}
          attachments={pendingAttachments}
          enableSlashCommands
          slashCommandMode="command"
        />
      </div>
      )}
      {/* Static shell — visually clones the empty single-line composer.
          Real editor mounts (hidden) on first intent; shell stays visible
          until it's ready so the card never blanks or shifts. */}
      {!lazy.ready && (
        <div
          data-testid="comment-composer-shell"
          role="button"
          tabIndex={0}
          aria-label={t(($) => $.comment.leave_comment_placeholder)}
          className="flex-1 min-h-0 cursor-text px-3 py-2"
          onClick={() => lazy.activate()}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              lazy.activate();
            }
          }}
        >
          {/* rich-text-editor + <p>: the shell line inherits the editor's
              exact type metrics (line-height 1.625 from prose.css), so the
              shell→editor swap doesn't shift layout. */}
          <div className="rich-text-editor text-sm">
            <p className="text-muted-foreground">{t(($) => $.comment.leave_comment_placeholder)}</p>
          </div>
        </div>
      )}
      <div className="absolute bottom-1 left-2 right-28 min-w-0">
        <CommentTriggerChips
          agents={triggerPreview.agents}
          blocked={triggerPreview.blocked}
          draftContent={content}
          suppressedAgentIds={suppressedAgentIds}
          onToggle={toggleSuppressedAgent}
        />
      </div>
      <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
        <FileUploadButton
          size="sm"
          multiple
          onSelect={(file) => lazy.uploadOrQueue([file])}
        />
        <SubmitButton
          onClick={handleSubmit}
          disabled={isEmpty}
          loading={submitting}
          busy={uploadGate.uploading}
          tooltip={uploadGate.uploading
            ? tEditor(($) => $.upload.in_progress)
            : sendShortcut
              ? `${t(($) => $.comment.send_tooltip)} · ${formatShortcut(sendShortcut)}`
              : t(($) => $.comment.send_tooltip)}
          ariaLabel={uploadGate.uploading
            ? tEditor(($) => $.upload.in_progress)
            : t(($) => $.comment.send_tooltip)}
        />
      </div>
      {isDragOver && <FileDropOverlay />}
    </div>
  );
}

export { CommentInput };
