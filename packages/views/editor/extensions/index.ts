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
 * All link styling is controlled by styles/prose.css (var(--brand) color),
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
import { TaskList } from "@tiptap/extension-list";
import { Markdown } from "@tiptap/markdown";
import { ReactNodeViewRenderer } from "@tiptap/react";
import type { AnyExtension } from "@tiptap/core";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { escapeMarkdownLabel } from "../utils/escape-markdown-label";
import { BaseMentionExtension } from "./mention-extension";
import { createMentionSuggestion, type MentionItem } from "./mention-suggestion";
import { SlashCommandExtension } from "./slash-command-extension";
import { createSlashCommandSuggestion, createBuiltinCommandSuggestion } from "./slash-command-suggestion";
import { CodeBlockView } from "./code-block-view";
import { PatchedListItem, PatchedTaskItem } from "./list-item";
import { createMarkdownPasteExtension } from "./markdown-paste";
import { createMarkdownCopyExtension } from "./markdown-copy";
import { createSubmitExtension } from "./submit-shortcut";
import { createBlurShortcutExtension } from "./blur-shortcut";
import { createFileUploadExtension } from "./file-upload";
import { FileCardExtension } from "./file-card";
import { ImageView } from "./image-view";
import { BlockMathExtension, InlineMathExtension } from "./math";
import { HighlightExtension } from "./highlight";
import { AutolinkEmailRepairExtension } from "./autolink-email-repair";

const lowlight = createLowlight(common);

const LinkExtension = Link.extend({ inclusive: false }).configure({
  openOnClick: false,
  autolink: true,
  linkOnPaste: true,
  defaultProtocol: "https",
});

export const ImageExtension = Image.extend({
  addAttributes() {
    return {
      ...this.parent?.(),
      uploading: {
        default: false,
        renderHTML: (attrs: Record<string, unknown>) =>
          attrs.uploading ? { "data-uploading": "" } : {},
        parseHTML: (el: HTMLElement) => el.hasAttribute("data-uploading"),
      },
      // Intrinsic pixel dimensions, captured on upload (file-upload.ts). The
      // browser uses width/height on <img> to compute aspect-ratio and reserve
      // the box before the image decodes, so inserting an image causes no
      // layout shift (and the post-insert scrollIntoView stays correct). Not
      // serialized to markdown — `renderMarkdown` only emits src/alt/title — so
      // round-trips stay clean.
      width: {
        default: null,
        renderHTML: (attrs: Record<string, unknown>) =>
          attrs.width ? { width: attrs.width as number } : {},
        parseHTML: (el: HTMLElement) => {
          const w = parseInt(el.getAttribute("width") || "", 10);
          return Number.isFinite(w) ? w : null;
        },
      },
      height: {
        default: null,
        renderHTML: (attrs: Record<string, unknown>) =>
          attrs.height ? { height: attrs.height as number } : {},
        parseHTML: (el: HTMLElement) => {
          const h = parseInt(el.getAttribute("height") || "", 10);
          return Number.isFinite(h) ? h : null;
        },
      },
    };
  },
  addNodeView() {
    return ReactNodeViewRenderer(ImageView);
  },
  renderMarkdown: (node: any) => {
    const src = node.attrs?.src || "";
    const alt = escapeMarkdownLabel(node.attrs?.alt || "");
    const title = node.attrs?.title;
    if (title) {
      return `![${alt}](${src} "${title}")`;
    }
    return `![${alt}](${src})`;
  },
}).configure({
  inline: false,
  allowBase64: false,
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
  /** Override @ behavior for chat context suggestions. */
  mentionMode?: "default" | "context";
  getMentionContextItems?: () => MentionItem[];
  /** When true, attach the `/` picker. Default false. */
  enableSlashCommands?: boolean;
  /**
   * Which `/` menu to attach when enableSlashCommands is true:
   * - "skill" (default) — the chat picker listing the active agent's skills.
   * - "command" — the fixed built-in command menu (issue comments), e.g. /note.
   */
  slashCommandMode?: "skill" | "command";
}

export function createEditorExtensions(
  options: EditorExtensionsOptions,
): AnyExtension[] {
  const { placeholder: placeholderText } = options;

  return [
    StarterKit.configure({
      heading: { levels: [1, 2, 3] },
      link: false,
      codeBlock: false,
      // Disable StarterKit's stock ListItem — its Enter keybind binds only
      // `splitListItem`, which leaves the user stuck inside an empty top-level
      // list item (see list-item.ts). PatchedListItem below restores the
      // standard split → lift fallback chain.
      listItem: false,
    }),
    PatchedListItem,
    // Checkbox task lists: `- [ ]` / `- [x]`. TaskList + TaskItem ship their own
    // markdown tokenizer / renderMarkdown, an input rule (typing `[] ` / `[x] `),
    // and a checkbox NodeView. The taskList tokenizer is consulted before
    // marked's built-in list tokenizer, so `- [ ]` becomes a task while a plain
    // `- ` still falls through to PatchedListItem's bullet list.
    TaskList,
    PatchedTaskItem,
    CodeBlockLowlight.extend({
      addNodeView() {
        return ReactNodeViewRenderer(CodeBlockView);
      },
    }).configure({ lowlight }),
    // ⚠️ Link MUST appear before markdownPaste in this array.
    // linkOnPaste relies on Link's handlePaste plugin firing first;
    // markdownPaste's handlePaste is a catch-all that returns true.
    LinkExtension,
    AutolinkEmailRepairExtension,
    ImageExtension,
    // renderWrapper wraps the table in `<div class="tableWrapper">` (the same
    // wrapper the resizable NodeView emits), which prose.css styles with
    // `overflow-x: auto`. Without it a wide table is a bare <table> that can't
    // shrink below min-content, so the horizontal scrollbar lands on the
    // page-level scroll container instead of the table itself.
    Table.configure({ resizable: false, renderWrapper: true }),
    TableRow,
    TableHeader,
    TableCell,
    BlockMathExtension,
    InlineMathExtension,
    HighlightExtension,
    // 3-space indent so nested ordered lists survive CommonMark in ReadonlyContent.
    Markdown.configure({ indentation: { style: "space", size: 3 } }),
    // Make Cmd+C / Cmd+X / drag write Markdown source to clipboard text/plain
    // so users can copy rich content out as the original Markdown.
    createMarkdownCopyExtension(),
    FileCardExtension,
    BaseMentionExtension.configure({
      HTMLAttributes: { class: "mention" },
      ...(options.disableMentions
        ? { suggestion: { allow: () => false } }
        : options.queryClient
          ? { suggestion: createMentionSuggestion(options.queryClient, { mode: options.mentionMode, getContextItems: options.getMentionContextItems }) }
          : {}),
    }),
    SlashCommandExtension.configure({
      HTMLAttributes: { class: "slash-command" },
      suggestion: !options.enableSlashCommands
        ? { char: "/", allow: () => false }
        : options.slashCommandMode === "command"
          ? createBuiltinCommandSuggestion()
          : options.queryClient
            ? createSlashCommandSuggestion(options.queryClient)
            : { char: "/", allow: () => false },
    }),
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
