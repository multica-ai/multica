"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { BubbleMenu } from "@tiptap/react/menus";
import type { Editor } from "@tiptap/core";
import { NodeSelection } from "@tiptap/pm/state";
import type { EditorState } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { Toggle } from "@multica/ui/components/ui/toggle";
import { Separator } from "@multica/ui/components/ui/separator";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
} from "@multica/ui/components/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import {
  Bold,
  Italic,
  Strikethrough,
  Code,
  Link2,
  List,
  ListOrdered,
  Quote,
  ChevronDown,
  Check,
  X,
  Unlink,
  Type,
  Heading1,
  Heading2,
  Heading3,
} from "lucide-react";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Force re-render when editor state changes so isActive() returns fresh values */
function useEditorTransactionUpdate(editor: Editor) {
  const [, setState] = useState(0);
  useEffect(() => {
    const handler = () => setState((n) => n + 1);
    editor.on("transaction", handler);
    return () => {
      editor.off("transaction", handler);
    };
  }, [editor]);
}

function shouldShowBubbleMenu({
  editor,
  state,
  from,
  to,
}: {
  editor: Editor;
  view: EditorView;
  state: EditorState;
  oldState?: EditorState;
  from: number;
  to: number;
}) {
  if (!editor.isEditable) return false;
  if (state.selection.empty) return false;
  if (!state.doc.textBetween(from, to).length) return false;
  if (state.selection instanceof NodeSelection) return false;
  const $from = state.doc.resolve(from);
  if ($from.parent.type.name === "codeBlock") return false;
  return true;
}

/** Detect macOS for keyboard shortcut labels */
const isMac =
  typeof navigator !== "undefined" && /Mac/.test(navigator.platform);
const mod = isMac ? "\u2318" : "Ctrl";

/** Hoisted to avoid new reference on every render (triggers plugin updateOptions) */
const BUBBLE_MENU_OPTIONS = {
  strategy: "fixed" as const,
  placement: "top" as const,
  offset: 8,
  flip: true,
  shift: { padding: 8 },
};

// ---------------------------------------------------------------------------
// Mark Toggle Button
// ---------------------------------------------------------------------------

type InlineMark = "bold" | "italic" | "strike" | "code";

const toggleMarkActions: Record<InlineMark, (editor: Editor) => void> = {
  bold: (e) => e.chain().focus().toggleBold().run(),
  italic: (e) => e.chain().focus().toggleItalic().run(),
  strike: (e) => e.chain().focus().toggleStrike().run(),
  code: (e) => e.chain().focus().toggleCode().run(),
};

function MarkButton({
  editor,
  mark,
  icon: Icon,
  label,
  shortcut,
}: {
  editor: Editor;
  mark: InlineMark;
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  shortcut: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Toggle
            size="sm"
            pressed={editor.isActive(mark)}
            onPressedChange={() => toggleMarkActions[mark](editor)}
            onMouseDown={(e) => e.preventDefault()}
          />
        }
      >
        <Icon className="size-3.5" />
      </TooltipTrigger>
      <TooltipContent side="top" sideOffset={8}>
        {label}
        <span className="ml-1.5 text-muted-foreground">{shortcut}</span>
      </TooltipContent>
    </Tooltip>
  );
}

// ---------------------------------------------------------------------------
// Link Edit Bar
// ---------------------------------------------------------------------------

function LinkEditBar({
  editor,
  onClose,
}: {
  editor: Editor;
  onClose: () => void;
}) {
  const existingHref = editor.getAttributes("link").href as string | undefined;
  const [url, setUrl] = useState(existingHref ?? "");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    // autoFocus workaround — setTimeout to ensure the input is mounted
    const t = setTimeout(() => inputRef.current?.focus(), 0);
    return () => clearTimeout(t);
  }, []);

  const apply = useCallback(() => {
    let href = url.trim();
    if (!href) {
      editor.chain().focus().extendMarkRange("link").unsetLink().run();
    } else {
      if (!/^https?:\/\//.test(href) && !href.startsWith("/")) {
        href = `https://${href}`;
      }
      editor
        .chain()
        .focus()
        .extendMarkRange("link")
        .setLink({ href })
        .run();
    }
    onClose();
  }, [editor, url, onClose]);

  const remove = useCallback(() => {
    editor.chain().focus().extendMarkRange("link").unsetLink().run();
    onClose();
  }, [editor, onClose]);

  return (
    <div
      className="bubble-menu-link-edit"
      onMouseDown={(e) => e.preventDefault()}
    >
      <Input
        ref={inputRef}
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        placeholder="https://..."
        aria-label="URL"
        className="h-7 flex-1 text-xs"
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            apply();
          }
          if (e.key === "Escape") {
            e.preventDefault();
            onClose();
            editor.commands.focus();
          }
        }}
      />
      <Button
        size="icon-xs"
        variant="ghost"
        onClick={apply}
        onMouseDown={(e) => e.preventDefault()}
      >
        <Check className="size-3.5" />
      </Button>
      {existingHref && (
        <Button
          size="icon-xs"
          variant="ghost"
          onClick={remove}
          onMouseDown={(e) => e.preventDefault()}
        >
          <Unlink className="size-3.5" />
        </Button>
      )}
      <Button
        size="icon-xs"
        variant="ghost"
        onClick={() => {
          onClose();
          editor.commands.focus();
        }}
        onMouseDown={(e) => e.preventDefault()}
      >
        <X className="size-3.5" />
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Heading Dropdown
// ---------------------------------------------------------------------------

