"use client";

/**
 * ContentEditor — the rich-text editor used wherever the user TYPES content.
 *
 * Architecture decisions (April 2026 refactor):
 *
 * 1. EDITING ONLY. Read-only display is handled by `ReadonlyContent` (a
 *    react-markdown renderer), not this component. There used to be an
 *    `editable` prop here that toggled between modes, but every readonly
 *    callsite migrated to ReadonlyContent and the prop only invited
 *    misuse — Tiptap's `useEditor` reads `editable` at mount, so toggling
 *    the prop later silently failed (mounted-as-readonly editors stayed
 *    unfocusable forever). To express "currently disabled", wrap this
 *    component in a layout that sets `pointer-events-none` / `aria-disabled`
 *    — don't reach into the editor.
 *
 * 2. ONE MARKDOWN PIPELINE via @tiptap/markdown. Content is loaded with
 *    `contentType: 'markdown'` and saved with `editor.getMarkdown()`.
 *    Previously we had a custom `markdownToHtml()` pipeline (Marked library)
 *    for loading and regex post-processing for saving — two asymmetric paths
 *    that caused roundtrip inconsistencies. The @tiptap/markdown extension
 *    (v3.21.0+) handles table cell <p> wrapping and custom mention tokenizers
 *    natively, eliminating the need for the HTML detour.
 *
 * 3. PREPROCESSING is minimal: only legacy mention shortcode migration and
 *    URL linkification (preprocessMarkdown). No HTML conversion.
 *
 * Tech: Tiptap v3 (ProseMirror wrapper), @tiptap/markdown for
 * bidirectional Markdown ↔ ProseMirror JSON conversion.
 */

import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from "react";
import { useEditor, EditorContent, type Editor } from "@tiptap/react";
import { cn } from "@multica/ui/lib/utils";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { useWorkspaceSlug } from "@multica/core/paths";
import { useQueryClient } from "@tanstack/react-query";
import { issueIdentifierOptions } from "@multica/core/issues/queries";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { isIssueIdentifier } from "@multica/ui/markdown";
import type { Attachment } from "@multica/core/types";
import {
  parseMarkdownChunked,
  MARKDOWN_CHUNK_THRESHOLD,
  type MarkdownManagerLike,
} from "./utils/parse-markdown-chunked";
import type { MentionItem } from "./extensions/mention-suggestion";
import type { IssueIdentifierResolver } from "./extensions/issue-identifier-autolink";
import { createEditorExtensions } from "./extensions";
import { uploadAndInsertFile } from "./extensions/file-upload";
import { preprocessMarkdown } from "./utils/preprocess";
import { repairEmptyListItems } from "./utils/repair-list-items";
import { openLink, isMentionHref } from "./utils/link-handler";
import { EditorBubbleMenu } from "./bubble-menu";
import { useLinkHover, LinkHoverCard } from "./link-hover-card";
import { AttachmentDownloadProvider } from "./attachment-download-context";
import "katex/dist/katex.min.css";
import "./styles/index.css";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Blob URLs (blob:http://…) are process-local and expire on reload. Strip them
 *  from serialised markdown so they never reach the database. */
const BLOB_IMAGE_RE = /!\[[^\]]*\]\(blob:[^)]*\)\n?/g;

function stripBlobUrls(md: string): string {
  return md.replace(BLOB_IMAGE_RE, "");
}

/** Canonical comparison form for a markdown string: drop process-local blob
 *  URLs and trailing blank lines so both sides of a dirty check compare
 *  like-for-like. One definition for the normalization rule — a future tweak
 *  (e.g. stripping another ephemeral token) lands here instead of in the
 *  several call sites it used to be copy-pasted across. */
function normalizeMarkdown(md: string): string {
  return stripBlobUrls(md).trimEnd();
}

/** `normalizeMarkdown` applied to the live editor's serialized content. */
function normalizeEditorMarkdown(editor: Editor): string {
  return normalizeMarkdown(editor.getMarkdown());
}

/** True when any node in the document is mid-upload (`attrs.uploading`). The
 *  `return !found` early-out matches the original inline scans verbatim: in
 *  ProseMirror it only stops descending into the matched node's subtree (not
 *  the whole walk), but once `found` flips true the boolean result is fixed. */
