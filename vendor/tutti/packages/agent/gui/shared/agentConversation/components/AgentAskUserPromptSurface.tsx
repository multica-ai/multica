import { canonicalInteractionKey } from "@tutti-os/agent-activity-core";
import { Button } from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import { useState, type JSX } from "react";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import { MessageSquareMoreIcon } from "../../../app/renderer/components/icons/MessageSquareMoreIcon";
import type { AgentConversationPromptVM } from "../contracts/agentConversationVM";
import { buildAskUserAnswerPayload } from "../interactiveAnswerPayload";
import type {
  AgentInteractivePromptConversationReturn,
  AgentInteractivePromptSurfaceProps,
  AgentInteractivePromptVariant
} from "./AgentInteractivePromptSurface";
import {
  interactiveOptionLabel,
  interactivePromptCardClassName,
  interactivePromptClassName,
  stripPromptTitlePunctuation
} from "./interactivePromptPresentation";
import { useAskUserAnswerFlow } from "./useAskUserAnswerFlow";

type AskUserPrompt = Extract<AgentConversationPromptVM, { kind: "ask-user" }>;

type SharedAskUserSurfaceProps = Pick<
  AgentInteractivePromptSurfaceProps,
  "edgeGlow" | "isInteractionDisabled" | "isSubmitting" | "labels" | "onSubmit"
> & {
  embedded?: boolean;
  prompt: AskUserPrompt;
};

export const AgentAskUserPromptSurface = observer(
  function AgentAskUserPromptSurface({
    prompt,
    variant,
    conversationReturn,
    ...props
  }: SharedAskUserSurfaceProps & {
    variant: AgentInteractivePromptVariant;
    conversationReturn?: AgentInteractivePromptConversationReturn;
  }): JSX.Element {
    "use memo";
    const question = prompt.questions[0] ?? null;
    const useCompactQuickAnswer =
      variant === "compact" &&
      prompt.questions.length === 1 &&
      question !== null &&
      !question.multiSelect &&
      question.options.length > 0;

    if (useCompactQuickAnswer) {
      return (
        <CompactQuickAnswerSurface
          {...props}
          prompt={prompt}
          question={question}
        />
      );
    }

    return (
      <AskUserAnswerFlowSurface
        key={canonicalInteractionKey(
          prompt.agentSessionId ?? "",
          prompt.turnId ?? "",
          prompt.requestId
        )}
        {...props}
        prompt={prompt}
        conversationReturn={variant === "full" ? conversationReturn : undefined}
      />
    );
  }
);

function CompactQuickAnswerSurface({
  prompt,
  question,
  embedded = false,
  edgeGlow = false,
  isSubmitting,
  isInteractionDisabled = false,
  onSubmit
}: Omit<SharedAskUserSurfaceProps, "labels"> & {
  question: AskUserPrompt["questions"][number];
}): JSX.Element {
  return (
    <section
      className={interactivePromptClassName(embedded)}
      data-agent-interaction-id={prompt.requestId}
      data-agent-interaction-kind="ask-user"
      data-agent-question-id={question.id}
      data-testid={`agent-question-${prompt.requestId}-${question.id}`}
    >
      <div className={interactivePromptCardClassName(edgeGlow)}>
        <div className={styles.interactivePromptHeader}>
          <span className={styles.interactivePromptLead}>
            {stripPromptTitlePunctuation(question.header)}
          </span>
        </div>
        <div className={styles.interactivePromptQuestion}>
          {question.question}
        </div>
        <div className={styles.interactivePromptOptions}>
          {question.options.map((option) => (
            <button
              key={option.label}
              type="button"
              className={styles.interactiveOptionButton}
              aria-label={interactiveOptionLabel(
                option.label,
                option.description
              )}
              data-agent-question-option-id={option.id}
              data-testid={
                option.id
                  ? `agent-question-${prompt.requestId}-${question.id}-option-${option.id}`
                  : undefined
              }
              disabled={isSubmitting || isInteractionDisabled}
              onClick={() =>
                onSubmit({
                  requestId: prompt.requestId,
                  action: "submit",
                  payload: {
                    ...buildAskUserAnswerPayload({
                      [question.id]: option.label
                    })
                  }
                })
              }
            >
              <span className={styles.interactiveOptionContent}>
                <span className={styles.interactiveOptionTitle}>
                  {option.label}
                </span>
                {option.description ? (
                  <span className={styles.interactiveOptionDescription}>
                    {option.description}
                  </span>
                ) : null}
              </span>
            </button>
          ))}
        </div>
      </div>
    </section>
  );
}

