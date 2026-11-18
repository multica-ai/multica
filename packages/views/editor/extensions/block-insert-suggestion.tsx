"use client";

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type ComponentType,
  type RefObject,
} from "react";
import type { SuggestionOptions } from "@tiptap/suggestion";
import { PluginKey } from "@tiptap/pm/state";
import {
  Code,
  Heading1,
  Heading2,
  Heading3,
  Image,
  List,
  ListOrdered,
  ListTodo,
  Minus,
  Paperclip,
  Quote,
  Table2,
  Type,
  Workflow,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { isImeComposing } from "@multica/core/utils";
import { useT } from "../../i18n";
import {
  isPickerAcceptKey,
  pickerNavigationDirection,
} from "../../common/picker-keys";
import { createSuggestionPopupRender } from "./suggestion-popup";
import { isTriggerArmedAt } from "./suggestion-trigger-arming";
import { matchesPinyin } from "./pinyin-match";
import { insertBlock, type InsertBlockKind, type UploadHandler } from "./block-commands";

export interface BlockMenuItem {
  kind: InsertBlockKind;
  icon: ComponentType<{ className?: string }>;
  /** The Markdown a writer can type instead of opening the menu. */
  shortcut?: string;
  /** Search aliases, matched by prefix in addition to the localized label. */
  keywords: readonly string[];
  /** Only offered when the editor can upload. */
  needsUpload?: boolean;
}

export const BLOCK_MENU_ITEMS: readonly BlockMenuItem[] = [
  { kind: "paragraph", icon: Type, keywords: ["text", "paragraph", "正文", "文本"] },
  { kind: "heading1", icon: Heading1, shortcut: "#", keywords: ["h1", "heading", "title", "标题"] },
  { kind: "heading2", icon: Heading2, shortcut: "##", keywords: ["h2", "heading", "标题"] },
  { kind: "heading3", icon: Heading3, shortcut: "###", keywords: ["h3", "heading", "标题"] },
  { kind: "bulletList", icon: List, shortcut: "-", keywords: ["bullet", "list", "ul", "列表"] },
  { kind: "orderedList", icon: ListOrdered, shortcut: "1.", keywords: ["numbered", "ordered", "list", "ol", "列表"] },
  { kind: "taskList", icon: ListTodo, shortcut: "[ ]", keywords: ["todo", "task", "checkbox", "checklist", "待办"] },
  { kind: "blockquote", icon: Quote, shortcut: ">", keywords: ["quote", "blockquote", "引用"] },
  { kind: "codeBlock", icon: Code, shortcut: "```", keywords: ["code", "代码"] },
  { kind: "mermaid", icon: Workflow, keywords: ["mermaid", "diagram", "flowchart", "chart", "图表", "流程图"] },
  { kind: "table", icon: Table2, keywords: ["table", "grid", "表格"] },
  { kind: "divider", icon: Minus, shortcut: "---", keywords: ["divider", "hr", "separator", "line", "分割线"] },
  { kind: "image", icon: Image, keywords: ["image", "picture", "photo", "img", "图片"], needsUpload: true },
  { kind: "file", icon: Paperclip, keywords: ["file", "attachment", "upload", "文件", "附件"], needsUpload: true },
];

/** Items matching `query` by localized label, alias prefix, or pinyin. */
export function filterBlockMenuItems(
  items: readonly BlockMenuItem[],
  query: string,
  labelOf: (kind: InsertBlockKind) => string,
): BlockMenuItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...items];
  return items.filter((item) => {
    const label = labelOf(item.kind);
    return (
      label.toLowerCase().includes(q) ||
      item.keywords.some((keyword) => keyword.startsWith(q)) ||
      matchesPinyin(label, q)
    );
  });
}

interface BlockInsertListProps {
  items: BlockMenuItem[];
  query: string;
  command: (item: BlockMenuItem) => void;
}

export interface BlockInsertListRef {
  /**
   * `query` is the text typed after `/` as of this key press. It can be ahead
   * of the rendered list when a fast typist hits Enter before React re-renders,
   * and the pick must match what was typed, not what was last drawn.
   */
  onKeyDown: (props: { event: KeyboardEvent; query?: string }) => boolean;
}

