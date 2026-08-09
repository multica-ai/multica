"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { Editor } from "@tiptap/react";
import {
  Bold,
  Check,
  ChevronDown,
  Code,
  Ellipsis,
  Heading1,
  Heading2,
  Heading3,
  Highlighter,
  IndentDecrease,
  IndentIncrease,
  Italic,
  Link2,
  List,
  ListOrdered,
  ListTodo,
  MessageSquarePlus,
  Pilcrow,
  Quote,
  Strikethrough,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { Separator } from "@multica/ui/components/ui/separator";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Toggle } from "@multica/ui/components/ui/toggle";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import "@multica/cerebro-app-kit/styles.css";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import {
  EDITOR_TOOLBAR_ACTION_LABELS,
} from "./editor-toolbar-settings";
import {
  EDITOR_LIST_TYPES,
  type EditorListType,
  type EditorToolbarActionId,
} from "./editor-toolbar-preferences";
import { useEditorListType } from "./use-editor-list-type";
import { useEditorToolbarOrder } from "./use-editor-toolbar-order";

type ToolbarIcon = React.ComponentType<{ className?: string }>;

// ContentEditor installs these Tiptap extensions at runtime. Their command
// declaration merges live in the editor package, so this shared Cerebro package
// describes only the chain surface it consumes.
type FormattingChain = {
  focus: () => FormattingChain;
  toggleBold: () => FormattingChain;
  toggleHighlight: () => FormattingChain;
  toggleTaskList: () => FormattingChain;
  toggleItalic: () => FormattingChain;
  toggleStrike: () => FormattingChain;
  toggleBulletList: () => FormattingChain;
  toggleOrderedList: () => FormattingChain;
  toggleBlockquote: () => FormattingChain;
  toggleCode: () => FormattingChain;
  sinkListItem: (type: "taskItem" | "listItem") => FormattingChain;
  liftListItem: (type: "taskItem" | "listItem") => FormattingChain;
  setParagraph: () => FormattingChain;
  toggleHeading: (attrs: { level: 1 | 2 | 3 }) => FormattingChain;
  extendMarkRange: (mark: "link") => FormattingChain;
  setLink: (attrs: { href: string }) => FormattingChain;
  unsetLink: () => FormattingChain;
  run: () => boolean;
};

function formattingChain(editor: Editor): FormattingChain {
  return editor.chain() as unknown as FormattingChain;
}

const ACTION_ICONS: Record<EditorToolbarActionId, ToolbarIcon> = {
  bold: Bold,
  link: Link2,
  heading: Pilcrow,
  highlight: Highlighter,
  comment: MessageSquarePlus,
  italic: Italic,
  strike: Strikethrough,
  lists: List,
  blockquote: Quote,
  code: Code,
};

// List types are not toolbar actions any more — they live inside the one lists
// control — so they carry their own icons and labels.
const LIST_TYPE_ICONS: Record<EditorListType, ToolbarIcon> = {
  bulletList: List,
  orderedList: ListOrdered,
  taskList: ListTodo,
};

const LIST_TYPE_LABELS: Record<EditorListType, string> = {
  bulletList: "Bullet list",
  orderedList: "Ordered list",
  taskList: "Task list",
};

const INDENT_LABEL = "Increase indent";
const OUTDENT_LABEL = "Decrease indent";

const ACTION_SHORTCUTS: Partial<Record<EditorToolbarActionId, string>> = {
  bold: "⌘B",
  italic: "⌘I",
  strike: "⌘⇧X",
  highlight: "⌘⇧H",
  code: "⌘E",
  link: "⌘K",
  blockquote: "⌘⇧B",
};

const TOGGLE_CLASS_NAME =
  "border border-transparent hover:bg-muted data-pressed:border-[var(--toolbar-active-border)] data-pressed:bg-[var(--toolbar-active)] data-pressed:hover:bg-[var(--toolbar-active)]";

const MARK_ACTIONS = new Set<EditorToolbarActionId>([
  "bold",
  "italic",
  "strike",
  "highlight",
  "code",
  "blockquote",
]);

function safeLink(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return null;
  const href = /^[a-z][a-z0-9+.-]*:/i.test(trimmed)
    ? trimmed
    : `https://${trimmed}`;
  try {
    const parsed = new URL(href);
    return ["http:", "https:", "mailto:"].includes(parsed.protocol)
      ? href
      : null;
  } catch {
    return null;
  }
}

