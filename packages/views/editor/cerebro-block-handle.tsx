"use client";

/* eslint-disable i18next/no-literal-string --
   Cerebro fork component: its block-menu labels are English by workspace policy
   (app UI is English). The editor i18n catalog (locales/en/editor.json) is
   upstream zone and owned by track A, so cerebro-only strings are not added
   there; adding keys would also carry no CEREBRO-PATCH marker (JSON). */

/**
 * CEREBRO-PATCH(list-editing): FIR-4707 Phase 6 slice 12 — the hover drag handle
 * and its block menu, shown in the left margin of every block. Built on
 * @tiptap/extension-drag-handle-react, whose `<DragHandle nested>` supplies the
 * vertical drag-to-move and horizontal drag-to-indent gestures and keeps a
 * block's children following it. This component adds the "+" affordance and the
 * click menu — Turn into, Indent, Outdent, Duplicate, Comment, Create issue
 * from item, Delete — wiring each to a command in cerebro-block-actions.ts or a
 * built-in editor command. When a text selection already spans several list
 * items the menu leaves that selection in place, so one choice applies to all
 * of them.
 *
 * Mounted by content-editor.tsx and gated on isListEditingEnabled(), so it
 * ships dark until Phase 9. The pointer gestures and portal positioning are
 * verified by the track-C browser run; the command logic behind the menu is
 * unit-covered in cerebro-block-actions.test.ts.
 */
import { useState } from "react";
import { DragHandle } from "@tiptap/extension-drag-handle-react";
import type { Editor } from "@tiptap/react";
import type { Node as PMNode } from "@tiptap/pm/model";
import { TextSelection } from "@tiptap/pm/state";
import {
  GripVertical,
  Plus,
  Type,
  Heading1,
  Heading2,
  Heading3,
  List,
  ListOrdered,
  ListChecks,
  TextQuote,
  Code,
  Copy,
  Trash2,
  MessageSquare,
  CircleDot,
  IndentIncrease,
  IndentDecrease,
} from "lucide-react";
import { toast } from "sonner";
import { useCreateIssue } from "@multica/core/issues/mutations";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from "@multica/ui/components/ui/dropdown-menu";

interface Target {
  node: PMNode;
  pos: number;
}

export interface CerebroBlockHandleProps {
  editor: Editor;
  /** Parent issue for "Create issue from item"; hidden when absent. */
  currentIssueId?: string | null;
  /** Opens the comment composer for the given text; hidden when absent. */
  onCommentOnSelection?: (text: string) => void;
}

