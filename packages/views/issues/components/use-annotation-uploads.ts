"use client";

import { useMemo, useRef, type RefObject } from "react";
import type { DraftUpload } from "@multica/core/drafts";
import type { ReplyAnnotation } from "@multica/core/drafts/reply-annotation";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import type { ContentEditorRef } from "../../editor";
import { useCoordinatedUploads, type UploadDraftBinding } from "../../editor/use-coordinated-uploads";
import type { UploadGate } from "../../editor/use-upload-gate";

const EMPTY_UPLOADS: DraftUpload[] = [];
const TRAY_GATE: UploadGate = { uploading: false, onUploadingChange: () => {}, isBlocked: () => false };

function selectUploads(uploads: DraftUpload[], annotation?: ReplyAnnotation): DraftUpload[] {
  const ids = annotation?.attachmentUploadIds;
  return ids?.length ? uploads.filter((upload) => ids.includes(upload.clientUploadId)) : EMPTY_UPLOADS;
}

export function useAnnotationUploads(
  draftKey: CommentDraftKey | undefined,
  annotation: ReplyAnnotation,
  existing: boolean,
  editorRef: RefObject<ContentEditorRef | null>,
) {
  const latest = useRef({ annotation, existing });
  latest.current = { annotation, existing };
  const annotationId = annotation.id;
  const pending = useCommentDraftStore((s) => draftKey ? s.getPendingAnnotation(draftKey) : undefined);
  const uploads = useCommentDraftStore((s) => draftKey ? s.getUploads(draftKey) : EMPTY_UPLOADS);
  const hadAttachments = useCommentDraftStore((s) => !!draftKey &&
    s.getAnnotations(draftKey).some((saved) => saved.id === annotationId && !!saved.attachmentUploadIds?.length));
  const selection = pending?.annotation.id === annotationId ? pending.annotation : annotation;
  const visibleUploads = useMemo(() => selectUploads(uploads, selection), [uploads, selection]);

  const binding = useMemo((): UploadDraftBinding | undefined => {
    if (!draftKey) return;
    const getPending = () => {
      const pending = useCommentDraftStore.getState().getPendingAnnotation(draftKey);
      return pending?.annotation.id === annotationId ? pending : undefined;
    };
    return {
      placement: "tray",
      registryKey: `annotation:${draftKey}:${annotationId}`,
      // A late completion can only settle an upload the original note still owns.
      getUploads: () => selectUploads(useCommentDraftStore.getState().getUploads(draftKey), getPending()?.annotation),
      addUpload: (upload) => {
        const store = useCommentDraftStore.getState();
        const current = getPending() ?? latest.current;
        store.addUpload(draftKey, upload);
        store.setPendingAnnotation(draftKey, {
          ...current,
          annotation: {
            ...current.annotation,
            note: editorRef.current?.getMarkdown() ?? current.annotation.note,
            attachmentUploadIds: [...(current.annotation.attachmentUploadIds ?? []), upload.clientUploadId],
          },
        });
      },
      settleUpload: (id, attachment) => useCommentDraftStore.getState().settleUpload(draftKey, id, attachment),
      failUpload: (id, reason) => useCommentDraftStore.getState().failUpload(draftKey, id, reason),
      removeUpload: (id) => {
        const store = useCommentDraftStore.getState();
        const current = getPending() ?? latest.current;
        store.setPendingAnnotation(draftKey, {
          ...current,
          annotation: {
            ...current.annotation,
            attachmentUploadIds: current.annotation.attachmentUploadIds?.filter((uploadId) => uploadId !== id) ?? [],
          },
        });
        // Keep the saved copy until the edit is confirmed. Escape restores it.
        if (!store.getAnnotations(draftKey).some((saved) => saved.attachmentUploadIds?.includes(id))) {
          store.removeUpload(draftKey, id);
        }
      },
    };
  }, [annotationId, draftKey, editorRef]);

  const coordinated = useCoordinatedUploads(
    binding, visibleUploads, { chatSessionId: draftKey?.slice("chat:".length) }, TRAY_GATE, editorRef,
  );
  const removedAllAttachments = existing && hadAttachments &&
    pending?.annotation.id === annotationId && pending.annotation.attachmentUploadIds?.length === 0;
  return { ...coordinated, removedAllAttachments };
}
