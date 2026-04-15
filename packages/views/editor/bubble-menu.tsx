"use client";

/**
 * EditorBubbleMenu — floating formatting toolbar for text selection.
 *
 * Uses Tiptap's native <BubbleMenu> component which has battle-tested
 * focus management (preventHide flag, relatedTarget checks, mousedown
 * capture). We only add scroll-container visibility detection on top,
 * because the plugin's hide middleware can't detect nested scroll
 * container clipping (virtual element has no contextElement).
 */

import { useState, useEffect, useCallback, useRef } from "react";
import { BubbleMenu } from "@tiptap/react/menus";
import { useEditorState } from "@tiptap/react";
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
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
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

function shouldShowBubbleMenu({
  editor,
  view,
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
  if (!state.doc.textBetween(from, to).trim().length) return false;
  if (state.selection instanceof NodeSelection) return false;
  if (!view.hasFocus()) return false;
  const $from = state.doc.resolve(from);
  if ($from.parent.type.name === "codeBlock") return false;
  return true;
}

const isMac =
  typeof navigator !== "undefined" && /Mac/.test(navigator.platform);
const mod = isMac ? "\u2318" : "Ctrl";

/** Walk up from `el` to find the nearest ancestor with overflow: auto/scroll. */
function getScrollParent(el: HTMLElement): HTMLElement | Window {
  let parent = el.parentElement;
  while (parent) {
    const style = getComputedStyle(parent);
    if (/(auto|scroll)/.test(style.overflow + style.overflowY)) return parent;
    parent = parent.parentElement;
  }
  return window;
}

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
  isActive,
}: {
  editor: Editor;
  mark: InlineMark;
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  shortcut: string;
  isActive: boolean;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Toggle
            size="sm"
            pressed={isActive}
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
// URL normalisation
// ---------------------------------------------------------------------------

/** Protocols that can execute code in the browser — the only ones we block. */
const DANGEROUS_PROTOCOL_RE = /^(javascript|data|vbscript):/i;
const HAS_PROTOCOL_RE = /^[a-z][a-z0-9+.-]*:\/?\/?/i;
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/**
 * Normalise a user-entered URL: add protocol, detect mailto, block XSS.
 *
 * Uses a blocklist (not allowlist) for protocols — only `javascript:`,
 * `data:`, and `vbscript:` are blocked. All other protocols pass through
 * because they can't execute code in the browser and are legitimate
 * deep-link targets in a team tool (slack://, vscode://, figma://).
 * Tiptap's `isAllowedUri` in the `setLink` command provides a second
 * safety layer.
 */
function normalizeUrl(input: string): string {
  const trimmed = input.trim();
  if (!trimmed) return "";
  if (trimmed.startsWith("/")) return trimmed;
  if (DANGEROUS_PROTOCOL_RE.test(trimmed)) return "";
  if (HAS_PROTOCOL_RE.test(trimmed)) return trimmed;
  if (EMAIL_RE.test(trimmed)) return `mailto:${trimmed}`;
  if (trimmed.startsWith("//")) return `https:${trimmed}`;
  return `https://${trimmed}`;
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
    const t = setTimeout(() => inputRef.current?.focus(), 0);
    return () => clearTimeout(t);
  }, []);

  const apply = useCallback(() => {
    const href = normalizeUrl(url);
    if (!href) {
      editor.chain().focus().extendMarkRange("link").unsetLink().run();
    } else {
      editor.chain().focus().extendMarkRange("link").setLink({ href }).run();
    }
    onClose();
  }, [editor, url, onClose]);

  const remove = useCallback(() => {
    editor.chain().focus().extendMarkRange("link").unsetLink().run();
    onClose();
  }, [editor, onClose]);

  return (
    <div className="bubble-menu-link-edit" onMouseDown={(e) => e.preventDefault()}>
      <Input
        ref={inputRef}
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        placeholder="https://..."
        aria-label="URL"
        className="h-7 flex-1 text-xs"
        onKeyDown={(e) => {
          if (e.key === "Enter") { e.preventDefault(); apply(); }
          if (e.key === "Escape") { e.preventDefault(); onClose(); editor.commands.focus(); }
        }}
      />
      <Button size="icon-xs" variant="ghost" onClick={apply} onMouseDown={(e) => e.preventDefault()}>
        <Check className="size-3.5" />
      </Button>
      {existingHref && (
        <Button size="icon-xs" variant="ghost" onClick={remove} onMouseDown={(e) => e.preventDefault()}>
          <Unlink className="size-3.5" />
        </Button>
      )}
      <Button size="icon-xs" variant="ghost" onClick={() => { onClose(); editor.commands.focus(); }} onMouseDown={(e) => e.preventDefault()}>
        <X className="size-3.5" />
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Heading Dropdown
// ---------------------------------------------------------------------------

function HeadingDropdown({ editor, onOpenChange, activeLevel }: { editor: Editor; onOpenChange: (open: boolean) => void; activeLevel: number | undefined }) {
  const [open, setOpen] = useState(false);
  const label = activeLevel ? `H${activeLevel}` : "Text";
  const items = [
    { label: "Normal Text", icon: Type, active: !activeLevel, action: () => editor.chain().focus().setParagraph().run() },
    { label: "Heading 1", icon: Heading1, active: activeLevel === 1, action: () => editor.chain().focus().toggleHeading({ level: 1 }).run() },
    { label: "Heading 2", icon: Heading2, active: activeLevel === 2, action: () => editor.chain().focus().toggleHeading({ level: 2 }).run() },
    { label: "Heading 3", icon: Heading3, active: activeLevel === 3, action: () => editor.chain().focus().toggleHeading({ level: 3 }).run() },
  ];

  const handleOpenChange = useCallback((next: boolean) => {
    setOpen(next);
    onOpenChange(next);
  }, [onOpenChange]);

  return (
    <Popover modal={false} open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        className="inline-flex h-7 items-center gap-0.5 rounded-md px-1.5 text-xs font-medium hover:bg-muted"
        onMouseDown={(e) => e.preventDefault()}
      >
        {label}
        <ChevronDown className="size-3" />
      </PopoverTrigger>
      <PopoverContent
        side="bottom"
        sideOffset={8}
        align="start"
        className="w-auto min-w-32 p-1"
        initialFocus={false}
        finalFocus={false}
      >
        {items.map((item) => (
          <button
            key={item.label}
            className="flex w-full cursor-default items-center gap-2 rounded-md px-1.5 py-1 text-xs outline-hidden select-none hover:bg-accent hover:text-accent-foreground"
            onMouseDown={(e) => {
              e.preventDefault();
              item.action();
              handleOpenChange(false);
            }}
          >
            <item.icon className="size-3.5" />
            {item.label}
            {item.active && <Check className="ml-auto size-3.5" />}
          </button>
        ))}
      </PopoverContent>
    </Popover>
  );
}

// ---------------------------------------------------------------------------
// List Dropdown
// ---------------------------------------------------------------------------

function ListDropdown({ editor, onOpenChange, isBullet, isOrdered }: { editor: Editor; onOpenChange: (open: boolean) => void; isBullet: boolean; isOrdered: boolean }) {
  const [open, setOpen] = useState(false);

  const handleOpenChange = useCallback((next: boolean) => {
    setOpen(next);
    onOpenChange(next);
  }, [onOpenChange]);

  return (
    <Popover modal={false} open={open} onOpenChange={handleOpenChange}>
      <Tooltip>
        <TooltipTrigger render={
          <PopoverTrigger className="inline-flex h-7 items-center gap-0.5 rounded-md px-1.5 text-xs font-medium hover:bg-muted aria-pressed:bg-muted" aria-pressed={isBullet || isOrdered} onMouseDown={(e) => e.preventDefault()} />
        }>
          <List className="size-3.5" />
          <ChevronDown className="size-3" />
        </TooltipTrigger>
        <TooltipContent side="top" sideOffset={8}>List</TooltipContent>
      </Tooltip>
      <PopoverContent
        side="bottom"
        sideOffset={8}
        align="start"
        className="w-auto min-w-32 p-1"
        initialFocus={false}
        finalFocus={false}
      >
        <button
          className="flex w-full cursor-default items-center gap-2 rounded-md px-1.5 py-1 text-xs outline-hidden select-none hover:bg-accent hover:text-accent-foreground"
          onMouseDown={(e) => {
            e.preventDefault();
            editor.chain().focus().toggleBulletList().run();
            handleOpenChange(false);
          }}
        >
          <List className="size-3.5" /> Bullet List
          {isBullet && <Check className="ml-auto size-3.5" />}
        </button>
        <button
          className="flex w-full cursor-default items-center gap-2 rounded-md px-1.5 py-1 text-xs outline-hidden select-none hover:bg-accent hover:text-accent-foreground"
          onMouseDown={(e) => {
            e.preventDefault();
            editor.chain().focus().toggleOrderedList().run();
            handleOpenChange(false);
          }}
        >
          <ListOrdered className="size-3.5" /> Ordered List
          {isOrdered && <Check className="ml-auto size-3.5" />}
        </button>
      </PopoverContent>
    </Popover>
  );
}

// ---------------------------------------------------------------------------
// Main Bubble Menu — native Tiptap <BubbleMenu>
// ---------------------------------------------------------------------------

function EditorBubbleMenu({ editor }: { editor: Editor }) {
  const [mode, setMode] = useState<"toolbar" | "link-edit">("toolbar");
  const [scrollTarget, setScrollTarget] = useState<HTMLElement | Window>(window);
  const menuElRef = useRef<HTMLDivElement>(null);

  // Precise subscription to formatting state — only re-renders when these
  // values actually change, replacing direct editor.isActive() calls that
  // relied on the parent re-rendering on every transaction.
  const fmt = useEditorState({
    editor,
    selector: ({ editor: e }) => ({
      bold: e.isActive("bold"),
      italic: e.isActive("italic"),
      strike: e.isActive("strike"),
      code: e.isActive("code"),
      link: e.isActive("link"),
      blockquote: e.isActive("blockquote"),
      bulletList: e.isActive("bulletList"),
      orderedList: e.isActive("orderedList"),
      heading1: e.isActive("heading", { level: 1 }),
      heading2: e.isActive("heading", { level: 2 }),
      heading3: e.isActive("heading", { level: 3 }),
    }),
  });

  // Find the real scroll container once the editor view is ready.
  // editor.view.dom throws if the view hasn't been mounted yet or has been
  // destroyed — the Proxy only stubs state/isDestroyed, everything else throws.
  // This race happens on fast page transitions in Desktop (Inbox switching)
  // because useEditor delays destruction via setTimeout(..., 1) for StrictMode
  // survival (TipTap issue #7346).
  useEffect(() => {
    const detect = () => {
      if (!editor.isInitialized) return; // view not ready yet
      setScrollTarget(getScrollParent(editor.view.dom));
    };
    detect();
    editor.on("create", detect);
    return () => { editor.off("create", detect); };
  }, [editor]);

  // Hide when the selection scrolls outside the scroll container's
  // visible area. The plugin's hide middleware can't detect this because
  // its virtual reference element has no contextElement — Floating UI
  // only checks viewport bounds. We use `display` (not managed by the
  // plugin) as an additive visibility layer.
  const scrollHiddenRef = useRef(false);
  const [, forceRender] = useState(0);
  useEffect(() => {
    if (scrollTarget === window) return;
    const el = scrollTarget as HTMLElement;

    const onScroll = () => {
      if (editor.state.selection.empty) {
        if (scrollHiddenRef.current) {
          scrollHiddenRef.current = false;
          forceRender((n) => n + 1);
        }
        return;
      }
      // editor.view.coordsAtPos throws if the view has been destroyed
      // during a fast unmount race (same Proxy guard as view.dom above).
      let coords: { top: number };
      try {
        coords = editor.view.coordsAtPos(editor.state.selection.from);
      } catch {
        return;
      }
      const rect = el.getBoundingClientRect();
      const visible = coords.top >= rect.top && coords.top <= rect.bottom;
      if (scrollHiddenRef.current !== !visible) {
        scrollHiddenRef.current = !visible;
        forceRender((n) => n + 1);
      }
    };

    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, [editor, scrollTarget]);

  // Reset scroll-hidden and mode when selection changes
  useEffect(() => {
    const handler = () => {
      setMode("toolbar");
      if (scrollHiddenRef.current) {
        scrollHiddenRef.current = false;
        forceRender((n) => n + 1);
      }
    };
    editor.on("selectionUpdate", handler);
    return () => { editor.off("selectionUpdate", handler); };
  }, [editor]);

  // Refocus editor when Base UI dropdown closes
  const handleMenuOpenChange = useCallback(
    (open: boolean) => { if (!open) editor.commands.focus(); },
    [editor],
  );

  return (
    <BubbleMenu
      ref={menuElRef}
      editor={editor}
      shouldShow={shouldShowBubbleMenu}
      updateDelay={0}
      style={{
        zIndex: 50,
        display: scrollHiddenRef.current ? "none" : undefined,
      }}
      options={{
        strategy: "fixed",
        placement: "top",
        offset: 8,
        flip: true,
        shift: { padding: 8 },
        hide: true,
        scrollTarget,
        // Tiptap's React wrapper initialises the menu element with
        // position:absolute, but computePosition (called right after
        // show()) needs position:fixed so that getOffsetParent returns
        // the viewport instead of a positioned ancestor. Without this,
        // the first positioning computes coordinates relative to the
        // wrong containing block and the menu flies off-screen.
        onShow: () => {
          if (menuElRef.current) {
            menuElRef.current.style.position = "fixed";
          }
        },
      }}
    >
      {mode === "link-edit" ? (
        <LinkEditBar editor={editor} onClose={() => { setMode("toolbar"); editor.commands.focus(); }} />
      ) : (
        <TooltipProvider delay={300}>
          <div className="bubble-menu">
            <MarkButton editor={editor} mark="bold" icon={Bold} label="Bold" shortcut={`${mod}+B`} isActive={fmt.bold} />
            <MarkButton editor={editor} mark="italic" icon={Italic} label="Italic" shortcut={`${mod}+I`} isActive={fmt.italic} />
            <MarkButton editor={editor} mark="strike" icon={Strikethrough} label="Strikethrough" shortcut={`${mod}+Shift+S`} isActive={fmt.strike} />
            <MarkButton editor={editor} mark="code" icon={Code} label="Code" shortcut={`${mod}+E`} isActive={fmt.code} />
            <Separator orientation="vertical" className="mx-0.5 h-5" />
            <Tooltip>
              <TooltipTrigger render={
                <Toggle size="sm" pressed={fmt.link} onPressedChange={() => setMode("link-edit")} onMouseDown={(e) => e.preventDefault()} />
              }>
                <Link2 className="size-3.5" />
              </TooltipTrigger>
              <TooltipContent side="top" sideOffset={8}>Link</TooltipContent>
            </Tooltip>
            <Separator orientation="vertical" className="mx-0.5 h-5" />
            <HeadingDropdown editor={editor} onOpenChange={handleMenuOpenChange} activeLevel={fmt.heading1 ? 1 : fmt.heading2 ? 2 : fmt.heading3 ? 3 : undefined} />
            <ListDropdown editor={editor} onOpenChange={handleMenuOpenChange} isBullet={fmt.bulletList} isOrdered={fmt.orderedList} />
            <Tooltip>
              <TooltipTrigger render={
                <Toggle size="sm" pressed={fmt.blockquote} onPressedChange={() => editor.chain().focus().toggleBlockquote().run()} onMouseDown={(e) => e.preventDefault()} />
              }>
                <Quote className="size-3.5" />
              </TooltipTrigger>
              <TooltipContent side="top" sideOffset={8}>Quote</TooltipContent>
            </Tooltip>
          </div>
        </TooltipProvider>
      )}
    </BubbleMenu>
  );
}

export { EditorBubbleMenu };