function AskUserAnswerFlowSurface({
  prompt,
  conversationReturn,
  embedded = false,
  edgeGlow = false,
  isSubmitting,
  isInteractionDisabled = false,
  onSubmit,
  labels
}: SharedAskUserSurfaceProps & {
  conversationReturn?: AgentInteractivePromptConversationReturn;
}): JSX.Element {
  "use memo";
  const [collapsed, setCollapsed] = useState(false);
  const flow = useAskUserAnswerFlow({
    isSubmitting: isSubmitting || isInteractionDisabled,
    questions: prompt.questions
  });
  const question = flow.currentQuestion;

  if (!question) {
    return (
      <section className={interactivePromptClassName(embedded)}>
        <div className={interactivePromptCardClassName(edgeGlow)}>
          <div
            className={`${styles.interactivePromptLead} inline-flex items-center gap-1.5`}
          >
            <MessageSquareMoreIcon
              size={15}
              active
              aria-hidden="true"
              className="shrink-0"
            />
            {stripPromptTitlePunctuation(labels.waitingForAnswer)}
          </div>
        </div>
      </section>
    );
  }

  if (collapsed && conversationReturn) {
    return (
      <section
        className={interactivePromptClassName(embedded)}
        data-agent-interaction-id={prompt.requestId}
        data-agent-interaction-kind="ask-user"
        data-testid={`agent-question-${prompt.requestId}-collapsed`}
      >
        <div
          className={`${interactivePromptCardClassName(edgeGlow)} ${styles.interactivePromptCollapsed}`}
        >
          <span className={styles.interactivePromptLead} role="status">
            {stripPromptTitlePunctuation(labels.waitingForAnswer)}
          </span>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={isSubmitting}
            onClick={() => setCollapsed(false)}
          >
            {conversationReturn.continueAnswering}
          </Button>
        </div>
      </section>
    );
  }

  return (
    <section
      className={interactivePromptClassName(embedded)}
      data-agent-interaction-id={prompt.requestId}
      data-agent-interaction-kind="ask-user"
      data-agent-question-id={question.id}
      data-testid={`agent-question-${prompt.requestId}-${question.id}`}
    >
      <div className={interactivePromptCardClassName(edgeGlow)}>
        <div className={styles.interactivePromptHeader}>
          <span className={styles.interactivePromptLead}>
            {stripPromptTitlePunctuation(question.header)}
          </span>
          <span className={styles.interactivePromptMeta}>
            {flow.currentIndex + 1}/{prompt.questions.length}
          </span>
        </div>
        <div className={styles.interactivePromptQuestion}>
          {question.question}
        </div>
        {question.options.length > 0 ? (
          <div className={styles.interactivePromptOptions}>
            {question.options.map((option) => {
              const active = flow.selectedOptions.includes(option.label);
              return (
                <button
                  key={option.label}
                  type="button"
                  className={styles.interactiveOptionButton}
                  data-active={active}
                  aria-pressed={active}
                  aria-label={interactiveOptionLabel(
                    option.label,
                    option.description
                  )}
                  data-agent-question-option-id={option.id}
                  data-testid={
                    option.id
                      ? `agent-question-${prompt.requestId}-${question.id}-option-${option.id}`
                      : undefined
                  }
                  disabled={isSubmitting || isInteractionDisabled}
                  onClick={() => flow.toggleOption(option.label)}
                >
                  <span className={styles.interactiveOptionContent}>
                    <span className={styles.interactiveOptionTitle}>
                      {option.label}
                    </span>
                    {option.description ? (
                      <span className={styles.interactiveOptionDescription}>
                        {option.description}
                      </span>
                    ) : null}
                  </span>
                </button>
              );
            })}
          </div>
        ) : null}
        {question.allowFreeText !== false ? (
          <textarea
            aria-label={question.question}
            value={flow.freeText}
            placeholder={labels.answerPlaceholder}
            disabled={isSubmitting || isInteractionDisabled}
            className={styles.interactivePromptTextarea}
            data-testid={`agent-question-${prompt.requestId}-${question.id}-custom-answer`}
            onChange={(event) => flow.setFreeText(event.currentTarget.value)}
          />
        ) : null}
        <div className={styles.interactivePromptActions}>
          {conversationReturn ? (
            <Button
              type="button"
              variant="secondary"
              size="sm"
              disabled={isSubmitting}
              onClick={() => setCollapsed(true)}
            >
              {conversationReturn.returnToConversation}
            </Button>
          ) : null}
          {prompt.questions.length > 1 ? (
            <Button
              type="button"
              variant="secondary"
              size="sm"
              disabled={
                isSubmitting || isInteractionDisabled || flow.currentIndex === 0
              }
              onClick={flow.goToPreviousQuestion}
            >
              {labels.previousQuestion}
            </Button>
          ) : null}
          {flow.isLastQuestion ? (
            <Button
              type="button"
              variant="default"
              size="sm"
              disabled={
                isSubmitting ||
                isInteractionDisabled ||
                !flow.allQuestionsAnswered
              }
              data-testid={`agent-question-${prompt.requestId}-${question.id}-submit`}
              onClick={() =>
                onSubmit({
                  requestId: prompt.requestId,
                  action: "submit",
                  payload: { ...flow.answerPayload }
                })
              }
            >
              {labels.submitAnswers}
            </Button>
          ) : (
            <Button
              type="button"
              variant="default"
              size="sm"
              disabled={
                isSubmitting ||
                isInteractionDisabled ||
                !flow.currentQuestionAnswered
              }
              onClick={flow.goToNextQuestion}
            >
              {labels.nextQuestion}
            </Button>
          )}
        </div>
      </div>
    </section>
  );
}
