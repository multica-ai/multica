"use client";

import * as React from "react";
import type { Editor } from "@tiptap/react";
import type { LucideIcon } from "lucide-react";
import {
  ClipboardPaste,
  Code2,
  Copy,
  Heading1,
  Heading2,
  Heading3,
  IndentDecrease,
  IndentIncrease,
  List,
  ListChecks,
  ListOrdered,
  ListPlus,
  MessageSquarePlus,
  Pilcrow,
  Quote,
  Scissors,
  Shapes,
  Trash2,
} from "lucide-react";
import {
  ContextMenu,
  ContextMenuTrigger,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubTrigger,
  ContextMenuSubContent,
} from "@multica/ui/components/ui/context-menu";

// ContentEditor installs these Tiptap extensions at runtime; their command
// declarations live in the editor package, so this shared Cerebro package
// describes only the chain surface it consumes (same shape the formatting
// toolbar uses).
type BlockChain = {
  focus: () => BlockChain;
  setParagraph: () => BlockChain;
  toggleHeading: (attrs: { level: 1 | 2 | 3 }) => BlockChain;
  toggleBulletList: () => BlockChain;
  toggleOrderedList: () => BlockChain;
  toggleTaskList: () => BlockChain;
  toggleBlockquote: () => BlockChain;
  toggleCodeBlock: () => BlockChain;
  // Indent / outdent apply to whichever list item the cursor sits in; running
  // both item types covers bullet/ordered and task lists, and no-ops elsewhere.
  sinkListItem: (type: "listItem" | "taskItem") => BlockChain;
  liftListItem: (type: "listItem" | "taskItem") => BlockChain;
  // Registered by track B's CerebroBlockActions (extensions/index.ts), so these
  // exist on the editor only while the toolbar flag is on.
  duplicateBlock: () => BlockChain;
  deleteBlock: () => BlockChain;
  deleteSelection: () => BlockChain;
  insertContent: (value: string) => BlockChain;
  run: () => boolean;
};

function blockChain(editor: Editor): BlockChain {
  return editor.chain() as unknown as BlockChain;
}

export type EditorContextOption = {
  key: string;
  label: string;
  icon: LucideIcon;
  run: (editor: Editor) => void;
};

export type EditorContextItem = {
  key: string;
  label: string;
  icon: LucideIcon;
  /** A leaf action. Mutually exclusive with `options`. */
  run?: (editor: Editor, selectedText: string) => void;
  /** A submenu of block types (Turn into). */
  options?: EditorContextOption[];
  separatorBefore?: boolean;
};

const TURN_INTO: EditorContextOption[] = [
  {
    key: "paragraph",
    label: "Text",
    icon: Pilcrow,
    run: (e) => blockChain(e).focus().setParagraph().run(),
  },
  {
    key: "heading-1",
    label: "Heading 1",
    icon: Heading1,
    run: (e) => blockChain(e).focus().toggleHeading({ level: 1 }).run(),
  },
  {
    key: "heading-2",
    label: "Heading 2",
    icon: Heading2,
    run: (e) => blockChain(e).focus().toggleHeading({ level: 2 }).run(),
  },
  {
    key: "heading-3",
    label: "Heading 3",
    icon: Heading3,
    run: (e) => blockChain(e).focus().toggleHeading({ level: 3 }).run(),
  },
  {
    key: "bulletList",
    label: "Bulleted list",
    icon: List,
    run: (e) => blockChain(e).focus().toggleBulletList().run(),
  },
  {
    key: "orderedList",
    label: "Numbered list",
    icon: ListOrdered,
    run: (e) => blockChain(e).focus().toggleOrderedList().run(),
  },
  {
    key: "blockquote",
    label: "Quote",
    icon: Quote,
    run: (e) => blockChain(e).focus().toggleBlockquote().run(),
  },
  {
    key: "codeBlock",
    label: "Code block",
    icon: Code2,
    run: (e) => blockChain(e).focus().toggleCodeBlock().run(),
  },
];

