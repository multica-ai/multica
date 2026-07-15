"use client";

import { useRef, useState, useCallback, useEffect } from "react";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay, useLazyEditor } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { ActorAvatar } from "../../common/actor-avatar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import type { Attachment } from "@multica/core/types";
import { contentReferencesAttachment } from "@multica/core/types";
import { formatShortcut, useShortcut } from "@multica/core/shortcuts";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import { cn } from "@multica/ui/lib/utils";
import type { AvatarSize } from "@multica/ui/lib/avatar-size";
import { useT } from "../../i18n";
import { CommentTriggerChips } from "./comment-trigger-chips";
import { useCommentTriggerPreview } from "../hooks/use-comment-trigger-preview";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface ReplyInputProps {
  issueId: string;
  parentId: string;
  placeholder?: string;
  avatarType: string;
  avatarId: string;
  /** Resolves true on success, false on failure — the reply box keeps its text
   *  (locked + spinning) until then, clearing only on success. */
  onSubmit: (content: string, attachmentIds?: string[], suppressAgentIds?: string[]) => Promise<boolean>;
  size?: "sm" | "default";
  /** When set, hydrates/persists the in-progress reply via the draft store.
   *  Required for replies inside virtualized timeline threads, where the
   *  enclosing CommentCard may unmount on scroll-out. */
  draftKey?: CommentDraftKey;
}

// ---------------------------------------------------------------------------
// ReplyInput
// ---------------------------------------------------------------------------

function ReplyInput({
  issueId,
  parentId,
  placeholder,
  avatarType,
  avatarId,
  onSubmit,
  size = "default",
  draftKey,
}: ReplyInputProps) {
  const { t } = useT("issues");
  const sendShortcut = useShortcut("send");
  const placeholderText = placeholder ?? t(($) => $.reply.placeholder);
  const editorRef = useRef<ContentEditorRef>(null);
  // If a draft key is provided, hydrate from store on mount (defaultValue is
  // the only injection point on ContentEditorRef) and flush on every onUpdate.
  const initialDraft = draftKey
    ? useCommentDraftStore.getState().getDraft(draftKey)
    : undefined;
  const [content, setContent] = useState(initialDraft ?? "");
  const setDraft = useCommentDraftStore((s) => s.setDraft);
  const clearDraft = useCommentDraftStore((s) => s.clearDraft);
  const [isEmpty, setIsEmpty] = useState(!initialDraft?.trim());
  const [submitting, setSubmitting] = useState(false);
  const [suppressedAgentIds, setSuppressedAgentIds] = useState<Set<string>>(() => new Set());
  const triggerPreview = useCommentTriggerPreview({ issueId, parentId, content });
  // Attachments uploaded in this composer session — see CommentInput for the
  // rationale (drives both submit-time attachment_ids and editor previews).
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const { uploadWithToast } = useFileUpload(api);
  // Readonly-first: static shell until intent; an unsent draft mounts the
  // real editor immediately (see CommentInput). This is also what keeps the
  // reply box working across Virtuoso scroll-out — a typed draft rehydrates
  // into a live editor when the card remounts, an untouched box folds back
  // to the shell.
  const lazy = useLazyEditor({
    initialActive: !!initialDraft?.trim(),
    editorRef,
  });
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: lazy.uploadOrQueue,
  });

  // Flush on tab close / mobile background — same rationale as CommentInput.
  useEffect(() => {
    if (!draftKey) return;
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
  }, [issueId, parentId]);

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
    // Track every attachment whose stable download URL OR legacy
    // storage URL is referenced in the markdown body. Both shapes
    // can appear in the same comment during the MUL-3130 rollout.
    const activeIds = pendingAttachments
      .filter((a) => contentReferencesAttachment(content, a))
      .map((a) => a.id);
    const suppressAgentIds = triggerPreview.agents
      .filter((agent) => suppressedAgentIds.has(agent.id))
      .map((agent) => agent.id);
    // Pessimistic submit (see CommentInput): keep the text, lock + spin, clear
    // only once the server accepts it.
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
        if (draftKey) clearDraft(draftKey);
      }
    } finally {
      setSubmitting(false);
    }
  };

  const avatarSize: AvatarSize = size === "sm" ? "sm" : "md";

  return (
    <div className="group/editor flex items-start gap-2.5">
      <ActorAvatar
        actorType={avatarType}
        actorId={avatarId}
        size={avatarSize}
        className="mt-0.5 shrink-0"
      />
      <div
        {...dropZoneProps}
        className={cn(
          "relative min-w-0 flex-1 flex flex-col",
          !isEmpty && "pb-9",
        )}
      >
        {/* Lock the editor while the reply is in flight — see CommentInput. */}
        {lazy.active && (
        <div
          className={cn(
            "flex-1 min-h-0 overflow-y-auto",
            submitting && "pointer-events-none opacity-60",
            !lazy.ready && "hidden",
          )}
          aria-busy={submitting || undefined}
        >
          <ContentEditor
            ref={editorRef}
            defaultValue={initialDraft}
            onReady={lazy.onReady}
            placeholder={placeholderText}
            onUpdate={(md) => {
              setContent(md);
              setIsEmpty(!md.trim());
              if (draftKey) {
                if (md.trim().length > 0) setDraft(draftKey, md);
                else clearDraft(draftKey);
              }
            }}
            onSubmit={handleSubmit}
            onUploadFile={handleUpload}
            debounceMs={100}
            currentIssueId={issueId}
            attachments={pendingAttachments}
            enableSlashCommands
            slashCommandMode="command"
          />
        </div>
        )}
        {/* Static shell — clones the empty single-line reply box (see
            CommentInput for the pattern). */}
        {!lazy.ready && (
          <div
            data-testid="reply-composer-shell"
            role="button"
            tabIndex={0}
            aria-label={placeholderText}
            className="flex-1 min-h-0 cursor-text rich-text-editor text-sm"
            onClick={() => lazy.activate()}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                lazy.activate();
              }
            }}
          >
            {/* <p> under rich-text-editor: same type metrics as the real
                editor's empty paragraph — no height jump on swap. */}
            <p className="text-muted-foreground">{placeholderText}</p>
          </div>
        )}
        <div className="absolute bottom-0 left-0 right-24 min-w-0">
          <CommentTriggerChips
            agents={triggerPreview.agents}
            blocked={triggerPreview.blocked}
            draftContent={content}
            suppressedAgentIds={suppressedAgentIds}
            onToggle={toggleSuppressedAgent}
          />
        </div>
        <div className="absolute bottom-0 right-0 flex items-center gap-1">
          <FileUploadButton
            size="sm"
            multiple
            onSelect={(file) => lazy.uploadOrQueue([file])}
          />
          <SubmitButton
            onClick={handleSubmit}
            disabled={isEmpty}
            loading={submitting}
            tooltip={sendShortcut
              ? `${t(($) => $.comment.send_tooltip)} · ${formatShortcut(sendShortcut)}`
              : t(($) => $.comment.send_tooltip)}
          />
        </div>
        {isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}

export { ReplyInput, type ReplyInputProps };
