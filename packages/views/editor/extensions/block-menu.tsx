"use client";

/**
 * The `/` block menu of document editors (issue and project descriptions):
 * turn the current block into another type, or insert a block Markdown can
 * hold. The same commands back the block handle's menu.
 */

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
import type { ChainedCommands, Editor, Range } from "@tiptap/core";
import { PluginKey, TextSelection } from "@tiptap/pm/state";
import type { SuggestionOptions } from "@tiptap/suggestion";
import {
  ChevronRight,
  Code,
  Heading1,
  Heading2,
  Heading3,
  Image,
  List,
  ListOrdered,
  ListTodo,
  Minus,
  Pilcrow,
  Table,
  TextQuote,
  Workflow,
  type LucideProps,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { isImeComposing } from "@multica/core/utils";
import { useT } from "../../i18n";
import { isPickerAcceptKey, pickerNavigationDirection } from "../../common/picker-keys";
import { createSuggestionPopupRender } from "./suggestion-popup";
import { isTriggerArmedAt } from "./suggestion-trigger-arming";
import { matchesPinyin } from "./pinyin-match";
import { emptyDetailsContent } from "./details";
import { uploadAndInsertFile } from "./file-upload";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";

export type BlockTypeId =
  | "paragraph"
  | "heading1"
  | "heading2"
  | "heading3"
  | "bulletList"
  | "orderedList"
  | "taskList"
  | "blockquote";

export type BlockMenuItemId = BlockTypeId | "divider" | "table" | "codeBlock" | "mermaid" | "details" | "file";

export interface BlockMenuItem {
  id: BlockMenuItemId;
  /** Block types sit in the icon row; inserts are listed with their names. */
  group: "turn_into" | "insert";
  icon: ComponentType<LucideProps>;
  /** Matched by prefix in every language, alongside the translated label. */
  keywords: string[];
}

export const BLOCK_MENU_ITEMS: BlockMenuItem[] = [
  { id: "paragraph", group: "turn_into", icon: Pilcrow, keywords: ["text", "paragraph"] },
  { id: "heading1", group: "turn_into", icon: Heading1, keywords: ["h1", "heading", "title"] },
  { id: "heading2", group: "turn_into", icon: Heading2, keywords: ["h2", "heading"] },
  { id: "heading3", group: "turn_into", icon: Heading3, keywords: ["h3", "heading"] },
  { id: "bulletList", group: "turn_into", icon: List, keywords: ["bullet", "list", "ul"] },
  { id: "orderedList", group: "turn_into", icon: ListOrdered, keywords: ["numbered", "ordered", "list", "ol"] },
  { id: "taskList", group: "turn_into", icon: ListTodo, keywords: ["todo", "task", "checkbox", "list"] },
  { id: "blockquote", group: "turn_into", icon: TextQuote, keywords: ["quote", "blockquote"] },
  { id: "divider", group: "insert", icon: Minus, keywords: ["divider", "hr", "line", "separator"] },
  { id: "table", group: "insert", icon: Table, keywords: ["table", "grid"] },
  { id: "codeBlock", group: "insert", icon: Code, keywords: ["code", "snippet"] },
  { id: "mermaid", group: "insert", icon: Workflow, keywords: ["mermaid", "diagram", "chart", "flowchart"] },
  { id: "details", group: "insert", icon: ChevronRight, keywords: ["details", "toggle", "collapse", "fold"] },
  { id: "file", group: "insert", icon: Image, keywords: ["image", "file", "upload", "attachment"] },
];

export const BLOCK_TYPE_ITEMS = BLOCK_MENU_ITEMS.filter((item) => item.group === "turn_into");

type Translate = ReturnType<typeof useT<"editor">>["t"];

export function blockMenuLabel(t: Translate, id: BlockMenuItemId): string {
  switch (id) {
    case "paragraph":
      return t(($) => $.bubble_menu.heading_dropdown.normal_text);
    case "heading1":
      return t(($) => $.bubble_menu.heading_dropdown.heading_1);
    case "heading2":
      return t(($) => $.bubble_menu.heading_dropdown.heading_2);
    case "heading3":
      return t(($) => $.bubble_menu.heading_dropdown.heading_3);
    case "bulletList":
      return t(($) => $.bubble_menu.list_dropdown.bullet_list);
    case "orderedList":
      return t(($) => $.bubble_menu.list_dropdown.ordered_list);
    case "taskList":
      return t(($) => $.bubble_menu.list_dropdown.task_list);
    case "blockquote":
      return t(($) => $.bubble_menu.quote);
    case "divider":
      return t(($) => $.block_menu.divider);
    case "table":
      return t(($) => $.block_menu.table);
    case "codeBlock":
      return t(($) => $.block_menu.code_block);
    case "mermaid":
      return t(($) => $.block_menu.mermaid);
    case "details":
      return t(($) => $.block_menu.details);
    case "file":
      return t(($) => $.block_menu.file);
  }
}

export function matchesBlockMenuItem(item: BlockMenuItem, label: string, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return (
    label.toLowerCase().includes(q) ||
    matchesPinyin(label, q) ||
    item.keywords.some((keyword) => keyword.startsWith(q))
  );
}

/** Moves the caret into the summary of the details block holding it. */
function focusDetailsSummary(tr: import("@tiptap/pm/state").Transaction): boolean {
  const { $from } = tr.selection;
  for (let depth = $from.depth; depth > 0; depth--) {
    if ($from.node(depth).type.name === "details") {
      tr.setSelection(TextSelection.create(tr.doc, $from.before(depth) + 2));
      return true;
    }
  }
  return true;
}

/** Adds the block change for `id` to a chain whose selection is in the block. */
export function applyBlockCommand(chain: ChainedCommands, id: Exclude<BlockMenuItemId, "file">): ChainedCommands {
  switch (id) {
    case "paragraph":
      return chain.setParagraph();
    case "heading1":
      return chain.setHeading({ level: 1 });
    case "heading2":
      return chain.setHeading({ level: 2 });
    case "heading3":
      return chain.setHeading({ level: 3 });
    case "bulletList":
      return chain.toggleBulletList();
    case "orderedList":
      return chain.toggleOrderedList();
    case "taskList":
      return chain.toggleTaskList();
    case "blockquote":
      return chain.toggleBlockquote();
    case "divider":
      return chain.setHorizontalRule();
    case "table":
      return chain.insertTable({ rows: 3, cols: 3, withHeaderRow: true });
    case "codeBlock":
      return chain.setCodeBlock();
    case "mermaid":
      return chain.setCodeBlock({ language: "mermaid" });
    case "details":
      return chain.insertContent(emptyDetailsContent()).command(({ tr }) => focusDetailsSummary(tr));
  }
}

type UploadHandler = (file: File, uploadId: string) => Promise<UploadResult | null>;

function pickFiles(editor: Editor, upload: UploadHandler) {
  const input = document.createElement("input");
  input.type = "file";
  input.multiple = true;
  input.addEventListener("change", () => {
    for (const file of Array.from(input.files ?? [])) {
      void uploadAndInsertFile(editor, file, upload);
    }
  });
  input.click();
}

/** Runs a block menu pick, replacing the typed `/query` when there is one. */
export function runBlockMenuItem(
  editor: Editor,
  id: BlockMenuItemId,
  range: Range | null,
  upload?: UploadHandler,
): void {
  const chain = editor.chain().focus();
  if (range) chain.deleteRange(range);
  if (id === "file") {
    chain.run();
    if (upload) pickFiles(editor, upload);
    return;
  }
  applyBlockCommand(chain, id).run();
}

interface BlockMenuListProps {
  items: BlockMenuItem[];
  query: string;
  command: (item: BlockMenuItem) => void;
}

export interface BlockMenuListRef {
  onKeyDown: (props: { event: KeyboardEvent }) => boolean;
}

export const BlockMenuList = forwardRef<BlockMenuListRef, BlockMenuListProps>(function BlockMenuList(
  { items, query, command },
  ref,
) {
  const { t } = useT("editor");
  const filtering = query.trim() !== "";
  const visible = useMemo(
    () => items.filter((item) => matchesBlockMenuItem(item, blockMenuLabel(t, item.id), query)),
    [items, query, t],
  );
  // With no query the block types render as an icon row ahead of the list.
  const rowCount = filtering ? 0 : visible.filter((item) => item.group === "turn_into").length;
  const ordered = useMemo(
    () => (filtering ? visible : [...visible.filter((i) => i.group === "turn_into"), ...visible.filter((i) => i.group === "insert")]),
    [filtering, visible],
  );

  const [selected, setSelected] = useState(0);
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);
  useEffect(() => {
    setSelected(rowCount < ordered.length ? rowCount : 0);
  }, [ordered, rowCount]);
  useEffect(() => {
    itemRefs.current[selected]?.scrollIntoView({ block: "nearest" });
  }, [selected]);

  const pick = useCallback(
    (index: number) => {
      const item = ordered[index];
      if (item) command(item);
    },
    [ordered, command],
  );

  useImperativeHandle(ref, () => ({
    onKeyDown: ({ event }) => {
      if (isImeComposing(event) || ordered.length === 0) return false;
      const inRow = selected < rowCount;
      if (inRow && (event.key === "ArrowLeft" || event.key === "ArrowRight")) {
        const delta = event.key === "ArrowRight" ? 1 : rowCount - 1;
        setSelected((selected + delta) % rowCount);
        return true;
      }
      const direction = pickerNavigationDirection(event);
      if (direction !== null) {
        // The icon row is one stop on the way up and down the list.
        const stops = Array.from(ordered.keys()).filter((i) => i === 0 || i >= rowCount);
        const current = inRow ? 0 : stops.indexOf(selected);
        const next = (current + (direction === "next" ? 1 : stops.length - 1)) % stops.length;
        setSelected(stops[next] ?? 0);
        return true;
      }
      if (isPickerAcceptKey(event)) {
        pick(selected);
        return true;
      }
      return false;
    },
  }));

  if (ordered.length === 0) return null;

  const heading = "px-1.5 pt-1 pb-0.5 text-caption font-medium text-muted-foreground";
  const insertStart = rowCount;

  return (
    <div
      role="listbox"
      aria-label={t(($) => $.block_menu.insert)}
      className="w-72 overflow-y-auto rounded-lg bg-surface-raised p-1 text-popover-foreground shadow-[var(--menu-shadow)] ring-1 ring-surface-border max-h-[min(360px,var(--suggestion-available-height,360px))]"
    >
      {rowCount > 0 && (
        <>
          <div className={heading}>{t(($) => $.block_menu.turn_into)}</div>
          <div className="flex gap-0.5 px-0.5 pb-1">
            {ordered.slice(0, rowCount).map((item, index) => {
              const Icon = item.icon;
              const label = blockMenuLabel(t, item.id);
              return (
                <button
                  key={item.id}
                  ref={(el) => {
                    itemRefs.current[index] = el;
                  }}
                  type="button"
                  role="option"
                  aria-selected={selected === index}
                  aria-label={label}
                  title={label}
                  className={cn(
                    "flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground",
                    selected === index && "bg-accent text-accent-foreground",
                  )}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => pick(index)}
                >
                  <Icon className="size-4" />
                </button>
              );
            })}
          </div>
          {ordered.length > rowCount && <div className={heading}>{t(($) => $.block_menu.insert)}</div>}
        </>
      )}
      {ordered.slice(insertStart).map((item, offset) => {
        const index = insertStart + offset;
        const Icon = item.icon;
        return (
          <button
            key={item.id}
            ref={(el) => {
              itemRefs.current[index] = el;
            }}
            type="button"
            role="option"
            aria-selected={selected === index}
            className={cn(
              "flex w-full items-center gap-2 rounded-md px-1.5 py-1 text-left text-body transition-colors hover:bg-accent hover:text-accent-foreground",
              selected === index && "bg-accent text-accent-foreground",
            )}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => pick(index)}
          >
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            {blockMenuLabel(t, item.id)}
          </button>
        );
      })}
    </div>
  );
});

