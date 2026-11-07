"use client";

import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import type { ReplyAnnotation } from "@multica/core/drafts/reply-annotation";
import { ReplyAnnotations as AnnotationList } from "../../common/reply-annotations";

export function ReplyAnnotations({ draftKey, ...props }: {
  draftKey: CommentDraftKey;
  annotations: ReplyAnnotation[];
  disabled: boolean;
  onEditAnnotation?: (id: string) => boolean;
}) {
  return <AnnotationList {...props}
    onRemoveAnnotation={(id) => useCommentDraftStore.getState().removeAnnotation(draftKey, id)} />;
}