function activeState(editor: Editor, action: EditorToolbarActionId): boolean {
  const marks: Partial<Record<EditorToolbarActionId, string>> = {
    bold: "bold",
    link: "link",
    highlight: "highlight",
    italic: "italic",
    strike: "strike",
    blockquote: "blockquote",
    code: "code",
  };
  const mark = marks[action];
  return mark ? editor.isActive(mark) : false;
}

function listItemType(editor: Editor): "taskItem" | "listItem" {
  return editor.isActive("taskItem") ? "taskItem" : "listItem";
}

const LIST_SHORTCUTS: Record<EditorListType, string> = {
  orderedList: "⌘⇧7",
  bulletList: "⌘⇧8",
  taskList: "⌘⇧9",
};

function toggleListType(editor: Editor, type: EditorListType): void {
  const chain = formattingChain(editor).focus();
  if (type === "bulletList") chain.toggleBulletList().run();
  else if (type === "orderedList") chain.toggleOrderedList().run();
  else chain.toggleTaskList().run();
}

/**
 * One control for all list work. The left half toggles the type the user
 * reached for last; the chevron opens the other types plus Indent and Outdent.
 * Replaces three near-identical list buttons and the two indent buttons, which
 * is what makes the row fit a phone without hiding anything past the edge.
 */
function ListsControl({ editor }: { editor: Editor }) {
  const { listType, saveListType } = useEditorListType();
  const itemType = listItemType(editor);
  const primaryLabel = LIST_TYPE_LABELS[listType];
  const PrimaryIcon = LIST_TYPE_ICONS[listType];

  const choose = (type: EditorListType) => {
    toggleListType(editor, type);
    if (type !== listType) void saveListType(type);
  };

  return (
    <div className="flex items-center">
      <Tooltip>
        <TooltipTrigger
          render={
            <Toggle
              size="sm"
              aria-label={primaryLabel}
              pressed={editor.isActive(listType)}
              onMouseDown={(event) => event.preventDefault()}
              onPressedChange={() => toggleListType(editor, listType)}
              className={TOGGLE_CLASS_NAME}
            />
          }
        >
          <PrimaryIcon className="size-4" />
        </TooltipTrigger>
        <TooltipContent>
          {primaryLabel} <Kbd>{LIST_SHORTCUTS[listType]}</Kbd>
        </TooltipContent>
      </Tooltip>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="List options"
              // Half a control needs less width than a whole one on a mouse
              // row, but 24px is below what a fingertip can hit — and this
              // chevron sits directly beside the half that toggles the list.
              className="size-6 pointer-coarse:size-9"
              onMouseDown={(event) => event.preventDefault()}
            />
          }
        >
          <ChevronDown className="size-3.5" />
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          {EDITOR_LIST_TYPES.map((type) => {
            const Icon = LIST_TYPE_ICONS[type];
            return (
              <DropdownMenuItem key={type} onClick={() => choose(type)}>
                <Icon /> {LIST_TYPE_LABELS[type]}
                <Kbd className="ml-auto">{LIST_SHORTCUTS[type]}</Kbd>
              </DropdownMenuItem>
            );
          })}
          <DropdownMenuItem
            disabled={!editor.can().sinkListItem(itemType)}
            onClick={() =>
              formattingChain(editor).focus().sinkListItem(itemType).run()
            }
          >
            <IndentIncrease /> {INDENT_LABEL}
            <Kbd className="ml-auto">⇥</Kbd>
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!editor.can().liftListItem(itemType)}
            onClick={() =>
              formattingChain(editor).focus().liftListItem(itemType).run()
            }
          >
            <IndentDecrease /> {OUTDENT_LABEL}
            <Kbd className="ml-auto">⇧⇥</Kbd>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

/**
 * The controls that stay in the row at every width. Everything else steps into
 * the overflow menu when the pane gets narrow — the row never scrolls an action
 * past its own edge, which is the rule the whole design rests on.
 */
/**
 * Which group each action belongs to. A separator is drawn wherever two
 * neighbours in the rendered row fall in different groups, so the dividers
 * follow the user's own order instead of being pinned to one action.
 */
const ACTION_GROUP: Record<EditorToolbarActionId, number> = {
  heading: 0,
  bold: 1,
  italic: 1,
  strike: 1,
  highlight: 1,
  code: 1,
  link: 2,
  lists: 3,
  blockquote: 3,
  comment: 4,
};

