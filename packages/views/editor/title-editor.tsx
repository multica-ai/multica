"use client";

import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { useEditor, EditorContent } from "@tiptap/react";
import { Extension } from "@tiptap/core";
import { Document } from "@tiptap/extension-document";
import { Paragraph } from "@tiptap/extension-paragraph";
import { Text } from "@tiptap/extension-text";
import Placeholder from "@tiptap/extension-placeholder";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import "./title-editor.css";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface TitleEditorProps {
  defaultValue?: string;
  placeholder?: string;
  className?: string;
  autoFocus?: boolean;
  onSubmit?: () => void;
  onBlur?: (value: string) => void;
  onChange?: (value: string) => void;
  /**
   * Called once when the Tiptap instance exists and its DOM is attached
   * (creation is deferred past first paint by `immediatelyRender: false`).
   * Same contract as ContentEditorProps.onReady.
   */
  onReady?: () => void;
}

interface TitleEditorRef {
  getText: () => string;
  focus: () => void;
  /**
   * Focus and place the caret at the document position under the given
   * viewport coordinates — same contract as ContentEditorRef.focusAtCoords,
   * so readonly-first hosts (useLazyEditor) can treat both editors alike.
   * Must be called while the editor element is laid out (not display: none).
   */
  focusAtCoords: (coords: { x: number; y: number }) => void;
}

// ---------------------------------------------------------------------------
// Single-paragraph document — prevents Enter from creating new lines
// ---------------------------------------------------------------------------

const SingleLineDocument = Document.extend({
  content: "paragraph",
});

// ---------------------------------------------------------------------------
// Keyboard shortcuts: Enter → submit, Escape → blur
// ---------------------------------------------------------------------------

function createTitleKeymap(opts: {
  onSubmitRef: React.RefObject<(() => void) | undefined>;
}) {
  return Extension.create({
    name: "titleKeymap",
    addKeyboardShortcuts() {
      return {
        Enter: ({ editor }) => {
          opts.onSubmitRef.current?.();
          editor.commands.blur();
          return true;
        },
        "Shift-Enter": () => true, // swallow — no line breaks
        Escape: ({ editor }) => {
          editor.commands.blur();
          return true;
        },
      };
    },
  });
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

const TitleEditor = forwardRef<TitleEditorRef, TitleEditorProps>(
  function TitleEditor(
    {
      defaultValue = "",
      placeholder: placeholderText = "",
      className,
      autoFocus = false,
      onSubmit,
      onBlur,
      onChange,
      onReady,
    },
    ref,
  ) {
    const { t } = useT("editor");
    const onSubmitRef = useRef(onSubmit);
    const onBlurRef = useRef(onBlur);
    const onChangeRef = useRef(onChange);
    const onReadyRef = useRef(onReady);

    onSubmitRef.current = onSubmit;
    onBlurRef.current = onBlur;
    onChangeRef.current = onChange;
    onReadyRef.current = onReady;

    const editor = useEditor({
      immediatelyRender: false,
      content: defaultValue
        ? { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: defaultValue }] }] }
        : "",
      extensions: [
        SingleLineDocument,
        Paragraph,
        Text,
        Placeholder.configure({
          placeholder: placeholderText,
          showOnlyCurrent: false,
        }),
        createTitleKeymap({ onSubmitRef }),
      ],
      editorProps: {
        attributes: {
          class: cn("title-editor outline-none", className),
          role: "textbox",
          "aria-multiline": "false",
          "aria-label": placeholderText || t(($) => $.title_editor.title_aria_label),
        },
      },
      onUpdate: ({ editor: ed }) => {
        onChangeRef.current?.(ed.getText());
      },
      onBlur: ({ editor: ed }) => {
        onBlurRef.current?.(ed.getText());
      },
    });

    // Signal readonly-first hosts that the deferred editor now exists. Fired
    // from a passive effect so it runs after the commit that attached the
    // editor DOM — same pattern as ContentEditor.
    const readyFiredRef = useRef(false);
    useEffect(() => {
      if (!editor || readyFiredRef.current) return;
      readyFiredRef.current = true;
      onReadyRef.current?.();
    }, [editor]);

    // Auto-focus after mount — delay to wait for Dialog open animation
    useEffect(() => {
      if (autoFocus && editor) {
        const timer = setTimeout(() => {
          editor.commands.focus("end");
        }, 50);
        return () => clearTimeout(timer);
      }
      return undefined;
    }, [autoFocus, editor]);

    // Track the last `defaultValue` we've reconciled against, so we can tell
    // "focused + dirty" (user typed something that diverges from external)
    // apart from "focused + clean" (user just clicked in without typing).
    const lastDefaultValueRef = useRef(defaultValue);

    // Sync external `defaultValue` changes into the editor.
    // Tiptap `useEditor` consumes `content` only at mount, so a WS-driven
    // title update would otherwise leave the editor showing stale text — and
    // the next blur would silently roll the external change back via onBlur's
    // value-vs-issue.title compare.
    useEffect(() => {
      if (!editor || editor.isDestroyed) return;
      const prevDefaultValue = lastDefaultValueRef.current;
      lastDefaultValueRef.current = defaultValue;

      // Already in sync — nothing to do.
      if (editor.getText() === defaultValue) return;

      // Focused + dirty: editor text diverges from the previous external
      // value, meaning the user has typed in this session. Preserve input.
      // Focused + clean (text still equals prev defaultValue) falls through
      // so we accept the new external value instead of letting the next blur
      // roll it back.
      if (editor.isFocused && editor.getText() !== prevDefaultValue) return;

      editor.commands.setContent(
        defaultValue
          ? {
              type: "doc",
              content: [
                { type: "paragraph", content: [{ type: "text", text: defaultValue }] },
              ],
            }
          : "",
        { emitUpdate: false },
      );
    }, [defaultValue, editor]);

    useImperativeHandle(ref, () => ({
      getText: () => editor?.getText() ?? "",
      focus: () => {
        editor?.commands.focus("end");
      },
      focusAtCoords: (coords: { x: number; y: number }) => {
        if (!editor) return;
        const pos = editor.view.posAtCoords({ left: coords.x, top: coords.y });
        if (pos) editor.commands.focus(pos.pos);
        else editor.commands.focus("end");
      },
    }));

    if (!editor) return null;

    return <EditorContent editor={editor} />;
  },
);

export { TitleEditor, type TitleEditorProps, type TitleEditorRef };
