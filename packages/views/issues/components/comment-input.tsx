"use client";

import { useRef, useState, useCallback, useEffect } from "react";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay, useLazyEditor, useUploadGate, useComposerSubmit } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { contentReferencesAttachment } from "@multica/core/types";
import { formatShortcut, useShortcut } from "@multica/core/shortcuts";
import { useCommentComposerStore, useCommentDraftStore } from "@multica/core/issues/stores";
import { useT } from "../../i18n";
import { CommentTriggerChips } from "./comment-trigger-chips";
import { useCommentTriggerPreview } from "../hooks/use-comment-trigger-preview";
import { useCommentUploads } from "./use-comment-uploads";
import { useQuickActionMenu } from "../hooks/use-quick-action-menu";

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
  // Quick actions in the `/` menu: picking one inserts the server-rendered
  // body so the user can edit before sending, instead of firing immediately.
  const quickActionMenu = useQuickActionMenu(issueId);
  const draftKey = `new:${issueId}` as const;
  const [initialDraft] = useState(() =>
    useCommentDraftStore.getState().getDraft(draftKey),
  );
  const [content, setContent] = useState(initialDraft ?? "");
  const [isEmpty, setIsEmpty] = useState(() => !initialDraft?.trim());
  const [suppressedAgentIds, setSuppressedAgentIds] = useState<Set<string>>(() => new Set());
  const triggerPreview = useCommentTriggerPreview({ issueId, content });
  // Uploads for this composer session (MUL-5181). Owned by the module-level
  // coordinator and persisted in the draft store, so closing/scrolling the
  // composer away no longer drops an in-flight upload — its result lands in the
  // draft. `attachments` (completed rows) drives both the submit `attachment_ids`
  // payload and the editor's AttachmentDownloadProvider; `uploads` drives the
  // status chips (uploading / failed / interrupted).
  // `gate` widens the editor gate with coordinator-owned placeholders, so a
  // composer reopened over a still-in-flight upload cannot send past it.
  const { attachments: pendingAttachments, handleUpload, gate } =
    useCommentUploads(draftKey, { issueId }, uploadGate, editorRef);

  // Readonly-first: the composer renders as a same-looking static shell until
  // the user shows intent (click / keyboard / file drop). An unsent draft is
  // standing intent — mount the real editor immediately so the draft is
  // visible and editable, exactly like the pre-lazy behavior.
  const lazy = useLazyEditor({
    initialActive:
      !!initialDraft?.trim() ||
      useCommentDraftStore.getState().getUploads(draftKey).length > 0,
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

  // Await-then-render send (MUL-5181): the shared hook reads the markdown,
  // guards empty/in-flight, re-checks the upload gate, locks + spins via
  // `submitting`, and clears only once the server accepts — a failed send keeps
  // the draft instead of silently dropping it.
  // Stale-submit guard (MUL-5181 P0): if this composer unmounts mid-submit
  // (issue detail closed) and the user reopens and types a new draft under the
  // same key, the late success may only clear the draft it submitted.
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const submittedEntryRef = useRef<unknown>(null);
  // Set by `onAccepted` only on the branch that actually wiped the editor; read
  // back by `afterAccepted`. Bound this way because the stale-submit guard can
  // leave a mid-flight draft in place: dropping the caret then would yank it
  // out of the sentence the user is still typing.
  const editorScrubbedRef = useRef(false);

  const { submitting, submit } = useComposerSubmit({
    editorRef,
    uploadGate: gate,
    // A top-level comment ends a turn: the caret is dropped rather than kept,
    // so the composer stops reading as "still writing" once the comment is
    // posted above it. Thread replies are the opposite — see ReplyInput.
    afterAccepted: () => (editorScrubbedRef.current ? "blur" : "none"),
    onSubmit: (content) => {
      editorScrubbedRef.current = false;
      // Flush the editor's pending debounce before snapshotting — a late flush
      // of pre-submit typing must not read as an edit made during the request.
      const pending = editorRef.current?.flushPendingUpdate?.();
      if (pending != null) setDraft(draftKey, pending);
      submittedEntryRef.current = useCommentDraftStore.getState().drafts[draftKey];
      // Bind only uploads the BODY still references (MUL-5181): deleting an
      // inline image really unbinds it. Uploads that finished after a close
      // are written back into the body by the settle handler, so surviving
      // files are referenced too — never silently attached.
      const activeIds = pendingAttachments
        .filter((a) => contentReferencesAttachment(content, a))
        .map((a) => a.id);
      const suppressAgentIds = triggerPreview.agents
        .filter((agent) => suppressedAgentIds.has(agent.id))
        .map((agent) => agent.id);
      return onSubmit(
        content,
        activeIds.length > 0 ? activeIds : undefined,
        suppressAgentIds.length > 0 ? suppressAgentIds : undefined,
      );
    },
    onAccepted: () => {
      // Success may only consume the entry it submitted (MUL-5181 P0): edits
      // made while the request was in flight — or by a reopened composer after
      // this one unmounted — survive both in the store and in the editor.
      // Flush the pending debounce first so typing still inside the window is
      // judged correctly (a no-op flush preserves entry identity).
      const lateMd = editorRef.current?.flushPendingUpdate?.();
      if (lateMd != null) setDraft(draftKey, lateMd);
      const store = useCommentDraftStore.getState();
      const live = store.drafts[draftKey];
      const untouched = live === undefined || live === submittedEntryRef.current;
      if (untouched) store.clearDraft(draftKey);
      if (!mountedRef.current || !untouched) return;
      editorRef.current?.clearContent();
      setContent("");
      setIsEmpty(true);
      setSuppressedAgentIds(new Set());
      editorScrubbedRef.current = true;
    },
  });

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
            // setDraft keeps any pending attachments and drops the entry only
            // when text AND attachments are both empty.
            setDraft(draftKey, md);
          }}
          onSubmit={submit}
          onUploadFile={handleUpload}
          onUploadingChange={uploadGate.onUploadingChange}
          debounceMs={100}
          currentIssueId={issueId}
          attachments={pendingAttachments}
          enableSlashCommands
          slashCommandMode="command"
          quickActionMenu={quickActionMenu}
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
          <div className="rich-text-editor text-body">
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
          onClick={submit}
          disabled={isEmpty}
          loading={submitting}
          busy={gate.uploading}
          tooltip={gate.uploading
            ? tEditor(($) => $.upload.in_progress)
            : sendShortcut
              ? `${t(($) => $.comment.send_tooltip)} · ${formatShortcut(sendShortcut)}`
              : t(($) => $.comment.send_tooltip)}
          ariaLabel={gate.uploading
            ? tEditor(($) => $.upload.in_progress)
            : t(($) => $.comment.send_tooltip)}
        />
      </div>
      {isDragOver && <FileDropOverlay />}
    </div>
  );
}

export { CommentInput };