export const BlockInsertList = forwardRef<BlockInsertListRef, BlockInsertListProps>(
  function BlockInsertList({ items, query, command }, ref) {
    const { t } = useT("editor");
    const labelOf = useCallback(
      (kind: InsertBlockKind) => t(($) => $.block_menu.blocks[kind]),
      [t],
    );
    const visible = useMemo(
      () => filterBlockMenuItems(items, query, labelOf),
      [items, query, labelOf],
    );
    const [selectedIndex, setSelectedIndex] = useState(0);
    const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);

    useEffect(() => {
      setSelectedIndex(0);
    }, [visible]);

    useEffect(() => {
      itemRefs.current[selectedIndex]?.scrollIntoView({ block: "nearest" });
    }, [selectedIndex]);

    useImperativeHandle(ref, () => ({
      onKeyDown: ({ event, query: liveQuery = query }) => {
        if (isImeComposing(event)) return false;
        const stale = liveQuery !== query;
        const current = stale ? filterBlockMenuItems(items, liveQuery, labelOf) : visible;
        if (current.length === 0) return false;
        const direction = pickerNavigationDirection(event);
        if (direction !== null) {
          const delta = direction === "next" ? 1 : current.length - 1;
          setSelectedIndex((i) => (i + delta) % current.length);
          return true;
        }
        if (isPickerAcceptKey(event)) {
          const item = current[stale ? 0 : selectedIndex];
          if (item) command(item);
          return true;
        }
        return false;
      },
    }));

    // Nothing matched: stay out of the way so a `/` in prose (paths, dates)
    // reads as text and Enter keeps its normal meaning.
    if (visible.length === 0) return null;

    return (
      <div
        data-mc="editor-menu"
        data-kind="block"
        role="listbox"
        aria-label={t(($) => $.block_menu.label)}
        className="w-64 max-h-[min(320px,var(--suggestion-available-height,320px))] overflow-y-auto rounded-md border bg-popover p-1 shadow-md"
      >
        {visible.map((item, index) => {
          const Icon = item.icon;
          return (
            <button
              key={item.kind}
              type="button"
              role="option"
              aria-selected={selectedIndex === index}
              ref={(el) => {
                itemRefs.current[index] = el;
              }}
              className={cn(
                "flex w-full items-center gap-2 rounded-sm px-2 py-1 text-left text-body transition-colors",
                selectedIndex === index ? "bg-accent" : "hover:bg-accent/50",
              )}
              onMouseEnter={() => setSelectedIndex(index)}
              onClick={() => command(item)}
            >
              <span className="flex size-6 shrink-0 items-center justify-center rounded-sm border bg-background text-muted-foreground">
                <Icon className="size-3.5" />
              </span>
              <span className="min-w-0 flex-1 truncate">{labelOf(item.kind)}</span>
              {item.shortcut && (
                <kbd className="shrink-0 font-mono text-caption text-muted-foreground">
                  {item.shortcut}
                </kbd>
              )}
            </button>
          );
        })}
      </div>
    );
  },
);

/**
 * The `/` menu for long-form documents: inserts blocks rather than skills or
 * commands. Image and file are offered only when the host can upload.
 */
export function createBlockInsertSuggestion(
  uploadRef: RefObject<UploadHandler | undefined> | undefined,
): Omit<SuggestionOptions<BlockMenuItem>, "editor"> {
  const pluginKey = new PluginKey("blockInsertSuggestion");
  return {
    char: "/",
    pluginKey,
    // Only open over a `/` the user typed (or asked for through the block
    // handle), never over a pasted path (MUL-5429).
    shouldShow: ({ editor, range }) => isTriggerArmedAt(editor, range.from),
    items: () =>
      BLOCK_MENU_ITEMS.filter((item) => !item.needsUpload || !!uploadRef?.current),
    command: ({ editor, range, props }) => {
      insertBlock(editor, range, props.kind, { upload: uploadRef?.current });
    },
    render: createSuggestionPopupRender<
      BlockMenuItem,
      BlockMenuItem,
      BlockInsertListRef,
      BlockInsertListProps
    >({
      pluginKey,
      component: BlockInsertList,
      placement: "bottom-start",
      getProps: (props) => ({
        items: props.items,
        query: props.query,
        command: props.command,
      }),
      onKeyDown: (ref, props) =>
        ref?.onKeyDown({
          event: props.event,
          // The range spans the trigger and the query: `/image`.
          query: props.view.state.doc.textBetween(props.range.from + 1, props.range.to),
        }) ?? false,
    }),
  };
}
