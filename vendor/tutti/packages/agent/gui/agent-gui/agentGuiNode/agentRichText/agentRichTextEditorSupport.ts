import { Extension, type Editor, type JSONContent } from "@tiptap/core";
import {
  DOMParser as ProseMirrorDOMParser,
  DOMSerializer,
  type Node as ProseMirrorNode
} from "@tiptap/pm/model";
import {
  Plugin,
  PluginKey,
  TextSelection,
  type EditorState,
  type Transaction
} from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import type { AgentFileMentionKind } from "./agentFileMentionExtension";
import { AGENT_RICH_TEXT_CARET_ANCHOR } from "./agentRichTextCaretAnchor";
import { agentRichTextContentToPromptText } from "./agentRichTextDocument";

import { shouldStagePastedText } from "@multica/tutti-agent-gui-host/large-text-paste";

export const AGENT_RICH_TEXT_LARGE_PASTE_MIN_CHARS = 5_000;

export function isAgentRichTextLargeTextPaste(text: string): boolean {
  return shouldStagePastedText(text, AGENT_RICH_TEXT_LARGE_PASTE_MIN_CHARS);
}

export type AgentRichTextTextPasteKind =
  | "empty"
  | "large-text"
  | "plain-text"
  | "structured-mention";

export interface AgentRichTextClipboardPayload {
  html: string;
  text: string;
}

export function serializeAgentRichTextSelection(
  editor: Editor
): AgentRichTextClipboardPayload | null {
  const selection = readPromptSelection(editor);
  if (!selection.text) {
    return null;
  }
  const slice = editor.state.doc.slice(selection.from, selection.to);
  const wrapper = document.createElement("div");
  wrapper.appendChild(
    DOMSerializer.fromSchema(editor.schema).serializeFragment(slice.content)
  );
  return { html: wrapper.innerHTML, text: selection.text };
}

export function parseAgentRichTextClipboardHtml(
  editor: Editor,
  html: string
): JSONContent[] | null {
  if (!html.includes("data-agent-file-mention")) {
    return null;
  }
  const wrapper = document.createElement("div");
  wrapper.innerHTML = html;
  const slice = ProseMirrorDOMParser.fromSchema(editor.schema).parseSlice(
    wrapper,
    { preserveWhitespace: true }
  );
  return slice.content.toJSON();
}

export function insertAgentRichTextClipboardHtml(
  editor: Editor,
  html: string
): boolean {
  if (!editor.isFocused) {
    editor.commands.setTextSelection(editor.state.doc.content.size);
  }
  const content = parseAgentRichTextClipboardHtml(editor, html);
  if (!content) {
    return false;
  }
  editor.commands.insertContent(content);
  return true;
}

export function classifyAgentRichTextTextPaste(
  text: string,
  html: string,
  largeTextHandlingAvailable: boolean
): AgentRichTextTextPasteKind {
  if (!text) {
    return "empty";
  }
  if (largeTextHandlingAvailable && isAgentRichTextLargeTextPaste(text)) {
    return "large-text";
  }
  return html.includes("data-agent-file-mention")
    ? "structured-mention"
    : "plain-text";
}

/**
 * Hosts may resolve a single absolute path paste into a file/directory mention.
 * Keep the sync gate strict so quoted paths, spaces, and multi-path pastes stay
 * plain text until a host explicitly resolves them.
 */
