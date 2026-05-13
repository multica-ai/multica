/**
 * Shared extension factory for ContentEditor.
 *
 * One function builds the extension array for BOTH edit and readonly modes.
 * This ensures visual consistency — the same extensions parse and render
 * content identically regardless of mode.
 *
 * Split:
 * - Both modes: StarterKit, CodeBlock, Link, Image, Table, Markdown, Mention
 * - Edit only: Typography, Placeholder, markdownPaste, submitShortcut,
 *   fileUpload, Mention suggestion popup
 *
 * Link config differs: edit mode has autolink (detects URLs while typing),
 * readonly does not (prevents false positives on display).
 *
 * Mention suggestion is only attached in edit mode — readonly doesn't need
 * the autocomplete popup.
 *
 * All link styling is controlled by content-editor.css (var(--brand) color),
 * not Tailwind HTMLAttributes, to keep a single source of truth.
 */
import type { RefObject } from "react";
import StarterKit from "@tiptap/starter-kit";
import CodeBlockLowlight from "@tiptap/extension-code-block-lowlight";
import { common, createLowlight } from "lowlight";
import Placeholder from "@tiptap/extension-placeholder";
import Link from "@tiptap/extension-link";
import Typography from "@tiptap/extension-typography";
import Image from "@tiptap/extension-image";
import TableRow from "@tiptap/extension-table-row";
import TableHeader from "@tiptap/extension-table-header";
import TableCell from "@tiptap/extension-table-cell";
import { Table } from "@tiptap/extension-table";
import { Markdown } from "@tiptap/markdown";
import { ReactNodeViewRenderer } from "@tiptap/react";
import { Extension, InputRule, type AnyExtension } from "@tiptap/core";
import { Suggestion } from "@tiptap/suggestion";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { BaseMentionExtension } from "./mention-extension";
import { createMentionSuggestion, createBroadcastSuggestion } from "./mention-suggestion";
import { CodeBlockView } from "./code-block-view";
import { createMarkdownPasteExtension } from "./markdown-paste";
import { createMarkdownCopyExtension } from "./markdown-copy";
import { createSubmitExtension } from "./submit-shortcut";
import { createBlurShortcutExtension } from "./blur-shortcut";
import { createFileUploadExtension } from "./file-upload";
import { FileCardExtension } from "./file-card";
import { ImageView } from "./image-view";
import { BlockMathExtension, InlineMathExtension } from "./math";

const lowlight = createLowlight(common);

const LinkExtension = Link.extend({ inclusive: false }).configure({
  openOnClick: false,
  autolink: true,
  linkOnPaste: true,
  defaultProtocol: "https",
});

const ImageExtension = Image.extend({
  addAttributes() {
    return {
      ...this.parent?.(),
      uploading: {
        default: false,
        renderHTML: (attrs: Record<string, unknown>) =>
          attrs.uploading ? { "data-uploading": "" } : {},
        parseHTML: (el: HTMLElement) => el.hasAttribute("data-uploading"),
      },
    };
  },
  addNodeView() {
    return ReactNodeViewRenderer(ImageView);
  },
}).configure({
  inline: false,
  allowBase64: false,
});

/**
 * Prevents the OrderedList wrapping input rule from creating a nested
 * ordered list when the user types "N. " while already inside a listItem.
 *
 * Flow that causes the bug:
 *   1. User types "1. text" -> orderedList created (childCount=1)
 *   2. User presses Enter -> empty sibling listItem created (childCount=2)
 *   3. User types "2. " in the empty item -> input rule fires:
 *      joinPredicate = childCount(2) + start(1) = 3 != 2 -> no join
 *      -> wraps the paragraph in a NEW nested orderedList
 *
 * Fix: run with priority 200 (above StarterKit's default 100) so this
 * handler fires first. When inside a listItem, preserve the typed text
 * as-is and add a step to the transaction. That marks the rule as handled
 * and prevents the StarterKit orderedList rule from firing.
 */
