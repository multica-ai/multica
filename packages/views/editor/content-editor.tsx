"use client";

/**
 * ContentEditor — the single rich-text editor for the entire application.
 *
 * Architecture decisions (April 2026 refactor):
 *
 * 1. ONE COMPONENT for both editing and readonly display. The `editable` prop
 *    controls the mode. Previously we had RichTextEditor + ReadonlyEditor as
 *    separate components with duplicated extension configs — this caused
 *    visual inconsistency between edit and display modes.
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
import "./content-editor.css";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Blob URLs (blob:http://…) are process-local and expire on reload. Strip them
 *  from serialised markdown so they never reach the database. */
const BLOB_IMAGE_RE = /!\[[^\]]*\]\(blob:[^)]*\)\n?/g;

function stripBlobUrls(md: string): string {
  return md.replace(BLOB_IMAGE_RE, "");
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface ContentEditorProps {
  defaultValue?: string;
  onUpdate?: (markdown: string) => void;
  placeholder?: string;
  editable?: boolean;
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
}

interface ContentEditorRef {
  getMarkdown: () => string;
  clearContent: () => void;
  focus: () => void;
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
      editable = true,
      className,
      debounceMs = 300,
      onSubmit,
      onBlur,
      onUploadFile,
      showBubbleMenu = true,
      submitOnEnter = false,
      currentIssueId,
    },
    ref,
  ) {
    const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
    const onUpdateRef = useRef(onUpdate);
    const onSubmitRef = useRef(onSubmit);
    const onBlurRef = useRef(onBlur);
    const onUploadFileRef = useRef(onUploadFile);
    const prevContentRef = useRef(defaultValue);
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

    const editor = useEditor({
      immediatelyRender: false,
      // Note: in v3.22.1 the default is already false/undefined (same behavior).
      // Explicit for clarity — the real perf win is useEditorState in BubbleMenu.
      shouldRerenderOnTransaction: false,
      editable,
      onCreate: ({ editor: ed }) => {
        lastEmittedRef.current = stripBlobUrls(ed.getMarkdown());
      },
      content: defaultValue ? preprocessMarkdown(defaultValue) : "",
      contentType: defaultValue ? "markdown" : undefined,
      extensions: createEditorExtensions({
        editable,
        placeholder: placeholderText,
        queryClient,
        onSubmitRef,
        onUploadFileRef,
        submitOnEnter,
      }),
      onUpdate: ({ editor: ed }) => {
        if (!onUpdateRef.current) return;
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = setTimeout(() => {
          const md = stripBlobUrls(ed.getMarkdown());
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
          class: cn(
            "rich-text-editor text-sm outline-none",
            !editable && "readonly",
            className,
          ),
        },
      },
    });

    // Cleanup debounce on unmount
    useEffect(() => {
      return () => {
        if (debounceRef.current) clearTimeout(debounceRef.current);
      };
    }, []);

    // External content update: when defaultValue changes from outside, re-set
    // the editor content. Two scenarios:
    //   1. Readonly mode — e.g. after editing a comment, the readonly view
    //      should reflect the new content.
    //   2. Editable mode — the server may rewrite the content on save (e.g.
    //      expanding bare issue identifiers like `MUL-117` into mention
    //      capsules). We pick those changes up as long as the user is not
    //      actively typing and the new value differs from what we last
    //      emitted — otherwise we'd clobber in-progress edits.
    useEffect(() => {
      if (!editor) return;
      if (defaultValue === prevContentRef.current) return;
      if (editable) {
        // Skip if the user is still typing — resyncing would steal focus and
        // reset the cursor.
        if (editor.isFocused) return;
        // Skip if the incoming value matches what we last emitted (no external
        // change to apply).
        if (defaultValue === lastEmittedRef.current) return;
      }
      prevContentRef.current = defaultValue;
      const processed = defaultValue ? preprocessMarkdown(defaultValue) : "";
      if (processed) {
        editor.commands.setContent(processed, { contentType: "markdown" });
      } else {
        editor.commands.clearContent();
      }
      // Keep the emitted marker in sync so the next onUpdate diff is accurate.
      lastEmittedRef.current = stripBlobUrls(editor.getMarkdown());
    }, [editor, editable, defaultValue]);

    useImperativeHandle(ref, () => ({
      getMarkdown: () => stripBlobUrls(editor?.getMarkdown() ?? ""),
      clearContent: () => {
        editor?.commands.clearContent();
      },
      focus: () => {
        editor?.commands.focus();
      },
      uploadFile: (file: File) => {
        if (!editor || !onUploadFileRef.current) return;
        const endPos = editor.state.doc.content.size;
        uploadAndInsertFile(editor, file, onUploadFileRef.current, endPos);
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
      if (!editable || !editor) return;

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
        {editable && showBubbleMenu && (
          <EditorBubbleMenu editor={editor} currentIssueId={currentIssueId} />
        )}
        <LinkHoverCard {...hover} />
      </div>
    );
  },
);

export { ContentEditor, type ContentEditorProps, type ContentEditorRef };