export function isAgentRichTextAbsolutePathPasteCandidate(
  text: string
): boolean {
  const candidate = text.trim();
  if (!candidate || candidate[0] !== "/") {
    return false;
  }
  // Reject multi-line and whitespace/quotes inside the path.
  if (/[\s"'`]/.test(candidate)) {
    return false;
  }
  return true;
}

export function buildWorkspaceFileMentionDropContent(
  entries: ReadonlyArray<{
    path: string;
    name: string;
    kind: AgentFileMentionKind;
  }>,
  options: { prefixCaretAnchor?: boolean } = {}
): JSONContent[] {
  return entries.flatMap((entry, index) => [
    ...(index === 0 && options.prefixCaretAnchor
      ? ([
          { type: "text", text: AGENT_RICH_TEXT_CARET_ANCHOR }
        ] as JSONContent[])
      : []),
    {
      type: "agentFileMention",
      attrs: {
        kind: "file",
        href: entry.path,
        path: entry.path,
        name: entry.name,
        entryKind: entry.kind
      }
    },
    { type: "text", text: " " }
  ]);
}

export function isPromptVisualLineStart(
  editor: Editor,
  position: number
): boolean {
  if (position <= 1) {
    return true;
  }
  return (
    editor.state.doc.textBetween(
      Math.max(1, position - 1),
      position,
      "\n",
      "\n"
    ) === "\n"
  );
}

export function isMentionTriggerBoundaryBeforeSelection(
  editor: Editor
): boolean {
  const position = editor.state.selection.from;
  if (position <= 1) {
    return true;
  }
  const previous = editor.state.doc.textBetween(
    Math.max(1, position - 1),
    position,
    "\n",
    "\n"
  );
  return previous === "" || /\s/.test(previous);
}

export function findCaretAnchorBeforeAtomicRun(
  doc: ProseMirrorNode,
  position: number
): number | null {
  let anchorPosition: number | null = null;
  doc.descendants((node, nodePosition) => {
    if (nodePosition >= position) {
      return false;
    }
    if (node.isText) {
      const text = node.text ?? "";
      for (let offset = 0; offset < text.length; offset += 1) {
        const characterPosition = nodePosition + offset;
        if (characterPosition >= position) {
          return false;
        }
        if (text[offset] === AGENT_RICH_TEXT_CARET_ANCHOR) {
          anchorPosition = characterPosition;
          continue;
        }
        if (anchorPosition !== null) {
          anchorPosition = null;
        }
      }
      return true;
    }
    if (node.type.name === "agentFileMention") {
      return false;
    }
    if (node.isInline && anchorPosition !== null) {
      anchorPosition = null;
    }
    return true;
  });
  return anchorPosition;
}

export function moveSelectionOverCaretAnchor(
  state: EditorState,
  dispatch: (transaction: Transaction) => void,
  key: string
): boolean {
  const { doc, selection } = state;
  if (!selection.empty) {
    return false;
  }
  const position = selection.from;
  if (key === "ArrowLeft") {
    const anchorPosition = findCaretAnchorBeforeAtomicRun(doc, position);
    if (anchorPosition === null) {
      return false;
    }
    dispatch(state.tr.setSelection(TextSelection.create(doc, anchorPosition)));
    return true;
  }
  if (
    key === "ArrowRight" &&
    position < doc.content.size &&
    doc.textBetween(position, position + 1) === AGENT_RICH_TEXT_CARET_ANCHOR
  ) {
    const afterAnchor = position + 1;
    const mentionAfterAnchor = doc.resolve(afterAnchor).nodeAfter;
    dispatch(
      state.tr.setSelection(
        TextSelection.create(
          doc,
          afterAnchor +
            (mentionAfterAnchor?.type.name === "agentFileMention"
              ? mentionAfterAnchor.nodeSize
              : 0)
        )
      )
    );
    return true;
  }
  return false;
}

export function deleteLineStartMentionWithCaretAnchor(
  state: EditorState,
  dispatch: (transaction: Transaction) => void,
  key: string
): boolean {
  const { doc, selection } = state;
  if (!selection.empty) {
    return false;
  }

  if (key === "Backspace") {
    const mention = selection.$from.nodeBefore;
    if (mention?.type.name !== "agentFileMention") {
      return false;
    }
    const mentionFrom = selection.from - mention.nodeSize;
    const anchorFrom = mentionFrom - AGENT_RICH_TEXT_CARET_ANCHOR.length;
    if (
      anchorFrom < 1 ||
      doc.textBetween(anchorFrom, mentionFrom) !== AGENT_RICH_TEXT_CARET_ANCHOR
    ) {
      return false;
    }
    const nextNode = selection.$from.nodeAfter;
    dispatch(
      state.tr.delete(
        nextNode?.type.name === "agentFileMention" ? mentionFrom : anchorFrom,
        selection.from
      )
    );
    return true;
  }

  if (key !== "Delete") {
    return false;
  }
  const anchorFrom = selection.from;
  const mentionFrom = anchorFrom + AGENT_RICH_TEXT_CARET_ANCHOR.length;
  if (
    doc.textBetween(anchorFrom, mentionFrom) !== AGENT_RICH_TEXT_CARET_ANCHOR
  ) {
    return false;
  }
  const mention = doc.resolve(mentionFrom).nodeAfter;
  if (mention?.type.name !== "agentFileMention") {
    return false;
  }
  const mentionTo = mentionFrom + mention.nodeSize;
  const nextNode = doc.resolve(mentionTo).nodeAfter;
  dispatch(
    state.tr.delete(
      nextNode?.type.name === "agentFileMention" ? mentionFrom : anchorFrom,
      mentionTo
    )
  );
  return true;
}

export function createAgentRichTextPlaceholderExtension(
  getPlaceholder: () => string
): Extension {
  return Extension.create({
    name: "agentRichTextPlaceholder",
    addProseMirrorPlugins() {
      return [
        new Plugin({
          key: new PluginKey("agentRichTextPlaceholder"),
          props: {
            decorations(state) {
              const firstNode = state.doc.firstChild;
              const isEmptyPrompt =
                state.doc.childCount === 1 &&
                firstNode?.type.name === "paragraph" &&
                firstNode.content.size === 0;

              if (!firstNode || !isEmptyPrompt) {
                return DecorationSet.empty;
              }

              return DecorationSet.create(state.doc, [
                Decoration.node(0, firstNode.nodeSize, {
                  class: "agent-rich-text-placeholder-node",
                  "data-agent-rich-text-placeholder": getPlaceholder()
                })
              ]);
            }
          }
        })
      ];
    }
  });
}

export function createAgentRichTextCaretAnchorExtension(): Extension {
  return Extension.create({
    name: "agentRichTextCaretAnchor",
    addProseMirrorPlugins() {
      return [
        new Plugin({
          key: new PluginKey("agentRichTextCaretAnchor"),
          props: {
            handleKeyDown(view, event) {
              if (
                (event.key !== "ArrowLeft" &&
                  event.key !== "ArrowRight" &&
                  event.key !== "Backspace" &&
                  event.key !== "Delete") ||
                event.shiftKey ||
                event.metaKey ||
                event.ctrlKey ||
                event.altKey
              ) {
                return false;
              }
              if (
                deleteLineStartMentionWithCaretAnchor(
                  view.state,
                  (transaction) => view.dispatch(transaction),
                  event.key
                )
              ) {
                event.preventDefault();
                return true;
              }
              if (
                moveSelectionOverCaretAnchor(
                  view.state,
                  (transaction) => view.dispatch(transaction),
                  event.key
                )
              ) {
                event.preventDefault();
                return true;
              }
              return false;
            }
          }
        })
      ];
    }
  });
}

export function scrollEditorSelectionIntoView(editor: Editor): void {
  const scrollContainer = editor.view.dom;
  if (!(scrollContainer instanceof HTMLElement)) {
    return;
  }

  const maxScrollTop =
    scrollContainer.scrollHeight - scrollContainer.clientHeight;
  if (maxScrollTop <= 0) {
    return;
  }

  const promptEnd = editor.state.doc.content.size;
  if (editor.state.selection.to >= Math.max(0, promptEnd - 1)) {
    scrollContainer.scrollTop = maxScrollTop;
    return;
  }

  const selectionRect = editor.view.coordsAtPos(editor.state.selection.head);
  const containerRect = scrollContainer.getBoundingClientRect();
  const overflowBottom = selectionRect.bottom - containerRect.bottom;
  if (overflowBottom > 0) {
    scrollContainer.scrollTop = Math.min(
      maxScrollTop,
      scrollContainer.scrollTop + overflowBottom
    );
    return;
  }

  const overflowTop = containerRect.top - selectionRect.top;
  if (overflowTop > 0) {
    scrollContainer.scrollTop = Math.max(
      0,
      scrollContainer.scrollTop - overflowTop
    );
  }
}

export function readSelectedPlainText(editor: Editor): string {
  return readPromptSelection(editor).text;
}

export function readPromptSelection(editor: Editor): {
  from: number;
  text: string;
  to: number;
} {
  const selection = editor.state.selection;
  const domSelection = selection.empty
    ? readEditorDomSelectionRange(editor)
    : null;
  const from = domSelection?.from ?? selection.from;
  const to = domSelection?.to ?? selection.to;
  return {
    from,
    text: readPromptTextRange(editor, from, to),
    to
  };
}

export function readPromptTextRange(
  editor: Editor,
  from: number,
  to: number
): string {
  if (from === to) {
    return "";
  }
  return agentRichTextContentToPromptText(
    editor.state.doc.slice(from, to).content.toJSON()
  );
}

export function readEditorDomSelectionRange(
  editor: Editor
): { from: number; to: number } | null {
  const dom = editor.view.dom;
  const selection = dom.ownerDocument.getSelection();
  if (
    !selection ||
    selection.rangeCount === 0 ||
    !selection.anchorNode ||
    !selection.focusNode ||
    selection.isCollapsed ||
    !dom.contains(selection.anchorNode) ||
    !dom.contains(selection.focusNode)
  ) {
    return null;
  }

  try {
    const anchor = editor.view.posAtDOM(
      selection.anchorNode,
      selection.anchorOffset
    );
    const focus = editor.view.posAtDOM(
      selection.focusNode,
      selection.focusOffset
    );
    if (anchor === focus) {
      return null;
    }
    return {
      from: Math.min(anchor, focus),
      to: Math.max(anchor, focus)
    };
  } catch {
    return null;
  }
}

export async function writePlainTextToClipboard(
  text: string
): Promise<boolean> {
  if (!text || typeof navigator.clipboard?.writeText !== "function") {
    return false;
  }

  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

export async function readPlainTextFromClipboard(): Promise<string | null> {
  if (typeof navigator.clipboard?.readText !== "function") {
    return null;
  }

  try {
    return await navigator.clipboard.readText();
  } catch {
    return null;
  }
}
