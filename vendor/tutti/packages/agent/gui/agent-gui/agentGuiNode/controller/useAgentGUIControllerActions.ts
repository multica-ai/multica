import { useMemo } from "react";
import type { AgentGUINodeViewProps } from "../view/AgentGUINodeView.types";
import { useStableControllerEventCallback } from "./agentGuiController.stableHelpers";

type AgentGUIControllerActions = AgentGUINodeViewProps["actions"];
type AgentGUIControllerActionInputs = Omit<
  AgentGUIControllerActions,
  "updateSelectedProjectPath"
> &
  Required<Pick<AgentGUIControllerActions, "updateSelectedProjectPath">>;

export function useAgentGUIControllerActions(
  actions: AgentGUIControllerActionInputs
): AgentGUIControllerActions {
  const updateConversationFilter = useStableControllerEventCallback(
    actions.updateConversationFilter
  );
  const selectConversationFilterTarget = useStableControllerEventCallback(
    actions.selectConversationFilterTarget
  );
  const selectHomeComposerAgentTarget = useStableControllerEventCallback(
    actions.selectHomeComposerAgentTarget
  );
  const createConversation = useStableControllerEventCallback(
    actions.createConversation
  );
  const selectConversation = useStableControllerEventCallback(
    actions.selectConversation
  );
  const submitPrompt = useStableControllerEventCallback(actions.submitPrompt);
  const submitGuidancePrompt = useStableControllerEventCallback(
    actions.submitGuidancePrompt
  );
  const loadOlderConversationMessages = useStableControllerEventCallback(
    actions.loadOlderConversationMessages
  );
  const showPromptImagesUnsupported = useStableControllerEventCallback(
    actions.showPromptImagesUnsupported
  );
  const submitApprovalOption = useStableControllerEventCallback(
    actions.submitApprovalOption
  );
  const submitInteractivePrompt = useStableControllerEventCallback(
    actions.submitInteractivePrompt
  );
  const interruptCurrentTurn = useStableControllerEventCallback(
    actions.interruptCurrentTurn
  );
  const updateDraftContent = useStableControllerEventCallback(
    actions.updateDraftContent
  );
  const updateSelectedProjectPath = useStableControllerEventCallback(
    actions.updateSelectedProjectPath
  );
  const updateComposerSettings = useStableControllerEventCallback(
    actions.updateComposerSettings
  );
  const retryComposerOptions = useStableControllerEventCallback(
    actions.retryComposerOptions
  );
  const updatePlanIssueBudgetPreset = useStableControllerEventCallback(
    actions.updatePlanIssueBudgetPreset
  );
  const sendQueuedPromptNext = useStableControllerEventCallback(
    actions.sendQueuedPromptNext
  );
  const removeQueuedPrompt = useStableControllerEventCallback(
    actions.removeQueuedPrompt
  );
  const editQueuedPrompt = useStableControllerEventCallback(
    actions.editQueuedPrompt
  );
  const removeProject = useStableControllerEventCallback(actions.removeProject);
  const moveProject = useStableControllerEventCallback(actions.moveProject);
  const toggleProjectPinned = useStableControllerEventCallback(
    actions.toggleProjectPinned
  );
  const toggleConversationPinned = useStableControllerEventCallback(
    actions.toggleConversationPinned
  );
  const renameConversation = useStableControllerEventCallback(
    actions.renameConversation
  );
  const forkConversationThroughTurn = useStableControllerEventCallback(
    actions.forkConversationThroughTurn
  );
  const openForkSourceConversation = useStableControllerEventCallback(
    actions.openForkSourceConversation
  );
  const requestDeleteConversation = useStableControllerEventCallback(
    actions.requestDeleteConversation
  );
  const retryActivation = useStableControllerEventCallback(
    actions.retryActivation
  );
  const continueInNewConversation = useStableControllerEventCallback(
    actions.continueInNewConversation
  );
  const cancelDeleteConversation = useStableControllerEventCallback(
    actions.cancelDeleteConversation
  );
  const confirmDeleteConversation = useStableControllerEventCallback(
    actions.confirmDeleteConversation
  );

  return useMemo(
    () => ({
      updateConversationFilter,
      selectConversationFilterTarget,
      selectHomeComposerAgentTarget,
      createConversation,
      selectConversation,
      submitPrompt,
      submitGuidancePrompt,
      loadOlderConversationMessages,
      showPromptImagesUnsupported,
      submitApprovalOption,
      submitInteractivePrompt,
      interruptCurrentTurn,
      updateDraftContent,
      updateSelectedProjectPath,
      updateComposerSettings,
      retryComposerOptions,
      updatePlanIssueBudgetPreset,
      sendQueuedPromptNext,
      removeQueuedPrompt,
      editQueuedPrompt,
      removeProject,
      moveProject,
      toggleProjectPinned,
      toggleConversationPinned,
      renameConversation,
      forkConversationThroughTurn,
      openForkSourceConversation,
      requestDeleteConversation,
      retryActivation,
      continueInNewConversation,
      cancelDeleteConversation,
      confirmDeleteConversation
    }),
    [
      cancelDeleteConversation,
      confirmDeleteConversation,
      continueInNewConversation,
      createConversation,
      editQueuedPrompt,
      interruptCurrentTurn,
      loadOlderConversationMessages,
      removeProject,
      moveProject,
      toggleProjectPinned,
      removeQueuedPrompt,
      renameConversation,
      forkConversationThroughTurn,
      openForkSourceConversation,
      requestDeleteConversation,
      retryActivation,
      selectConversation,
      selectConversationFilterTarget,
      selectHomeComposerAgentTarget,
      sendQueuedPromptNext,
      showPromptImagesUnsupported,
      submitApprovalOption,
      submitGuidancePrompt,
      submitInteractivePrompt,
      submitPrompt,
      toggleConversationPinned,
      updateComposerSettings,
      retryComposerOptions,
      updatePlanIssueBudgetPreset,
      updateConversationFilter,
      updateDraftContent,
      updateSelectedProjectPath
    ]
  );
}
