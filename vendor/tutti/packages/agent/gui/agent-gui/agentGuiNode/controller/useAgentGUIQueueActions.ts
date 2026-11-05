import {
  useCallback,
  type Dispatch,
  type RefObject,
  type SetStateAction
} from "react";
import type { AgentConversationServices } from "../../../state/conversation-services";

import type { ConversationServices } from "../../../conversationServices";
import {
  agentComposerDraftImages,
  agentPromptContentToComposerDraft,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";
import { resolveAgentComposerDraftScopeKey } from "../model/agentComposerDraftScope";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import { QueuedPromptImageLoadOwner } from "../queuedPromptImageLoadOwner";

export interface UseAgentGUIQueueActionsInput {
  activeConversationIdRef: RefObject<string | null>;
  agentActivityRuntime: ConversationServices;
  conversationServices: AgentConversationServices;
  setDraftByScopeKey: Dispatch<
    SetStateAction<Record<string, AgentComposerDraft>>
  >;
  workspaceId: string;
}

/** Owns queued-prompt mutations without coupling them to session activation. */
export function useAgentGUIQueueActions({
  activeConversationIdRef,
  agentActivityRuntime,
  conversationServices,
  setDraftByScopeKey,
  workspaceId
}: UseAgentGUIQueueActionsInput) {
  const removeQueuedPrompt = useCallback(
    (queuedPromptId: string) => {
      const agentSessionId = activeConversationIdRef.current;
      const normalizedQueuedPromptId = queuedPromptId.trim();
      if (!agentSessionId || !normalizedQueuedPromptId) {
        return;
      }
      const queuedPrompt = conversationServices.prompts.queuedPrompt(
        agentSessionId,
        normalizedQueuedPromptId
      );
      queuedPrompt?.clientSubmitId
        ? conversationServices.prompts.update(
            agentSessionId,
            "remove",
            queuedPrompt.clientSubmitId
          )
        : conversationServices.prompts.update(
            agentSessionId,
            "remove",
            normalizedQueuedPromptId
          );
    },
    [activeConversationIdRef, conversationServices]
  );

  const editQueuedPrompt = useCallback(
    (queuedPromptId: string) => {
      const agentSessionId = activeConversationIdRef.current;
      const normalizedQueuedPromptId = queuedPromptId.trim();
      if (!agentSessionId || !normalizedQueuedPromptId) {
        return;
      }
      const queuedPrompt = conversationServices.prompts.queuedPrompt(
        agentSessionId,
        normalizedQueuedPromptId
      );
      if (!queuedPrompt) {
        return;
      }
      const draftScopeKey = resolveAgentComposerDraftScopeKey({
        agentSessionId
      });
      const restoredDraft = agentPromptContentToComposerDraft(
        queuedPrompt.content,
        `restore-${queuedPrompt.id}`
      );
      queuedPrompt.clientSubmitId
        ? conversationServices.prompts.update(
            agentSessionId,
            "remove",
            queuedPrompt.clientSubmitId
          )
        : conversationServices.prompts.update(
            agentSessionId,
            "remove",
            normalizedQueuedPromptId
          );
      setDraftByScopeKey((current) => ({
        ...current,
        [draftScopeKey]: restoredDraft
      }));
      for (const restoredImage of agentComposerDraftImages(restoredDraft)) {
        const attachmentId = restoredImage.attachmentId?.trim() ?? "";
        const path = restoredImage.path?.trim() ?? "";
        if (restoredImage.previewUrl || (!attachmentId && !path)) {
          continue;
        }
        const owner = new QueuedPromptImageLoadOwner(
          {
            agentSessionId,
            attachmentId,
            imageKey: restoredImage.id,
            mimeType: restoredImage.mimeType,
            name: restoredImage.name,
            path,
            remoteUrl: restoredImage.url?.trim() ?? "",
            runtime: agentActivityRuntime,
            workspaceId
          },
          (source) => {
            if (!source) {
              return;
            }
            setDraftByScopeKey((current) => {
              const currentDraft = current[draftScopeKey];
              if (!currentDraft) {
                return current;
              }
              let updated = false;
              const images = agentComposerDraftImages(currentDraft).map(
                (currentImage) => {
                  if (
                    currentImage.id !== restoredImage.id ||
                    currentImage.previewUrl ||
                    currentImage.attachmentId !== restoredImage.attachmentId ||
                    currentImage.path !== restoredImage.path ||
                    currentImage.url !== restoredImage.url
                  ) {
                    return currentImage;
                  }
                  updated = true;
                  return { ...currentImage, previewUrl: source };
                }
              );
              return updated
                ? {
                    ...current,
                    [draftScopeKey]: updateAgentComposerDraft(currentDraft, {
                      images
                    })
                  }
                : current;
            });
          }
        );
        owner.start();
      }
    },
    [
      activeConversationIdRef,
      agentActivityRuntime,
      conversationServices,
      setDraftByScopeKey,
      workspaceId
    ]
  );

  const sendQueuedPromptNext = useCallback(
    (queuedPromptId: string) => {
      const agentSessionId = activeConversationIdRef.current;
      const normalizedQueuedPromptId = queuedPromptId.trim();
      if (!agentSessionId || !normalizedQueuedPromptId) {
        return;
      }
      conversationServices.prompts.update(
        agentSessionId,
        "sendNow",
        normalizedQueuedPromptId
      );
    },
    [activeConversationIdRef, conversationServices]
  );

  return {
    editQueuedPrompt,
    removeQueuedPrompt,
    sendQueuedPromptNext
  };
}
