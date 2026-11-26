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
import { CodeBlockExtension } from "./code-block";
import Placeholder from "@tiptap/extension-placeholder";
import Link from "@tiptap/extension-link";
import Typography from "@tiptap/extension-typography";
import TableRow from "@tiptap/extension-table-row";
import TableHeader from "@tiptap/extension-table-header";
import TableCell from "@tiptap/extension-table-cell";
import { Table } from "@tiptap/extension-table";
import { TaskList } from "@tiptap/extension-list";
import { Markdown } from "@tiptap/markdown";
import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import { CollaborationSelectionSync } from "./collaboration-selection";
import type * as Y from "yjs";
import type { Awareness } from "y-protocols/awareness";
import { coalesceAwarenessChanges } from "./coalesced-awareness";
import {
  COLLAB_FIELD,
  createMarkedInstance,
  ImageSchema,
  MARKDOWN_OPTIONS,
  STARTER_KIT_OPTIONS,
} from "../schema";
import { ReactNodeViewRenderer } from "@tiptap/react";
import type { AnyExtension } from "@tiptap/core";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { shouldAutoLink } from "@multica/ui/markdown/linkify";
import { BaseMentionExtension } from "./mention-extension";
import { createMentionSuggestion, type MentionItem } from "./mention-suggestion";
import {
  createIssueIdentifierAutolinkExtension,
  type IssueIdentifierResolver,
} from "./issue-identifier-autolink";
import { SlashCommandExtension } from "./slash-command-extension";
import { createSlashCommandSuggestion, createBuiltinCommandSuggestion } from "./slash-command-suggestion";
import type { BuiltinCommandSuggestionOptions } from "./slash-command-suggestion";
import { createBlockInsertSuggestion } from "./block-insert-suggestion";
import { SuggestionTriggerArmingExtension } from "./suggestion-trigger-arming";
import { CodeBlockView } from "./code-block-view";
import { TableView } from "./table-view";
import { RICH_TABLE_CHROME_ATTRIBUTE } from "../../rich-content/rich-table";
import { PatchedListItem, PatchedOrderedList, PatchedTaskItem } from "./list-item";
import { createMarkdownPasteExtension } from "./markdown-paste";
import { createMarkdownCopyExtension } from "./markdown-copy";
import { createBlurShortcutExtension } from "./blur-shortcut";
import { createSubmitShortcutExtension } from "./submit-shortcut";
import { createFileUploadExtension } from "./file-upload";
import { FileCardExtension } from "./file-card";
import { ImageView } from "./image-view";
import { BlockMathExtension, InlineMathExtension } from "./math";
import { HighlightExtension } from "./highlight";
import { codeLowlight } from "../syntax-highlight";

const LinkExtension = Link.extend({ inclusive: false }).configure({
  openOnClick: false,
  autolink: true,
  linkOnPaste: true,
  defaultProtocol: "https",
  shouldAutoLink,
});

export const ImageExtension = ImageSchema.extend({
  addNodeView() {
    return ReactNodeViewRenderer(ImageView);
  },
});

export interface EditorCollaboration {
  document: Y.Doc;
  provider: { awareness: Awareness | null };
  /** Shared with everyone in the session; the caret label shows `name`. */
  user: { name: string; color: string; [field: string]: unknown };
}

