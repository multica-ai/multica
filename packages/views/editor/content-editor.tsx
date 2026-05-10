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
 * Tech: Tiptap v3.22.1 (ProseMirror wrapper), @tiptap/markdown for
 * bidirectional Markdown ↔ ProseMirror JSON conversion.
 */

import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from "react";
import { useEditor, EditorContent } from "@tiptap/react";
import { cn } from "@multica/ui/lib/utils";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { useWorkspaceSlug } from "@multica/core/paths";
import { useQueryClient } from "@tanstack/react-query";
import { createEditorExtensions } from "./extensions";
import { uploadAndInsertFile } from "./extensions/file-upload";
import { preprocessMarkdown } from "./utils/preprocess";
import { openLink, isMentionHref } from "./utils/link-handler";
import { EditorBubbleMenu } from "./bubble-menu";
import { useLinkHover, LinkHoverCard } from "./link-hover-card";
import "katex/dist/katex.min.css";
import "./content-editor.css";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Blob URLs (blob:http://…) are process-local and expire on reload. Strip them
 *  from serialised markdown so they never reach the database. */
const BLOB_IMAGE_RE = /!\[[^\]]*\]\(blob:[^)]*\)\n?/g;
// CEREBRO-PATCH(input-autofocus): JEH-756 — retry briefly while a closing dialog still owns focus.
const AUTOFOCUS_DIALOG_RETRY_MS = 750;
const AUTOFOCUS_DIALOG_RETRY_STEP_MS = 32;

function stripBlobUrls(md: string): string {
  return md.replace(BLOB_IMAGE_RE, "");
}

function activeElementIsOwnedByDialog(): boolean {
  if (typeof document === "undefined") return false;
  const active = document.activeElement;
  if (!active || active === document.body) return false;
  return Boolean(active.closest('[role="dialog"], [role="alertdialog"]'));
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
  /** When true, bare Enter submits (chat-style). Mod-Enter always submits. */
  submitOnEnter?: boolean;
  /**
   * ID of the issue this editor belongs to. When set, the bubble menu exposes
   * a "Create sub-issue from selection" action that parents the new issue
   * under this ID and replaces the selection with a mention link.
   */
  currentIssueId?: string;
  /**
   * When true, the @mention extension is not registered. Use for editors
   * where mentioning members/agents has no business meaning (e.g. agent
   * system prompts, where the content is fed to an LLM as plain text).
   */
  disableMentions?: boolean;
  /**
   * CEREBRO-PATCH(input-autofocus): JEH-756 — when true, focus the editor
   * once it finishes initialising. The focus call is owned by ContentEditor
   * so it runs only after `useEditor({ immediatelyRender: false })` has
   * produced a real editor instance, then yields a couple frames for closing
   * menus/dialogs to finish their focus restoration.
   *
   * Static at create time — re-keying the parent (e.g. `key={draftKey}`)
   * remounts the editor and re-applies the prop on session/agent switch.
   *
   * If a focus-trapping dialog (`[role="dialog"]` / `[role="alertdialog"]`)
   * still owns focus at the deferred focus time, the editor yields so the
   * dialog keeps focus.
   */
  autoFocus?: boolean;
}