export function CerebroBlockHandle({
  editor,
  currentIssueId,
  onCommentOnSelection,
}: CerebroBlockHandleProps) {
  const [target, setTarget] = useState<Target | null>(null);
  const createIssue = useCreateIssue();

  // Place the cursor inside the targeted block before running a command — but
  // leave a multi-block text selection alone, so the menu applies to all of it.
  function focusTarget() {
    if (!target) return;
    if (!editor.state.selection.empty) return;
    editor
      .chain()
      .focus()
      .command(({ tr, dispatch }) => {
        const sel = TextSelection.near(tr.doc.resolve(target.pos + 1));
        if (dispatch) tr.setSelection(sel);
        return true;
      })
      .run();
  }

  function turnInto(kind: string) {
    focusTarget();
    if (kind === "bulletList" || kind === "orderedList" || kind === "taskList") {
      const itemType = kind === "taskList" ? "taskItem" : "listItem";
      editor.chain().focus().toggleList(kind, itemType).run();
      return;
    }
    // Non-list targets: leave any list first (no-op outside one), then set type.
    const chain = editor
      .chain()
      .focus()
      .command(({ commands }) => {
        commands.liftListItem("taskItem");
        commands.liftListItem("listItem");
        return true;
      });
    switch (kind) {
      case "paragraph":
        chain.setParagraph();
        break;
      case "heading1":
        chain.setHeading({ level: 1 });
        break;
      case "heading2":
        chain.setHeading({ level: 2 });
        break;
      case "heading3":
        chain.setHeading({ level: 3 });
        break;
      case "blockquote":
        chain.toggleBlockquote();
        break;
      case "codeBlock":
        chain.toggleCodeBlock();
        break;
    }
    chain.run();
  }

  function indent() {
    focusTarget();
    editor
      .chain()
      .focus()
      .command(({ commands }) => {
        commands.sinkListItem("taskItem");
        commands.sinkListItem("listItem");
        return true;
      })
      .run();
  }

  function outdent() {
    focusTarget();
    editor
      .chain()
      .focus()
      .command(({ commands }) => {
        commands.liftListItem("taskItem");
        commands.liftListItem("listItem");
        return true;
      })
      .run();
  }

  function duplicate() {
    focusTarget();
    editor.chain().focus().duplicateBlock().run();
  }

  function remove() {
    focusTarget();
    editor.chain().focus().deleteBlock().run();
  }

  function comment() {
    if (!onCommentOnSelection || !target) return;
    const text = target.node.textContent.trim();
    if (text) onCommentOnSelection(text);
  }

  async function createIssueFromItem() {
    if (!currentIssueId || !target) return;
    const title = target.node.textContent.replace(/\s+/g, " ").trim().slice(0, 200);
    if (!title) return;
    try {
      const issue = await createIssue.mutateAsync({
        title,
        parent_issue_id: currentIssueId,
      });
      toast.success(issue.identifier);
    } catch {
      toast.error("Could not create issue");
    }
  }

  function insertBelow() {
    if (!target) return;
    const after = target.pos + target.node.nodeSize;
    editor
      .chain()
      .focus()
      .insertContentAt(after, { type: "paragraph" })
      .setTextSelection(after + 1)
      .run();
  }

  return (
    <DragHandle
      editor={editor}
      nested
      className="cerebro-block-handle"
      onNodeChange={(data) =>
        setTarget(data.node ? { node: data.node, pos: data.pos } : null)
      }
    >
      <div className="flex items-center gap-0.5 text-muted-foreground">
        <button
          type="button"
          aria-label="Insert block below"
          className="cerebro-block-handle__plus rounded p-0.5 hover:bg-muted"
          onClick={insertBelow}
        >
          <Plus className="size-4" />
        </button>
        <DropdownMenu>
          <DropdownMenuTrigger
            aria-label="Block actions"
            className="cerebro-block-handle__grip cursor-grab rounded p-0.5 hover:bg-muted active:cursor-grabbing"
          >
            <GripVertical className="size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-52">
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <Type className="mr-2 size-4" /> Turn into
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent>
                <DropdownMenuItem onClick={() => turnInto("paragraph")}>
                  <Type className="mr-2 size-4" /> Text
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => turnInto("heading1")}>
                  <Heading1 className="mr-2 size-4" /> Heading 1
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => turnInto("heading2")}>
                  <Heading2 className="mr-2 size-4" /> Heading 2
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => turnInto("heading3")}>
                  <Heading3 className="mr-2 size-4" /> Heading 3
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => turnInto("bulletList")}>
                  <List className="mr-2 size-4" /> Bulleted list
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => turnInto("orderedList")}>
                  <ListOrdered className="mr-2 size-4" /> Numbered list
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => turnInto("taskList")}>
                  <ListChecks className="mr-2 size-4" /> Task list
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => turnInto("blockquote")}>
                  <TextQuote className="mr-2 size-4" /> Quote
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => turnInto("codeBlock")}>
                  <Code className="mr-2 size-4" /> Code
                </DropdownMenuItem>
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            <DropdownMenuItem onClick={indent}>
              <IndentIncrease className="mr-2 size-4" /> Indent
            </DropdownMenuItem>
            <DropdownMenuItem onClick={outdent}>
              <IndentDecrease className="mr-2 size-4" /> Outdent
            </DropdownMenuItem>
            <DropdownMenuItem onClick={duplicate}>
              <Copy className="mr-2 size-4" /> Duplicate
            </DropdownMenuItem>
            {onCommentOnSelection && (
              <DropdownMenuItem onClick={comment}>
                <MessageSquare className="mr-2 size-4" /> Comment
              </DropdownMenuItem>
            )}
            {currentIssueId && (
              <DropdownMenuItem onClick={createIssueFromItem}>
                <CircleDot className="mr-2 size-4" /> Create issue from item
              </DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={remove}
              className="text-destructive focus:text-destructive"
            >
              <Trash2 className="mr-2 size-4" /> Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </DragHandle>
  );
}
