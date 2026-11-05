import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { canonicalInteractionKey } from "@tutti-os/agent-activity-core";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import type { JSX } from "react";
import type {
  AgentConversationPromptVM,
  AgentInteractionResponseInput
} from "../contracts/agentConversationVM";
import { AgentAskUserPromptSurface } from "./AgentAskUserPromptSurface";
import {
  ApprovalPromptSurface,
  ExitPlanPromptSurface,
  PlanImplementationSurface
} from "./AgentInteractiveDecisionPromptSurfaces";

/**
 * Where the prompt is rendered, which sets its interaction budget:
 * - "full" (conversation / composer): the user is focused here, so every action
 *   is shown — primary decisions plus rich follow-ups (feedback textareas,
 *   multi-step wizards, "stay in plan").
 * - "compact" (message-center attention deck): a glanceable needs-attention card
 *   across many sessions. Ask-user prompts retain the full answer flow in a
 *   denser layout; other prompt kinds may intentionally limit secondary actions.
 */
export type AgentInteractivePromptVariant = "full" | "compact";

export interface AgentInteractivePromptConversationReturn {
  continueAnswering: string;
  returnToConversation: string;
}

export interface AgentInteractivePromptSurfaceProps {
  prompt: AgentConversationPromptVM;
  variant?: AgentInteractivePromptVariant;
  /**
   * Opt in only when the host Composer can answer this exact pending
   * Interaction. The surface also fails closed for non-canonical and
   * fixed-choice prompts.
   */
  conversationReturn?: AgentInteractivePromptConversationReturn;
  edgeGlow?: boolean;
  keyboardShortcuts?: boolean;
  isSubmitting: boolean;
  isInteractionDisabled?: boolean;
  interactionDisabledReason?: string | null;
  onSubmit: (input: AgentInteractionResponseInput) => boolean | void;
  labels: {
    approvalLead: string;
    fileChangeApprovalLead: string;
    planLead: string;
    planModes: Array<{ id: string; label: string; description: string }>;
    stayInPlan: string;
    sendFeedback: string;
    feedbackPlaceholder: string;
    previousQuestion: string;
    nextQuestion: string;
    submitAnswers: string;
    answerPlaceholder: string;
    waitingForAnswer: string;
    planImplementationLead: string;
    planImplementationConfirm: string;
    planImplementationFeedbackPlaceholder: string;
    planImplementationSend: string;
    planImplementationSkip: string;
  };
}

export const AgentInteractivePromptSurface = observer(
  function AgentInteractivePromptSurface({
    prompt,
    variant = "full",
    conversationReturn,
    edgeGlow = false,
    embedded = false,
    keyboardShortcuts = true,
    isSubmitting,
    isInteractionDisabled = false,
    interactionDisabledReason = null,
    onSubmit,
    labels
  }: AgentInteractivePromptSurfaceProps & {
    embedded?: boolean;
  }): JSX.Element | null {
    "use memo";

    const submitPrompt = (
      input: AgentInteractionResponseInput
    ): boolean | void => {
      const agentSessionId =
        "agentSessionId" in prompt ? prompt.agentSessionId?.trim() : "";
      const turnId = "turnId" in prompt ? prompt.turnId?.trim() : "";
      return onSubmit({
        ...input,
        ...(agentSessionId && turnId ? { agentSessionId, turnId } : {}),
        requestId: prompt.requestId
      });
    };
    const submitPromptCallback = usePersistCallback(submitPrompt);
    const isComposerAnswerableAskUser =
      prompt.kind === "ask-user" &&
      Boolean(prompt.agentSessionId?.trim()) &&
      Boolean(prompt.turnId?.trim()) &&
      prompt.questions.length > 0 &&
      prompt.questions.every((question) => question.allowFreeText !== false);
    const askUserConversationReturn = isComposerAnswerableAskUser
      ? conversationReturn
      : undefined;

    let promptSurface: JSX.Element;
    if (prompt.kind === "approval") {
      promptSurface = (
        <ApprovalPromptSurface
          prompt={prompt}
          embedded={embedded}
          edgeGlow={edgeGlow}
          keyboardShortcuts={keyboardShortcuts}
          isSubmitting={isSubmitting}
          isInteractionDisabled={isInteractionDisabled}
          onSubmit={submitPromptCallback}
          labels={labels}
        />
      );
    } else if (prompt.kind === "exit-plan") {
      promptSurface = (
        <ExitPlanPromptSurface
          prompt={prompt}
          variant={variant}
          embedded={embedded}
          edgeGlow={edgeGlow}
          isSubmitting={isSubmitting}
          isInteractionDisabled={isInteractionDisabled}
          onSubmit={submitPromptCallback}
          labels={labels}
        />
      );
    } else if (prompt.kind === "plan-implementation") {
      promptSurface = (
        <PlanImplementationSurface
          prompt={prompt}
          variant={variant}
          embedded={embedded}
          edgeGlow={edgeGlow}
          isSubmitting={isSubmitting}
          isInteractionDisabled={isInteractionDisabled}
          onSubmit={submitPromptCallback}
          labels={labels}
        />
      );
    } else {
      promptSurface = (
        <AgentAskUserPromptSurface
          key={canonicalInteractionKey(
            prompt.agentSessionId ?? "",
            prompt.turnId ?? "",
            prompt.requestId
          )}
          prompt={prompt}
          variant={variant}
          conversationReturn={askUserConversationReturn}
          embedded={embedded}
          edgeGlow={edgeGlow}
          isSubmitting={isSubmitting}
          isInteractionDisabled={isInteractionDisabled}
          onSubmit={submitPromptCallback}
          labels={labels}
        />
      );
    }

    const normalizedReason = interactionDisabledReason?.trim() ?? "";
    if (!isInteractionDisabled || !normalizedReason) {
      return promptSurface;
    }
    const hasPresentationNavigation =
      prompt.kind === "ask-user" &&
      variant === "full" &&
      askUserConversationReturn !== undefined;

    return (
      <TooltipProvider delayDuration={120} skipDelayDuration={0}>
        <Tooltip>
          <TooltipTrigger asChild>
            <div
              aria-disabled={hasPresentationNavigation ? undefined : "true"}
              aria-label={normalizedReason}
              className={
                hasPresentationNavigation
                  ? "rounded-md outline-none"
                  : "cursor-not-allowed rounded-md outline-none"
              }
              data-agent-interaction-disabled="true"
              role="group"
              tabIndex={0}
            >
              {/* Without a presentation-only escape action, keep disabled
                controls out of hit testing so the wrapper owns the tooltip. */}
              <div
                className={
                  hasPresentationNavigation ? undefined : "pointer-events-none"
                }
              >
                {promptSurface}
              </div>
            </div>
          </TooltipTrigger>
          <TooltipContent
            side="top"
            align="start"
            className="max-w-[min(360px,calc(100vw-32px))] whitespace-normal text-left [overflow-wrap:anywhere]"
          >
            {normalizedReason}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }
);