const PRIMARY_SLOTS = new Set<EditorToolbarActionId>([
  "heading",
  "bold",
  "italic",
  "link",
  "lists",
  "comment",
]);

function runMarkAction(editor: Editor, action: EditorToolbarActionId): void {
  const chain = formattingChain(editor).focus();
  switch (action) {
    case "bold":
      chain.toggleBold().run();
      break;
    case "italic":
      chain.toggleItalic().run();
      break;
    case "strike":
      chain.toggleStrike().run();
      break;
    case "highlight":
      chain.toggleHighlight().run();
      break;
    case "code":
      chain.toggleCode().run();
      break;
    case "blockquote":
      chain.toggleBlockquote().run();
      break;
  }
}

/**
 * A phone, not merely a touch screen. An iPad reports a coarse pointer too, and
 * it has the width to keep the row where it is — 767px is the boundary this
 * repo already uses (`MOBILE_BREAKPOINT` in packages/ui/hooks/use-mobile.ts).
 */
const PHONE_VIEWPORT = "(pointer: coarse) and (max-width: 767px)";

/** `min-h-11` on the row below — the height it claims from the bottom edge. */
const DOCKED_ROW_HEIGHT = 44;

/**
 * FIR-4028 slice 10 — the phone.
 *
 * A formatting row that always sits above the text costs writing space a phone
 * does not have. So on a touch device the row does not exist until the editor
 * has focus, and from then on it rides the top edge of the on-screen keyboard:
 * `visualViewport` shrinks by exactly the keyboard's height, and the difference
 * against `window.innerHeight` is the inset a `fixed` element needs at the
 * bottom. Same listener set as `use-composer-height.ts` — the keyboard also
 * moves on scroll, not only on resize.
 */
function useKeyboardDock(editor: Editor | null) {
  const [isPhone, setIsPhone] = useState(
    () =>
      typeof window !== "undefined" &&
      window.matchMedia?.(PHONE_VIEWPORT).matches === true,
  );
  const [isFocused, setIsFocused] = useState(() => editor?.isFocused === true);
  const [keyboardInset, setKeyboardInset] = useState(0);

  // Tracked rather than read once: a phone turned sideways crosses the width
  // boundary, and a tablet must never lose the row it has room for.
  useEffect(() => {
    const query = window.matchMedia?.(PHONE_VIEWPORT);
    if (!query) return;
    const update = () => setIsPhone(query.matches === true);
    update();
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);

  useEffect(() => {
    if (!editor) return;
    setIsFocused(editor.isFocused === true);
    const onFocus = () => setIsFocused(true);
    const onBlur = () => setIsFocused(false);
    editor.on("focus", onFocus);
    editor.on("blur", onBlur);
    return () => {
      editor.off("focus", onFocus);
      editor.off("blur", onBlur);
    };
  }, [editor]);

  useEffect(() => {
    if (!isPhone || typeof window === "undefined") return;
    const vv = window.visualViewport;
    const update = () =>
      setKeyboardInset(
        vv ? Math.max(0, window.innerHeight - vv.height - vv.offsetTop) : 0,
      );
    update();
    vv?.addEventListener("resize", update);
    vv?.addEventListener("scroll", update);
    window.addEventListener("resize", update);
    return () => {
      vv?.removeEventListener("resize", update);
      vv?.removeEventListener("scroll", update);
      window.removeEventListener("resize", update);
    };
  }, [isPhone]);

  // The docked row is `fixed`, so anything else anchored to the bottom of the
  // screen — the outline button on both surfaces — lands underneath it. Publish
  // the height it occupies so those can step over it instead.
  useEffect(() => {
    const root = document.documentElement;
    if (!isPhone || !isFocused) {
      root.style.removeProperty("--cerebro-editor-dock-height");
      return;
    }
    root.style.setProperty(
      "--cerebro-editor-dock-height",
      `${keyboardInset + DOCKED_ROW_HEIGHT}px`,
    );
    return () => {
      root.style.removeProperty("--cerebro-editor-dock-height");
    };
  }, [isPhone, isFocused, keyboardInset]);

  return { isPhone, isFocused, keyboardInset };
}

/**
 * The overflow control on a phone. Not a `Sheet` and not a `DropdownMenu`: both
 * move focus into themselves, and on a phone that dismisses the keyboard — so
 * the panel that formats the text would close the text. Every target here
 * refuses the focus change instead, and the panel is a plain layer above the
 * docked row.
 */
function FormatSheet({
  editor,
  actions,
}: {
  editor: Editor;
  actions: EditorToolbarActionId[];
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label="More formatting"
        aria-expanded={open}
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => setOpen((value) => !value)}
      >
        <Ellipsis className="size-4" />
      </Button>
      {open && (
        <div
          role="menu"
          aria-label="More formatting"
          className="absolute inset-x-0 bottom-full mb-1 max-h-[50vh] overflow-y-auto rounded-t-lg border bg-popover p-1 shadow-lg"
        >
          {actions.map((action) => {
            const Icon = ACTION_ICONS[action];
            return (
              <button
                key={action}
                type="button"
                role="menuitem"
                data-overflow-action={action}
                className="flex min-h-[38px] w-full items-center gap-2 rounded px-3 text-sm"
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => {
                  runMarkAction(editor, action);
                  setOpen(false);
                }}
              >
                <Icon className="size-4" />
                {EDITOR_TOOLBAR_ACTION_LABELS[action]}
                {activeState(editor, action) && <Check className="ml-auto size-4" />}
              </button>
            );
          })}
        </div>
      )}
    </>
  );
}

