"use client";

// Composer image tray (FIR-2693). Instead of dropping/pasting/picking images
// straight into the editor text, images collect as numbered thumbnails just
// above the input — the "craft agents" pattern. The user can preview, embed
// inline, or remove each one; on send the completed images are appended to the
// message as numbered markdown links so the numbering the user sees ("image 2")
// matches the order the reader/agent receives.
//
// This hook owns only the pending-tray state + uploads. Layout, the preview
// modal, and the drop/paste/button wiring live in BaseComposer; the serialize
// helper is a pure function so it can be unit-tested without a DOM.

import { useCallback, useEffect, useRef, useState } from "react";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { createSafeId } from "@multica/core/utils";

export type ImageTrayStatus = "uploading" | "completed" | "failed";

export interface ImageTrayItem {
  /** Stable local id assigned when the file is picked — React key + lookup id.
   *  We don't key on the server `id`, which doesn't exist until upload lands. */
  localId: string;
  /** `blob:` URL for instant preview + thumbnail, before (and after) upload. */
  blobUrl: string;
  filename: string;
  /** Kept so "embed inline" can re-insert the original file into the editor.
   *  Absent for items seeded from already-saved markdown (field editors), which
   *  have a server URL but no local File to re-upload. */
  file?: File;
  status: ImageTrayStatus;
  /** Populated on "completed" — the canonical server URL used in the submitted
   *  markdown and matched by BaseComposer's attachment-id tracking. */
  uploadedUrl?: string;
  /** Populated on "completed" — server attachment id (channels/comments). */
  attachmentId?: string;
}

export type ImageTrayUploader = (file: File) => Promise<UploadResult | null>;

/**
 * Build the trailing markdown + attachment ids for the completed tray images.
 * Pure: numbering is 1-based and matches the tray order the user sees, so a
 * later "look at image 2" lines up with `![image 2](…)` in the message.
 */
export function serializeTrayImages(items: ImageTrayItem[]): {
  markdown: string;
  attachmentIds: string[];
} {
  const completed = items.filter(
    (i) => i.status === "completed" && !!i.uploadedUrl,
  );
  const markdown = completed
    .map((i, idx) => `![image ${idx + 1}](${i.uploadedUrl})`)
    .join("\n");
  const attachmentIds = completed
    .map((i) => i.attachmentId)
    .filter((id): id is string => !!id);
  return { markdown, attachmentIds };
}

function revoke(url: string) {
  if (typeof URL !== "undefined" && typeof URL.revokeObjectURL === "function") {
    URL.revokeObjectURL(url);
  }
}

function createBlobUrl(file: File): string {
  if (typeof URL !== "undefined" && typeof URL.createObjectURL === "function") {
    return URL.createObjectURL(file);
  }
  return "";
}

export interface ImageTray {
  items: ImageTrayItem[];
  /** Add image files: create thumbnails immediately, upload in the background. */
  addFiles: (files: File[]) => void;
  remove: (localId: string) => void;
  /** Drop from the tray and hand the original file back to the caller (used to
   *  embed the image inline in the editor instead). */
  takeForEmbed: (localId: string) => File | null;
  clear: () => void;
  hasUploading: boolean;
  hasCompleted: boolean;
}

/**
 * Pending image tray state + background uploads.
 * `upload` is BaseComposer's own upload handler (toast + attachment-id map), so
 * completed items are already registered for attachment tracking.
 */
export function useImageTray(
  upload: ImageTrayUploader,
  /** Seed the tray from already-saved images (field editors lift their trailing
   *  image block into the tray on mount). Read once — later changes are ignored. */
  initialItems?: ImageTrayItem[],
): ImageTray {
  const [items, setItems] = useState<ImageTrayItem[]>(() => initialItems ?? []);
  const uploadRef = useRef(upload);
  uploadRef.current = upload;

  // Revoke every blob URL still alive when the composer unmounts.
  const itemsRef = useRef(items);
  itemsRef.current = items;
  useEffect(() => {
    return () => {
      itemsRef.current.forEach((i) => revoke(i.blobUrl));
    };
  }, []);

  const patch = useCallback(
    (localId: string, next: Partial<ImageTrayItem>) => {
      setItems((prev) =>
        prev.map((i) => (i.localId === localId ? { ...i, ...next } : i)),
      );
    },
    [],
  );

  const addFiles = useCallback(
    (files: File[]) => {
      const images = files.filter((f) => f.type.startsWith("image/"));
      if (images.length === 0) return;

      const fresh: ImageTrayItem[] = images.map((file) => ({
        localId: createSafeId(),
        blobUrl: createBlobUrl(file),
        filename: file.name,
        file,
        status: "uploading",
      }));
      setItems((prev) => [...prev, ...fresh]);

      fresh.forEach((item) => {
        if (!item.file) return;
        uploadRef.current(item.file).then(
          (result) => {
            if (result) {
              patch(item.localId, {
                status: "completed",
                uploadedUrl: result.link,
                attachmentId: result.id,
                filename: result.filename || item.filename,
              });
            } else {
              patch(item.localId, { status: "failed" });
            }
          },
          () => patch(item.localId, { status: "failed" }),
        );
      });
    },
    [patch],
  );

  // remove / takeForEmbed / clear read the current items off the ref (not from
  // inside the setItems updater) so the blob-URL revoke and the returned file
  // resolve synchronously — a state updater runs at flush time, not on call.
  const remove = useCallback((localId: string) => {
    const target = itemsRef.current.find((i) => i.localId === localId);
    if (target) revoke(target.blobUrl);
    setItems((prev) => prev.filter((i) => i.localId !== localId));
  }, []);

  const takeForEmbed = useCallback((localId: string) => {
    const target = itemsRef.current.find((i) => i.localId === localId);
    if (!target) return null;
    revoke(target.blobUrl);
    setItems((prev) => prev.filter((i) => i.localId !== localId));
    return target.file ?? null;
  }, []);

  const clear = useCallback(() => {
    itemsRef.current.forEach((i) => revoke(i.blobUrl));
    setItems([]);
  }, []);

  return {
    items,
    addFiles,
    remove,
    takeForEmbed,
    clear,
    hasUploading: items.some((i) => i.status === "uploading"),
    hasCompleted: items.some((i) => i.status === "completed"),
  };
}
