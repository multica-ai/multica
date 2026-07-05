"use client";

// EditorImageTray (FIR-2693 follow-up). The numbered image tray, extended from
// message/comment composers to the shared ContentEditor used by FIELD editors:
// issue descriptions, notes, and documents. Behind the same
// `cerebro_composer_image_tray` flag, images dropped/pasted into a field collect
// as numbered thumbnails above it instead of landing inline in the text.
//
// A field differs from a composer in one way that shapes this whole file: it has
// no "send". Its markdown IS the persisted value and is re-edited later. So the
// tray must round-trip — completed images live as a trailing block of
// `![image N](url)` lines in the saved markdown (what `serializeTrayImages`
// emits), and on load we lift that block back out of the body into the tray
// (`parseTrayImages`). The upstream ContentEditor is never modified: this
// wrapper owns the split, the tray UI, and the capture-phase drop/paste routing.

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import { cn } from "@multica/ui/lib/utils";
import { createSafeId } from "@multica/core/utils";
import {
  ContentEditor,
  type ContentEditorProps,
  type ContentEditorRef,
  FileDropOverlay,
  useAttachmentPreview,
} from "@multica/views/editor";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { ComposerImageTray } from "./composer-image-tray";
import {
  useImageTray,
  serializeTrayImages,
  type ImageTrayItem,
} from "./use-image-tray";
import { parseTrayImages, combineBodyAndTray } from "./parse-tray-images";

export type EditorImageTrayProps = ContentEditorProps;

/**
 * Drop-in replacement for ContentEditor in field editors. When the tray flag is
 * off it renders a plain ContentEditor (today's inline-image behavior); when on
 * it wraps the editor with the numbered tray + round-trip persistence below.
 */
export const EditorImageTray = forwardRef<ContentEditorRef, EditorImageTrayProps>(
  function EditorImageTray(props, ref) {
    const enabled = useFeatureFlag("cerebro_composer_image_tray");
    if (!enabled) return <ContentEditor ref={ref} {...props} />;
    return <FieldImageTray ref={ref} {...props} />;
  },
);