function OverflowMenu({
  editor,
  actions,
}: {
  editor: Editor;
  actions: EditorToolbarActionId[];
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="More formatting"
            onMouseDown={(event) => event.preventDefault()}
          />
        }
      >
        <Ellipsis className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        {actions.map((action) => {
          const Icon = ACTION_ICONS[action];
          const label = EDITOR_TOOLBAR_ACTION_LABELS[action];
          return (
            <DropdownMenuItem
              key={action}
              data-overflow-action={action}
              onClick={() => runMarkAction(editor, action)}
            >
              <Icon /> {label}
              {activeState(editor, action) && <Check className="ml-auto" />}
              {ACTION_SHORTCUTS[action] && (
                <Kbd className="ml-auto">{ACTION_SHORTCUTS[action]}</Kbd>
              )}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * Comment sits furthest right whatever order the user saved: it acts on a
 * selection and belongs beside the comments rail, not in the middle of the
 * formatting controls where it spends most of its life disabled.
 */
function pinCommentLast(
  actions: EditorToolbarActionId[],
): EditorToolbarActionId[] {
  const rest = actions.filter((action) => action !== "comment");
  return actions.includes("comment") ? [...rest, "comment"] : rest;
}

/**
 * Roving focus: the row is a single tab stop and the arrow keys walk it, which
 * is what `role="toolbar"` promises a screen reader. tabIndex is written onto
 * the rendered buttons rather than threaded through every control component —
 * the controls are five different shapes and none of them owns focus order.
 */
function useRovingToolbarFocus(enabled: boolean) {
  const ref = useRef<HTMLDivElement>(null);
  const activeIndex = useRef(0);

  const items = useCallback(
    () =>
      Array.from(ref.current?.querySelectorAll<HTMLElement>("button") ?? []),
    [],
  );

  useEffect(() => {
    if (!enabled) return;
    const all = items();
    if (activeIndex.current >= all.length) activeIndex.current = 0;
    all.forEach((element, index) => {
      element.dataset.toolbarItem = "";
      element.tabIndex = index === activeIndex.current ? 0 : -1;
    });
  });

  const onKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLDivElement>) => {
      const step =
        event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
      if (!step) return;
      const all = items();
      if (all.length === 0) return;
      const current = all.indexOf(document.activeElement as HTMLElement);
      const from = current === -1 ? activeIndex.current : current;
      const next = (from + step + all.length) % all.length;
      activeIndex.current = next;
      all.forEach((element, index) => {
        element.tabIndex = index === next ? 0 : -1;
      });
      all[next]?.focus();
      event.preventDefault();
    },
    [items],
  );

  return { ref, onKeyDown };
}

function ToolbarButton({
  action,
  editor,
  onCommentOnSelection,
}: {
  action: Exclude<EditorToolbarActionId, "link" | "heading" | "lists">;
  editor: Editor;
  onCommentOnSelection?: (text: string) => void;
}) {
  const Icon = ACTION_ICONS[action];
  const label = EDITOR_TOOLBAR_ACTION_LABELS[action];
  const selectedText = editor.state.doc
    .textBetween(
      editor.state.selection.from,
      editor.state.selection.to,
      " ",
      " ",
    )
    .trim();
  const disabled =
    action === "comment" &&
    (!onCommentOnSelection || editor.state.selection.empty || !selectedText);

  const run = () => {
    if (action === "comment") {
      if (selectedText) onCommentOnSelection?.(selectedText);
      return;
    }
    runMarkAction(editor, action);
  };

  if (MARK_ACTIONS.has(action)) {
    const pressed = activeState(editor, action);
    const shortcut = ACTION_SHORTCUTS[action];
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <Toggle
              size="sm"
              aria-label={label}
              pressed={pressed}
              onMouseDown={(event) => event.preventDefault()}
              onPressedChange={run}
              className={TOGGLE_CLASS_NAME}
            />
          }
        >
          <Icon className="size-4" />
        </TooltipTrigger>
        <TooltipContent>
          {label} {shortcut && <Kbd>{shortcut}</Kbd>}
        </TooltipContent>
      </Tooltip>
    );
  }

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label={label}
      title={label}
      aria-pressed={activeState(editor, action)}
      disabled={disabled}
      onMouseDown={(event) => event.preventDefault()}
      onClick={run}
      className="aria-pressed:bg-muted"
    >
      <Icon className="size-4" />
    </Button>
  );
}

