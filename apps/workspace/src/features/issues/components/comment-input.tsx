"use client";

import { useRef, useState } from "react";
import { ArrowUp, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ContentEditor, type ContentEditorRef } from "@/features/editor";
import { FileUploadButton } from "@/components/common/file-upload-button";
import { useFileUpload } from "@/shared/hooks/use-file-upload";

interface CommentInputProps {
  issueId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
}

function CommentInput({ issueId, onSubmit }: CommentInputProps) {
  const editorRef = useRef<ContentEditorRef>(null);
  const [isEmpty, setIsEmpty] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const { uploadWithToast } = useFileUpload();

  const handleUpload = async (file: File) => {
    return await uploadWithToast(file, { issueId });
  };

  const handleSubmit = async () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || submitting) return;
    setSubmitting(true);
    try {
      await onSubmit(content);
      editorRef.current?.clearContent();
      setIsEmpty(true);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="relative flex max-h-56 flex-col rounded-lg bg-card pb-8 ring-1 ring-border">
      <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
        <ContentEditor
          ref={editorRef}
          placeholder="Leave a comment..."
          onUpdate={(md) => setIsEmpty(!md.trim())}
          onSubmit={handleSubmit}
          onUploadFile={handleUpload}
          debounceMs={100}
        />
      </div>
      <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
        <FileUploadButton
          size="sm"
          onSelect={(file) => editorRef.current?.uploadFile(file)}
        />
        <Button
          size="icon-xs"
          disabled={isEmpty || submitting}
          onClick={handleSubmit}
          aria-label="Post comment"
        >
          {submitting ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <ArrowUp className="h-3.5 w-3.5" />
          )}
        </Button>
      </div>
    </div>
  );
}

export { CommentInput };