const FieldImageTray = forwardRef<ContentEditorRef, EditorImageTrayProps>(
  function FieldImageTray(
    { defaultValue, onUpdate, onUploadFile, className, ...rest },
    ref,
  ) {
    const innerRef = useRef<ContentEditorRef>(null);
    const onUpdateRef = useRef(onUpdate);
    onUpdateRef.current = onUpdate;

    const preview = useAttachmentPreview();

    // Parse the saved value ONCE on mount: body seeds the editor, the trailing
    // image block seeds the tray. Re-keying the host (e.g. `key={note.id}`)
    // remounts this component and re-parses, which is the intended reset point.
    const initial = useMemo(
      () => parseTrayImages(defaultValue ?? ""),
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [],
    );
    const seededItems = useMemo<ImageTrayItem[]>(
      () =>
        initial.images.map((img) => ({
          localId: createSafeId(),
          blobUrl: "",
          filename: img.filename,
          status: "completed" as const,
          uploadedUrl: img.url,
        })),
      [initial],
    );

    const uploader = useCallback(
      (file: File) =>
        onUploadFile ? onUploadFile(file) : Promise.resolve(null),
      [onUploadFile],
    );
    const tray = useImageTray(uploader, seededItems);

    // The persisted value is always the live editor body + the serialized tray.
    // Reading the body off the ref at emit time (not a cached string) keeps it
    // fresh when a tray change races the editor's debounced onUpdate.
    const traySignature = tray.items
      .map((i) => `${i.localId}:${i.status}:${i.uploadedUrl ?? ""}`)
      .join("|");
    const emit = useCallback(() => {
      const body = innerRef.current?.getMarkdown() ?? "";
      const { markdown } = serializeTrayImages(tray.items);
      onUpdateRef.current?.(combineBodyAndTray(body, markdown));
    }, [tray.items]);

    // Editor body changed → re-emit combined content.
    const handleUpdate = useCallback(() => emit(), [emit]);

    // Tray changed (image completed / removed) → re-emit. Skip the seed render
    // so loading a note doesn't fire a redundant save of unchanged content.
    const mountedRef = useRef(false);
    useEffect(() => {
      if (!mountedRef.current) {
        mountedRef.current = true;
        return;
      }
      emit();
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [traySignature]);

    // Route dropped/pasted files: images → tray, other files → inline card.
    const routeFiles = useCallback(
      (files: File[]) => {
        const images = files.filter((f) => f.type.startsWith("image/"));
        const others = files.filter((f) => !f.type.startsWith("image/"));
        if (images.length) tray.addFiles(images);
        others.forEach((f) => innerRef.current?.uploadFile(f));
      },
      [tray],
    );
    const routeFilesRef = useRef(routeFiles);
    routeFilesRef.current = routeFiles;
    const trayAddRef = useRef(tray.addFiles);
    trayAddRef.current = tray.addFiles;

    const boxRef = useRef<HTMLDivElement>(null);
    const [dragOver, setDragOver] = useState(false);

    // Capture-phase drop/paste on the field box (an ancestor of ProseMirror's
    // contenteditable). Capturing lets us divert image files to the tray BEFORE
    // the editor's own handlers insert them inline — same technique BaseComposer
    // uses, so the upstream editor stays untouched.
    useEffect(() => {
      const el = boxRef.current;
      if (!el) return;

      const hasFiles = (dt: DataTransfer | null) =>
        !!dt && Array.from(dt.types).includes("Files");

      const onDragEnter = (e: DragEvent) => {
        if (!hasFiles(e.dataTransfer)) return;
        e.preventDefault();
        setDragOver(true);
      };
      const onDragOver = (e: DragEvent) => {
        if (!hasFiles(e.dataTransfer)) return;
        e.preventDefault();
      };
      const onDragLeave = (e: DragEvent) => {
        if (!el.contains(e.relatedTarget as Node)) setDragOver(false);
      };
      const onDrop = (e: DragEvent) => {
        setDragOver(false);
        const files = e.dataTransfer?.files;
        if (!files?.length) return;
        e.preventDefault();
        e.stopPropagation();
        routeFilesRef.current(Array.from(files));
      };
      const onPaste = (e: ClipboardEvent) => {
        const files = e.clipboardData?.files;
        const images = files
          ? Array.from(files).filter((f) => f.type.startsWith("image/"))
          : [];
        if (images.length === 0) return;
        // Don't hijack a normal text paste that merely carries an image too.
        if (e.clipboardData?.getData("text/plain")) return;
        e.preventDefault();
        e.stopPropagation();
        trayAddRef.current(images);
      };
      const clearDrag = () => setDragOver(false);

      el.addEventListener("dragenter", onDragEnter, true);
      el.addEventListener("dragover", onDragOver, true);
      el.addEventListener("dragleave", onDragLeave, true);
      el.addEventListener("drop", onDrop, true);
      el.addEventListener("paste", onPaste, true);
      document.addEventListener("drop", clearDrag);
      document.addEventListener("dragend", clearDrag);
      return () => {
        el.removeEventListener("dragenter", onDragEnter, true);
        el.removeEventListener("dragover", onDragOver, true);
        el.removeEventListener("dragleave", onDragLeave, true);
        el.removeEventListener("drop", onDrop, true);
        el.removeEventListener("paste", onPaste, true);
        document.removeEventListener("drop", clearDrag);
        document.removeEventListener("dragend", clearDrag);
      };
    }, []);

    // Forward the ContentEditor handle, but make getMarkdown return body + tray
    // so a Save button (documents) or onBlur save (notes) captures the images.
    useImperativeHandle(
      ref,
      () => ({
        getMarkdown: () =>
          combineBodyAndTray(
            innerRef.current?.getMarkdown() ?? "",
            serializeTrayImages(tray.items).markdown,
          ),
        clearContent: () => innerRef.current?.clearContent(),
        focus: () => innerRef.current?.focus(),
        insertText: (text: string) => innerRef.current?.insertText(text),
        replaceDictationPreview: (text: string) =>
          innerRef.current?.replaceDictationPreview(text),
        commitDictationPreview: (text: string) =>
          innerRef.current?.commitDictationPreview(text),
        clearDictationPreview: () => innerRef.current?.clearDictationPreview(),
        blur: () => innerRef.current?.blur(),
        uploadFile: (file: File, options?: { embedImage?: boolean }) =>
          innerRef.current?.uploadFile(file, options),
        hasActiveUploads: () => innerRef.current?.hasActiveUploads() ?? false,
      }),
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [tray.items],
    );

    return (
      <div className={cn("flex flex-col", className)}>
        {preview.modal}
        <ComposerImageTray
          items={tray.items}
          hideEmbed
          onPreview={(item) =>
            preview.open({
              kind: "url",
              url: item.uploadedUrl || item.blobUrl,
              filename: item.filename,
            })
          }
          onEmbed={() => {}}
          onRemove={tray.remove}
        />
        <div ref={boxRef} className="relative flex flex-1 flex-col">
          <ContentEditor
            ref={innerRef}
            defaultValue={initial.body}
            onUpdate={handleUpdate}
            onUploadFile={onUploadFile}
            {...rest}
          />
          {dragOver && <FileDropOverlay />}
        </div>
      </div>
    );
  },
);
