"use client";

import katex from "katex";
import { ReactNodeViewRenderer, NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { BlockMathSchema, InlineMathSchema } from "../schema/math";

function renderMath(expression: string, displayMode: boolean): string {
  return katex.renderToString(expression, {
    displayMode,
    output: "htmlAndMathml",
    strict: "warn",
    throwOnError: false,
  });
}

function InlineMathView({ node }: NodeViewProps) {
  const expression = String(node.attrs.expression ?? "");
  return (
    <NodeViewWrapper
      as="span"
      className="math-node inline"
      data-type="inline-math"
      data-expression={expression}
      contentEditable={false}
    >
      <span dangerouslySetInnerHTML={{ __html: renderMath(expression, false) }} />
    </NodeViewWrapper>
  );
}

function BlockMathView({ node }: NodeViewProps) {
  const expression = String(node.attrs.expression ?? "");
  return (
    <NodeViewWrapper
      as="div"
      className="math-node block"
      data-type="block-math"
      data-expression={expression}
      contentEditable={false}
    >
      <div dangerouslySetInnerHTML={{ __html: renderMath(expression, true) }} />
    </NodeViewWrapper>
  );
}

export const InlineMathExtension = InlineMathSchema.extend({
  addNodeView() {
    return ReactNodeViewRenderer(InlineMathView);
  },
});

export const BlockMathExtension = BlockMathSchema.extend({
  addNodeView() {
    return ReactNodeViewRenderer(BlockMathView);
  },
});
