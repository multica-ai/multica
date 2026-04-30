"use client";

import { useRef, useState } from "react";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import { Button } from "@multica/ui/components/ui/button";
import { useChannelsStore, useSendChannelMessage } from "@multica/core/channels";
import { Send } from "lucide-react";

interface ChannelComposerProps {
  channelId: string;
  channelName: string;
  disabled?: boolean;
}

/**
 * ChannelComposer is the bottom-of-screen input. It reuses the shared
 * ContentEditor so we get markdown, mentions (@member, @agent), styling,
 * and file-drop affordances for free.
 *
 * Drafts persist per-channel via the channels store — switching channels
 * preserves whatever you were typing.
 *
 * Submit is wired to Enter (Shift+Enter for newline) via the editor's
 * submitOnEnter prop.
 */
export function ChannelComposer({ channelId, channelName, disabled }: ChannelComposerProps) {
  const editorRef = useRef<ContentEditorRef>(null);
  const inputDraft = useChannelsStore((s) => s.inputDrafts[channelId] ?? "");
  const setInputDraft = useChannelsStore((s) => s.setInputDraft);
  const clearInputDraft = useChannelsStore((s) => s.clearInputDraft);
  const sendMut = useSendChannelMessage(channelId);
  const [isEmpty, setIsEmpty] = useState(!inputDraft.trim());

  const handleSend = () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || disabled || sendMut.isPending) return;
    sendMut.mutate({ content });
    editorRef.current?.clearContent();
    clearInputDraft(channelId);
    setIsEmpty(true);
  };

  // Upstream removed the `editable` prop from ContentEditor (Tiptap mounts
  // editable-state at construction; toggling later was silently a no-op).
  // The migration guidance is to wrap in pointer-events-none / aria-disabled
  // for "currently disabled" — applied here so the composer reads as
  // disabled without monkey-patching the editor.
  return (
    <div className="border-t border-border bg-background px-4 py-3">
      <div className="flex items-end gap-2">
        <div
          className={[
            "flex-1 rounded-md border border-input bg-background px-3 py-2 focus-within:ring-2 focus-within:ring-ring",
            disabled ? "pointer-events-none opacity-60" : "",
          ].join(" ")}
          aria-disabled={disabled || undefined}
        >
          <ContentEditor
            ref={editorRef}
            defaultValue={inputDraft}
            onUpdate={(md) => {
              setInputDraft(channelId, md);
              setIsEmpty(!md.trim());
            }}
            placeholder={`Message #${channelName}`}
            submitOnEnter
            onSubmit={handleSend}
          />
        </div>
        <Button
          size="sm"
          onClick={handleSend}
          disabled={isEmpty || disabled || sendMut.isPending}
          aria-label="Send message"
        >
          <Send className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}
