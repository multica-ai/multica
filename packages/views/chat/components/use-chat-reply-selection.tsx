"use client";

import { useChatStore } from "@multica/core/chat";
import { EMPTY_REPLY_ANNOTATIONS, MAX_REPLY_ANNOTATIONS } from "@multica/core/drafts/reply-annotation";
import { useReplyAnnotations } from "../../common/use-reply-annotations";

export function useChatReplySelection(draftKey: string, sources: { id: string; name: string }[], enabled: boolean) {
  const annotations = useChatStore((s) => s.inputDraftAnnotations[draftKey] ?? EMPTY_REPLY_ANNOTATIONS);
  return useReplyAnnotations({
    draftKey, sources, annotations, enabled,
    contentAttribute: "data-chat-reply-content", scopeAttribute: "data-chat-reply-scope",
    actions: {
      add: (annotation) => {
        const store = useChatStore.getState();
        const current = store.inputDraftAnnotations[draftKey] ?? EMPTY_REPLY_ANNOTATIONS;
        if (current.length >= MAX_REPLY_ANNOTATIONS) return null;
        store.setInputDraftAnnotations(draftKey, [...current, annotation]);
        return annotation.id;
      },
      update: (id, note) => {
        const store = useChatStore.getState();
        store.setInputDraftAnnotations(draftKey, (store.inputDraftAnnotations[draftKey] ?? [])
          .map((annotation) => annotation.id === id ? { ...annotation, note } : annotation));
      },
      remove: (id) => {
        const store = useChatStore.getState();
        store.setInputDraftAnnotations(draftKey, (store.inputDraftAnnotations[draftKey] ?? [])
          .filter((annotation) => annotation.id !== id));
      },
    },
  });
}
