"use client";

import { useRef, useState } from "react";
import { ArrowUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import { RichTextEditor, type RichTextEditorRef } from "@/components/common/rich-text-editor";
import { ActorAvatar } from "@/components/common/actor-avatar";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface ReplyInputProps {
  placeholder?: string;
  avatarType: string;
  avatarId: string;
  onSubmit: (content: string) => Promise<void>;
  size?: "sm" | "default";
}

// ---------------------------------------------------------------------------
// ReplyInput
// ---------------------------------------------------------------------------

function ReplyInput({
  placeholder = "Leave a reply...",
  avatarType,
  avatarId,
  onSubmit,
  size = "default",
}: ReplyInputProps) {
  const editorRef = useRef<RichTextEditorRef>(null);
  const [isEmpty, setIsEmpty] = useState(true);
  const [submitting, setSubmitting] = useState(false);

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

  const avatarSize = size === "sm" ? 22 : 28;

  return (
    <div className="flex items-start gap-2.5">
      <ActorAvatar
        actorType={avatarType}
        actorId={avatarId}
        size={avatarSize}
        className="mt-0.5 shrink-0"
      />
      <div className="min-w-0 flex-1">
        <div
          className={`overflow-y-auto text-sm ${
            size === "sm" ? "max-h-32" : "max-h-48"
          }`}
        >
          <RichTextEditor
            ref={editorRef}
            placeholder={placeholder}
            onUpdate={(md) => setIsEmpty(!md.trim())}
            onSubmit={handleSubmit}
            debounceMs={100}
          />
        </div>
        <div
          className={`grid transition-all duration-150 ${
            isEmpty ? "grid-rows-[0fr] opacity-0" : "grid-rows-[1fr] opacity-100"
          }`}
        >
          <div className="overflow-hidden">
            <div className="flex items-center justify-end pt-1">
              <Button
                size="icon-xs"
                disabled={isEmpty || submitting}
                onClick={handleSubmit}
                tabIndex={isEmpty ? -1 : 0}
              >
                <ArrowUp className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export { ReplyInput, type ReplyInputProps };
