"use client";

import { useRef, useState, useCallback, useEffect } from "react";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { ActorAvatar } from "../../common/actor-avatar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import type { Attachment } from "@multica/core/types";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface ReplyInputProps {
  issueId: string;
  placeholder?: string;
  avatarType: string;
  avatarId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
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
  placeholder,
  avatarType,
  avatarId,
  onSubmit,
  size = "default",
  draftKey,
}: ReplyInputProps) {
  const { t } = useT("issues");
  const placeholderText = placeholder ?? t(($) => $.reply.placeholder);
  const editorRef = useRef<ContentEditorRef>(null);
  // If a draft key is provided, hydrate from store on mount (defaultValue is
  // the only injection point on ContentEditorRef) and flush on every onUpdate.
  const initialDraft = draftKey
    ? useCommentDraftStore.getState().getDraft(draftKey)
    : undefined;
  const setDraft = useCommentDraftStore((s) => s.setDraft);
  const clearDraft = useCommentDraftStore((s) => s.clearDraft);
  const [isEmpty, setIsEmpty] = useState(!initialDraft?.trim());
  const [submitting, setSubmitting] = useState(false);
  // Attachments uploaded in this composer session — see CommentInput for the
  // rationale (drives both submit-time attachment_ids and editor previews).
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
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

  const handleSubmit = async () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || submitting) return;
    // Only send attachment IDs for uploads still present in the content.
    const activeIds = pendingAttachments
      .filter((a) => content.includes(a.url))
      .map((a) => a.id);
    setSubmitting(true);
    try {
      await onSubmit(content, activeIds.length > 0 ? activeIds : undefined);
      editorRef.current?.clearContent();
      setIsEmpty(true);
      setPendingAttachments([]);
      if (draftKey) clearDraft(draftKey);
    } finally {
      setSubmitting(false);
    }
  };

  const avatarSize = size === "sm" ? 22 : 28;

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
          !isEmpty && "pb-7",
        )}
      >
        <div className="flex-1 min-h-0 overflow-y-auto">
          <ContentEditor
            ref={editorRef}
            defaultValue={initialDraft}
            placeholder={placeholderText}
            onUpdate={(md) => {
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
        <div className="absolute bottom-0 right-0 flex items-center gap-1">
          <FileUploadButton
            size="sm"
            multiple
            onSelect={(file) => editorRef.current?.uploadFile(file)}
          />
          <SubmitButton
            onClick={handleSubmit}
            disabled={isEmpty}
            loading={submitting}
          />
        </div>
        {isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}

export { ReplyInput, type ReplyInputProps };