export function buildEditorContextItems({
  onComment,
  onCreateIssue,
}: {
  onComment?: (selectedText: string) => void;
  onCreateIssue?: (selectedText: string) => void;
}): EditorContextItem[] {
  const entries: Array<EditorContextItem | false | undefined> = [
    onComment && {
      key: "comment",
      label: "Comment",
      icon: MessageSquarePlus,
      run: (_editor: Editor, text: string) => onComment(text),
    },
    {
      key: "cut",
      label: "Cut",
      icon: Scissors,
      separatorBefore: Boolean(onComment),
      run: (editor: Editor, text: string) => {
        void navigator.clipboard?.writeText(text);
        blockChain(editor).focus().deleteSelection().run();
      },
    },
    {
      key: "copy",
      label: "Copy",
      icon: Copy,
      run: (_editor: Editor, text: string) => {
        void navigator.clipboard?.writeText(text);
      },
    },
    {
      key: "paste-plain",
      label: "Paste as plain text",
      icon: ClipboardPaste,
      run: (editor: Editor) => {
        void navigator.clipboard?.readText().then((text) => {
          if (text) blockChain(editor).focus().insertContent(text).run();
        });
      },
    },
    {
      key: "turn-into",
      label: "Turn into",
      icon: Shapes,
      separatorBefore: true,
      options: TURN_INTO,
    },
    onCreateIssue && {
      key: "create-issue",
      label: "Create issue from selection",
      icon: ListPlus,
      separatorBefore: true,
      run: (_editor: Editor, text: string) => onCreateIssue(text),
    },
  ];
  return entries.filter((i): i is EditorContextItem => Boolean(i));
}

// The no-selection menu mirrors the Phase 6 block menu (the hover drag handle),
// so right-clicking plain text offers the same actions. Task list is added to
// Turn into here, matching that menu.
const BLOCK_TURN_INTO: EditorContextOption[] = [
  ...TURN_INTO.slice(0, 6),
  {
    key: "taskList",
    label: "Task list",
    icon: ListChecks,
    run: (e) => blockChain(e).focus().toggleTaskList().run(),
  },
  ...TURN_INTO.slice(6),
];

export function buildBlockContextItems({
  onComment,
  onCreateIssue,
}: {
  onComment?: (blockText: string) => void;
  onCreateIssue?: (blockText: string) => void;
}): EditorContextItem[] {
  const entries: Array<EditorContextItem | false | undefined> = [
    {
      key: "turn-into",
      label: "Turn into",
      icon: Shapes,
      options: BLOCK_TURN_INTO,
    },
    {
      key: "indent",
      label: "Indent",
      icon: IndentIncrease,
      run: (editor: Editor) =>
        blockChain(editor)
          .focus()
          .sinkListItem("taskItem")
          .sinkListItem("listItem")
          .run(),
    },
    {
      key: "outdent",
      label: "Outdent",
      icon: IndentDecrease,
      run: (editor: Editor) =>
        blockChain(editor)
          .focus()
          .liftListItem("taskItem")
          .liftListItem("listItem")
          .run(),
    },
    {
      key: "duplicate",
      label: "Duplicate",
      icon: Copy,
      run: (editor: Editor) => blockChain(editor).focus().duplicateBlock().run(),
    },
    onComment && {
      key: "comment",
      label: "Comment",
      icon: MessageSquarePlus,
      run: (_editor: Editor, text: string) => onComment(text),
    },
    onCreateIssue && {
      key: "create-issue",
      label: "Create issue from item",
      icon: ListPlus,
      run: (_editor: Editor, text: string) => onCreateIssue(text),
    },
    {
      key: "delete",
      label: "Delete",
      icon: Trash2,
      separatorBefore: true,
      run: (editor: Editor) => blockChain(editor).focus().deleteBlock().run(),
    },
  ];
  return entries.filter((i): i is EditorContextItem => Boolean(i));
}

/**
 * FIR-4028 slice 8 — right-click inside the editor.
 *
 *  - With text selected the menu leads with Comment, the action people were
 *    reaching for and could previously only find in the selection bubble.
 *  - With no selection it shows the Phase 6 block menu (Turn into, Indent,
 *    Outdent, Duplicate, Comment, Create issue, Delete) — but only once track
 *    B's block-action commands are registered, which happens only when the
 *    toolbar flag is on. While the flag is off the browser's own menu opens, as
 *    before.
 *  - Shift+right-click always yields the browser's native menu, so spellcheck,
 *    dictation and the platform clipboard stay reachable.
 *  - Link is not repeated here: the formatting toolbar owns it, with ⌘K.
 */
