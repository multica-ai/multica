"use client";

import { NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { formatSlashCommandLabel } from "./slash-command-utils";

export function SlashCommandView({ node }: NodeViewProps) {
  const { label } = node.attrs;
  return (
    <NodeViewWrapper as="span" className="inline">
      <span className="slash-command">/{formatSlashCommandLabel(label)}</span>
    </NodeViewWrapper>
  );
}