function HeadingDropdown({
  editor,
  onOpenChange,
}: {
  editor: Editor;
  onOpenChange: (open: boolean) => void;
}) {
  const activeLevel = [1, 2, 3].find((l) =>
    editor.isActive("heading", { level: l }),
  );

  const label = activeLevel ? `H${activeLevel}` : "Text";

  const items = [
    {
      label: "Normal Text",
      icon: Type,
      active: !activeLevel,
      action: () => editor.chain().focus().setParagraph().run(),
    },
    {
      label: "Heading 1",
      icon: Heading1,
      active: activeLevel === 1,
      action: () => editor.chain().focus().toggleHeading({ level: 1 }).run(),
    },
    {
      label: "Heading 2",
      icon: Heading2,
      active: activeLevel === 2,
      action: () => editor.chain().focus().toggleHeading({ level: 2 }).run(),
    },
    {
      label: "Heading 3",
      icon: Heading3,
      active: activeLevel === 3,
      action: () => editor.chain().focus().toggleHeading({ level: 3 }).run(),
    },
  ];

  return (
    <DropdownMenu onOpenChange={onOpenChange}>
      <DropdownMenuTrigger
        className="inline-flex h-7 items-center gap-0.5 rounded-md px-1.5 text-xs font-medium hover:bg-muted"
        onMouseDown={(e) => e.preventDefault()}
      >
        {label}
        <ChevronDown className="size-3" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="bottom"
        sideOffset={8}
        align="start"
        className="w-auto"
      >
        {items.map((item) => (
          <DropdownMenuItem
            key={item.label}
            onClick={item.action}
            className="gap-2 text-xs"
          >
            <item.icon className="size-3.5" />
            {item.label}
            {item.active && <Check className="ml-auto size-3.5" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// ---------------------------------------------------------------------------
// List Dropdown
// ---------------------------------------------------------------------------

function ListDropdown({
  editor,
  onOpenChange,
}: {
  editor: Editor;
  onOpenChange: (open: boolean) => void;
}) {
  const isBullet = editor.isActive("bulletList");
  const isOrdered = editor.isActive("orderedList");

  return (
    <DropdownMenu onOpenChange={onOpenChange}>
      <Tooltip>
        <TooltipTrigger
          render={
            <DropdownMenuTrigger
              className="inline-flex h-7 items-center gap-0.5 rounded-md px-1.5 text-xs font-medium hover:bg-muted aria-pressed:bg-muted"
              aria-pressed={isBullet || isOrdered}
              onMouseDown={(e) => e.preventDefault()}
            />
          }
        >
          <List className="size-3.5" />
          <ChevronDown className="size-3" />
        </TooltipTrigger>
        <TooltipContent side="top" sideOffset={8}>
          List
        </TooltipContent>
      </Tooltip>
      <DropdownMenuContent
        side="bottom"
        sideOffset={8}
        align="start"
        className="w-auto"
      >
        <DropdownMenuItem
          onClick={() => editor.chain().focus().toggleBulletList().run()}
          className="gap-2 text-xs"
        >
          <List className="size-3.5" />
          Bullet List
          {isBullet && <Check className="ml-auto size-3.5" />}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => editor.chain().focus().toggleOrderedList().run()}
          className="gap-2 text-xs"
        >
          <ListOrdered className="size-3.5" />
          Ordered List
          {isOrdered && <Check className="ml-auto size-3.5" />}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// ---------------------------------------------------------------------------
// Main Bubble Menu
// ---------------------------------------------------------------------------

function EditorBubbleMenu({ editor }: { editor: Editor }) {
  const [mode, setMode] = useState<"toolbar" | "link-edit">("toolbar");
  const [focused, setFocused] = useState(editor.view.hasFocus());
  const modeRef = useRef(mode);
  modeRef.current = mode;
  // Track whether a child dropdown is open — blur during dropdown interaction should not hide
  const menuOpenRef = useRef(false);
  const handleMenuOpenChange = useCallback((open: boolean) => {
    menuOpenRef.current = open;
  }, []);

  useEditorTransactionUpdate(editor);

  // Hide bubble menu when editor loses focus (but not when a child dropdown is open)
  useEffect(() => {
    const onFocus = () => setFocused(true);
    const onBlur = () => {
      setTimeout(() => {
        if (!editor.isDestroyed && !editor.view.hasFocus() && !menuOpenRef.current) {
          setFocused(false);
        }
      }, 0);
    };
    editor.on("focus", onFocus);
    editor.on("blur", onBlur);
    return () => {
      editor.off("focus", onFocus);
      editor.off("blur", onBlur);
    };
  }, [editor]);

  // Reset to toolbar mode when selection changes — but not during link editing.
  // Also restore focused state (scroll sets it to false, new selection should bring it back).
  useEffect(() => {
    const handler = () => {
      if (modeRef.current !== "link-edit") setMode("toolbar");
      if (editor.view.hasFocus()) setFocused(true);
    };
    editor.on("selectionUpdate", handler);
    return () => {
      editor.off("selectionUpdate", handler);
    };
  }, [editor]);

  // Hide when an ancestor of the editor scrolls (capture phase catches non-bubbling scroll events).
  // Scoped to ancestors only — dropdown/sidebar scrolls won't trigger this.
  useEffect(() => {
    const handler = (e: Event) => {
      const target = e.target;
      if (
        target instanceof HTMLElement &&
        target.contains(editor.view.dom)
      ) {
        setFocused(false);
      }
    };
    document.addEventListener("scroll", handler, true);
    return () => document.removeEventListener("scroll", handler, true);
  }, [editor]);

  const openLinkEdit = useCallback(() => {
    setMode("link-edit");
  }, []);

  const closeLinkEdit = useCallback(() => {
    setMode("toolbar");
  }, []);

  if (!focused) return null;

  return (
    <BubbleMenu
      editor={editor}
      shouldShow={shouldShowBubbleMenu}
      updateDelay={0}
      style={{ zIndex: 50 }}
      options={BUBBLE_MENU_OPTIONS}
    >
      {mode === "link-edit" ? (
        <LinkEditBar editor={editor} onClose={closeLinkEdit} />
      ) : (
        <TooltipProvider delay={300}>
          <div className="bubble-menu">
            {/* Group 1: Inline Marks */}
            <MarkButton
              editor={editor}
              mark="bold"
              icon={Bold}
              label="Bold"
              shortcut={`${mod}+B`}
            />
            <MarkButton
              editor={editor}
              mark="italic"
              icon={Italic}
              label="Italic"
              shortcut={`${mod}+I`}
            />
            <MarkButton
              editor={editor}
              mark="strike"
              icon={Strikethrough}
              label="Strikethrough"
              shortcut={`${mod}+Shift+S`}
            />
            <MarkButton
              editor={editor}
              mark="code"
              icon={Code}
              label="Code"
              shortcut={`${mod}+E`}
            />

            <Separator orientation="vertical" className="mx-0.5 h-5" />

            {/* Group 2: Link */}
            <Tooltip>
              <TooltipTrigger
                render={
                  <Toggle
                    size="sm"
                    pressed={editor.isActive("link")}
                    onPressedChange={openLinkEdit}
                    onMouseDown={(e) => e.preventDefault()}
                  />
                }
              >
                <Link2 className="size-3.5" />
              </TooltipTrigger>
              <TooltipContent side="top" sideOffset={8}>
                Link
              </TooltipContent>
            </Tooltip>

            <Separator orientation="vertical" className="mx-0.5 h-5" />

            {/* Group 3: Block Transforms */}
            <HeadingDropdown editor={editor} onOpenChange={handleMenuOpenChange} />
            <ListDropdown editor={editor} onOpenChange={handleMenuOpenChange} />
            <Tooltip>
              <TooltipTrigger
                render={
                  <Toggle
                    size="sm"
                    pressed={editor.isActive("blockquote")}
                    onPressedChange={() =>
                      editor.chain().focus().toggleBlockquote().run()
                    }
                    onMouseDown={(e) => e.preventDefault()}
                  />
                }
              >
                <Quote className="size-3.5" />
              </TooltipTrigger>
              <TooltipContent side="top" sideOffset={8}>
                Quote
              </TooltipContent>
            </Tooltip>
          </div>
        </TooltipProvider>
      )}
    </BubbleMenu>
  );
}

export { EditorBubbleMenu };
