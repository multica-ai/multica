"use client";

import { useRef, useState, useCallback, useEffect } from "react";
import { ArrowUp, Loader2, Maximize2, Minimize2 } from "lucide-react";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
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
  const [isExpanded, setIsExpanded] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const uploadMapRef = useRef<Map<string, string>>(new Map());
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
      uploadMapRef.current.set(result.link, result.id);
    }
    return result;
  }, [uploadWithToast, issueId]);

  const handleSubmit = async () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || submitting) return;
    // Only send attachment IDs for uploads still present in the content.
    const activeIds: string[] = [];
    for (const [url, id] of uploadMapRef.current) {
      if (content.includes(url)) activeIds.push(id);
    }
    setSubmitting(true);
    try {
      await onSubmit(content, activeIds.length > 0 ? activeIds : undefined);
      editorRef.current?.clearContent();
      setIsEmpty(true);
      uploadMapRef.current.clear();
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
          isExpanded
            ? "h-[60vh]"
            : size === "sm" ? "max-h-40" : "max-h-56",
          (!isEmpty || isExpanded) && "pb-7",
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
          />
        </div>
        <div className="absolute bottom-0 right-0 flex items-center gap-1">
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={() => {
                    setIsExpanded((v) => !v);
                    editorRef.current?.focus();
                  }}
                  className="inline-flex h-6 w-6 items-center justify-center rounded-sm text-muted-foreground opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                >
                  {isExpanded ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
                </button>
              }
            />
            <TooltipContent side="top">{isExpanded ? t(($) => $.reply.collapse_tooltip) : t(($) => $.reply.expand_tooltip)}</TooltipContent>
          </Tooltip>
          <FileUploadButton
            size="sm"
            onSelect={(file) => editorRef.current?.uploadFile(file)}
          />
          <button
            type="button"
            disabled={isEmpty || submitting}
            onClick={handleSubmit}
            className="inline-flex h-6 w-6 items-center justify-center rounded-full text-muted-foreground hover:bg-accent hover:text-foreground transition-colors disabled:opacity-50 disabled:pointer-events-none"
          >
            {submitting ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <ArrowUp className="h-3.5 w-3.5" />
            )}
          </button>
        </div>
        {isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}

export { ReplyInput, type ReplyInputProps };