const orderedListInputGuard = Extension.create({
  name: "orderedListInputGuard",
  priority: 200,

  addInputRules() {
    return [
      new InputRule({
        find: /^(\d+)\.\s$/,
        handler: ({ state, range, match }) => {
          const $from = state.selection.$from;
          for (let depth = $from.depth; depth > 0; depth--) {
            if ($from.node(depth).type.name === "listItem") {
              state.tr.insertText(match[0], range.from, range.to);
              return;
            }
          }
          return null;
        },
      }),
    ];
  },
});

export interface EditorExtensionsOptions {
  placeholder?: string;
  queryClient?: import("@tanstack/react-query").QueryClient;
  onSubmitRef?: RefObject<(() => void) | undefined>;
  onUploadFileRef?: RefObject<
    ((file: File) => Promise<UploadResult | null>) | undefined
  >;
  /** When true, bare Enter also submits (chat-style). Default false. */
  submitOnEnter?: boolean;
  /**
   * When true, the `@` suggestion picker is not attached. The mention node
   * type is still registered in the schema so any mention pasted in from
   * another Multica editor renders as the normal mention pill instead of
   * being silently dropped by ProseMirror's schema check. Use for editors
   * where *creating* a new mention has no business meaning (e.g. agent
   * system prompts) but *preserving* an existing one still matters.
   */
  disableMentions?: boolean;
}

export function createEditorExtensions(
  options: EditorExtensionsOptions,
): AnyExtension[] {
  const { placeholder: placeholderText } = options;

  return [
    orderedListInputGuard,
    StarterKit.configure({
      heading: { levels: [1, 2, 3] },
      link: false,
      codeBlock: false,
    }),
    CodeBlockLowlight.extend({
      addNodeView() {
        return ReactNodeViewRenderer(CodeBlockView);
      },
    }).configure({ lowlight }),
    // ⚠️ Link MUST appear before markdownPaste in this array.
    // linkOnPaste relies on Link's handlePaste plugin firing first;
    // markdownPaste's handlePaste is a catch-all that returns true.
    LinkExtension,
    ImageExtension,
    Table.configure({ resizable: false }),
    TableRow,
    TableHeader,
    TableCell,
    BlockMathExtension,
    InlineMathExtension,
    // 3-space indent so nested ordered lists survive CommonMark in ReadonlyContent.
    Markdown.configure({ indentation: { style: "space", size: 3 } }),
    // Make Cmd+C / Cmd+X / drag write Markdown source to clipboard text/plain
    // so users can copy rich content out as the original Markdown.
    createMarkdownCopyExtension(),
    FileCardExtension,
    ...(options.disableMentions
      ? []
      : [
          BaseMentionExtension.configure({
            HTMLAttributes: { class: "mention" },
            ...(options.queryClient
              ? { suggestion: createMentionSuggestion(options.queryClient) }
              : {}),
          }),
          // @@ broadcast suggestion — fires on double-@, inserts a mention
          // node with type="broadcast" that the server fans out to all agents.
          // Only active when the editor has a queryClient (interactive mode).
          ...(options.queryClient
            ? [
                Extension.create({
                  name: "broadcast-suggestion",
                  addProseMirrorPlugins() {
                    return [
                      Suggestion({
                        editor: this.editor,
                        ...createBroadcastSuggestion(),
                      }),
                    ];
                  },
                }),
              ]
            : []),
        ]),
    Typography,
    Placeholder.configure({ placeholder: placeholderText }),
    createMarkdownPasteExtension(),
    createSubmitExtension(
      () => {
        const fn = options.onSubmitRef?.current;
        if (!fn) return false; // no submit wired — let default Enter insert newline
        fn();
        return true;
      },
      { submitOnEnter: options.submitOnEnter ?? false },
    ),
    createBlurShortcutExtension(),
    createFileUploadExtension(options.onUploadFileRef!),
  ];
}
