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
  useState,
} from "react";
import { useEditor, EditorContent } from "@tiptap/react";
import { cn } from "@multica/ui/lib/utils";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { useQueryClient } from "@tanstack/react-query";
import { createEditorExtensions } from "./extensions";
import { uploadAndInsertFile } from "./extensions/file-upload";
import { preprocessMarkdown } from "./utils/preprocess";
import "./content-editor.css";

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
}

interface ContentEditorRef {
  getMarkdown: () => string;
  clearContent: () => void;
  focus: () => void;
  uploadFile: (file: File) => void;
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
    },
    ref,
  ) {
    const [dragOver, setDragOver] = useState(false);
    const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
    const onUpdateRef = useRef(onUpdate);
    const onSubmitRef = useRef(onSubmit);
    const onBlurRef = useRef(onBlur);
    const onUploadFileRef = useRef(onUploadFile);
    const prevContentRef = useRef(defaultValue);

    // Keep refs in sync without recreating editor
    onUpdateRef.current = onUpdate;
    onSubmitRef.current = onSubmit;
    onBlurRef.current = onBlur;
    onUploadFileRef.current = onUploadFile;

    const queryClient = useQueryClient();

    const editor = useEditor({
      immediatelyRender: false,
      editable,
      content: defaultValue ? preprocessMarkdown(defaultValue) : "",
      contentType: defaultValue ? "markdown" : undefined,
      extensions: createEditorExtensions({
        editable,
        placeholder: placeholderText,
        queryClient,
        onSubmitRef,
        onUploadFileRef,
      }),
      onUpdate: ({ editor: ed }) => {
        if (!onUpdateRef.current) return;
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = setTimeout(() => {
          onUpdateRef.current?.(ed.getMarkdown());
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
            if (!href || href.startsWith("mention://")) return false;

            if (!editable) {
              // Readonly: any click on link opens new tab
              event.preventDefault();
              window.open(href, "_blank", "noopener,noreferrer");
              return true;
            }

            if (event.metaKey || event.ctrlKey) {
              // Edit mode: Cmd/Ctrl+click opens link
              window.open(href, "_blank", "noopener,noreferrer");
              event.preventDefault();
              return true;
            }

            return false;
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

    // Always clear drag overlay on any drop/dragend anywhere in the document
    useEffect(() => {
      const clear = () => setDragOver(false);
      document.addEventListener("drop", clear);
      document.addEventListener("dragend", clear);
      return () => {
        document.removeEventListener("drop", clear);
        document.removeEventListener("dragend", clear);
      };
    }, []);

    // Readonly content update: when defaultValue changes and editor is readonly,
    // re-set the content (e.g. after editing a comment, the readonly view updates)
    useEffect(() => {
      if (!editor || editable) return;
      if (defaultValue === prevContentRef.current) return;
      prevContentRef.current = defaultValue;
      const processed = defaultValue ? preprocessMarkdown(defaultValue) : "";
      if (processed) {
        editor.commands.setContent(processed, { contentType: "markdown" });
      } else {
        editor.commands.clearContent();
      }
    }, [editor, editable, defaultValue]);

    useImperativeHandle(ref, () => ({
      getMarkdown: () => editor?.getMarkdown() ?? "",
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
    }));

    if (!editor) return null;

    return (
      <div
        className={cn("relative min-h-full", dragOver && "editor-drag-over")}
        onDragEnter={(e) => {
          e.preventDefault();
          if (editable && e.dataTransfer.types.includes("Files"))
            setDragOver(true);
        }}
        onDragOver={(e) => {
          e.preventDefault();
        }}
        onDragLeave={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node))
            setDragOver(false);
        }}
        onDrop={(e) => {
          const alreadyHandled = e.nativeEvent.defaultPrevented;
          e.preventDefault();
          setDragOver(false);
          // Only upload if ProseMirror didn't already handle the drop.
          // When drop lands on the editor area, ProseMirror's handleDrop
          // processes it and calls preventDefault on the native event.
          // This fallback only fires when the overlay intercepted the drop.
          if (alreadyHandled) return;
          const files = e.dataTransfer?.files;
          if (files?.length && editor && onUploadFileRef.current) {
            const endPos = editor.state.doc.content.size;
            for (const file of Array.from(files)) {
              uploadAndInsertFile(editor, file, onUploadFileRef.current, endPos);
            }
          }
        }}
      >
        <EditorContent editor={editor} />
        {dragOver && (
          <div className="editor-drop-overlay">
            <p>Drop files to upload</p>
          </div>
        )}
      </div>
    );
  },
);

export { ContentEditor, type ContentEditorProps, type ContentEditorRef };
