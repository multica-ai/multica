"use client";

import { useRef, useState, useEffect } from "react";
import { ArrowUp, Loader2 } from "lucide-react";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { ActorAvatar } from "../../common/actor-avatar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { cn } from "@multica/ui/lib/utils";

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
  const [isEmpty, setIsEmpty] = useState(true);
  const [isExpanded, setIsExpanded] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [attachmentIds, setAttachmentIds] = useState<string[]>([]);
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

  const handleUpload = async (file: File) => {
    const result = await uploadWithToast(file, { issueId });
    if (result) {
      setAttachmentIds((prev) => [...prev, result.id]);
    }
    return result;
  };

  const handleSubmit = async () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || submitting) return;
    setSubmitting(true);
    try {
      await onSubmit(content, attachmentIds.length > 0 ? attachmentIds : undefined);
      editorRef.current?.clearContent();
      setIsEmpty(true);
      setAttachmentIds([]);
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
