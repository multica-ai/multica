"use client";

import { useRef, useState, useCallback } from "react";
import { ArrowUp, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { cn } from "@multica/ui/lib/utils";

interface CommentInputProps {
  /** The ID of the parent entity (e.g. issueId) */
  entityId: string;
  /** The key to use for the ID in the upload options (e.g. 'issueId') */
  entityKey?: string;
  /** Placeholder text for the editor */
  placeholder?: string;
  /** Submit handler */
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
  /** Optional class name for the wrapper */
  className?: string;
}

export function CommentInput({
  entityId,
  entityKey = "issueId",
  placeholder = "Leave a comment...",
  onSubmit,
  className,
}: CommentInputProps) {
  const editorRef = useRef<ContentEditorRef>(null);
  const [isEmpty, setIsEmpty] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [attachmentIds, setAttachmentIds] = useState<string[]>([]);
  const { uploadWithToast } = useFileUpload(api);

  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => editorRef.current?.uploadFiles(files),
  });

  const handleUpload = useCallback(
    async (file: File) => {
      const uploadOptions = { [entityKey]: entityId };
      const result = await uploadWithToast(file, uploadOptions);
      if (result) {
        setAttachmentIds((prev) => [...prev, result.id]);
      }
      return result;
    },
    [entityId, entityKey, uploadWithToast],
  );

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

  return (
    <div
      {...dropZoneProps}
      className={cn(
        "relative flex max-h-56 flex-col rounded-lg bg-card pb-8 ring-1 ring-border transition-all",
        className
      )}
    >
      <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
        <ContentEditor
          ref={editorRef}
          placeholder={placeholder}
          onUpdate={(md) => setIsEmpty(!md.trim())}
          onSubmit={handleSubmit}
          onUploadFile={handleUpload}
          debounceMs={100}
        />
      </div>
      <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
        <FileUploadButton
          size="sm"
          onSelect={(file) => editorRef.current?.uploadFiles([file])}
        />
        <Button
          size="icon-sm"
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
      {/* Refined drag overlay matching issue style */}
      {isDragOver && <FileDropOverlay className="-inset-1 rounded-xl border-brand/40 bg-brand/[0.04]" />}
    </div>
  );
}
