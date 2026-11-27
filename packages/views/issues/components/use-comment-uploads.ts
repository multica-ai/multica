"use client";

/**
 * Comment-composer binding for the coordinated-upload engine (MUL-5181, L2).
 *
 * All upload mechanics — coordinator ownership, generation guards, the
 * confirmed write-back of a finished upload's link into the draft body, the
 * combined submit gate — live in `editor/use-coordinated-uploads`. This file
 * only maps the engine onto the comment draft store, keyed by
 * {@link CommentDraftKey}. Without a key (a reply box with no persistence
 * context) the engine falls back to component-local uploads that die with the
 * mount, matching pre-L2 behavior.
 */

import { useMemo, type RefObject } from "react";
import type { DraftUpload } from "@multica/core/drafts";
import type { UploadContext } from "@multica/core/hooks/use-file-upload";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import type { UploadGate } from "../../editor/use-upload-gate";
import type { ContentEditorRef } from "../../editor/content-editor";
import {
  useCoordinatedUploads,
  type CoordinatedUploads,
  type UploadDraftBinding,
} from "../../editor/use-coordinated-uploads";

const EMPTY_UPLOADS: DraftUpload[] = [];

export type CommentUploads = CoordinatedUploads;

export function useCommentUploads(
  draftKey: CommentDraftKey | undefined,
  ctx: UploadContext,
  editorGate: UploadGate,
  editorRef: RefObject<ContentEditorRef | null>,
): CommentUploads {
  const storeUploads = useCommentDraftStore((s) =>
    draftKey ? s.getUploads(draftKey) : EMPTY_UPLOADS,
  );

  // Referentially stable per key — settle closures capture it and must keep
  // working after this component unmounts, so every accessor goes through
  // getState().
  const binding = useMemo<UploadDraftBinding | undefined>(() => {
    if (!draftKey) return undefined;
    return {
      registryKey: `comment:${draftKey}`,
      getUploads: () => useCommentDraftStore.getState().getUploads(draftKey),
      addUpload: (u) => useCommentDraftStore.getState().addUpload(draftKey, u),
      settleUpload: (id, att) => useCommentDraftStore.getState().settleUpload(draftKey, id, att),
      failUpload: (id, err) => useCommentDraftStore.getState().failUpload(draftKey, id, err),
      removeUpload: (id) => useCommentDraftStore.getState().removeUpload(draftKey, id),
      getBody: () => useCommentDraftStore.getState().getDraft(draftKey) ?? "",
      appendToBody: (md) => useCommentDraftStore.getState().appendToDraftContent(draftKey, md),
    };
  }, [draftKey]);

  return useCoordinatedUploads(binding, storeUploads, ctx, editorGate, editorRef);
}