export interface EditorExtensionsOptions {
  /**
   * Placeholder text, or a getter for it. Prefer a getter when the value can
   * change over the editor's lifetime: Tiptap's Placeholder snapshots a string
   * option at mount, but re-invokes a function every time it recomputes its
   * decorations — so a getter (paired with an empty-transaction nudge) lets the
   * placeholder update live without remounting the editor. See ContentEditor.
   */
  placeholder?: string | (() => string);
  queryClient?: import("@tanstack/react-query").QueryClient;
  onSubmitRef?: RefObject<(() => void) | undefined>;
  onEnterRef?: RefObject<(() => void) | undefined>;
  onUploadFileRef?: RefObject<
    ((file: File, uploadId: string) => Promise<UploadResult | null>) | undefined
  >;
  /**
   * Character count above which a plain-text paste becomes a .txt attachment
   * instead of document text. A ref so the value can change without
   * recreating the editor. Omitted (the default) keeps every paste as text.
   */
  pasteAsFileThresholdRef?: RefObject<number | undefined>;
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
  mentionHideGuests?: boolean;
  mentionHideSquads?: boolean;
  /** When true, attach the `/` picker. Default false. */
  enableSlashCommands?: boolean;
  /**
   * Which `/` menu to attach when enableSlashCommands is true:
   * - "skill" (default) — the chat picker listing the active agent's skills.
   * - "command" — Issue quick actions, skills matching a nonempty query, and /note.
   * - "blocks" — the document block menu: headings, lists, code, tables, uploads.
   */
  slashCommandMode?: "skill" | "command" | "blocks";
  /**
   * Quick actions and Issue target-agent skills offered in the "command" `/`
   * menu, plus the resolver that turns a quick-action pick into draft text.
   * Functions keep the editor mounted while reading live React Query data.
   */
  quickActionMenu?: BuiltinCommandSuggestionOptions;
  /**
   * Resolver for Linear-style bare issue-identifier autolinking. When present
   * (and mentions are enabled), typing a boundary after `MUL-123` or pasting
   * text with identifiers resolves them and swaps in real issue mentions. A
   * ref so the editor is created once while the resolver reads live workspace
   * context; the setup layer owns React Query + workspace access.
   */
  resolveIssueIdentifierRef?: RefObject<IssueIdentifierResolver | undefined>;
  /**
   * Bind the editor to a shared Yjs document instead of its own content.
   * The document, not the `content` option, is then the source of truth, and
   * history comes from Yjs so undo only reverts this user's changes.
   */
  collaboration?: EditorCollaboration;
}

