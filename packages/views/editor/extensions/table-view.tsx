"use client";

import { useMemo } from "react";
import {
  getTextBetween,
  getTextSerializersFromSchema,
  type Editor,
  type TextSerializer,
} from "@tiptap/core";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { NodeViewWrapper, useReactNodeView, type NodeViewProps } from "@tiptap/react";
import { RichContent } from "../../rich-content/rich-content";
import { RichTableFrame } from "../../rich-content/rich-table";
import type { RichTableModel } from "../../rich-content/rich-table-model";
import { BaseMentionExtension, mentionText } from "./mention-extension";

function serialize(editor: Editor, content: ProseMirrorNode[]): string {
  return (
    editor.markdown?.serialize({ type: "doc", content: content.map((node) => node.toJSON()) }).trim() ?? ""
  );
}

// Serialized only when the expanded view renders it, not on every keystroke.
function TableCellContent({ cell, editor }: { cell: ProseMirrorNode; editor: Editor }) {
  const markdown = useMemo(() => {
    const blocks: ProseMirrorNode[] = [];
    cell.forEach((block) => {
      blocks.push(block);
    });
    return serialize(editor, blocks);
  }, [cell, editor]);
  return <RichContent content={markdown} density="compact" />;
}

// The text sorting, filtering and CSV read. Atom nodes such as mentions have
// no text content, so they are written out the way they display.
function cellText(cell: ProseMirrorNode, textSerializers: Record<string, TextSerializer>): string {
  return getTextBetween(cell, { from: 0, to: cell.content.size }, { blockSeparator: " ", textSerializers }).trim();
}

export function tableModelFromNode(node: ProseMirrorNode, editor: Editor): RichTableModel {
  const grid: ProseMirrorNode[][] = [];
  node.forEach((row) => {
    const cells: ProseMirrorNode[] = [];
    row.forEach((cell) => {
      cells.push(cell);
    });
    grid.push(cells);
  });
  const [head = [], ...body] = grid;
  const serializers: Record<string, TextSerializer> = {
    ...getTextSerializersFromSchema(editor.schema),
    [BaseMentionExtension.name]: ({ node: mention }) => mentionText(mention.attrs),
  };
  const text = (cell: ProseMirrorNode) => cellText(cell, serializers);
  const render = (cell: ProseMirrorNode, index: number) => (
    <TableCellContent key={index} cell={cell} editor={editor} />
  );
  return {
    header: head.map(render),
    rows: body.map((row) => row.map(render)),
    headerText: head.map(text),
    rowText: body.map((row) => row.map(text)),
  };
}

/**
 * The editor's table, in the same frame as readonly tables. ProseMirror keeps
 * owning the rows: its `<tbody>` is handed to the frame's `<table>`.
 */
export function TableView({ node, editor }: NodeViewProps) {
  const { nodeViewContentRef } = useReactNodeView();
  const model = useMemo(() => tableModelFromNode(node, editor), [node, editor]);
  return (
    <NodeViewWrapper className="table-node">
      <RichTableFrame
        model={model}
        getMarkdown={() => serialize(editor, [node])}
        tableRef={nodeViewContentRef}
      />
    </NodeViewWrapper>
  );
}
