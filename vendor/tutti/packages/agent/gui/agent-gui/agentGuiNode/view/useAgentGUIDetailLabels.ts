import { useMemo } from "react";
import type { AgentGUIViewLabels } from "../AgentGUINodeView";
import { resolveAgentGUIConversationReturn } from "./agentGUIDetailModelHelpers";

interface Input {
  canAnswerPendingInteractivePromptFromComposer?: boolean;
  isCancelPending: boolean;
  labels: AgentGUIViewLabels;
}

export function useAgentGUIDetailLabels(input: Input) {
  const {
    canAnswerPendingInteractivePromptFromComposer,
    isCancelPending,
    labels
  } = input;
  const conversationFlowLabels = useMemo(
    () => ({
      thinkingLabel: labels.thinkingLabel,
      toolCallsLabel: labels.toolCallsLabel,
      processing: labels.processing,
      turnSummary: labels.turnSummary,
      userMessageLocator: labels.userMessageLocator
    }),
    [
      labels.processing,
      labels.thinkingLabel,
      labels.toolCallsLabel,
      labels.turnSummary,
      labels.userMessageLocator
    ]
  );
  const chromeLabels = useMemo(
    () => ({
      approvalRequired: labels.approvalRequired,
      authRequired: labels.authRequired,
      authLogin: labels.authLogin,
      // A pending cancel takes precedence over the connecting label.
      activatingSession: isCancelPending
        ? labels.cancellingSession
        : labels.activatingSession,
      retryActivation: labels.retryActivation,
      continueInNewConversation: labels.continueInNewConversation
    }),
    [
      labels.activatingSession,
      labels.cancellingSession,
      labels.approvalRequired,
      labels.authRequired,
      labels.continueInNewConversation,
      labels.retryActivation,
      isCancelPending
    ]
  );
  const interactivePromptLabels = useMemo(
    () => ({
      approvalLead: labels.approvalRequired,
      fileChangeApprovalLead: labels.fileChangeApprovalRequired,
      planLead: labels.planLead,
      planModes: labels.planModes,
      stayInPlan: labels.stayInPlan,
      sendFeedback: labels.sendFeedback,
      feedbackPlaceholder: labels.feedbackPlaceholder,
      previousQuestion: labels.previousQuestion,
      nextQuestion: labels.nextQuestion,
      submitAnswers: labels.submitAnswers,
      answerPlaceholder: labels.answerPlaceholder,
      waitingForAnswer: labels.waitingForAnswer,
      conversationReturn: resolveAgentGUIConversationReturn(
        labels,
        canAnswerPendingInteractivePromptFromComposer
      ),
      planImplementationLead: labels.planImplementationLead,
      planImplementationConfirm: labels.planImplementationConfirm,
      planImplementationFeedbackPlaceholder:
        labels.planImplementationFeedbackPlaceholder,
      planImplementationSend: labels.planImplementationSend,
      planImplementationSkip: labels.planImplementationSkip
    }),
    [
      labels.answerPlaceholder,
      labels.approvalRequired,
      labels.continueAnswering,
      labels.fileChangeApprovalRequired,
      labels.feedbackPlaceholder,
      labels.nextQuestion,
      labels.planLead,
      labels.planModes,
      labels.previousQuestion,
      labels.returnToConversation,
      labels.sendFeedback,
      labels.stayInPlan,
      labels.submitAnswers,
      labels.waitingForAnswer,
      canAnswerPendingInteractivePromptFromComposer,
      labels.planImplementationLead,
      labels.planImplementationConfirm,
      labels.planImplementationFeedbackPlaceholder,
      labels.planImplementationSend,
      labels.planImplementationSkip
    ]
  );

  return {
    chromeLabels,
    conversationFlowLabels,
    interactivePromptLabels
  };
}
