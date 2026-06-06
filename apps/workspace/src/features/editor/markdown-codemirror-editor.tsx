"use client";

import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
} from "react";
import { markdown } from "@codemirror/lang-markdown";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { EditorState, type Extension } from "@codemirror/state";
import {
  EditorView,
  keymap,
  placeholder as codemirrorPlaceholder,
  type ViewUpdate,
} from "@codemirror/view";
import { cn } from "@/lib/utils";
import type { UploadResult } from "@/shared/hooks/use-file-upload";
import "./markdown-codemirror-editor.css";

interface MarkdownCodeMirrorEditorProps {
  defaultValue?: string;
  onUpdate?: (markdown: string) => void;
  placeholder?: string;
  className?: string;
  debounceMs?: number;
  onSubmit?: () => void;
  onBlur?: () => void;
  onUploadFile?: (file: File) => Promise<UploadResult | null>;
}

interface MarkdownCodeMirrorEditorRef {
  getMarkdown: () => string;
  clearContent: () => void;
  focus: () => void;
  uploadFile: (file: File) => void;
}

function escapeMarkdownLabel(label: string): string {
  return label.replace(/\\/g, "\\\\").replace(/\]/g, "\\]");
}

function markdownForUpload(file: File, result: UploadResult): string {
  const label = escapeMarkdownLabel(result.filename);
  if (file.type.startsWith("image/")) {
    return `![${label}](${result.link})`;
  }
  return `[${label}](${result.link})`;
}

function addBlockSpacing(doc: string, from: number, to: number, markdown: string): string {
  const before = doc.slice(0, from);
  const after = doc.slice(to);
  const needsLeadingBreak = before.length > 0 && !before.endsWith("\n");
  const needsTrailingBreak = after.length > 0 && !after.startsWith("\n");
  return `${needsLeadingBreak ? "\n" : ""}${markdown}${needsTrailingBreak ? "\n" : ""}`;
}

const baseTheme = EditorView.theme({
  "&": {
    backgroundColor: "transparent",
  },
  ".cm-content": {
    whiteSpace: "pre-wrap",
    wordBreak: "break-word",
  },
  ".cm-line": {
    color: "inherit",
  },
});

const MarkdownCodeMirrorEditor = forwardRef<
  MarkdownCodeMirrorEditorRef,
  MarkdownCodeMirrorEditorProps
>(function MarkdownCodeMirrorEditor(
  {
    defaultValue = "",
    onUpdate,
    placeholder: placeholderText = "",
    className,
    debounceMs = 300,
    onSubmit,
    onBlur,
    onUploadFile,
  },
  ref,
) {
  const hostRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const defaultValueRef = useRef(defaultValue);
  const onUpdateRef = useRef(onUpdate);
  const onSubmitRef = useRef(onSubmit);
  const onBlurRef = useRef(onBlur);
  const onUploadFileRef = useRef(onUploadFile);

  onUpdateRef.current = onUpdate;
  onSubmitRef.current = onSubmit;
  onBlurRef.current = onBlur;
  onUploadFileRef.current = onUploadFile;

  const emitUpdate = (doc: string) => {
    if (!onUpdateRef.current) return;
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      onUpdateRef.current?.(doc);
    }, debounceMs);
  };

  const insertUploadedFile = async (
    view: EditorView,
    file: File,
    from: number,
    to = from,
  ): Promise<number> => {
    const handler = onUploadFileRef.current;
    if (!handler) return from;
    const result = await handler(file);
    if (!result) return from;

    const doc = view.state.doc.toString();
    const markdownText = markdownForUpload(file, result);
    const insertText = file.type.startsWith("image/")
      ? addBlockSpacing(doc, from, to, markdownText)
      : markdownText;

    view.dispatch({
      changes: { from, to, insert: insertText },
      selection: { anchor: from + insertText.length },
    });
    view.focus();
    return from + insertText.length;
  };

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    const uploadFilesAt = async (
      view: EditorView,
      files: File[],
      initialPos: number,
    ) => {
      let pos = initialPos;
      for (const file of files) {
        pos = await insertUploadedFile(view, file, pos);
      }
    };

    const extensions: Extension[] = [
      history(),
      markdown(),
      baseTheme,
      EditorView.lineWrapping,
      EditorView.contentAttributes.of({
        "aria-label": placeholderText || "Editor",
      }),
      codemirrorPlaceholder(placeholderText),
      keymap.of([
        {
          key: "Mod-Enter",
          run: () => {
            onSubmitRef.current?.();
            return true;
          },
        },
        ...historyKeymap,
        ...defaultKeymap,
      ]),
      EditorView.updateListener.of((update: ViewUpdate) => {
        if (update.docChanged) {
          emitUpdate(update.state.doc.toString());
        }
      }),
      EditorView.domEventHandlers({
        blur: () => {
          onBlurRef.current?.();
        },
        paste: (event, view) => {
          const files = Array.from(event.clipboardData?.files ?? []);
          if (files.length === 0 || !onUploadFileRef.current) return false;
          event.preventDefault();
          const pos = view.state.selection.main.from;
          void uploadFilesAt(view, files, pos);
          return true;
        },
        drop: (event, view) => {
          const files = Array.from(event.dataTransfer?.files ?? []);
          if (files.length === 0 || !onUploadFileRef.current) return false;
          event.preventDefault();
          const pos =
            view.posAtCoords({ x: event.clientX, y: event.clientY }) ??
            view.state.selection.main.from;
          view.dispatch({ selection: { anchor: pos } });
          void uploadFilesAt(view, files, pos);
          return true;
        },
      }),
    ];

    const state = EditorState.create({
      doc: defaultValue,
      extensions,
    });
    const view = new EditorView({ state, parent: host });
    viewRef.current = view;

    return () => {
      view.destroy();
      viewRef.current = null;
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  useEffect(() => {
    const view = viewRef.current;
    if (!view || defaultValue === defaultValueRef.current) return;
    defaultValueRef.current = defaultValue;
    const current = view.state.doc.toString();
    if (current === defaultValue) return;
    view.dispatch({
      changes: { from: 0, to: current.length, insert: defaultValue },
    });
  }, [defaultValue]);

  useImperativeHandle(ref, () => ({
    getMarkdown: () => viewRef.current?.state.doc.toString() ?? "",
    clearContent: () => {
      const view = viewRef.current;
      if (!view) return;
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: "" },
      });
    },
    focus: () => {
      viewRef.current?.focus();
    },
    uploadFile: (file: File) => {
      const view = viewRef.current;
      if (!view) return;
      const endPos = view.state.doc.length;
      void insertUploadedFile(view, file, endPos);
    },
  }));

  return (
    <div
      ref={hostRef}
      className={cn("markdown-codemirror-editor text-sm outline-none", className)}
    />
  );
});

export {
  MarkdownCodeMirrorEditor,
  type MarkdownCodeMirrorEditorProps,
  type MarkdownCodeMirrorEditorRef,
};
