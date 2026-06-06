"use client";

import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
} from "react";
import { closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
import { markdown } from "@codemirror/lang-markdown";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { EditorSelection, EditorState, type Extension } from "@codemirror/state";
import {
  EditorView,
  keymap,
  placeholder as codemirrorPlaceholder,
  type ViewUpdate,
} from "@codemirror/view";
import { tags } from "@lezer/highlight";
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

function toggleInlineMark(view: EditorView, marker: string): boolean {
  const selection = view.state.selection.main;
  const selected = view.state.sliceDoc(selection.from, selection.to);

  if (selection.empty) {
    view.dispatch({
      changes: { from: selection.from, insert: marker + marker },
      selection: { anchor: selection.from + marker.length },
    });
    return true;
  }

  if (selected.startsWith(marker) && selected.endsWith(marker)) {
    view.dispatch({
      changes: {
        from: selection.from,
        to: selection.to,
        insert: selected.slice(marker.length, selected.length - marker.length),
      },
    });
    return true;
  }

  view.dispatch({
    changes: { from: selection.from, to: selection.to, insert: marker + selected + marker },
    selection: { anchor: selection.to + marker.length * 2 },
  });
  return true;
}

function insertLink(view: EditorView): boolean {
  const selection = view.state.selection.main;
  const selected = view.state.sliceDoc(selection.from, selection.to) || "text";
  const markdownLink = `[${selected}](url)`;
  const urlStart = selection.from + selected.length + 3;

  view.dispatch({
    changes: { from: selection.from, to: selection.to, insert: markdownLink },
    selection: EditorSelection.range(urlStart, urlStart + 3),
  });
  return true;
}

function setHeading(view: EditorView, level: 1 | 2 | 3): boolean {
  const line = view.state.doc.lineAt(view.state.selection.main.from);
  const existing = line.text.match(/^(#{1,6})\s+/);
  const prefix = `${"#".repeat(level)} `;
  const existingMarker = existing?.[1];

  if (existingMarker && existingMarker.length === level) {
    view.dispatch({
      changes: { from: line.from, to: line.from + existing[0].length, insert: "" },
    });
    return true;
  }

  view.dispatch({
    changes: {
      from: line.from,
      to: line.from + (existing?.[0].length ?? 0),
      insert: prefix,
    },
  });
  return true;
}

function toggleListPrefix(view: EditorView, prefix: "- " | "1. "): boolean {
  const line = view.state.doc.lineAt(view.state.selection.main.from);
  const marker = line.text.match(/^(\s*)([-*+]|\d+[.)])\s+/);

  if (marker) {
    const indent = marker[1] ?? "";
    view.dispatch({
      changes: { from: line.from + indent.length, to: line.from + marker[0].length, insert: prefix },
    });
    return true;
  }

  const indent = line.text.match(/^\s*/)?.[0] ?? "";
  view.dispatch({
    changes: { from: line.from + indent.length, insert: prefix },
  });
  return true;
}

function continueMarkdownList(view: EditorView): boolean {
  const selection = view.state.selection.main;
  if (!selection.empty) return false;

  const line = view.state.doc.lineAt(selection.from);
  const beforeCursor = line.text.slice(0, selection.from - line.from);
  const afterCursor = line.text.slice(selection.from - line.from);
  const match = beforeCursor.match(/^(\s*)(([-*+])|(\d+)([.)]))\s+(.*)$/);
  if (!match) return false;

  const indent = match[1] ?? "";
  const rawMarker = match[2] ?? "-";
  const unorderedMarker = match[3];
  const orderedNumber = match[4];
  const orderedSuffix = match[5] ?? ".";
  const itemText = match[6] ?? "";
  if (itemText.trim() === "" && afterCursor.trim() === "") {
    view.dispatch({
      changes: { from: line.from, to: selection.from, insert: "" },
    });
    return true;
  }

  const nextMarker = unorderedMarker
    ? rawMarker
    : `${Number(orderedNumber) + 1}${orderedSuffix}`;

  view.dispatch({
    changes: { from: selection.from, insert: `\n${indent}${nextMarker} ` },
    selection: { anchor: selection.from + indent.length + nextMarker.length + 2 },
  });
  return true;
}

function adjustListIndent(view: EditorView, direction: "in" | "out"): boolean {
  const line = view.state.doc.lineAt(view.state.selection.main.from);
  if (!/^(\s*)([-*+]|\d+[.)])\s+/.test(line.text)) return false;

  if (direction === "in") {
    view.dispatch({
      changes: { from: line.from, insert: "  " },
      selection: { anchor: view.state.selection.main.from + 2 },
    });
    return true;
  }

  const removable = line.text.match(/^ {1,2}/)?.[0] ?? "";
  if (!removable) return true;

  view.dispatch({
    changes: { from: line.from, to: line.from + removable.length, insert: "" },
    selection: {
      anchor: Math.max(line.from, view.state.selection.main.from - removable.length),
    },
  });
  return true;
}

const markdownHighlightStyle = HighlightStyle.define([
  { tag: tags.heading1, fontSize: "1.18em", fontWeight: "700" },
  { tag: tags.heading2, fontSize: "1.08em", fontWeight: "700" },
  { tag: tags.heading3, fontWeight: "650" },
  { tag: tags.strong, fontWeight: "700" },
  { tag: tags.emphasis, fontStyle: "italic" },
  { tag: tags.link, color: "hsl(var(--primary))", textDecoration: "none" },
  { tag: tags.url, color: "hsl(var(--primary))" },
  {
    tag: tags.monospace,
    backgroundColor: "hsl(var(--muted))",
    borderRadius: "4px",
    color: "hsl(var(--foreground))",
  },
  { tag: tags.quote, color: "hsl(var(--muted-foreground))" },
  { tag: tags.list, color: "hsl(var(--foreground))" },
  { tag: tags.meta, color: "hsl(var(--muted-foreground))" },
]);

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
      closeBrackets(),
      markdown(),
      baseTheme,
      syntaxHighlighting(markdownHighlightStyle),
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
        { key: "Mod-b", run: (view) => toggleInlineMark(view, "**") },
        { key: "Mod-i", run: (view) => toggleInlineMark(view, "*") },
        { key: "Mod-e", run: (view) => toggleInlineMark(view, "`") },
        { key: "Mod-k", run: insertLink },
        { key: "Mod-Alt-1", run: (view) => setHeading(view, 1) },
        { key: "Mod-Alt-2", run: (view) => setHeading(view, 2) },
        { key: "Mod-Alt-3", run: (view) => setHeading(view, 3) },
        { key: "Mod-Shift-7", run: (view) => toggleListPrefix(view, "1. ") },
        { key: "Mod-Shift-8", run: (view) => toggleListPrefix(view, "- ") },
        { key: "Enter", run: continueMarkdownList },
        { key: "Tab", run: (view) => adjustListIndent(view, "in") },
        { key: "Shift-Tab", run: (view) => adjustListIndent(view, "out") },
        ...closeBracketsKeymap,
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
