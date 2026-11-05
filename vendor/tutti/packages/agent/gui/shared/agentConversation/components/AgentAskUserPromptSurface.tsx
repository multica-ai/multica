import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
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
const CompactQuickAnswerSurface = observer(CompactQuickAnswerSurfaceRender);
const AskUserAnswerFlowSurface = observer(AskUserAnswerFlowSurfaceRender);

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

function CompactQuickAnswerSurfaceRender({
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
            <ButtonBinding1
              key={option.label}
              option={option}
              prompt={prompt}
              question={question}
              isSubmitting={isSubmitting}
              isInteractionDisabled={isInteractionDisabled}
              onSubmit={onSubmit}
            />
          ))}
        </div>
      </div>
    </section>
  );
}

function AskUserAnswerFlowSurfaceRender({
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
          <ButtonBinding2
            isSubmitting={isSubmitting}
            setCollapsed={setCollapsed}
            conversationReturn={conversationReturn}
          />
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
                <ButtonBinding3
                  key={option.label}
                  option={option}
                  active={active}
                  prompt={prompt}
                  question={question}
                  isSubmitting={isSubmitting}
                  isInteractionDisabled={isInteractionDisabled}
                  flow={flow}
                />
              );
            })}
          </div>
        ) : null}
        {question.allowFreeText !== false ? (
          <TextareaBinding4
            question={question}
            flow={flow}
            labels={labels}
            isSubmitting={isSubmitting}
            isInteractionDisabled={isInteractionDisabled}
            prompt={prompt}
          />
        ) : null}
        <div className={styles.interactivePromptActions}>
          {conversationReturn ? (
            <ButtonBinding5
              isSubmitting={isSubmitting}
              setCollapsed={setCollapsed}
              conversationReturn={conversationReturn}
            />
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
            <ButtonBinding6
              isSubmitting={isSubmitting}
              isInteractionDisabled={isInteractionDisabled}
              flow={flow}
              prompt={prompt}
              question={question}
              onSubmit={onSubmit}
              labels={labels}
            />
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

const ButtonBinding1 = observer(function ButtonBinding1({
  option,
  prompt,
  question,
  isSubmitting,
  isInteractionDisabled,
  onSubmit,
  ...slotProps
}: ButtonBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding1Props>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() =>
    onSubmit({
      requestId: prompt.requestId,
      action: "submit",
      payload: {
        ...buildAskUserAnswerPayload({
          [question.id]: option.label
        })
      }
    })
  );
  return (
    <Slot.Root {...slotProps}>
      <button
        key={option.label}
        type="button"
        className={styles.interactiveOptionButton}
        aria-label={interactiveOptionLabel(option.label, option.description)}
        data-agent-question-option-id={option.id}
        data-testid={
          option.id
            ? `agent-question-${prompt.requestId}-${question.id}-option-${option.id}`
            : undefined
        }
        disabled={isSubmitting || isInteractionDisabled}
        onClick={onClickProp1}
      >
        <span className={styles.interactiveOptionContent}>
          <span className={styles.interactiveOptionTitle}>{option.label}</span>
          {option.description ? (
            <span className={styles.interactiveOptionDescription}>
              {option.description}
            </span>
          ) : null}
        </span>
      </button>
    </Slot.Root>
  );
});

const ButtonBinding2 = observer(function ButtonBinding2({
  isSubmitting,
  setCollapsed,
  conversationReturn,
  ...slotProps
}: ButtonBinding2Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding2Props>) {
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => setCollapsed(false));
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        disabled={isSubmitting}
        onClick={onClickProp2}
      >
        {conversationReturn.continueAnswering}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding3 = observer(function ButtonBinding3({
  option,
  active,
  prompt,
  question,
  isSubmitting,
  isInteractionDisabled,
  flow,
  ...slotProps
}: ButtonBinding3Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding3Props>) {
  const onClickProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => flow.toggleOption(option.label));
  return (
    <Slot.Root {...slotProps}>
      <button
        key={option.label}
        type="button"
        className={styles.interactiveOptionButton}
        data-active={active}
        aria-pressed={active}
        aria-label={interactiveOptionLabel(option.label, option.description)}
        data-agent-question-option-id={option.id}
        data-testid={
          option.id
            ? `agent-question-${prompt.requestId}-${question.id}-option-${option.id}`
            : undefined
        }
        disabled={isSubmitting || isInteractionDisabled}
        onClick={onClickProp3}
      >
        <span className={styles.interactiveOptionContent}>
          <span className={styles.interactiveOptionTitle}>{option.label}</span>
          {option.description ? (
            <span className={styles.interactiveOptionDescription}>
              {option.description}
            </span>
          ) : null}
        </span>
      </button>
    </Slot.Root>
  );
});

const TextareaBinding4 = observer(function TextareaBinding4({
  question,
  flow,
  labels,
  isSubmitting,
  isInteractionDisabled,
  prompt
}: TextareaBinding4Props) {
  const onChangeProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<"textarea">["onChange"]>
  >((event) => flow.setFreeText(event.currentTarget.value));
  return (
    <textarea
      aria-label={question.question}
      value={flow.freeText}
      placeholder={labels.answerPlaceholder}
      disabled={isSubmitting || isInteractionDisabled}
      className={styles.interactivePromptTextarea}
      data-testid={`agent-question-${prompt.requestId}-${question.id}-custom-answer`}
      onChange={onChangeProp4}
    />
  );
});

const ButtonBinding5 = observer(function ButtonBinding5({
  isSubmitting,
  setCollapsed,
  conversationReturn,
  ...slotProps
}: ButtonBinding2Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding2Props>) {
  const onClickProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => setCollapsed(true));
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        disabled={isSubmitting}
        onClick={onClickProp5}
      >
        {conversationReturn.returnToConversation}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding6 = observer(function ButtonBinding6({
  isSubmitting,
  isInteractionDisabled,
  flow,
  prompt,
  question,
  onSubmit,
  labels,
  ...slotProps
}: ButtonBinding6Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding6Props>) {
  const onClickProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() =>
    onSubmit({
      requestId: prompt.requestId,
      action: "submit",
      payload: { ...flow.answerPayload }
    })
  );
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="default"
        size="sm"
        disabled={
          isSubmitting || isInteractionDisabled || !flow.allQuestionsAnswered
        }
        data-testid={`agent-question-${prompt.requestId}-${question.id}-submit`}
        onClick={onClickProp6}
      >
        {labels.submitAnswers}
      </Button>
    </Slot.Root>
  );
});

