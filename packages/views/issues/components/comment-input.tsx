"use client";

import { useRef, useState, useCallback } from "react";
import { ArrowUp, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";

interface CommentInputProps {
  issueId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
}

function CommentInput({ issueId, onSubmit }: CommentInputProps) {
  const editorRef = useRef<ContentEditorRef>(null);
  const [isEmpty, setIsEmpty] = useState(true);
  const [submitting, setSubmitting] = useState(false);
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
      className="relative flex max-h-56 flex-col rounded-lg bg-card pb-8 ring-1 ring-border"
    >
      <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
        <ContentEditor
          ref={editorRef}
          placeholder="Leave a comment..."
          onUpdate={(md) => setIsEmpty(!md.trim())}
          onSubmit={handleSubmit}
          onUploadFile={handleUpload}
          debounceMs={100}
          currentIssueId={issueId}
        />
      </div>
      <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
        <FileUploadButton
          size="sm"
          onSelect={(file) => editorRef.current?.uploadFile(file)}
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
      {isDragOver && <FileDropOverlay />}
    </div>
  );
}

export { CommentInput };
