import { selectEngineSession } from "@tutti-os/agent-activity-core";
import { useCallback, useRef } from "react";
import { translate } from "../../../i18n/index";
import { textPromptContent } from "../model/agentComposerDraft";
import {
  requestAgentGUINewConversation,
  type AgentGUINewConversationRequestOptions
} from "./agentGuiNewConversationRequest";
import { resolveConversationSummaryById } from "./useAgentConversationSelection";
import { useAgentGUIComposerSettingsActions } from "./useAgentGUIComposerSettingsActions";
import { useAgentGUIContinueConversation } from "./useAgentGUIContinueConversation";
import { useAgentGUIConversationDeletion } from "./useAgentGUIConversationDeletion";
import { useAgentGUIConversationHome } from "./useAgentGUIConversationHome";
import { useAgentGUIConversationMetadataActions } from "./useAgentGUIConversationMetadataActions";
import { useAgentGUINewConversationActivation } from "./useAgentGUINewConversationActivation";
import { useAgentGUIQueueActions } from "./useAgentGUIQueueActions";
import { useAgentGUISubmitInteractionActions } from "./useAgentGUISubmitInteractionActions";

type SubmitInput = Parameters<typeof useAgentGUISubmitInteractionActions>[0];
type HomeInput = Parameters<typeof useAgentGUIConversationHome>[0];
type NewConversationInput = Parameters<
  typeof useAgentGUINewConversationActivation
>[0];

type UseAgentGUIOperationActionsInput = Omit<
  SubmitInput,
  "isSessionMarkedNonResumable" | "startConversation"
> &
  Omit<HomeInput, "submitPrefillPrompt"> &
  Omit<NewConversationInput, "getCachedComposerOptions"> &
  Omit<
    Parameters<typeof useAgentGUIContinueConversation>[0],
    "createConversation"
  > &
  Parameters<typeof useAgentGUIComposerSettingsActions>[0] &
  Parameters<typeof useAgentGUIConversationDeletion>[0] &
  Parameters<typeof useAgentGUIQueueActions>[0] &
  Parameters<typeof useAgentGUIConversationMetadataActions>[0] &
  {
    providerComposerOptions: ReturnType<
      NewConversationInput["getCachedComposerOptions"]
    >;
  };

/**
 * Composes the vertical command owners used by the Agent GUI. This hook owns no
 * durable state; Session/Turn/Interaction state remains inside the engine.
 */
export function useAgentGUIOperationActions(
  input: UseAgentGUIOperationActionsInput
) {
  // startConversation is memoized without providerComposerOptions in its
  // dependency list. Read through a ref so Create densification sees the same
  // live options the home composer is presenting (e.g. auto after the user
  // switches away from a remembered full-access default).
  const providerComposerOptionsRef = useRef(input.providerComposerOptions);
  providerComposerOptionsRef.current = input.providerComposerOptions;
  const getCachedComposerOptions = useCallback(
    () => providerComposerOptionsRef.current,
    []
  );
  const startConversation = useAgentGUINewConversationActivation({
    ...input,
    getCachedComposerOptions
  });

  const { createConversation: enterConversationHome } =
    useAgentGUIConversationHome({
      ...input,
      submitPrefillPrompt: (prompt) => {
        queueMicrotask(() => {
          input.submitPromptRef.current(textPromptContent(prompt));
        });
      }
    });
  const createConversation = useCallback(
    (options?: AgentGUINewConversationRequestOptions) => {
      if (
        requestAgentGUINewConversation({
          activeConversationId: input.activeConversationIdRef.current,
          conversations: input.conversationsRef.current,
          createConversation: enterConversationHome,
          options,
          transientConversation: input.transientConversation,
          userProjects: input.userProjectsRef.current
        })
      ) {
        return;
      }
      input.setDetailError(
        translate("agentHost.agentGui.sessionActivationFailed")
      );
    },
    [
      enterConversationHome,
      input.activeConversationIdRef,
      input.conversationsRef,
      input.setDetailError,
      input.transientConversation
    ]
  );

  const continueInNewConversation = useAgentGUIContinueConversation(input);

  const isSessionMarkedNonResumable = useCallback(
    (agentSessionId: string): boolean => {
      if (
        selectEngineSession(input.sessionEngine.getSnapshot(), agentSessionId)
          ?.resumable === false
      ) {
        return true;
      }
      const conversation = resolveConversationSummaryById(
        input.conversationsRef.current,
        agentSessionId,
        input.transientConversation
      );
      return conversation?.resumable === false;
    },
    [input.sessionEngine, input.conversationsRef, input.transientConversation]
  );

  const submitActions = useAgentGUISubmitInteractionActions({
    ...input,
    isSessionMarkedNonResumable,
    startConversation
  });
  const settingsActions = useAgentGUIComposerSettingsActions(input);

  const deletionActions = useAgentGUIConversationDeletion(input);
  const queueActions = useAgentGUIQueueActions(input);
  const metadataActions = useAgentGUIConversationMetadataActions(input);

  return {
    ...submitActions,
    ...settingsActions,
    ...deletionActions,
    ...queueActions,
    ...metadataActions,
    continueInNewConversation,
    createConversation
  };
}