function ActiveBlockIcon({
  level,
  className,
}: {
  level?: 1 | 2 | 3;
  className?: string;
}) {
  const Icon =
    level === 1 ? Heading1 : level === 2 ? Heading2 : level === 3 ? Heading3 : Pilcrow;
  return <Icon className={className} />;
}

function HeadingControl({ editor }: { editor: Editor }) {
  const activeLevel = ([1, 2, 3] as const).find((level) =>
    editor.isActive("heading", { level }),
  );
  const label = activeLevel ? `Heading ${activeLevel}` : "Body text";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Toggle
            size="sm"
            aria-label={label}
            pressed={Boolean(activeLevel)}
            className={cn(
              TOGGLE_CLASS_NAME,
              "min-w-28 justify-between px-2",
              // Worded, this control is a third of a phone-width row and pushes
              // the overflow trigger past the edge. Below the collapse point it
              // keeps its meaning as an icon and gives the width back.
              "@max-[520px]:min-w-0 @max-[520px]:px-1.5",
            )}
          />
        }
      >
        <ActiveBlockIcon level={activeLevel} className="size-4 @[521px]:hidden" />
        <span className="@max-[520px]:hidden">{label}</span>
        <ChevronDown className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem
          onClick={() => formattingChain(editor).focus().setParagraph().run()}
        >
          <Pilcrow /> Body text
          {!activeLevel && <Check className="ml-auto" />}
        </DropdownMenuItem>
        {([1, 2, 3] as const).map((level) => {
          const Icon = level === 1 ? Heading1 : level === 2 ? Heading2 : Heading3;
          return (
            <DropdownMenuItem
              key={level}
              onClick={() =>
                formattingChain(editor).focus().toggleHeading({ level }).run()
              }
            >
              <Icon /> Heading {level}
              <Kbd className="ml-auto">⌘⌥{level}</Kbd>
              {activeLevel === level && <Check />}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function LinkControl({ editor }: { editor: Editor }) {
  const [open, setOpen] = useState(false);
  const [url, setUrl] = useState("");
  const isActive = editor.isActive("link");

  const handleOpen = (next: boolean) => {
    setOpen(next);
    if (next) setUrl(editor.getAttributes("link").href ?? "");
  };

  const apply = () => {
    const href = safeLink(url);
    if (!href) return;
    formattingChain(editor)
      .focus()
      .extendMarkRange("link")
      .setLink({ href })
      .run();
    setOpen(false);
  };

  return (
    <Tooltip>
      <Popover open={open} onOpenChange={handleOpen}>
        <TooltipTrigger
          render={
            <PopoverTrigger
              render={
                <Toggle
                  size="sm"
                  aria-label="Link"
                  pressed={isActive}
                  className={TOGGLE_CLASS_NAME}
                />
              }
            />
          }
        >
          <Link2 className="size-4" />
        </TooltipTrigger>
        <PopoverContent className="w-80 p-3" align="start">
          <form
            className="flex gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              apply();
            }}
          >
            <Input
              autoFocus
              aria-label="Link URL"
              placeholder="https://example.com"
              value={url}
              onChange={(event) => setUrl(event.target.value)}
            />
            <Button type="submit" size="sm">
              Apply
            </Button>
            {isActive && (
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => {
                  formattingChain(editor).focus().unsetLink().run();
                  setOpen(false);
                }}
              >
                Remove
              </Button>
            )}
          </form>
        </PopoverContent>
      </Popover>
      <TooltipContent>
        Link <Kbd>{ACTION_SHORTCUTS.link}</Kbd>
      </TooltipContent>
    </Tooltip>
  );
}

export function EditorFormattingToolbar({
  editor,
  onCommentOnSelection,
  className,
}: {
  editor: Editor | null;
  onCommentOnSelection?: (text: string) => void;
  className?: string;
}) {
  const { order, hidden } = useEditorToolbarOrder();
  const [, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const roving = useRovingToolbarFocus(Boolean(editor));
  const dock = useKeyboardDock(editor);

  useEffect(() => {
    if (!editor) return;
    editor.on("transaction", refresh);
    editor.on("selectionUpdate", refresh);
    return () => {
      editor.off("transaction", refresh);
      editor.off("selectionUpdate", refresh);
    };
  }, [editor, refresh]);

  const slots = pinCommentLast(
    order.filter((action) => !hidden.includes(action)),
  );
  // What the user chose to hide is in the ⋯ menu, not gone from the app.
  const hiddenActions = order.filter((action) => hidden.includes(action));

  // On a phone the row belongs to typing, not to reading: with the editor
  // unfocused there is no keyboard to sit on and no reason to spend the height.
  if (dock.isPhone && !dock.isFocused) return null;

  // Before the editor exists there is nothing to press. A row of disabled
  // buttons reads as "you may not", a skeleton reads as "wait a moment".
  if (!editor) {
    return (
      <div
        role="toolbar"
        aria-label="Formatting toolbar"
        aria-busy="true"
        className={cn(
          "flex min-h-11 items-center gap-1 border-b bg-muted/20 px-2 py-1.5",
          className,
        )}
      >
        {slots.map((action) => (
          <Skeleton
            key={action}
            className={cn("h-7", action === "heading" ? "w-28" : "w-7")}
          />
        ))}
      </div>
    );
  }

  const collapsible = slots.filter((slot) => !PRIMARY_SLOTS.has(slot));
  const overflowActions = [...hiddenActions, ...collapsible];

  return (
    <div
      ref={roving.ref}
      onKeyDown={roving.onKeyDown}
      role="toolbar"
      aria-label="Formatting toolbar"
      style={
        dock.isPhone
          ? {
              position: "fixed",
              left: 0,
              right: 0,
              bottom: dock.keyboardInset,
              zIndex: 30,
            }
          : undefined
      }
      className={cn(
        // The row is a field over the text, not another edge of the pane: its
        // own border all round, and sticky so it survives scrolling the note —
        // it mounts inside the writing pane's scroll container.
        "@container sticky top-0 z-20 m-2 flex min-h-9 items-center gap-0.5 rounded-lg border bg-card px-1.5 py-1",
        // Docked to the keyboard the row spans the full width, so the field
        // shape would only cut it off from the edges it is meant to sit on —
        // and it keeps the 44px touch height a thumb needs.
        dock.isPhone &&
          "m-0 min-h-11 rounded-none border-x-0 border-b-0 bg-background",
        className,
      )}
    >
      <TooltipProvider>
        {slots.map((action, index) => {
          const control =
            action === "lists" ? (
              <ListsControl editor={editor} />
            ) : action === "link" ? (
              <LinkControl editor={editor} />
            ) : action === "heading" ? (
              <HeadingControl editor={editor} />
            ) : (
              <ToolbarButton
                action={action}
                editor={editor}
                onCommentOnSelection={onCommentOnSelection}
              />
            );

          return (
            <div
              key={action}
              data-toolbar-slot={action}
              className={cn(
                "flex items-center gap-1",
                !PRIMARY_SLOTS.has(action) && "@max-[520px]:hidden",
              )}
            >
              {control}
              {slots[index + 1] !== undefined &&
                ACTION_GROUP[slots[index + 1]!] !== ACTION_GROUP[action] && (
                  <Separator orientation="vertical" className="h-5" />
                )}
            </div>
          );
        })}
        {overflowActions.length > 0 && (
          <div
            data-toolbar-slot="overflow"
            className={cn(hiddenActions.length === 0 && "@[521px]:hidden")}
          >
            {dock.isPhone ? (
              <FormatSheet editor={editor} actions={overflowActions} />
            ) : (
              <OverflowMenu editor={editor} actions={overflowActions} />
            )}
          </div>
        )}
      </TooltipProvider>
    </div>
  );
}
