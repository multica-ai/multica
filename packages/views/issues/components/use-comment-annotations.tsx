"use client";

import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import { useReplyAnnotations } from "../../common/use-reply-annotations";

export function useCommentAnnotations({ draftKey, ...options }: {
  draftKey: CommentDraftKey;
  sources: { id: string; name: string; revision?: number }[];
  enabled: boolean;
  onAdded?: () => void;
  editable?: boolean;
}) {
  const annotations = useCommentDraftStore((s) => s.getAnnotations(draftKey));
  return useReplyAnnotations({
    ...options, draftKey, annotations,
    actions: {
      add: (annotation) => useCommentDraftStore.getState().addAnnotation(draftKey, annotation) ?? null,
      update: (id, note) => useCommentDraftStore.getState().updateAnnotation(draftKey, id, note),
      remove: (id) => useCommentDraftStore.getState().removeAnnotation(draftKey, id),
    },
  });
}
