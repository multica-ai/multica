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
import type { AnyExtension } from "@tiptap/core";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { BaseMentionExtension } from "./mention-extension";
import { createMentionSuggestion } from "./mention-suggestion";
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

const LinkEditable = Link.extend({ inclusive: false }).configure({
  openOnClick: false,
  autolink: true,
  linkOnPaste: true,
  defaultProtocol: "https",
});

const LinkReadonly = Link.configure({
  openOnClick: false,
  autolink: false,
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

export interface EditorExtensionsOptions {
  editable: boolean;
  placeholder?: string;
  queryClient?: import("@tanstack/react-query").QueryClient;
  onSubmitRef?: RefObject<(() => void) | undefined>;
  onUploadFileRef?: RefObject<
    ((file: File) => Promise<UploadResult | null>) | undefined
  >;
  /** When true, bare Enter also submits (chat-style). Default false. */
  submitOnEnter?: boolean;
  /**
   * When true, the @mention extension is not registered at all. Use for
   * editors where mentioning members/agents has no business meaning (e.g.
   * agent system prompts) — typing `@` becomes inert and any pre-existing
   * `[@user](mention://...)` markdown renders as plain text instead of being
   * parsed into a mention node.
   */
  disableMentions?: boolean;
  /** i18n labels passed from the calling component. */
  labels?: { allMembers?: string };
}

export function createEditorExtensions(
  options: EditorExtensionsOptions,
): AnyExtension[] {
  const { editable, placeholder: placeholderText } = options;

  const extensions: AnyExtension[] = [
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
    editable ? LinkEditable : LinkReadonly,
    ImageExtension,
    Table.configure({ resizable: false }),
    TableRow,
    TableHeader,
    TableCell,
    BlockMathExtension,
    InlineMathExtension,
    // 3-space indent so nested ordered lists survive CommonMark in ReadonlyContent.
    Markdown.configure({ indentation: { style: "space", size: 3 } }),
    // Make Cmd+C / Cmd+X / drag write Markdown source to clipboard text/plain.
    // Registered for both editable and readonly so users can copy from rendered
    // comments and paste the original Markdown elsewhere.
    createMarkdownCopyExtension(),
    FileCardExtension,
    ...(options.disableMentions
      ? []
      : [
          BaseMentionExtension.configure({
            HTMLAttributes: { class: "mention" },
            ...(editable && options.queryClient
              ? { suggestion: createMentionSuggestion(options.queryClient, options.labels) }
              : {}),
          }),
        ]),
  ];

  if (editable) {
    extensions.push(
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
    );
  }

  return extensions;
}
