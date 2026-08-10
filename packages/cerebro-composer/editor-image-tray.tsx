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
  uploadAndInsertFile,
  useAttachmentPreview,
} from "@multica/views/editor";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  MOVE_IMAGE_TO_TRAY_EVENT,
  type MoveImageToTrayDetail,
} from "@multica/cerebro-editor-image";
import { ComposerImageTray } from "./composer-image-tray";
import {
  useImageTray,
  serializeTrayImages,
  type ImageTrayItem,
} from "./use-image-tray";
import { parseTrayImages, combineBodyAndTray } from "./parse-tray-images";
import { editorImageDropLanding } from "./editor-image-drop";

export type EditorImageTrayProps = ContentEditorProps;

/**
 * Drop-in replacement for ContentEditor in field editors. The numbered tray is
 * active when either the legacy tray or the unified editor-images feature is on.
 */
export const EditorImageTray = forwardRef<ContentEditorRef, EditorImageTrayProps>(
  function EditorImageTray(props, ref) {
    const trayEnabled = useFeatureFlag("cerebro_composer_image_tray");
    const editorImagesEnabled = useFeatureFlag("cerebro_editor_images");
    const enabled = trayEnabled || editorImagesEnabled;
    if (!enabled) return <ContentEditor ref={ref} {...props} />;
    return <FieldImageTray ref={ref} {...props} />;
  },
);