export interface BlockMenuSuggestionOptions {
  onUploadFileRef?: RefObject<UploadHandler | undefined>;
}

export function createBlockMenuSuggestion(
  options: BlockMenuSuggestionOptions = {},
): Omit<SuggestionOptions<BlockMenuItem>, "editor"> {
  const pluginKey = new PluginKey("blockMenuSuggestion");

  return {
    char: "/",
    pluginKey,
    // Only open over a `/` the user actually typed, so a pasted path
    // (`/usr/local/bin`) never opens the menu (MUL-5429).
    shouldShow: ({ editor, range }) => isTriggerArmedAt(editor, range.from),
    allow: ({ state, range }) => !state.doc.resolve(range.from).parent.type.spec.code,
    items: () => BLOCK_MENU_ITEMS.filter((item) => item.id !== "file" || !!options.onUploadFileRef?.current),
    command: ({ editor, range, props }) => {
      runBlockMenuItem(editor, props.id, range, options.onUploadFileRef?.current);
    },
    render: createSuggestionPopupRender<BlockMenuItem, BlockMenuItem, BlockMenuListRef, BlockMenuListProps>({
      pluginKey,
      component: BlockMenuList,
      placement: "bottom-start",
      getProps: (props) => ({ items: props.items, query: props.query, command: props.command }),
      onKeyDown: (ref, props) => ref?.onKeyDown(props) ?? false,
    }),
  };
}
