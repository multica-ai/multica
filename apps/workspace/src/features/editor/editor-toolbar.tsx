"use client";

/**
 * EditorToolbar — formatting toolbar for the ContentEditor.
 *
 * Renders a row of toggle buttons for common markdown formatting:
 * headings, bold/italic/strikethrough/code, blockquote, code block,
 * bullet/ordered lists, and link insertion.
 *
 * Uses useEditorState from @tiptap/react so each button re-renders
 * reactively when the cursor moves or formatting changes.
 */

import { useRef, useState, useCallback } from "react";
import { useEditorState } from "@tiptap/react";
import type { Editor } from "@tiptap/core";
import {
  Bold,
  Italic,
  Strikethrough,
  Code,
  Heading1,
  Heading2,
  Heading3,
  List,
  ListOrdered,
  Quote,
  SquareCode,
  Link2,
  Link2Off,
  Check,
} from "lucide-react";
import { Toggle, toggleVariants } from "@/components/ui/toggle";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface EditorToolbarProps {
  editor: Editor;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function EditorToolbar({ editor }: EditorToolbarProps) {
  const [linkUrl, setLinkUrl] = useState("");
  const [linkOpen, setLinkOpen] = useState(false);
  const linkInputRef = useRef<HTMLInputElement>(null);

  // Subscribe to editor state for reactive button highlighting
  const state = useEditorState({
    editor,
    selector: (ctx) => {
      const ed = ctx.editor;
      return {
        isBold: ed.isActive("bold"),
        isItalic: ed.isActive("italic"),
        isStrike: ed.isActive("strike"),
        isCode: ed.isActive("code"),
        isH1: ed.isActive("heading", { level: 1 }),
        isH2: ed.isActive("heading", { level: 2 }),
        isH3: ed.isActive("heading", { level: 3 }),
        isBulletList: ed.isActive("bulletList"),
        isOrderedList: ed.isActive("orderedList"),
        isBlockquote: ed.isActive("blockquote"),
        isCodeBlock: ed.isActive("codeBlock"),
        isLink: ed.isActive("link"),
        currentLinkHref: (ed.getAttributes("link").href as string) || "",
      };
    },
  });

  // Open/close link popover; pre-fill URL when opening
  const handleLinkOpen = useCallback(
    (open: boolean) => {
      setLinkOpen(open);
      if (open) {
        setLinkUrl(state.currentLinkHref);
        // Delay focus so the popover has time to mount
        setTimeout(() => linkInputRef.current?.focus(), 60);
      }
    },
    [state.currentLinkHref],
  );

  // Apply the typed URL as a link on the current selection
  const handleSetLink = useCallback(() => {
    const trimmed = linkUrl.trim();
    if (!trimmed) return;
    const href =
      trimmed.startsWith("http://") || trimmed.startsWith("https://")
        ? trimmed
        : `https://${trimmed}`;
    editor.chain().focus().extendMarkRange("link").setLink({ href }).run();
    setLinkOpen(false);
    setLinkUrl("");
  }, [editor, linkUrl]);

  // Remove the link mark from the current selection
  const handleRemoveLink = useCallback(() => {
    editor.chain().focus().extendMarkRange("link").unsetLink().run();
    setLinkOpen(false);
    setLinkUrl("");
  }, [editor]);

  return (
    <div className="flex flex-wrap items-center gap-0.5 border-b bg-muted/30 px-2 py-1">
      {/* ── Headings ── */}
      <Toggle
        size="sm"
        pressed={state.isH1}
        onPressedChange={() =>
          editor.chain().focus().toggleHeading({ level: 1 }).run()
        }
        aria-label="Heading 1"
      >
        <Heading1 />
      </Toggle>
      <Toggle
        size="sm"
        pressed={state.isH2}
        onPressedChange={() =>
          editor.chain().focus().toggleHeading({ level: 2 }).run()
        }
        aria-label="Heading 2"
      >
        <Heading2 />
      </Toggle>
      <Toggle
        size="sm"
        pressed={state.isH3}
        onPressedChange={() =>
          editor.chain().focus().toggleHeading({ level: 3 }).run()
        }
        aria-label="Heading 3"
      >
        <Heading3 />
      </Toggle>

      {/* divider */}
      <div className="mx-1 h-4 w-px bg-border" />

      {/* ── Inline formatting ── */}
      <Toggle
        size="sm"
        pressed={state.isBold}
        onPressedChange={() => editor.chain().focus().toggleBold().run()}
        aria-label="Bold"
      >
        <Bold />
      </Toggle>
      <Toggle
        size="sm"
        pressed={state.isItalic}
        onPressedChange={() => editor.chain().focus().toggleItalic().run()}
        aria-label="Italic"
      >
        <Italic />
      </Toggle>
      <Toggle
        size="sm"
        pressed={state.isStrike}
        onPressedChange={() => editor.chain().focus().toggleStrike().run()}
        aria-label="Strikethrough"
      >
        <Strikethrough />
      </Toggle>
      <Toggle
        size="sm"
        pressed={state.isCode}
        onPressedChange={() => editor.chain().focus().toggleCode().run()}
        aria-label="Inline code"
      >
        <Code />
      </Toggle>

      {/* divider */}
      <div className="mx-1 h-4 w-px bg-border" />

      {/* ── Block elements ── */}
      <Toggle
        size="sm"
        pressed={state.isBlockquote}
        onPressedChange={() =>
          editor.chain().focus().toggleBlockquote().run()
        }
        aria-label="Blockquote"
      >
        <Quote />
      </Toggle>
      <Toggle
        size="sm"
        pressed={state.isCodeBlock}
        onPressedChange={() => editor.chain().focus().toggleCodeBlock().run()}
        aria-label="Code block"
      >
        <SquareCode />
      </Toggle>

      {/* divider */}
      <div className="mx-1 h-4 w-px bg-border" />

      {/* ── Lists ── */}
      <Toggle
        size="sm"
        pressed={state.isBulletList}
        onPressedChange={() =>
          editor.chain().focus().toggleBulletList().run()
        }
        aria-label="Bullet list"
      >
        <List />
      </Toggle>
      <Toggle
        size="sm"
        pressed={state.isOrderedList}
        onPressedChange={() =>
          editor.chain().focus().toggleOrderedList().run()
        }
        aria-label="Ordered list"
      >
        <ListOrdered />
      </Toggle>

      {/* divider */}
      <div className="mx-1 h-4 w-px bg-border" />

      {/* ── Link ── */}
      <Popover open={linkOpen} onOpenChange={handleLinkOpen}>
        {/* Use PopoverTrigger styled via toggleVariants instead of nesting
            Toggle inside PopoverTrigger to avoid conflicting pressed handlers */}
        <PopoverTrigger
          className={cn(
            toggleVariants({ size: "sm" }),
            (state.isLink || linkOpen) && "bg-muted text-foreground",
          )}
          aria-label="Insert link"
          aria-pressed={state.isLink}
        >
          <Link2 className="size-4" />
        </PopoverTrigger>

        <PopoverContent side="bottom" align="start" className="flex w-64 flex-col gap-2">
          <input
            ref={linkInputRef}
            type="url"
            value={linkUrl}
            onChange={(e) => setLinkUrl(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                handleSetLink();
              }
              if (e.key === "Escape") setLinkOpen(false);
            }}
            placeholder="https://..."
            className="w-full rounded-md border border-input bg-background px-2.5 py-1.5 text-sm outline-none focus:ring-1 focus:ring-ring"
          />
          <div className="flex gap-2">
            <button
              onClick={handleSetLink}
              className="flex items-center gap-1 rounded-md bg-primary px-2.5 py-1 text-xs font-medium text-primary-foreground hover:bg-primary/90"
            >
              <Check className="size-3" />
              Apply
            </button>
            {state.isLink && (
              <button
                onClick={handleRemoveLink}
                className="flex items-center gap-1 rounded-md border border-destructive px-2.5 py-1 text-xs font-medium text-destructive hover:bg-destructive/10"
              >
                <Link2Off className="size-3" />
                Remove
              </button>
            )}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}
