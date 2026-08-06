"use client";

import { useCallback, useEffect, useState } from "react";
import type { Editor } from "@tiptap/react";
import {
  Bold,
  Code,
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
import type { EditorToolbarActionId } from "./editor-toolbar-preferences";
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
  taskList: ListTodo,
  comment: MessageSquarePlus,
  italic: Italic,
  strike: Strikethrough,
  bulletList: List,
  orderedList: ListOrdered,
  blockquote: Quote,
  code: Code,
  indent: IndentIncrease,
  outdent: IndentDecrease,
};

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
    taskList: "taskList",
    italic: "italic",
    strike: "strike",
    bulletList: "bulletList",
    orderedList: "orderedList",
    blockquote: "blockquote",
    code: "code",
  };
  const mark = marks[action];
  return mark ? editor.isActive(mark) : false;
}

function listItemType(editor: Editor): "taskItem" | "listItem" {
  return editor.isActive("taskItem") ? "taskItem" : "listItem";
}

function ToolbarButton({
  action,
  editor,
  onCommentOnSelection,
}: {
  action: Exclude<EditorToolbarActionId, "link" | "heading">;
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
  const itemType = listItemType(editor);
  const disabled =
    (action === "comment" &&
      (!onCommentOnSelection || editor.state.selection.empty || !selectedText)) ||
    (action === "indent" && !editor.can().sinkListItem(itemType)) ||
    (action === "outdent" && !editor.can().liftListItem(itemType));

  const run = () => {
    if (action === "comment") {
      if (selectedText) onCommentOnSelection?.(selectedText);
      return;
    }

    const chain = formattingChain(editor).focus();
    switch (action) {
      case "bold":
        chain.toggleBold().run();
        break;
      case "highlight":
        chain.toggleHighlight().run();
        break;
      case "taskList":
        chain.toggleTaskList().run();
        break;
      case "italic":
        chain.toggleItalic().run();
        break;
      case "strike":
        chain.toggleStrike().run();
        break;
      case "bulletList":
        chain.toggleBulletList().run();
        break;
      case "orderedList":
        chain.toggleOrderedList().run();
        break;
      case "blockquote":
        chain.toggleBlockquote().run();
        break;
      case "code":
        chain.toggleCode().run();
        break;
      case "indent":
        chain.sinkListItem(itemType).run();
        break;
      case "outdent":
        chain.liftListItem(itemType).run();
        break;
    }
  };

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

function HeadingControl({ editor }: { editor: Editor }) {
  const activeLevel = ([1, 2, 3] as const).find((level) =>
    editor.isActive("heading", { level }),
  );
  const label = activeLevel ? `Heading ${activeLevel}` : "Text style";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="Text style"
            title={label}
            aria-pressed={Boolean(activeLevel)}
          />
        }
      >
        <Pilcrow className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem
          onClick={() => formattingChain(editor).focus().setParagraph().run()}
        >
          <Pilcrow /> Paragraph
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
    <Popover open={open} onOpenChange={handleOpen}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="Link"
            title="Link"
            aria-pressed={isActive}
            className="aria-pressed:bg-muted"
          />
        }
      >
        <Link2 className="size-4" />
      </PopoverTrigger>
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
  const { order } = useEditorToolbarOrder();
  const [, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision((value) => value + 1), []);

  useEffect(() => {
    if (!editor) return;
    editor.on("transaction", refresh);
    editor.on("selectionUpdate", refresh);
    return () => {
      editor.off("transaction", refresh);
      editor.off("selectionUpdate", refresh);
    };
  }, [editor, refresh]);

  return (
    <div
      role="toolbar"
      aria-label="Formatting toolbar"
      className={cn(
        "flex min-h-11 items-center gap-0.5 overflow-x-auto border-b bg-muted/20 px-2 py-1.5",
        className,
      )}
    >
      {order.map((action) => {
        const Icon = ACTION_ICONS[action];
        if (!editor) {
          return (
            <Button
              key={action}
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={EDITOR_TOOLBAR_ACTION_LABELS[action]}
              title={EDITOR_TOOLBAR_ACTION_LABELS[action]}
              disabled
            >
              <Icon className="size-4" />
            </Button>
          );
        }
        if (action === "link") {
          return <LinkControl key={action} editor={editor} />;
        }
        if (action === "heading") {
          return <HeadingControl key={action} editor={editor} />;
        }
        return (
          <ToolbarButton
            key={action}
            action={action}
            editor={editor}
            onCommentOnSelection={onCommentOnSelection}
          />
        );
      })}
    </div>
  );
}
