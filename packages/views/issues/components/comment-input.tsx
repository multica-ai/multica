"use client";

import { forwardRef, useRef, useState, useCallback, useEffect, useImperativeHandle } from "react";
import { ClipboardList } from "lucide-react";
import { toast } from "sonner";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { ContentEditor, type ContentEditorRef, type SelectionQuoteActions, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import type { Attachment } from "@multica/core/types";
import { enterKey, formatShortcut, modKey } from "@multica/core/platform";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { useT } from "../../i18n";

interface CommentInputProps {
  issueId: string;
  onSubmit: (content: string, attachmentIds?: string[], type?: string) => Promise<void>;
  selectionQuoteActions?: SelectionQuoteActions;
}

interface CommentInputRef {
  appendMarkdown: (markdown: string) => void;
  focus: () => void;
}

const CommentInput = forwardRef<CommentInputRef, CommentInputProps>(function CommentInput(
  { issueId, onSubmit, selectionQuoteActions },
  ref,
) {
  const { t } = useT("issues");
  const editorRef = useRef<ContentEditorRef>(null);
  // Read the persisted draft once on mount. ContentEditor only honors
  // `defaultValue` at mount time, so this snapshot drives both the editor's
  // initial content and the submit-button enable state.
  const draftKey = `new:${issueId}` as const;
  const initialDraft = useCommentDraftStore.getState().getDraft(draftKey);
  const [isEmpty, setIsEmpty] = useState(() => !initialDraft?.trim());
  const [submitting, setSubmitting] = useState(false);
  // Attachments uploaded in this composer session. Drives both:
  //  - submit-time `attachment_ids` payload (filtered to URLs still in markdown)
  //  - the editor's AttachmentDownloadProvider, so file-card Eye buttons can
  //    resolve text/code/markdown previews that require the attachment id.
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => editorRef.current?.uploadFiles(files),
  });

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
    const onVis = () => {
      if (document.visibilityState === "hidden") flush();
    };
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

  const handleSubmit = async (type = "comment") => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || submitting) return;
    // Only send attachment IDs for uploads still present in the content.
    const activeIds = pendingAttachments
      .filter((a) => content.includes(a.url))
      .map((a) => a.id);
    setSubmitting(true);
    try {
      await onSubmit(content, activeIds.length > 0 ? activeIds : undefined, type);
      editorRef.current?.clearContent();
      setIsEmpty(true);
      setPendingAttachments([]);
      clearDraft(draftKey);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.comment.send_failed));
    } finally {
      setSubmitting(false);
    }
  };

  const handleEditorUpdate = useCallback((md: string) => {
    setIsEmpty(!md.trim());
    if (md.trim().length > 0) setDraft(draftKey, md);
    else clearDraft(draftKey);
  }, [clearDraft, draftKey, setDraft]);

  useImperativeHandle(ref, () => ({
    appendMarkdown: (markdown: string) => {
      editorRef.current?.appendMarkdown(markdown);
      const next = editorRef.current?.getMarkdown() ?? "";
      setIsEmpty(!next.trim());
      if (next.trim().length > 0) setDraft(draftKey, next);
      else clearDraft(draftKey);
      editorRef.current?.focus();
    },
    focus: () => {
      editorRef.current?.focus();
    },
  }), [clearDraft, draftKey, setDraft]);

  return (
    <div
      {...dropZoneProps}
      className="relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border"
    >
      <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
        <ContentEditor
          ref={editorRef}
          defaultValue={initialDraft}
          placeholder={t(($) => $.comment.leave_comment_placeholder)}
          onUpdate={handleEditorUpdate}
          onSubmit={handleSubmit}
          onUploadFile={handleUpload}
          debounceMs={100}
          currentIssueId={issueId}
          attachments={pendingAttachments}
          selectionQuoteActions={selectionQuoteActions}
        />
      </div>
      <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
        <FileUploadButton
          size="sm"
          multiple
          onSelect={(file) => editorRef.current?.uploadFile(file)}
          onSelectMany={(files) => editorRef.current?.uploadFiles(files)}
        />
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                disabled={isEmpty || submitting}
                onClick={() => void handleSubmit("plan_request")}
                className="rounded-sm p-1.5 text-muted-foreground opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer disabled:cursor-not-allowed disabled:opacity-40"
              >
                <ClipboardList className="size-4" />
              </button>
            }
          />
          <TooltipContent side="top">Plan only</TooltipContent>
        </Tooltip>
        <SubmitButton
          onClick={() => void handleSubmit()}
          disabled={isEmpty}
          loading={submitting}
          tooltip={`${t(($) => $.comment.send_tooltip)} · ${formatShortcut(modKey, enterKey)}`}
        />
      </div>
      {isDragOver && <FileDropOverlay />}
    </div>
  );
});

export { CommentInput, type CommentInputProps, type CommentInputRef };
