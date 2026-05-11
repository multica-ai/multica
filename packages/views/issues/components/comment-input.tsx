"use client";

import { useRef, useState, useCallback, useEffect } from "react";
import { Maximize2, Minimize2 } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { enterKey, formatShortcut, modKey } from "@multica/core/platform";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { useT } from "../../i18n";

interface CommentInputProps {
  issueId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
}

function CommentInput({ issueId, onSubmit }: CommentInputProps) {
  const { t } = useT("issues");
  const editorRef = useRef<ContentEditorRef>(null);
  // Read the persisted draft once on mount. ContentEditor only honors
  // `defaultValue` at mount time, so this snapshot drives both the editor's
  // initial content and the submit-button enable state — without this the
  // button would be disabled even though the editor visibly contains text.
  const draftKey = `new:${issueId}` as const;
  const initialDraft = useCommentDraftStore.getState().getDraft(draftKey);
  const [isEmpty, setIsEmpty] = useState(() => !initialDraft?.trim());
  const [submitting, setSubmitting] = useState(false);
  const [isExpanded, setIsExpanded] = useState(false);
  const uploadMapRef = useRef<Map<string, string>>(new Map());
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
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
      clearDraft(draftKey);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      {...dropZoneProps}
      className={cn(
        "relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border",
        isExpanded ? "h-[70vh]" : "max-h-56",
      )}
    >
      <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
        <ContentEditor
          ref={editorRef}
          defaultValue={initialDraft}
          placeholder={t(($) => $.comment.leave_comment_placeholder)}
          onUpdate={(md) => {
            setIsEmpty(!md.trim());
            // Debounced upstream (debounceMs=100). Persist on every tick so a
            // reload or scroll-out-of-viewport restores work to the keystroke.
            if (md.trim().length > 0) setDraft(draftKey, md);
            else clearDraft(draftKey);
          }}
          onSubmit={handleSubmit}
          onUploadFile={handleUpload}
          debounceMs={100}
          currentIssueId={issueId}
        />
      </div>
      <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={() => {
                  setIsExpanded((v) => !v);
                  editorRef.current?.focus();
                }}
                className="rounded-sm p-1.5 text-muted-foreground opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
              >
                {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
              </button>
            }
          />
          <TooltipContent side="top">{isExpanded ? t(($) => $.comment.collapse_tooltip) : t(($) => $.comment.expand_tooltip)}</TooltipContent>
        </Tooltip>
        <FileUploadButton
          size="sm"
          onSelect={(file) => editorRef.current?.uploadFile(file)}
        />
        <SubmitButton
          onClick={handleSubmit}
          disabled={isEmpty}
          loading={submitting}
          tooltip={`${t(($) => $.comment.send_tooltip)} · ${formatShortcut(modKey, enterKey)}`}
        />
      </div>
      {isDragOver && <FileDropOverlay />}
    </div>
  );
}

export { CommentInput };
