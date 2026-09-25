"use client";

import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { useEditor, EditorContent } from "@tiptap/react";
import { Extension } from "@tiptap/core";
import { Document } from "@tiptap/extension-document";
import { Paragraph } from "@tiptap/extension-paragraph";
import { Text } from "@tiptap/extension-text";
import Placeholder from "@tiptap/extension-placeholder";
import {
  getShortcut,
  isPlainShortcut,
  type ShortcutChord,
} from "@multica/core/shortcuts";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { createSubmitShortcutExtension } from "./extensions/submit-shortcut";
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
  /**
   * Fires on the configured `send` chord, independent of `onSubmit`'s plain
   * Enter path. Hosts that submit on plain Enter pass `onSubmit`; hosts that
   * want an explicit chord (create-issue, MUL-4931) pass this instead.
   *
   * Plain Enter is deliberately never a trigger here even when `send` is
   * configured as plain Enter: the keymap below already owns that key for
   * "finish editing", and this single-line editor has no newline to trade it
   * against. Shadowing it would silently create from a half-typed title —
   * exactly what #5532 removed.
   */
  onSubmitShortcut?: () => void;
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

/**
 * Whether `onSubmitShortcut` may fire for the configured `send` chord.
 *
 * Plain Enter is excluded on purpose. The keymap below already owns that key
 * ("finish editing"), and unlike a prose editor a single-line title has no
 * newline to trade it against — so a `send` configured as plain Enter would
 * turn every Enter into a create from a half-typed title. That is exactly the
 * misfire #5532 removed, so the title keeps plain Enter inert and only honors
 * an explicit chord. Hosts wanting plain-Enter submit still pass `onSubmit`.
 */
export function titleShortcutSubmitAllowed(
  sendShortcut: ShortcutChord | null,
): boolean {
  if (!sendShortcut) return false;
  return !isPlainShortcut(sendShortcut, "Enter");
}

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
      onSubmitShortcut,
      onBlur,
      onChange,
      onReady,
    },
    ref,
  ) {
    const { t } = useT("editor");
    const onSubmitRef = useRef(onSubmit);
    const onSubmitShortcutRef = useRef(onSubmitShortcut);
    const onBlurRef = useRef(onBlur);
    const onChangeRef = useRef(onChange);
    const onReadyRef = useRef(onReady);

    onSubmitRef.current = onSubmit;
    onSubmitShortcutRef.current = onSubmitShortcut;
    onBlurRef.current = onBlur;
    onChangeRef.current = onChange;
    onReadyRef.current = onReady;

    // `useEditor` reads `extensions` once at mount and no host toggles this
    // prop over its lifetime, so pin the mount-time answer rather than let the
    // extension list depend on whichever render happened to create the editor.
    const shortcutSubmitEnabled = useRef(onSubmitShortcut !== undefined).current;

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
        // Added last so its ProseMirror plugin sits ahead of the keymap above
        // — same placement the ContentEditor extension list relies on. It
        // brings the configured `send` chord, the IME guards, and key-repeat
        // protection with it, none of which a hand-rolled keydown would have.
        ...(shortcutSubmitEnabled
          ? [
              createSubmitShortcutExtension(() => {
                const fn = onSubmitShortcutRef.current;
                if (!fn) return false;
                if (!titleShortcutSubmitAllowed(getShortcut("send"))) return false;
                fn();
                return true;
              }),
            ]
          : []),
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