function hasUploadingNode(editor: Editor): boolean {
  let found = false;
  editor.state.doc.descendants((node) => {
    if (node.attrs.uploading) found = true;
    return !found;
  });
  return found;
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface ContentEditorProps {
  defaultValue?: string;
  onUpdate?: (markdown: string) => void;
  placeholder?: string;
  className?: string;
  debounceMs?: number;
  onSubmit?: () => void;
  onBlur?: () => void;
  onUploadFile?: (file: File) => Promise<UploadResult | null>;
  /** Show the floating formatting toolbar on text selection. Defaults true. */
  showBubbleMenu?: boolean;
  /**
   * ID of the issue this editor belongs to. When set, the bubble menu exposes
   * a "Create sub-issue from selection" action that parents the new issue
   * under this ID and replaces the selection with a mention link.
   */
  currentIssueId?: string;
  /**
   * When true, the `@` suggestion picker is disabled but the mention node
   * type remains in the schema, so existing mentions pasted in from other
   * Multica editors still render as the normal pill. Use for editors where
   * *creating* a new mention has no business meaning (e.g. agent system
   * prompts) but *preserving* an existing one still matters.
   */
  disableMentions?: boolean;
  /** Chat can surface current/recent issue/project suggestions. Other editors use default mention behavior. */
  mentionMode?: "default" | "context";
  mentionContextItems?: MentionItem[];
  /** Enable the `/` command picker. Defaults false. */
  enableSlashCommands?: boolean;
  /**
   * Which `/` menu to show when enableSlashCommands is true: "skill" (default)
   * lists the active agent's skills (chat); "command" shows the fixed built-in
   * command menu (issue comments), e.g. /note.
   */
  slashCommandMode?: "skill" | "command";
  /**
   * Attachments referenced by this content. The download buttons on file
   * cards and images inside the editor look up an attachment by `url` and
   * fetch a fresh CloudFront signature at click time, so a stale URL
   * persisted in markdown never opens. Pass `issue.attachments` /
   * `comment.attachments` etc.; omit when no attachment context is
   * available (NodeView buttons fall back to opening the raw URL).
   */
  attachments?: Attachment[];
  /**
   * Flush a pending debounced `onUpdate` when the editor unmounts instead of
   * dropping it. Default false ON PURPOSE: most composers clear their draft
   * and then unmount (comment edit cancel, create-issue / feedback submit),
   * and a flush there would hand the discarded content right back to
   * `onUpdate`, resurrecting the cleared draft. Opt in only where closing
   * means "keep what the user last saw" — e.g. the issue-detail description
   * editor, whose 1500ms debounce would otherwise drop a paste made just
   * before the modal closes.
   */
  flushPendingOnUnmount?: boolean;
}

interface ContentEditorRef {
  getMarkdown: () => string;
  clearContent: () => void;
  focus: () => void;
  /** Drop focus from the editor — used by chat after send so the caret
   *  stops competing with the StatusPill / streaming reply for the user's
   *  attention. */
  blur: () => void;
  uploadFile: (file: File) => void;
  /** True when file uploads are still in progress. */
  hasActiveUploads: () => boolean;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

const ContentEditor = forwardRef<ContentEditorRef, ContentEditorProps>(
  function ContentEditor(
    {
      defaultValue = "",
      onUpdate,
      placeholder: placeholderText = "",
      className,
      debounceMs = 300,
      onSubmit,
      onBlur,
      onUploadFile,
      showBubbleMenu = true,
      currentIssueId,
      disableMentions = false,
      mentionMode = "default",
      mentionContextItems,
      enableSlashCommands = false,
      slashCommandMode = "skill",
      attachments,
      flushPendingOnUnmount = false,
    },
    ref,
  ) {
    const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
    const flushPendingOnUnmountRef = useRef(flushPendingOnUnmount);
    // Markdown serialized at `onUpdate` time, awaiting its debounce fire. The
    // unmount flush emits this cached copy — it runs mid-teardown and can't
    // assume the editor instance is still readable.
    const pendingFlushRef = useRef<string | null>(null);
    const onUpdateRef = useRef(onUpdate);
    const onSubmitRef = useRef(onSubmit);
    const onBlurRef = useRef(onBlur);
    const onUploadFileRef = useRef<
      ((file: File) => Promise<UploadResult | null>) | undefined
    >(undefined);
    const mentionContextItemsRef = useRef<MentionItem[]>(mentionContextItems ?? []);
    const lastEmittedRef = useRef<string | null>(null);
    // Live placeholder text. Passed into the Placeholder extension as a getter
    // (not a static string) so the plugin re-reads it on every decoration pass —
    // the sync effect below updates this ref and nudges a repaint. Tiptap
    // snapshots a *string* placeholder at mount, so a getter is what lets it
    // change without remounting the editor.
    const placeholderRef = useRef(placeholderText);

    // In-session record of attachments freshly uploaded through this editor.
    // Surfaces (like the quick-create modal) that don't have a server-supplied
    // `attachments` prop still need the AttachmentDownloadProvider to know
    // about images the user just pasted/dropped — without a record in scope,
    // Attachment.normalize() can't swap the persisted /api/attachments/<id>/
    // download URL to a freshly-loadable one, and the <img> renders broken in
    // any environment where the renderer's origin doesn't proxy /api to the
    // API host (MUL-3192, Desktop/Electron).
    const [sessionUploads, setSessionUploads] = useState<Attachment[]>([]);
    // Wrap the caller-supplied uploader so we can stash each successful result
    // in `sessionUploads`. The wrapper is rebuilt only when the underlying
    // `onUploadFile` identity changes, so the inner ref handed to Tiptap stays
    // stable across renders the way the original passthrough did.
    const wrappedOnUploadFile = useMemo(() => {
      if (!onUploadFile) return undefined;
      return async (file: File): Promise<UploadResult | null> => {
        const result = await onUploadFile(file);
        // Only track attachments that carry a persisted id — the no-workspace
        // avatar branch returns an id-less record that the resolver can't key
        // off of, and tracking it would just bloat memory without helping
        // anyone. See useFileUpload's `markdownLink` docstring for why.
        if (result?.id) {
          setSessionUploads((prev) =>
            // Deduplicate on id so a re-upload (or a paste-then-drop of the
            // same blob) doesn't create a parallel record.
            prev.some((a) => a.id === result.id) ? prev : [...prev, result],
          );
        }
        return result;
      };
    }, [onUploadFile]);

    // Merged list fed to AttachmentDownloadProvider. Caller-supplied attachments
    // (issue / comment editors that pre-load the full attachments[] from the
    // server) take precedence — we only append session uploads the caller
    // doesn't already have, so a parent re-render that includes the same record
    // doesn't end up with two copies.
    //
    // One exception on id collision: when the caller's copy has an EMPTY
    // `download_url` (the create-issue draft strips the short-lived signed URL
    // before persisting), backfill it from the session upload. The session copy
    // holds the this-response signed URL, so the just-pasted image first-paints
    // from it instead of taking an extra redirect hop through `markdown_url`.
    const providerAttachments = useMemo(() => {
      if (sessionUploads.length === 0) return attachments;
      const sessionById = new Map(sessionUploads.map((a) => [a.id, a]));
      const merged: Attachment[] = [];
      for (const a of attachments ?? []) {
        const session = a.id ? sessionById.get(a.id) : undefined;
        if (session) sessionById.delete(a.id);
        merged.push(
          session && !a.download_url
            ? { ...a, download_url: session.download_url }
            : a,
        );
      }
      merged.push(...sessionById.values());
      return merged;
    }, [attachments, sessionUploads]);

    // Current workspace slug kept in a ref so the click handler always sees the
    // latest value without recreating the editor. Used by openLink to prefix
    // legacy /issues/... style paths that lack a workspace slug.
    const workspaceSlug = useWorkspaceSlug();
    const workspaceSlugRef = useRef(workspaceSlug);
    workspaceSlugRef.current = workspaceSlug;

    // Keep refs in sync without recreating editor
    onUpdateRef.current = onUpdate;
    onSubmitRef.current = onSubmit;
    onBlurRef.current = onBlur;
    onUploadFileRef.current = wrappedOnUploadFile;
    mentionContextItemsRef.current = mentionContextItems ?? [];
    flushPendingOnUnmountRef.current = flushPendingOnUnmount;

    const queryClient = useQueryClient();

    // Linear-style bare identifier autolink resolver. Fully lazy — it runs only
    // on user input, never on render, so it adds no query hook to this widely
    // used component. It reads the current workspace from the query cache (via
    // the slug ref) and returns null outside a workspace, for non-identifier
    // tokens, or when the prefix can't match this workspace, so no network call
    // happens for those; the exact-match filter enforces correctness.
    const resolveIssueIdentifierRef = useRef<IssueIdentifierResolver | undefined>(
      undefined,
    );
    resolveIssueIdentifierRef.current = async (identifier) => {
      if (!isIssueIdentifier(identifier)) return null;
      const slug = workspaceSlugRef.current;
      if (!slug) return null;
      const workspaces = await queryClient.fetchQuery(workspaceListOptions());
      const ws = workspaces.find((w) => w.slug === slug);
      if (!ws) return null;
      const prefix = ws.issue_prefix;
      if (
        prefix &&
        !identifier.toUpperCase().startsWith(`${prefix.toUpperCase()}-`)
      ) {
        return null;
      }
      const issue = await queryClient.fetchQuery(
        issueIdentifierOptions(ws.id, identifier),
      );
      return issue ? { id: issue.id, identifier: issue.identifier } : null;
    };

    const initialContent = defaultValue ? preprocessMarkdown(defaultValue) : "";
    // With `immediatelyRender: false` the Tiptap instance is created after
    // mount, so an imperative `focus()` fired on the same tick (e.g. chat
    // auto-focusing a brand-new conversation) would hit a null editor and no-op.
    // Latch the intent here and honor it in `onCreate` once the editor exists.
    const focusOnReadyRef = useRef(false);
    // Large markdown is parsed in chunks to dodge marked's O(n²) tokenizer (see
    // parseMarkdownChunked). Small docs stay on the single-parse fast path.
    const mountChunked = initialContent.length > MARKDOWN_CHUNK_THRESHOLD;

    const editor = useEditor({
      immediatelyRender: false,
      // Explicit for clarity — the real perf win is useEditorState in BubbleMenu.
      shouldRerenderOnTransaction: false,
      onCreate: ({ editor: ed }) => {
        // For large docs we mount empty (below) and parse in chunks here, so the
        // O(n²) marked tokenizer never sees the whole document at once.
        if (mountChunked) {
          const manager = (
            ed.storage as { markdown?: { manager?: MarkdownManagerLike } }
          ).markdown?.manager;
          if (manager) {
            ed.commands.setContent(
              parseMarkdownChunked(manager, initialContent),
              { emitUpdate: false },
            );
          } else {
            ed.commands.setContent(initialContent, {
              emitUpdate: false,
              contentType: "markdown",
            });
          }
        }
        // A markdown draft ending in an empty list item (e.g. `"1. \n\n"` left
        // after typing `1.`) parses into a caretless, schema-invalid item;
        // repair it so the mounted editor has a real cursor in the list.
        repairEmptyListItems(ed);
        lastEmittedRef.current = normalizeEditorMarkdown(ed);
        if (focusOnReadyRef.current) {
          focusOnReadyRef.current = false;
          ed.commands.focus("end");
        }
      },
      content: mountChunked ? "" : initialContent,
      contentType: mountChunked
        ? undefined
        : defaultValue
          ? "markdown"
          : undefined,
      extensions: createEditorExtensions({
        placeholder: () => placeholderRef.current,
        queryClient,
        onSubmitRef,
        onUploadFileRef,
        disableMentions,
        mentionMode,
        getMentionContextItems: () => mentionContextItemsRef.current,
        enableSlashCommands,
        slashCommandMode,
        resolveIssueIdentifierRef,
      }),
      onUpdate: ({ editor: ed }) => {
        if (!onUpdateRef.current) return;
        if (flushPendingOnUnmountRef.current) {
          pendingFlushRef.current = normalizeEditorMarkdown(ed);
        }
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = setTimeout(() => {
          debounceRef.current = undefined;
          pendingFlushRef.current = null;
          const md = normalizeEditorMarkdown(ed);
          if (md === lastEmittedRef.current) return;
          lastEmittedRef.current = md;
          onUpdateRef.current?.(md);
        }, debounceMs);
      },
      onBlur: () => {
        onBlurRef.current?.();
      },
      editorProps: {
        handleDOMEvents: {
          click(_view, event) {
            const target = event.target as HTMLElement;
            // Skip links inside NodeView wrappers — they handle their own clicks
            if (target.closest("[data-node-view-wrapper]")) return false;

            const link = target.closest("a");
            const href = link?.getAttribute("href");
            if (!href || isMentionHref(href)) return false;

            event.preventDefault();
            openLink(href, workspaceSlugRef.current);
            return true;
          },
        },
        attributes: {
          class: cn("flex-1 rich-text-editor text-sm outline-none", className),
        },
      },
    });

    // Cleanup on unmount. A pending debounced update is DROPPED by default,
    // not flushed — see the `flushPendingOnUnmount` prop doc for why. When the
    // owner opted in, emit the markdown cached at `onUpdate` time so a long
    // debounce can't swallow the last edit when the surrounding modal closes.
    useEffect(() => {
      return () => {
        if (!debounceRef.current) return;
        clearTimeout(debounceRef.current);
        debounceRef.current = undefined;
        if (!flushPendingOnUnmountRef.current) return;
        const pending = pendingFlushRef.current;
        pendingFlushRef.current = null;
        if (pending === null || pending === lastEmittedRef.current) return;
        lastEmittedRef.current = pending;
        onUpdateRef.current?.(pending);
      };
    }, []);

    // Sync external `defaultValue` changes into the editor.
    // Tiptap v3 `useEditor` reads `content` only at mount (ueberdosis/tiptap#5831);
    // without this effect, a WS-driven description update keeps the editor
    // showing stale content until the issue is closed and reopened.
    useEffect(() => {
      if (!editor || editor.isDestroyed) return;

      // Guard 0: never clobber an in-flight upload. An external `defaultValue`
      // change can arrive mid-upload — e.g. chat lazy-creates a session on the
      // first file upload, which flips `activeSessionId` → the draft key →
      // `defaultValue`. If we `setContent` over a document that still holds an
      // `uploading` image/fileCard node, that node is wiped and the upload's
      // finalize can no longer find it (the file vanishes, leaving an empty
      // `!file[name]()`). Like the dirty guards below, an uploading node is
      // local state that an external sync must not overwrite.
      if (hasUploadingNode(editor)) return;

      const current = normalizeEditorMarkdown(editor);
      // "Dirty" = user has local edits not yet flushed through the debounced
      // `onUpdate`. `lastEmittedRef` is advanced only after a debounce fire,
      // so a divergence means the editor holds unsaved bytes.
      const isDirty =
        lastEmittedRef.current !== null && current !== lastEmittedRef.current;

      // Guard 1: focused AND dirty — protect bytes the user is actively
      // typing. Focused-but-clean falls through: applying setContent is safe
      // (no user input to lose) and necessary, because onBlur has no replay
      // mechanism and a focused clean editor would otherwise drop this sync
      // permanently.
      if (editor.isFocused && isDirty) return;

      // Guard 2: unfocused-but-dirty — blur happened but the debounce window
      // (debounceMs, 1500ms for description) hasn't flushed yet. The pending
      // onUpdate will reach the server and the cache will reconcile; skipping
      // here avoids overwriting unsaved local edits.
      if (isDirty) return;

      const incoming = defaultValue ? preprocessMarkdown(defaultValue) : "";
      const incomingNormalized = normalizeMarkdown(incoming);
      // Guard 3: normalized-equal short-circuit. Avoids a no-op transaction
      // when the cache reflects a write this same editor just emitted.
      if (incomingNormalized === current) return;

      // Guard 4: `emitUpdate: false`. Tiptap v3's setContent defaults to
      // `emitUpdate: true`; without this we would re-trigger onUpdate →
      // server save → self-write loop.
      const { from, to } = editor.state.selection;
      // Same chunked path on WS-driven re-parse of a large description.
      const manager =
        incoming.length > MARKDOWN_CHUNK_THRESHOLD
          ? (editor.storage as { markdown?: { manager?: MarkdownManagerLike } })
              .markdown?.manager
          : undefined;
      if (manager) {
        editor.commands.setContent(parseMarkdownChunked(manager, incoming), {
          emitUpdate: false,
        });
      } else {
        editor.commands.setContent(incoming, {
          emitUpdate: false,
          contentType: "markdown",
        });
      }

      // An empty list item in the incoming markdown parses into a caretless,
      // schema-invalid node; repair it and let it own the caret. Otherwise clamp
      // the prior selection to the new doc size so the caret doesn't snap to
      // position 0 after ProseMirror replaces the document.
      if (!repairEmptyListItems(editor, { from, to })) {
        const docSize = editor.state.doc.content.size;
        editor.commands.setTextSelection({
          from: Math.min(from, docSize),
          to: Math.min(to, docSize),
        });
      }

      lastEmittedRef.current = normalizeEditorMarkdown(editor);
    }, [defaultValue, editor]);

    // Sync external `placeholder` changes into the mounted editor.
    // The Placeholder extension is configured with a getter over `placeholderRef`
    // (see createEditorExtensions above), which the plugin re-invokes every time
    // it recomputes its decorations. Update the ref, then dispatch an empty
    // transaction to force that recompute — the placeholder refreshes without a
    // remount. Without this, it stays frozen at its mount value: switching
    // between an archived and an active chat session under the same agent (no
    // editor remount) leaves the input stuck on "This session is archived" even
    // though it is usable.
    useEffect(() => {
      if (placeholderRef.current === placeholderText) return;
      placeholderRef.current = placeholderText;
      if (!editor || editor.isDestroyed) return;
      // `docChanged` is false on an empty transaction, so onUpdate never fires
      // and no self-write loop is triggered.
      editor.view.dispatch(editor.state.tr);
    }, [editor, placeholderText]);

    useImperativeHandle(ref, () => ({
      // Intentionally NOT routed through `normalizeMarkdown` — this refactor
      // must preserve the exact current return value (no `trimEnd`).
      getMarkdown: () => stripBlobUrls(editor?.getMarkdown() ?? ""),
      clearContent: () => {
        editor?.commands.clearContent();
      },
      focus: () => {
        if (editor) editor.commands.focus();
        // Editor not mounted yet — defer the focus to `onCreate`.
        else focusOnReadyRef.current = true;
      },
      blur: () => {
        editor?.commands.blur();
      },
      uploadFile: (file: File) => {
        if (!editor || !onUploadFileRef.current) return;
        const endPos = editor.state.doc.content.size;
        uploadAndInsertFile(editor, file, onUploadFileRef.current, endPos);
      },
      hasActiveUploads: () => (editor ? hasUploadingNode(editor) : false),
    }));

    // Link hover card — disabled when BubbleMenu is active (has selection)
    const wrapperRef = useRef<HTMLDivElement>(null);
    const hoverDisabled = !editor?.state.selection.empty;
    const hover = useLinkHover(wrapperRef, hoverDisabled);

    const handleContainerMouseDown = (event: ReactMouseEvent<HTMLDivElement>) => {
      if (!editor) return;

      const target = event.target as HTMLElement;
      if (target.closest(".ProseMirror")) return;
      if (target.closest("a, button, input, textarea, [role='button'], [data-node-view-wrapper]")) return;

      event.preventDefault();
      editor.commands.focus("end");
    };

    if (!editor) return null;

    return (
      <AttachmentDownloadProvider attachments={providerAttachments}>
        <div
          ref={wrapperRef}
          className="relative flex flex-1 min-h-full flex-col"
          onMouseDown={handleContainerMouseDown}
        >
          <EditorContent className="flex flex-1 flex-col" editor={editor} />
          {showBubbleMenu && (
            <EditorBubbleMenu editor={editor} currentIssueId={currentIssueId} />
          )}
          <LinkHoverCard {...hover} />
        </div>
      </AttachmentDownloadProvider>
    );
  },
);

export { ContentEditor, type ContentEditorProps, type ContentEditorRef };
