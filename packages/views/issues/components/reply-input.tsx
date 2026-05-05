"use client";

// CEREBRO-PATCH(reply-input-cerebro): cerebro modification of upstream file

import { useRef, useState, useEffect, useCallback } from "react";
import { ArrowUp, Loader2 } from "lucide-react";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { ActorAvatar } from "../../common/actor-avatar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { cn } from "@multica/ui/lib/utils";
import { useSubmitOnEnter } from "../../preferences/use-submit-on-enter";

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
}

// ---------------------------------------------------------------------------
// ReplyInput
// ---------------------------------------------------------------------------

function ReplyInput({
  issueId,
  placeholder = "Leave a reply...",
  avatarType,
  avatarId,
  onSubmit,
  size = "default",
}: ReplyInputProps) {
  const editorRef = useRef<ContentEditorRef>(null);
  const measureRef = useRef<HTMLDivElement>(null);
  const submitOnEnter = useSubmitOnEnter();
  const [isEmpty, setIsEmpty] = useState(true);
  const [isExpanded, setIsExpanded] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const uploadMapRef = useRef<Map<string, string>>(new Map());
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  useEffect(() => {
    const el = measureRef.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) setIsExpanded(entry.contentRect.height > 32);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

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
          size === "sm" ? "max-h-40" : "max-h-56",
          isExpanded && "pb-7",
        )}
      >
        <div className="flex-1 min-h-0 overflow-y-auto pr-14">
          <div ref={measureRef}>
            <ContentEditor
              ref={editorRef}
              placeholder={placeholder}
              onUpdate={(md) => setIsEmpty(!md.trim())}
              onSubmit={handleSubmit}
              onUploadFile={handleUpload}
              debounceMs={100}
              currentIssueId={issueId}
              submitOnEnter={submitOnEnter}
            />
          </div>
        </div>
        <div className="absolute bottom-0 right-0 flex items-center gap-1 text-muted-foreground transition-colors group-focus-within/editor:text-foreground">
          <FileUploadButton
            size="sm"
            onSelect={(file) => editorRef.current?.uploadFile(file)}
          />
          <button
            type="button"
            disabled={isEmpty || submitting}
            onClick={handleSubmit}
            aria-label="Send"
            className="inline-flex h-9 w-9 sm:h-7 sm:w-7 items-center justify-center rounded-full bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:bg-muted disabled:text-muted-foreground disabled:pointer-events-none"
          >
            {submitting ? (
              <Loader2 className="h-4 w-4 sm:h-3.5 sm:w-3.5 animate-spin" />
            ) : (
              <ArrowUp className="h-4 w-4 sm:h-3.5 sm:w-3.5" />
            )}
          </button>
        </div>
        {isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}

export { ReplyInput, type ReplyInputProps };