export function EditorContextMenu({
  editor,
  onComment,
  onCreateIssue,
  className,
  children,
}: {
  editor: Editor | null;
  onComment?: (selectedText: string) => void;
  onCreateIssue?: (selectedText: string) => void;
  // Applied to both wrapper elements, so the editor keeps the flex sizing it
  // had before it was wrapped.
  className?: string;
  children: React.ReactNode;
}) {
  const [hasSelection, setHasSelection] = React.useState(false);
  React.useEffect(() => {
    if (!editor) {
      setHasSelection(false);
      return;
    }
    const sync = () => setHasSelection(!editor.state.selection.empty);
    sync();
    editor.on("selectionUpdate", sync);
    editor.on("transaction", sync);
    return () => {
      editor.off("selectionUpdate", sync);
      editor.off("transaction", sync);
    };
  }, [editor]);

  // Shift is tracked from the keyboard rather than read off the contextmenu
  // event, because Base UI reads `disabled` synchronously when the event fires —
  // so the value has to be settled before the right-click, not during it.
  const [shiftHeld, setShiftHeld] = React.useState(false);
  React.useEffect(() => {
    const onDown = (e: KeyboardEvent) => e.key === "Shift" && setShiftHeld(true);
    const onUp = (e: KeyboardEvent) => e.key === "Shift" && setShiftHeld(false);
    window.addEventListener("keydown", onDown);
    window.addEventListener("keyup", onUp);
    return () => {
      window.removeEventListener("keydown", onDown);
      window.removeEventListener("keyup", onUp);
    };
  }, []);

  // The block menu's Duplicate/Delete come from track B's CerebroBlockActions.
  // Its presence on the editor is the flag itself: registered only when the
  // toolbar flag is on. Absent → the browser keeps its own no-selection menu.
  const blockActionsReady =
    typeof (editor as unknown as { commands?: Record<string, unknown> } | null)
      ?.commands?.duplicateBlock === "function";

  const items = hasSelection
    ? buildEditorContextItems({ onComment, onCreateIssue })
    : buildBlockContextItems({ onComment, onCreateIssue });

  const active = !shiftHeld && (hasSelection || blockActionsReady);

  const menuText = () => {
    if (!editor) return "";
    if (hasSelection) {
      const { from, to } = editor.state.selection;
      return editor.state.doc.textBetween(from, to, " ");
    }
    const sel = editor.state.selection as unknown as {
      $from?: { parent?: { textContent?: string } };
    };
    return sel.$from?.parent?.textContent ?? "";
  };

  return (
    // disabled lives on the root: Base UI reads it in both the trigger's own
    // handler and its document-level one. Off (Shift held, or nothing to show)
    // leaves the right-click unprevented so the browser opens its own menu.
    <ContextMenu disabled={!active}>
      {/* The shared trigger sets `select-none`, which is right for a normal
          context-menu target and wrong for one wrapping an editor: it is
          inherited by the whole document body and kills selecting text with the
          mouse. `select-text` wins the merge and puts selection back. */}
      <ContextMenuTrigger
        className="select-text"
        render={<div className={className} />}
      >
        {children}
      </ContextMenuTrigger>
      <ContextMenuContent className="w-56">
        {items.map((item) => (
          <React.Fragment key={item.key}>
            {item.separatorBefore && <ContextMenuSeparator />}
            {item.options ? (
              <ContextMenuSub>
                <ContextMenuSubTrigger>
                  <item.icon className="size-4" />
                  {item.label}
                </ContextMenuSubTrigger>
                <ContextMenuSubContent>
                  {item.options.map((option) => (
                    <ContextMenuItem
                      key={option.key}
                      onClick={() => editor && option.run(editor)}
                    >
                      <option.icon className="size-4" />
                      {option.label}
                    </ContextMenuItem>
                  ))}
                </ContextMenuSubContent>
              </ContextMenuSub>
            ) : (
              <ContextMenuItem
                onClick={() => editor && item.run?.(editor, menuText())}
              >
                <item.icon className="size-4" />
                {item.label}
              </ContextMenuItem>
            )}
          </React.Fragment>
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
}