const FieldImageTray = forwardRef<ContentEditorRef, EditorImageTrayProps>(
  function FieldImageTray(
    { defaultValue, onUpdate, onUploadFile, onEditorReady, className, ...rest },
    ref,
  ) {
    const innerRef = useRef<ContentEditorRef>(null);
    const onUpdateRef = useRef(onUpdate);
    onUpdateRef.current = onUpdate;

    // `getMarkdown()` answers "" for every note while the inner editor has no
    // live instance, so anything that persists during that window writes an
    // empty body over real content. Track the instance rather than a
    // "mounted once" flag: a remount destroys the editor and builds a new one,
    // and only a live instance can be trusted to report the body.
    const editorRef = useRef<Parameters<
      NonNullable<ContentEditorProps["onEditorReady"]>
    >[0] | null>(null);
    const onEditorReadyRef = useRef(onEditorReady);
    onEditorReadyRef.current = onEditorReady;
    const handleEditorReady = useCallback<
      NonNullable<ContentEditorProps["onEditorReady"]>
    >((editor) => {
      editorRef.current = editor;
      onEditorReadyRef.current?.(editor);
    }, []);
    const editorIsLive = () =>
      Boolean(editorRef.current && !editorRef.current.isDestroyed);

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
          caption: img.caption,
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
      // The editor's own onUpdate always has a live instance behind it; this is
      // the only path that can fire while there is none, and the body it would
      // read then is "" regardless of what the note actually holds.
      if (!editorIsLive()) return;
      emit();
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [traySignature]);

    const trayAddRef = useRef(tray.addFiles);
    trayAddRef.current = tray.addFiles;
    const trayAddUploadedRef = useRef(tray.addUploaded);
    trayAddUploadedRef.current = tray.addUploaded;
    const uploadRef = useRef(onUploadFile);
    uploadRef.current = onUploadFile;

    const rootRef = useRef<HTMLDivElement>(null);
    const editorBoxRef = useRef<HTMLDivElement>(null);
    const [landingTop, setLandingTop] = useState<number | null>(null);

    const placeInText = useCallback(
      (item: ImageTrayItem) => {
        const editor = editorRef.current;
        if (!editor || !item.uploadedUrl) return;
        const content: Array<Record<string, unknown>> = [
          {
            type: "image",
            attrs: {
              src: item.uploadedUrl,
              alt: item.filename,
              placement: "inline",
            },
          },
        ];
        if (item.caption) {
          content.push({
            type: "imageCaption",
            content: [{ type: "text", text: item.caption }],
          });
        }
        const placed = editor
          .chain()
          .focus()
          .insertContentAt(editor.state.selection.from, content)
          .run();
        if (placed) tray.takeForInline(item.localId);
      },
      [tray],
    );

    // Capture-phase drop/paste on the field box (an ancestor of ProseMirror's
    // contenteditable). Capturing lets us divert image files to the tray BEFORE
    // the editor's own handlers insert them inline — same technique BaseComposer
    // uses, so the upstream editor stays untouched.
    useEffect(() => {
      const el = rootRef.current;
      if (!el) return;

      const hasFiles = (dt: DataTransfer | null) =>
        !!dt && Array.from(dt.types).includes("Files");

      const isTrayTarget = (target: EventTarget | null) =>
        target instanceof Element && Boolean(target.closest("[data-image-tray]"));
      const updateLanding = (e: DragEvent) => {
        const editor = editorRef.current;
        const box = editorBoxRef.current;
        if (!editor || !box || isTrayTarget(e.target)) {
          setLandingTop(null);
          return null;
        }
        const landing = editorImageDropLanding(editor, e.clientY);
        setLandingTop(
          landing ? landing.lineY - box.getBoundingClientRect().top : null,
        );
        return landing;
      };
      const onDragEnter = (e: DragEvent) => {
        if (!hasFiles(e.dataTransfer)) return;
        e.preventDefault();
        updateLanding(e);
      };
      const onDragOver = (e: DragEvent) => {
        if (!hasFiles(e.dataTransfer)) return;
        e.preventDefault();
        updateLanding(e);
      };
      const onDragLeave = (e: DragEvent) => {
        if (!el.contains(e.relatedTarget as Node)) setLandingTop(null);
      };
      const onDrop = (e: DragEvent) => {
        setLandingTop(null);
        const files = e.dataTransfer?.files;
        if (!files?.length) return;
        const allFiles = Array.from(files);
        const images = allFiles.filter((file) =>
          file.type.startsWith("image/"),
        );
        const others = allFiles.filter(
          (file) => !file.type.startsWith("image/"),
        );
        if (images.length === 0) return;
        e.preventDefault();
        e.stopPropagation();
        if (isTrayTarget(e.target)) {
          trayAddRef.current(images);
          others.forEach((file) => innerRef.current?.uploadFile(file));
          return;
        }
        const editor = editorRef.current;
        const handler = uploadRef.current;
        if (!editor || !handler) return;
        const landing = editorImageDropLanding(editor, e.clientY);
        images.forEach((file, index) => {
          void uploadAndInsertFile(
            editor,
            file,
            handler,
            index === 0 ? landing?.position : undefined,
            { embedImage: true },
          );
        });
        others.forEach((file) => innerRef.current?.uploadFile(file));
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
      const onMoveToTray = (event: Event) => {
        const moveEvent = event as CustomEvent<MoveImageToTrayDetail>;
        if (!moveEvent.detail?.src) return;
        moveEvent.preventDefault();
        trayAddUploadedRef.current({
          uploadedUrl: moveEvent.detail.src,
          filename: moveEvent.detail.filename,
          caption: moveEvent.detail.caption,
        });
      };
      const clearDrag = () => setLandingTop(null);

      el.addEventListener("dragenter", onDragEnter, true);
      el.addEventListener("dragover", onDragOver, true);
      el.addEventListener("dragleave", onDragLeave, true);
      el.addEventListener("drop", onDrop, true);
      el.addEventListener("paste", onPaste, true);
      el.addEventListener(MOVE_IMAGE_TO_TRAY_EVENT, onMoveToTray);
      document.addEventListener("drop", clearDrag);
      document.addEventListener("dragend", clearDrag);
      return () => {
        el.removeEventListener("dragenter", onDragEnter, true);
        el.removeEventListener("dragover", onDragOver, true);
        el.removeEventListener("dragleave", onDragLeave, true);
        el.removeEventListener("drop", onDrop, true);
        el.removeEventListener("paste", onPaste, true);
        el.removeEventListener(MOVE_IMAGE_TO_TRAY_EVENT, onMoveToTray);
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
        // FIR-2714: an external attach button (e.g. the create-issue footer) calls
        // uploadFile directly. With the tray active, route image files to the tray
        // so they land at the top like drop/paste — unless the caller explicitly
        // asked to embed the image inline (options.embedImage), which stays inline.
        uploadFile: (file: File, options?: { embedImage?: boolean }) => {
          if (!options?.embedImage && file.type.startsWith("image/")) {
            tray.addFiles([file]);
            return;
          }
          innerRef.current?.uploadFile(file, options);
        },
        chooseImage: () => innerRef.current?.chooseImage(),
        hasActiveUploads: () => innerRef.current?.hasActiveUploads() ?? false,
      }),
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [tray.items],
    );

    return (
      <div
        ref={rootRef}
        data-editor-image-tray
        className={cn("flex min-w-0 max-w-full flex-col", className)}
      >
        {preview.modal}
        <ComposerImageTray
          items={tray.items}
          embedLabel="Place in text"
          onPreview={(item) =>
            preview.open({
              kind: "url",
              url: item.uploadedUrl || item.blobUrl,
              filename: item.filename,
            })
          }
          onEmbed={placeInText}
          onRemove={tray.remove}
        />
        <div
          ref={editorBoxRef}
          data-editor-image-box
          className="relative flex min-w-0 max-w-full flex-1 flex-col"
        >
          <ContentEditor
            ref={innerRef}
            defaultValue={initial.body}
            onUpdate={handleUpdate}
            onUploadFile={onUploadFile}
            {...rest}
            onEditorReady={handleEditorReady}
          />
          {landingTop != null && (
            <div
              data-testid="image-drop-landing-line"
              className="pointer-events-none absolute inset-x-0 z-20 h-0.5 bg-brand"
              style={{ top: landingTop }}
            />
          )}
        </div>
      </div>
    );
  },
);
