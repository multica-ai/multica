import { useCallback, useRef, type RefObject } from "react";
import type { ConversationServices } from "../../../conversationServices";
import {
  agentComposerDraftHasContent,
  agentComposerDraftImages,
  snapshotAgentComposerDraft,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";
import {
  areAgentComposerHistoryDraftsEqual,
  EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE,
  navigateAgentComposerInputHistory,
  resolvePendingAgentComposerInputHistory,
  type AgentComposerInputHistoryEntry,
  type AgentComposerInputHistoryState
} from "../model/agentComposerInputHistory";
import type { AgentComposerDraft } from "../model/agentGuiNodeTypes";
import { QueuedPromptImageLoadOwner } from "../queuedPromptImageLoadOwner";

interface Input {
  agentSessionId: string | null;
  currentDraft: AgentComposerDraft;
  draftByScopeKeyRef: RefObject<Record<string, AgentComposerDraft>>;
  draftScopeKey: string;
  entries: readonly AgentComposerInputHistoryEntry[];
  hasOlderPage: boolean;
  isLoadingOlderPage: boolean;
  onDraftContentChange: (
    draft: AgentComposerDraft,
    sourceScopeKey?: string
  ) => void;
  onRequestOlderPage?: () => void;
  runtime: ConversationServices | null;
  workspaceId: string;
}

export function useComposerInputHistory(input: Input): {
  disposeInputHistory: () => void;
  onHistoryNavigation: (direction: "older" | "newer") => boolean;
  settlePendingInputHistory: () => void;
} {
  const cursorRef = useRef<AgentComposerInputHistoryState>(
    EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE
  );
  const ownerScopeRef = useRef(input.draftScopeKey);
  const imageOwnersRef = useRef<QueuedPromptImageLoadOwner[]>([]);

  const disposeInputHistory = useCallback(() => {
    for (const owner of imageOwnersRef.current) {
      owner.dispose();
    }
    imageOwnersRef.current = [];
  }, []);
  if (ownerScopeRef.current !== input.draftScopeKey) {
    ownerScopeRef.current = input.draftScopeKey;
    cursorRef.current = EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE;
    disposeInputHistory();
  }

  const restoreDraft = useCallback(
    (draft: AgentComposerDraft, entryId: string | null) => {
      disposeInputHistory();
      const restoredDraft = snapshotAgentComposerDraft(draft);
      input.draftByScopeKeyRef.current[input.draftScopeKey] = restoredDraft;
      input.onDraftContentChange(restoredDraft, input.draftScopeKey);
      if (!input.runtime || !input.agentSessionId || !entryId) {
        return;
      }
      for (const restoredImage of agentComposerDraftImages(restoredDraft)) {
        const attachmentId = restoredImage.attachmentId?.trim() ?? "";
        const path = restoredImage.path?.trim() ?? "";
        if (restoredImage.previewUrl || (!attachmentId && !path)) {
          continue;
        }
        const owner = new QueuedPromptImageLoadOwner(
          {
            agentSessionId: input.agentSessionId,
            attachmentId,
            imageKey: restoredImage.id,
            mimeType: restoredImage.mimeType,
            name: restoredImage.name,
            path,
            remoteUrl: restoredImage.url?.trim() ?? "",
            runtime: input.runtime,
            workspaceId: input.workspaceId
          },
          (source) => {
            if (
              !source ||
              cursorRef.current.entryId !== entryId ||
              ownerScopeRef.current !== input.draftScopeKey
            ) {
              return;
            }
            const currentDraft =
              input.draftByScopeKeyRef.current[input.draftScopeKey];
            if (!currentDraft) {
              return;
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
            if (updated) {
              const updatedDraft = updateAgentComposerDraft(currentDraft, {
                images
              });
              input.draftByScopeKeyRef.current[input.draftScopeKey] =
                updatedDraft;
              input.onDraftContentChange(updatedDraft, input.draftScopeKey);
            }
          }
        );
        imageOwnersRef.current.push(owner);
        owner.start();
      }
    },
    [
      disposeInputHistory,
      input.agentSessionId,
      input.draftByScopeKeyRef,
      input.draftScopeKey,
      input.onDraftContentChange,
      input.runtime,
      input.workspaceId
    ]
  );

  const settlePendingInputHistory = useCallback(() => {
    if (input.isLoadingOlderPage) {
      return;
    }
    const pendingEntryId = cursorRef.current.pendingOlderPage
      ? cursorRef.current.entryId
      : null;
    const pendingEntry = pendingEntryId
      ? input.entries.find((entry) => entry.id === pendingEntryId)
      : null;
    const currentDraft =
      input.draftByScopeKeyRef.current[input.draftScopeKey] ??
      input.currentDraft;
    if (
      cursorRef.current.pendingOlderPage &&
      (pendingEntry
        ? !areAgentComposerHistoryDraftsEqual(currentDraft, pendingEntry.draft)
        : agentComposerDraftHasContent(currentDraft))
    ) {
      cursorRef.current = EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE;
      return;
    }
    const resolved = resolvePendingAgentComposerInputHistory({
      entries: input.entries,
      state: cursorRef.current
    });
    if (!resolved) {
      return;
    }
    cursorRef.current = resolved.state;
    if (resolved.draft) {
      restoreDraft(resolved.draft, resolved.state.entryId);
    }
  }, [
    input.currentDraft,
    input.draftByScopeKeyRef,
    input.draftScopeKey,
    input.entries,
    input.isLoadingOlderPage,
    restoreDraft
  ]);

  const onHistoryNavigation = useCallback(
    (direction: "older" | "newer"): boolean => {
      settlePendingInputHistory();
      const navigation = navigateAgentComposerInputHistory({
        currentDraft:
          input.draftByScopeKeyRef.current[input.draftScopeKey] ??
          input.currentDraft,
        direction,
        entries: input.entries,
        hasOlderPage: input.hasOlderPage,
        state: cursorRef.current
      });
      cursorRef.current = navigation.state;
      if (navigation.draft) {
        restoreDraft(navigation.draft, navigation.state.entryId);
      }
      if (navigation.requestOlderPage) {
        input.onRequestOlderPage?.();
      }
      return navigation.handled;
    },
    [
      input.currentDraft,
      input.draftByScopeKeyRef,
      input.draftScopeKey,
      input.entries,
      input.hasOlderPage,
      input.onRequestOlderPage,
      restoreDraft,
      settlePendingInputHistory
    ]
  );

  return {
    disposeInputHistory,
    onHistoryNavigation,
    settlePendingInputHistory
  };
}