interface ContentEditorRef {
  getMarkdown: () => string;
  clearContent: () => void;
  focus: () => void;
  /** Drop focus from the editor — used by chat after send so the caret
   *  stops competing with the StatusPill / streaming reply for the user's
   *  attention. */
  blur: () => void;
  /** Upload and insert a file. Pass `embedImage: true` to render an image
   *  inline instead of the default attached file card. */
  uploadFile: (file: File, options?: { embedImage?: boolean }) => void;
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
      submitOnEnter = false,
      currentIssueId,
      disableMentions = false,
      autoFocus = false,
    },
    ref,
  ) {
    const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
    const onUpdateRef = useRef(onUpdate);
    const onSubmitRef = useRef(onSubmit);
    const onBlurRef = useRef(onBlur);
    const onUploadFileRef = useRef(onUploadFile);
    const lastEmittedRef = useRef<string | null>(null);

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
    onUploadFileRef.current = onUploadFile;

    const queryClient = useQueryClient();

    // CEREBRO-PATCH(input-autofocus): JEH-756 — snapshot once per editor
    // instance. Parent re-keying (e.g. `key={draftKey}`) remounts the editor
    // and re-applies the request for session/agent/channel switches.
    const [autoFocusAtMount] = useState(() => autoFocus);

    const editor = useEditor({
      immediatelyRender: false,
      // Note: in v3.22.1 the default is already false/undefined (same behavior).
      // Explicit for clarity — the real perf win is useEditorState in BubbleMenu.
      shouldRerenderOnTransaction: false,
      autofocus: false,
      onCreate: ({ editor: ed }) => {
        lastEmittedRef.current = stripBlobUrls(ed.getMarkdown()).trimEnd();
      },
      content: defaultValue ? preprocessMarkdown(defaultValue) : "",
      contentType: defaultValue ? "markdown" : undefined,
      extensions: createEditorExtensions({
        placeholder: placeholderText,
        queryClient,
        onSubmitRef,
        onUploadFileRef,
        submitOnEnter,
        disableMentions,
      }),
      onUpdate: ({ editor: ed }) => {
        if (!onUpdateRef.current) return;
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = setTimeout(() => {
          const md = stripBlobUrls(ed.getMarkdown()).trimEnd();
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
          // text-base on mobile keeps font-size at 16px so iOS Safari doesn't
          // auto-zoom on focus (zoom persists on back-nav).
          class: cn("rich-text-editor text-base md:text-sm outline-none", className),
        },
      },
    });

    // Cleanup debounce on unmount
    useEffect(() => {
      return () => {
        if (debounceRef.current) clearTimeout(debounceRef.current);
      };
    }, []);

    // CEREBRO-PATCH(input-autofocus): JEH-756 — focus after the editor exists,
    // then wait briefly for closing dialogs/dropdowns to finish focus-restore.
    // The guard is checked at execution time, not mount time: the New Message
    // dialog can still own focus while its selection mounts InboxChatPanel,
    // then release focus a few frames later.
    useEffect(() => {
      if (!editor || !autoFocusAtMount) return;
      if (typeof window === "undefined") return;

      let didFocus = false;
      let timeoutId: number | undefined;
      const startedAt = window.performance?.now?.() ?? Date.now();

      const attemptFocus = () => {
        if (didFocus) return;
        if (editor.isDestroyed) return;

        if (activeElementIsOwnedByDialog()) {
          const now = window.performance?.now?.() ?? Date.now();
          if (now - startedAt < AUTOFOCUS_DIALOG_RETRY_MS) {
            timeoutId = window.setTimeout(attemptFocus, AUTOFOCUS_DIALOG_RETRY_STEP_MS);
          }
          return;
        }

        didFocus = true;
        editor.commands.focus("end");
      };

      timeoutId = window.setTimeout(attemptFocus, 0);

      return () => {
        if (timeoutId) window.clearTimeout(timeoutId);
      };
    }, [editor, autoFocusAtMount]);

    useImperativeHandle(ref, () => ({
      getMarkdown: () => stripBlobUrls(editor?.getMarkdown() ?? ""),
      clearContent: () => {
        editor?.commands.clearContent();
      },
      focus: () => {
        editor?.commands.focus();
      },
      blur: () => {
        editor?.commands.blur();
      },
      uploadFile: (file: File, options?: { embedImage?: boolean }) => {
        if (!editor || !onUploadFileRef.current) return;
        const endPos = editor.state.doc.content.size;
        uploadAndInsertFile(editor, file, onUploadFileRef.current, endPos, options);
      },
      hasActiveUploads: () => {
        if (!editor) return false;
        let uploading = false;
        editor.state.doc.descendants((node) => {
          if (node.attrs.uploading) uploading = true;
          return !uploading;
        });
        return uploading;
      },
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
      <div
        ref={wrapperRef}
        className="relative flex min-h-full flex-col"
        onMouseDown={handleContainerMouseDown}
      >
        <EditorContent className="flex-1 min-h-full" editor={editor} />
        {showBubbleMenu && (
          <EditorBubbleMenu editor={editor} currentIssueId={currentIssueId} />
        )}
        <LinkHoverCard {...hover} />
      </div>
    );
  },
);

export { ContentEditor, type ContentEditorProps, type ContentEditorRef };
