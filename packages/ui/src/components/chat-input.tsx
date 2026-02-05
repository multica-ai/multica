"use client";
import { useRef, useEffect, useImperativeHandle, forwardRef } from "react";
import { useEditor, EditorContent } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Placeholder from "@tiptap/extension-placeholder";
import { Button } from "@multica/ui/components/ui/button";
import { ArrowUpIcon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { cn } from "@multica/ui/lib/utils";
import "./chat-input.css";

export interface ChatInputRef {
  getText: () => string;
  setText: (text: string) => void;
  focus: () => void;
  clear: () => void;
}

interface ChatInputProps {
  onSubmit?: (value: string) => void;
  disabled?: boolean;
  placeholder?: string;
}

export const ChatInput = forwardRef<ChatInputRef, ChatInputProps>(
  function ChatInput({ onSubmit, disabled, placeholder = "Type a message..." }, ref) {
    // Use ref to avoid stale closure in Tiptap keydown handler
    const onSubmitRef = useRef(onSubmit);
    onSubmitRef.current = onSubmit;

    const editor = useEditor({
      extensions: [
        StarterKit.configure({
          // Disable all rich-text features — plain text only
          heading: false,
          bold: false,
          italic: false,
          strike: false,
          code: false,
          codeBlock: false,
          blockquote: false,
          bulletList: false,
          orderedList: false,
          listItem: false,
          horizontalRule: false,
        }),
        Placeholder.configure({ placeholder }),
      ],
      immediatelyRender: false,
      editorProps: {
        attributes: {
          class:
            "w-full resize-none bg-transparent px-1 py-1 text-base text-foreground outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed",
        },
        handleKeyDown(_view, event) {
          // Guard for IME composition (Chinese/Japanese input)
          if (event.isComposing) return false;

          if (event.key === "Enter" && !event.shiftKey) {
            event.preventDefault();
            const text = _view.state.doc.textContent;
            if (!text.trim()) return true;
            onSubmitRef.current?.(text);
            // Clear editor after submit
            _view.dispatch(
              _view.state.tr
                .delete(0, _view.state.doc.content.size)
                .setMeta("addToHistory", false),
            );
            return true;
          }

          return false;
        },
      },
    });

    // Sync disabled state
    useEffect(() => {
      if (!editor) return;
      editor.setEditable(!disabled);
    }, [editor, disabled]);

    // Sync placeholder
    useEffect(() => {
      if (!editor) return;
      editor.extensionManager.extensions.find(
        (ext) => ext.name === "placeholder",
      )!.options.placeholder = placeholder;
      // Force view update so placeholder re-renders
      editor.view.dispatch(editor.state.tr);
    }, [editor, placeholder]);

    // Expose imperative API
    useImperativeHandle(ref, () => ({
      getText: () => editor?.state.doc.textContent ?? "",
      setText: (text: string) => {
        editor?.commands.setContent(text ? `<p>${text}</p>` : "");
      },
      focus: () => editor?.commands.focus(),
      clear: () => editor?.commands.clearContent(),
    }), [editor]);

    const handleSubmit = () => {
      if (!editor) return;
      const text = editor.state.doc.textContent;
      if (!text.trim()) return;
      onSubmit?.(text);
      editor.commands.clearContent();
    };

    return (
      <div className={cn(
        "chat-input-editor bg-card rounded-xl p-3 border border-border transition-colors",
        disabled && "is-disabled cursor-not-allowed opacity-60",
      )}>
        <EditorContent editor={editor} />
        <div className="flex items-center justify-end pt-2">
          <Button size="icon-lg" onClick={handleSubmit} disabled={disabled}>
            <HugeiconsIcon className="size-4.5" strokeWidth={2.5} icon={ArrowUpIcon} />
          </Button>
        </div>
      </div>
    );
  },
);
