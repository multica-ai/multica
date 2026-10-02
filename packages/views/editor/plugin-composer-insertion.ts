import type { Editor } from "@tiptap/react";
import type { PluginInstallationListResponse } from "@multica/core/types";
import { collectComposerCommands, type PluginComposerContext } from "../plugins/plugin-composer-commands";
import type { PluginComposerCommandTarget } from "../plugins/plugin-composer-commands";

export interface PluginComposerInvocation {
  target: PluginComposerCommandTarget;
  workspaceId: string;
  editor: Editor;
  doc: Editor["state"]["doc"];
  range: { from: number; to: number };
  text: string;
}

export function capturePluginComposerInvocation(
  target: PluginComposerCommandTarget,
  workspaceId: string,
  editor: Editor,
  range: { from: number; to: number },
): PluginComposerInvocation {
  return {
    target,
    workspaceId,
    editor,
    doc: editor.state.doc,
    range: { from: range.from, to: range.to },
    text: editor.state.doc.textBetween(range.from, range.to),
  };
}

export function isPluginComposerInvocationInstalled(
  invocation: PluginComposerInvocation,
  installations: PluginInstallationListResponse,
  context: PluginComposerContext,
  platform: string,
): boolean {
  return collectComposerCommands(installations.plugins, context, platform).some((target) =>
    target.id === invocation.target.id &&
    target.installation.package_version_id === invocation.target.installation.package_version_id);
}

/** Replace only the exact draft and document that opened the plugin modal. */
export function insertPluginComposerMarkdown(
  invocation: PluginComposerInvocation,
  liveEditor: Editor | null,
  markdown: string,
): boolean {
  const { editor, range } = invocation;
  if (editor !== liveEditor || editor.isDestroyed || editor.state.doc !== invocation.doc ||
      range.from < 0 || range.to > editor.state.doc.content.size ||
      editor.state.doc.textBetween(range.from, range.to) !== invocation.text) return false;

  return editor.chain().focus().insertContentAt(range, markdown, { contentType: "markdown" }).run();
}
