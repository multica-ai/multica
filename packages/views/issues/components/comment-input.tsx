"use client";

// CEREBRO-PATCH(comment-input-cerebro): cerebro modification of upstream file

import { useEffect, useRef, useState, useCallback } from "react";
import { ArrowUp, Loader2, Maximize2, Minimize2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { useSubmitOnEnter } from "@multica/cerebro-preferences/views";
import { useT } from "../../i18n";

interface CommentInputProps {
  issueId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
  // CEREBRO-PATCH(input-autofocus): JEH-756 — opt-in autofocus for surfaces
  // where the user expects to start typing on entry (channels, DMs). Issue
  // pages stay opt-out: opening an issue is read-first.
  autoFocus?: boolean;
}

function CommentInput({ issueId, onSubmit, autoFocus = false }: CommentInputProps) {
  const { t } = useT("issues");
  const editorRef = useRef<ContentEditorRef>(null);
  const submitOnEnter = useSubmitOnEnter();
  const [isEmpty, setIsEmpty] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [isExpanded, setIsExpanded] = useState(false);
  const uploadMapRef = useRef<Map<string, string>>(new Map());
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  const handleUpload = useCallback(async (file: File) => {
    const result = await uploadWithToast(file, { issueId });
    if (result) {
      uploadMapRef.current.set(result.link, result.id);
    }
    return result;
  }, [uploadWithToast, issueId]);

  // CEREBRO-PATCH(input-autofocus): JEH-756 — focus the editor on mount and
  // when switching between channels/DMs so the user can start typing right
  // away. Skipped if an open dialog is trapping focus.
  useEffect(() => {
    if (!autoFocus) return;
    const id = requestAnimationFrame(() => {
      const active =
        typeof document !== "undefined" ? document.activeElement : null;
      if (
        active &&
        active !== document.body &&
        active.closest('[role="dialog"], [role="alertdialog"]')
      ) {
        return;
      }
      editorRef.current?.focus();
    });
    return () => cancelAnimationFrame(id);
  }, [autoFocus, issueId]);

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
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      {...dropZoneProps}
      data-testid="comment-input"
      className={cn(
        "relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border",
        isExpanded ? "h-[70vh]" : "max-h-56",
      )}
    >
      <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
        <ContentEditor
          ref={editorRef}
          placeholder={t(($) => $.comment.leave_comment_placeholder)}
          onUpdate={(md) => setIsEmpty(!md.trim())}
          onSubmit={handleSubmit}
          onUploadFile={handleUpload}
          debounceMs={100}
          currentIssueId={issueId}
          submitOnEnter={submitOnEnter}
        />
      </div>
      {/* CEREBRO-PATCH(file-upload-button-api): paperclip on the left, well
          away from the send button on touch screens, with the new
          onAttach/onEmbed FileUploadButton API. */}
      <div className="absolute bottom-1 left-1.5 flex items-center">
        <FileUploadButton
          size="sm"
          onAttach={(files) => files.forEach((f) => editorRef.current?.uploadFile(f))}
          onEmbed={(files) => files.forEach((f) => editorRef.current?.uploadFile(f, { embedImage: true }))}
        />
      </div>
      <div className="absolute bottom-1 right-1.5 flex items-center gap-2 sm:gap-1">
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
        <Button
          size="icon-sm"
          aria-label="Submit comment"
          disabled={isEmpty || submitting}
          onClick={handleSubmit}
        >
          {submitting ? (
            <Loader2 className="animate-spin" />
          ) : (
            <ArrowUp />
          )}
        </Button>
      </div>
      {isDragOver && <FileDropOverlay />}
    </div>
  );
}

export { CommentInput };