type ButtonBinding1Props = {
  option: import("../contracts/agentAskUserQuestionItemVM").AgentAskUserQuestionOptionVM;
  prompt: NonNullable<
    Omit<SharedAskUserSurfaceProps, "labels"> & {
      question: AskUserPrompt["questions"][number];
    }
  >["prompt"];
  question: NonNullable<
    Omit<SharedAskUserSurfaceProps, "labels"> & {
      question: AskUserPrompt["questions"][number];
    }
  >["question"];
  isSubmitting: NonNullable<
    Omit<SharedAskUserSurfaceProps, "labels"> & {
      question: AskUserPrompt["questions"][number];
    }
  >["isSubmitting"];
  isInteractionDisabled: boolean;
  onSubmit: NonNullable<
    Omit<SharedAskUserSurfaceProps, "labels"> & {
      question: AskUserPrompt["questions"][number];
    }
  >["onSubmit"];
};

type ButtonBinding2Props = {
  isSubmitting: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["isSubmitting"];
  setCollapsed: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
  conversationReturn: AgentInteractivePromptConversationReturn;
};

type ButtonBinding3Props = {
  option: import("../contracts/agentAskUserQuestionItemVM").AgentAskUserQuestionOptionVM;
  active: boolean;
  prompt: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["prompt"];
  question: import("../contracts/agentAskUserQuestionItemVM").AgentAskUserQuestionVM;
  isSubmitting: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["isSubmitting"];
  isInteractionDisabled: boolean;
  flow: import("./useAskUserAnswerFlow").AskUserAnswerFlow;
};

type TextareaBinding4Props = {
  question: import("../contracts/agentAskUserQuestionItemVM").AgentAskUserQuestionVM;
  flow: import("./useAskUserAnswerFlow").AskUserAnswerFlow;
  labels: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["labels"];
  isSubmitting: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["isSubmitting"];
  isInteractionDisabled: boolean;
  prompt: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["prompt"];
};

type ButtonBinding5Props = {
  isSubmitting: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["isSubmitting"];
  setCollapsed: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
  conversationReturn: AgentInteractivePromptConversationReturn;
};

type ButtonBinding6Props = {
  isSubmitting: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["isSubmitting"];
  isInteractionDisabled: boolean;
  flow: import("./useAskUserAnswerFlow").AskUserAnswerFlow;
  prompt: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["prompt"];
  question: import("../contracts/agentAskUserQuestionItemVM").AgentAskUserQuestionVM;
  onSubmit: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["onSubmit"];
  labels: NonNullable<
    SharedAskUserSurfaceProps & {
      conversationReturn?: AgentInteractivePromptConversationReturn;
    }
  >["labels"];
};