export function createEditorExtensions(
  options: EditorExtensionsOptions,
): AnyExtension[] {
  const { placeholder: placeholderText } = options;

  return [
    StarterKit.configure({
      ...STARTER_KIT_OPTIONS,
      // Yjs owns history in a collaborative document: its undo only reverts
      // this user's own changes, never a collaborator's or an agent's.
      ...(options.collaboration ? { undoRedo: false } : {}),
    }),
    PatchedListItem,
    PatchedOrderedList,
    // Checkbox task lists: `- [ ]` / `- [x]`. TaskList + TaskItem ship their own
    // markdown tokenizer / renderMarkdown, an input rule (typing `[] ` / `[x] `),
    // and a checkbox NodeView. The taskList tokenizer is consulted before
    // marked's built-in list tokenizer, so `- [ ]` becomes a task while a plain
    // `- ` still falls through to PatchedListItem's bullet list.
    TaskList,
    PatchedTaskItem,
    CodeBlockExtension.extend({
      addNodeView() {
        return ReactNodeViewRenderer(CodeBlockView);
      },
    }).configure({ lowlight: codeLowlight }),
    // ⚠️ Link MUST appear before markdownPaste in this array.
    // linkOnPaste relies on Link's handlePaste plugin firing first;
    // markdownPaste's handlePaste is a catch-all that returns true.
    LinkExtension,
    ImageExtension,
    // renderWrapper wraps the table in `<div class="tableWrapper">` (the same
    // wrapper the resizable NodeView emits), which prose.css styles with
    // `overflow-x: auto`. Without it a wide table is a bare <table> that can't
    // shrink below min-content, so the horizontal scrollbar lands on the
    // page-level scroll container instead of the table itself. That covers
    // renderHTML; the editor itself draws TableView, the readonly tables' frame.
    Table.extend({
      addNodeView() {
        return ReactNodeViewRenderer(TableView, {
          contentDOMElementTag: "tbody",
          // The frame's toolbar and resize handles are controls, not content.
          stopEvent: ({ event }) =>
            event.target instanceof Element &&
            event.target.closest(`[${RICH_TABLE_CHROME_ATTRIBUTE}]`) !== null,
        });
      },
    }).configure({ resizable: false, renderWrapper: true }),
    TableRow,
    TableHeader,
    TableCell,
    BlockMathExtension,
    InlineMathExtension,
    HighlightExtension,
    Markdown.configure({ ...MARKDOWN_OPTIONS, marked: createMarkedInstance() }),
    // Make Cmd+C / Cmd+X / drag write Markdown source to clipboard text/plain
    // so users can copy rich content out as the original Markdown.
    createMarkdownCopyExtension(),
    FileCardExtension,
    // Must precede the mention and slash pickers: it supplies the "the user
    // typed this trigger" signal their `shouldShow` reads (MUL-5429).
    SuggestionTriggerArmingExtension,
    BaseMentionExtension.configure({
      HTMLAttributes: { class: "mention" },
      ...(options.disableMentions
        ? { suggestion: { allow: () => false } }
        : options.queryClient
          ? { suggestion: createMentionSuggestion(options.queryClient, { mode: options.mentionMode, getContextItems: options.getMentionContextItems, hideGuests: options.mentionHideGuests, hideSquads: options.mentionHideSquads }) }
          : {}),
    }),
    // Linear-style bare identifier → issue mention. Attached only when a
    // resolver is provided AND mention creation is enabled (an editor that
    // suppresses new mentions should not synthesise them from identifiers).
    ...(!options.disableMentions && options.resolveIssueIdentifierRef
      ? [
          createIssueIdentifierAutolinkExtension({
            resolveRef: options.resolveIssueIdentifierRef,
          }),
        ]
      : []),
    SlashCommandExtension.configure({
      HTMLAttributes: { class: "slash-command" },
      suggestion: !options.enableSlashCommands
        ? { char: "/", allow: () => false }
        : options.slashCommandMode === "blocks"
          ? createBlockInsertSuggestion(options.onUploadFileRef)
          : options.slashCommandMode === "command"
            ? createBuiltinCommandSuggestion(options.quickActionMenu)
            : options.queryClient
              ? createSlashCommandSuggestion(options.queryClient)
              : { char: "/", allow: () => false },
    }),
    Typography,
    Placeholder.configure({ placeholder: placeholderText }),
    createMarkdownPasteExtension(),
    createSubmitShortcutExtension(() => {
      const fn = options.onSubmitRef?.current;
      if (!fn) return false;
      fn();
      return true;
    }, () => options.onEnterRef?.current),
    createBlurShortcutExtension(),
    createFileUploadExtension(options.onUploadFileRef!, options.pasteAsFileThresholdRef),
    ...(options.collaboration
      ? [
          Collaboration.configure({
            document: options.collaboration.document,
            field: COLLAB_FIELD,
          }),
          CollaborationSelectionSync.configure({ document: options.collaboration.document }),
          CollaborationCaret.configure({
            provider: {
              awareness:
                options.collaboration.provider.awareness &&
                coalesceAwarenessChanges(options.collaboration.provider.awareness),
            },
            user: options.collaboration.user,
            render: renderCaret,
          }),
        ]
      : []),
  ];
}

/** A collaborator's caret, as the extension draws it but hidden from the text
 *  index: comment anchors read the editor's text, and a caret label sitting
 *  inside quoted text would otherwise put a name in the middle of it. */
function renderCaret(user: Record<string, string>): HTMLElement {
  const caret = document.createElement("span");
  caret.classList.add("collaboration-carets__caret");
  caret.setAttribute("aria-hidden", "true");
  if (user.id) caret.dataset.collaborator = `${user.kind ?? "member"}:${user.id}`;
  caret.style.borderColor = user.color ?? "";
  const label = document.createElement("div");
  label.classList.add("collaboration-carets__label");
  label.style.backgroundColor = user.color ?? "";
  label.textContent = user.name ?? "";
  caret.append(label);
  return caret;
}
