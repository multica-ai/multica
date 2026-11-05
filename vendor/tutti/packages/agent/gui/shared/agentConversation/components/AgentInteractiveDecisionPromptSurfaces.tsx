import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button, ShortcutBadge } from "@tutti-os/ui-system";
import {
  getOptionalAgentHostApi,
  useOptionalAgentHostApi
} from "../../../agentActivityHost";
import {
  approvalOptionDisplayLabel,
  approvalOptionVisualPresentation
} from "../approvalOptionPresentation";
import type { AgentConversationPromptVM } from "../contracts/agentConversationVM";
import {
  PLAN_IMPLEMENTATION_ACTION_FEEDBACK,
  PLAN_IMPLEMENTATION_ACTION_IMPLEMENT,
  PLAN_IMPLEMENTATION_ACTION_SKIP
} from "../planImplementationPresentation";
import type { AgentInteractivePromptSurfaceProps } from "./AgentInteractivePromptSurface";
import {
  approvalFeedbackOptionId,
  approvalOptionShortcutLabel,
  CommandTextWithTooltip,
  formatApprovalToolPresentation,
  InteractiveOptionSpinner,
  interactiveOptionLabel,
  interactivePromptCardClassName,
  interactivePromptClassName,
  isDenyApprovalOptionToken,
  isDarwinPlatform,
  isEditableKeyboardTarget,
  isEnterLikeKey,
  PromptDetailValue,
  SendFilledIcon,
  stripPromptTitlePunctuation
} from "./interactivePromptPresentation";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import { observer } from "mobx-react-lite";
export const ApprovalPromptSurface = observer(ApprovalPromptSurfaceRender);
export const ExitPlanPromptSurface = observer(ExitPlanPromptSurfaceRender);
export const PlanImplementationSurface = observer(
  PlanImplementationSurfaceRender
);

function ApprovalPromptSurfaceRender({
  prompt,
  embedded = false,
  edgeGlow = false,
  keyboardShortcuts = true,
  isSubmitting,
  isInteractionDisabled = false,
  onSubmit,
  labels
}: AgentInteractivePromptSurfaceProps & {
  prompt: Extract<AgentConversationPromptVM, { kind: "approval" }>;
  embedded?: boolean;
}) {
  "use memo";
  const promptToolPresentation = useMemo(
    () => formatApprovalToolPresentation(prompt, labels),
    [labels, prompt]
  );
  const [submittingOptionId, setSubmittingOptionId] = useState<string | null>(
    null
  );
  const [pendingFeedbackOptionId, setPendingFeedbackOptionId] = useState<
    string | null
  >(null);
  const [feedback, setFeedback] = useState("");
  const feedbackTextareaRef = useRef<HTMLTextAreaElement | null>(null);
  const agentHostApi = useOptionalAgentHostApi() ?? getOptionalAgentHostApi();
  const isDarwin = isDarwinPlatform(agentHostApi?.meta?.platform);
  const controlsDisabled = isSubmitting || isInteractionDisabled;
  const feedbackOptionId = useMemo(
    () => approvalFeedbackOptionId(prompt.options),
    [prompt.options]
  );
  const feedbackValue = feedback.trim();
  const approveOptionId = useMemo(
    () =>
      prompt.options.find(
        (option) =>
          option.id !== feedbackOptionId &&
          !isDenyApprovalOptionToken(option.id) &&
          !isDenyApprovalOptionToken(option.kind)
      )?.id ?? null,
    [feedbackOptionId, prompt.options]
  );
  const denyOptionId = useMemo(
    () =>
      prompt.options.find(
        (option) =>
          option.id !== feedbackOptionId &&
          (isDenyApprovalOptionToken(option.id) ||
            isDenyApprovalOptionToken(option.kind))
      )?.id ?? null,
    [feedbackOptionId, prompt.options]
  );

  useEffect(() => {
    setSubmittingOptionId(null);
    setPendingFeedbackOptionId(null);
    setFeedback("");
  }, [prompt.requestId]);

  useEffect(() => {
    if (!isSubmitting) {
      setSubmittingOptionId(null);
    }
  }, [isSubmitting]);

  useEffect(() => {
    if (pendingFeedbackOptionId !== null) {
      feedbackTextareaRef.current?.focus();
    }
  }, [pendingFeedbackOptionId]);

  const submitOption = useCallback(
    (optionId: string) => {
      if (controlsDisabled) {
        return;
      }
      const feedbackOption = feedbackOptionId === optionId;
      if (feedbackOption && pendingFeedbackOptionId !== optionId) {
        setFeedback("");
        setPendingFeedbackOptionId(optionId);
        return;
      }
      setSubmittingOptionId(optionId);
      onSubmit({
        requestId: prompt.requestId,
        ...(feedbackOption ? { action: "deny" } : {}),
        optionId,
        ...(feedbackOption && feedbackValue
          ? { payload: { denyMessage: feedbackValue } }
          : {})
      });
    },
    [
      feedbackOptionId,
      feedbackValue,
      controlsDisabled,
      onSubmit,
      pendingFeedbackOptionId,
      prompt.requestId
    ]
  );
  const submitOptionCallback = usePersistCallback(submitOption);

  useEffect(() => {
    if (
      !keyboardShortcuts ||
      controlsDisabled ||
      submittingOptionId !== null ||
      prompt.options.length === 0
    ) {
      return undefined;
    }

    const handleKeyDown = (event: KeyboardEvent): void => {
      if (
        !isEnterLikeKey(event) ||
        isEditableKeyboardTarget(event.target) ||
        event.isComposing ||
        event.altKey ||
        event.shiftKey
      ) {
        return;
      }
      const optionIndex = event.metaKey || event.ctrlKey ? 1 : 0;
      const option = prompt.options[optionIndex];
      if (!option) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      submitOption(option.id);
    };

    window.addEventListener("keydown", handleKeyDown, true);
    return () => window.removeEventListener("keydown", handleKeyDown, true);
  }, [
    controlsDisabled,
    keyboardShortcuts,
    prompt.options,
    submitOption,
    submittingOptionId
  ]);

  return (
    <section
      className={interactivePromptClassName(embedded)}
      data-agent-interaction-id={prompt.requestId}
      data-agent-interaction-kind="approval"
      data-testid={`agent-interaction-${prompt.requestId}`}
    >
      <div className={interactivePromptCardClassName(edgeGlow)}>
        <div className={styles.interactivePromptLeadContent}>
          <div className={styles.interactivePromptLead}>
            {stripPromptTitlePunctuation(promptToolPresentation.lead)}
          </div>
          {promptToolPresentation.leadDetails.map((detail) => (
            <div
              key={`${detail.kind}:${detail.value}`}
              className={styles.interactivePromptQuestion}
            >
              {detail.value}
              {detail.meta ? ` ${detail.meta}` : null}
            </div>
          ))}
        </div>
        {promptToolPresentation.cardDetails.length > 0 ? (
          <div className={styles.interactivePromptOptions}>
            {promptToolPresentation.cardDetails.map((detail) => (
              <div
                key={`${detail.label}:${detail.value}`}
                className={styles.interactiveOptionDisplay}
              >
                <span className={styles.interactiveOptionTitle}>
                  {detail.label}
                </span>
                <PromptDetailValue detail={detail} />
                {detail.meta ? (
                  <span className={styles.interactiveOptionDescription}>
                    {detail.meta}
                  </span>
                ) : null}
              </div>
            ))}
          </div>
        ) : null}
        <div className={styles.interactivePromptOptions}>
          {prompt.options.map((option, optionIndex) => {
            const showSpinner = submittingOptionId === option.id;
            const optionSupportsFeedback = feedbackOptionId === option.id;
            const optionLabel = approvalOptionDisplayLabel(option, {
              feedback: optionSupportsFeedback
            });
            const optionPresentation = approvalOptionVisualPresentation(
              option,
              { feedback: optionSupportsFeedback }
            );
            const shortcutLabel = approvalOptionShortcutLabel(
              optionIndex,
              isDarwin
            );
            const showFeedbackComposer = pendingFeedbackOptionId === option.id;
            const action = approvalActionTestId(
              option.id,
              approveOptionId,
              denyOptionId,
              feedbackOptionId
            );
            if (showFeedbackComposer) {
              return (
                <div
                  key={option.id}
                  className={styles.interactiveFeedbackComposer}
                >
                  <TextareaBinding1
                    feedbackTextareaRef={feedbackTextareaRef}
                    feedback={feedback}
                    labels={labels}
                    controlsDisabled={controlsDisabled}
                    submittingOptionId={submittingOptionId}
                    optionLabel={optionLabel}
                    option={option}
                    prompt={prompt}
                    action={action}
                    setFeedback={setFeedback}
                    feedbackValue={feedbackValue}
                    submitOption={submitOptionCallback}
                  />
                  <ButtonBinding2
                    controlsDisabled={controlsDisabled}
                    submittingOptionId={submittingOptionId}
                    feedbackValue={feedbackValue}
                    labels={labels}
                    showSpinner={showSpinner}
                    prompt={prompt}
                    action={action}
                    feedbackTextareaRef={feedbackTextareaRef}
                    submitOption={submitOptionCallback}
                    option={option}
                  />
                </div>
              );
            }
            return (
              <ButtonBinding3
                key={option.id}
                option={option}
                optionLabel={optionLabel}
                action={action}
                prompt={prompt}
                controlsDisabled={controlsDisabled}
                submittingOptionId={submittingOptionId}
                submitOption={submitOptionCallback}
                optionPresentation={optionPresentation}
                keyboardShortcuts={keyboardShortcuts}
                shortcutLabel={shortcutLabel}
                showSpinner={showSpinner}
              />
            );
          })}
        </div>
      </div>
    </section>
  );
}

function approvalActionTestId(
  optionID: string,
  approveOptionID: string | null,
  denyOptionID: string | null,
  feedbackOptionID: string | null
): string {
  if (optionID === feedbackOptionID) return "feedback";
  if (optionID === denyOptionID) return "deny";
  if (optionID === approveOptionID) return "approve";
  return `option-${optionID}`;
}

function ExitPlanPromptSurfaceRender({
  prompt,
  variant = "full",
  embedded = false,
  edgeGlow = false,
  isSubmitting,
  isInteractionDisabled = false,
  onSubmit,
  labels
}: AgentInteractivePromptSurfaceProps & {
  prompt: Extract<AgentConversationPromptVM, { kind: "exit-plan" }>;
  embedded?: boolean;
}) {
  "use memo";
  const [feedback, setFeedback] = useState("");
  // Compact (message-center deck): only the permission-mode decision buttons are
  // offered; refining / staying in plan is deferred to the conversation.
  const showFeedbackFooter = variant !== "compact";
  const [submittingOptionId, setSubmittingOptionId] = useState<string | null>(
    null
  );
  const trimmed = feedback.trim();
  const continueLabel =
    trimmed === "" ? labels.stayInPlan : labels.sendFeedback;
  const controlsDisabled = isSubmitting || isInteractionDisabled;
  // Render the permission modes the runtime actually offered ("Yes, and ...")
  // so newly added modes (e.g. "auto") appear automatically. Localized
  // label/description are looked up by option id, falling back to the runtime's
  // own label for ids we don't have copy for. Only when the runtime sent no
  // options (Codex plan / legacy exitplanmode) do we use the curated defaults.
  const modes =
    prompt.options.length > 0
      ? prompt.options.map((option) => {
          const known = labels.planModes.find((mode) => mode.id === option.id);
          return {
            id: option.id,
            label: known?.label ?? option.label,
            description: known?.description ?? option.description ?? ""
          };
        })
      : labels.planModes;

  useEffect(() => {
    setSubmittingOptionId(null);
  }, [prompt.requestId]);

  useEffect(() => {
    if (!isSubmitting) {
      setSubmittingOptionId(null);
    }
  }, [isSubmitting]);

  return (
    <section
      className={interactivePromptClassName(embedded)}
      data-agent-interaction-id={prompt.requestId}
      data-agent-interaction-kind="exit-plan"
      data-testid={`agent-exit-plan-${prompt.requestId}`}
    >
      <div className={interactivePromptCardClassName(edgeGlow)}>
        <div className={styles.interactivePromptLead}>
          {stripPromptTitlePunctuation(labels.planLead)}
        </div>
        <div className={styles.interactivePromptOptions}>
          {modes.map((mode) => {
            const showSpinner = submittingOptionId === mode.id;
            return (
              <ButtonBinding4
                key={mode.id}
                mode={mode}
                controlsDisabled={controlsDisabled}
                submittingOptionId={submittingOptionId}
                setSubmittingOptionId={setSubmittingOptionId}
                onSubmit={onSubmit}
                prompt={prompt}
                showSpinner={showSpinner}
              />
            );
          })}
        </div>
        {showFeedbackFooter ? (
          <div className={styles.interactivePromptFooter}>
            <TextareaBinding5
              feedback={feedback}
              labels={labels}
              controlsDisabled={controlsDisabled}
              setFeedback={setFeedback}
            />
            <div className={styles.interactivePromptActions}>
              <ButtonBinding6
                controlsDisabled={controlsDisabled}
                onSubmit={onSubmit}
                prompt={prompt}
                trimmed={trimmed}
                continueLabel={continueLabel}
              />
            </div>
          </div>
        ) : (
          // Compact (deck): no textarea, but keep declining reachable — the deck
          // must still let the user keep planning (refining/feedback is deferred
          // to the conversation via the card's "open conversation" jump).
          <div className={styles.interactivePromptActions}>
            <ButtonBinding7
              controlsDisabled={controlsDisabled}
              onSubmit={onSubmit}
              prompt={prompt}
              labels={labels}
            />
          </div>
        )}
      </div>
    </section>
  );
}

// Codex plan-mode "implement this plan?" decision. No server request; actions
// are routed by the consumer (controller inline, desktop chrome in the message
// center) keyed on the action id rather than a server submitInteractive.
function PlanImplementationSurfaceRender({
  prompt,
  variant = "full",
  embedded = false,
  edgeGlow = false,
  isSubmitting,
  isInteractionDisabled = false,
  onSubmit,
  labels
}: AgentInteractivePromptSurfaceProps & {
  prompt: Extract<AgentConversationPromptVM, { kind: "plan-implementation" }>;
  embedded?: boolean;
}) {
  "use memo";
  const [feedback, setFeedback] = useState("");
  const trimmed = feedback.trim();
  const continueLabel =
    trimmed === ""
      ? labels.planImplementationSkip
      : labels.planImplementationSend;
  // Compact (message-center deck): only the "implement" decision is offered.
  // Refining or staying in plan mode is deferred to the conversation, reachable
  // via the card's "open conversation" jump.
  const showFeedbackFooter = variant !== "compact";
  const controlsDisabled = isSubmitting || isInteractionDisabled;

  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() =>
    onSubmit({
      requestId: prompt.requestId,
      action: PLAN_IMPLEMENTATION_ACTION_IMPLEMENT
    })
  );
  return (
    <section className={interactivePromptClassName(embedded)}>
      <div className={interactivePromptCardClassName(edgeGlow)}>
        <div className={styles.interactivePromptLead}>
          {stripPromptTitlePunctuation(labels.planImplementationLead)}
        </div>
        <div className={styles.interactivePromptOptions}>
          <button
            type="button"
            className={styles.interactiveOptionButton}
            data-testid="agent-plan-implementation-implement"
            disabled={controlsDisabled}
            onClick={onClickProp1}
          >
            <span className={styles.interactiveOptionContent}>
              <span className={styles.interactiveOptionTitle}>
                {labels.planImplementationConfirm}
              </span>
            </span>
          </button>
        </div>
        {showFeedbackFooter ? (
          <div className={styles.interactivePromptFooter}>
            <TextareaBinding8
              feedback={feedback}
              labels={labels}
              controlsDisabled={controlsDisabled}
              setFeedback={setFeedback}
            />
            <div className={styles.interactivePromptActions}>
              <ButtonBinding9
                controlsDisabled={controlsDisabled}
                onSubmit={onSubmit}
                prompt={prompt}
                trimmed={trimmed}
                continueLabel={continueLabel}
              />
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}

const TextareaBinding1 = observer(function TextareaBinding1({
  feedbackTextareaRef,
  feedback,
  labels,
  controlsDisabled,
  submittingOptionId,
  optionLabel,
  option,
  prompt,
  action,
  setFeedback,
  feedbackValue,
  submitOption
}: TextareaBinding1Props) {
  const onChangeProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<"textarea">["onChange"]>
  >((event) => setFeedback(event.currentTarget.value));
  const onKeyDownProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<"textarea">["onKeyDown"]>
  >((event) => {
    if (
      event.key !== "Enter" ||
      event.shiftKey ||
      event.nativeEvent.isComposing
    ) {
      return;
    }
    event.preventDefault();
    if (!feedbackValue) {
      feedbackTextareaRef.current?.focus();
      return;
    }
    submitOption(option.id);
  });
  return (
    <textarea
      ref={feedbackTextareaRef}
      value={feedback}
      placeholder={labels.feedbackPlaceholder}
      disabled={controlsDisabled || submittingOptionId !== null}
      className={styles.interactivePromptTextarea}
      aria-label={interactiveOptionLabel(optionLabel, option.description)}
      data-testid={`agent-approval-${prompt.requestId}-${action}-input`}
      onChange={onChangeProp2}
      onKeyDown={onKeyDownProp3}
    />
  );
});

const ButtonBinding2 = observer(function ButtonBinding2({
  controlsDisabled,
  submittingOptionId,
  feedbackValue,
  labels,
  showSpinner,
  prompt,
  action,
  feedbackTextareaRef,
  submitOption,
  option,
  ...slotProps
}: ButtonBinding2Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding2Props>) {
  const onClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => {
    if (!feedbackValue) {
      feedbackTextareaRef.current?.focus();
      return;
    }
    submitOption(option.id);
  });
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className={styles.interactiveFeedbackSendButton}
        disabled={
          controlsDisabled || submittingOptionId !== null || !feedbackValue
        }
        aria-label={labels.sendFeedback}
        title={labels.sendFeedback}
        aria-busy={showSpinner}
        data-testid={`agent-approval-${prompt.requestId}-${action}-submit`}
        onClick={onClickProp4}
      >
        {showSpinner ? <InteractiveOptionSpinner /> : <SendFilledIcon />}
      </button>
    </Slot.Root>
  );
});

const ButtonBinding3 = observer(function ButtonBinding3({
  option,
  optionLabel,
  action,
  prompt,
  controlsDisabled,
  submittingOptionId,
  submitOption,
  optionPresentation,
  keyboardShortcuts,
  shortcutLabel,
  showSpinner,
  ...slotProps
}: ButtonBinding3Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding3Props>) {
  const onClickProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => submitOption(option.id));
  return (
    <Slot.Root {...slotProps}>
      <button
        key={option.id}
        type="button"
        className={styles.interactiveOptionButton}
        aria-label={interactiveOptionLabel(optionLabel, option.description)}
        data-agent-approval-action={action}
        data-agent-approval-option-id={option.id}
        data-testid={`agent-approval-${prompt.requestId}-${action}`}
        disabled={controlsDisabled || submittingOptionId !== null}
        onClick={onClickProp5}
      >
        <span className={styles.interactiveOptionContent}>
          <span className={styles.interactiveOptionTitle}>
            {optionPresentation.label}
          </span>
          {optionPresentation.commandPrefix ? (
            <CommandTextWithTooltip
              value={optionPresentation.commandPrefix}
              testId="agent-interactive-command-prefix-option"
              tooltipsEnabled
            />
          ) : null}
          {option.description ? (
            <span className={styles.interactiveOptionDescription}>
              {option.description}
            </span>
          ) : null}
        </span>
        {keyboardShortcuts && shortcutLabel && !showSpinner ? (
          <ShortcutBadge
            className={styles.interactiveOptionShortcut}
            aria-hidden="true"
          >
            {shortcutLabel}
          </ShortcutBadge>
        ) : null}
        {showSpinner ? <InteractiveOptionSpinner /> : null}
      </button>
    </Slot.Root>
  );
});

const ButtonBinding4 = observer(function ButtonBinding4({
  mode,
  controlsDisabled,
  submittingOptionId,
  setSubmittingOptionId,
  onSubmit,
  prompt,
  showSpinner,
  ...slotProps
}: ButtonBinding4Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding4Props>) {
  const onClickProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => {
    setSubmittingOptionId(mode.id);
    onSubmit({
      requestId: prompt.requestId,
      action: "allow",
      optionId: mode.id
    });
  });
  return (
    <Slot.Root {...slotProps}>
      <button
        key={mode.id}
        type="button"
        className={styles.interactiveOptionButton}
        aria-label={interactiveOptionLabel(mode.label, mode.description)}
        data-testid={`agent-exit-plan-confirm-${mode.id}`}
        disabled={controlsDisabled || submittingOptionId !== null}
        onClick={onClickProp6}
      >
        <span className={styles.interactiveOptionContent}>
          <span className={styles.interactiveOptionTitle}>{mode.label}</span>
          <span className={styles.interactiveOptionDescription}>
            {mode.description}
          </span>
        </span>
        {showSpinner ? <InteractiveOptionSpinner /> : null}
      </button>
    </Slot.Root>
  );
});

const TextareaBinding5 = observer(function TextareaBinding5({
  feedback,
  labels,
  controlsDisabled,
  setFeedback
}: TextareaBinding5Props) {
  const onChangeProp7 = usePersistCallback<
    NonNullable<React.ComponentProps<"textarea">["onChange"]>
  >((event) => setFeedback(event.currentTarget.value));
  return (
    <textarea
      value={feedback}
      placeholder={labels.feedbackPlaceholder}
      disabled={controlsDisabled}
      className={styles.interactivePromptTextarea}
      data-testid="agent-exit-plan-feedback"
      onChange={onChangeProp7}
    />
  );
});

const ButtonBinding6 = observer(function ButtonBinding6({
  controlsDisabled,
  onSubmit,
  prompt,
  trimmed,
  continueLabel,
  ...slotProps
}: ButtonBinding6Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding6Props>) {
  const onClickProp8 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() =>
    onSubmit({
      requestId: prompt.requestId,
      // The runtime models exit-plan as an approval that requires
      // an option id, so "keep planning" submits its `plan` option
      // id (when present) rather than a bare deny. `action: deny`
      // is kept so the controller doesn't flip plan mode off.
      action: "deny",
      ...(prompt.keepPlanningOptionId
        ? { optionId: prompt.keepPlanningOptionId }
        : {}),
      payload: trimmed ? { denyMessage: trimmed } : undefined
    })
  );
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        disabled={controlsDisabled}
        data-testid="agent-exit-plan-keep-planning"
        onClick={onClickProp8}
      >
        {continueLabel}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding7 = observer(function ButtonBinding7({
  controlsDisabled,
  onSubmit,
  prompt,
  labels,
  ...slotProps
}: ButtonBinding7Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding7Props>) {
  const onClickProp9 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() =>
    onSubmit({
      requestId: prompt.requestId,
      action: "deny",
      ...(prompt.keepPlanningOptionId
        ? { optionId: prompt.keepPlanningOptionId }
        : {})
    })
  );
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        disabled={controlsDisabled}
        data-testid="agent-exit-plan-keep-planning"
        onClick={onClickProp9}
      >
        {labels.stayInPlan}
      </Button>
    </Slot.Root>
  );
});

const TextareaBinding8 = observer(function TextareaBinding8({
  feedback,
  labels,
  controlsDisabled,
  setFeedback
}: TextareaBinding8Props) {
  const onChangeProp10 = usePersistCallback<
    NonNullable<React.ComponentProps<"textarea">["onChange"]>
  >((event) => setFeedback(event.currentTarget.value));
  return (
    <textarea
      value={feedback}
      placeholder={labels.planImplementationFeedbackPlaceholder}
      disabled={controlsDisabled}
      className={styles.interactivePromptTextarea}
      data-testid="agent-plan-implementation-feedback"
      onChange={onChangeProp10}
    />
  );
});

const ButtonBinding9 = observer(function ButtonBinding9({
  controlsDisabled,
  onSubmit,
  prompt,
  trimmed,
  continueLabel,
  ...slotProps
}: ButtonBinding9Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding9Props>) {
  const onClickProp11 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() =>
    onSubmit({
      requestId: prompt.requestId,
      action:
        trimmed === ""
          ? PLAN_IMPLEMENTATION_ACTION_SKIP
          : PLAN_IMPLEMENTATION_ACTION_FEEDBACK,
      payload: trimmed ? { text: trimmed } : undefined
    })
  );
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        data-testid="agent-plan-implementation-continue"
        disabled={controlsDisabled}
        onClick={onClickProp11}
      >
        {continueLabel}
      </Button>
    </Slot.Root>
  );
});

type TextareaBinding1Props = {
  feedbackTextareaRef: import("react").RefObject<HTMLTextAreaElement | null>;
  feedback: string;
  labels: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "approval" }>;
      embedded?: boolean;
    }
  >["labels"];
  controlsDisabled: boolean;
  submittingOptionId: string | null;
  optionLabel: string;
  option: import("../contracts/agentApprovalItemVM").AgentApprovalOptionVM;
  prompt: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "approval" }>;
      embedded?: boolean;
    }
  >["prompt"];
  action: string;
  setFeedback: import("react").Dispatch<import("react").SetStateAction<string>>;
  feedbackValue: string;
  submitOption: (optionId: string) => void;
};

type ButtonBinding2Props = {
  controlsDisabled: boolean;
  submittingOptionId: string | null;
  feedbackValue: string;
  labels: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "approval" }>;
      embedded?: boolean;
    }
  >["labels"];
  showSpinner: boolean;
  prompt: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "approval" }>;
      embedded?: boolean;
    }
  >["prompt"];
  action: string;
  feedbackTextareaRef: import("react").RefObject<HTMLTextAreaElement | null>;
  submitOption: (optionId: string) => void;
  option: import("../contracts/agentApprovalItemVM").AgentApprovalOptionVM;
};

type ButtonBinding3Props = {
  option: import("../contracts/agentApprovalItemVM").AgentApprovalOptionVM;
  optionLabel: string;
  action: string;
  prompt: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "approval" }>;
      embedded?: boolean;
    }
  >["prompt"];
  controlsDisabled: boolean;
  submittingOptionId: string | null;
  submitOption: (optionId: string) => void;
  optionPresentation: import("../approvalOptionPresentation").ApprovalOptionVisualPresentation;
  keyboardShortcuts: boolean;
  shortcutLabel: string | null;
  showSpinner: boolean;
};

type ButtonBinding4Props = {
  mode: { id: string; label: string; description: string };
  controlsDisabled: boolean;
  submittingOptionId: string | null;
  setSubmittingOptionId: import("react").Dispatch<
    import("react").SetStateAction<string | null>
  >;
  onSubmit: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "exit-plan" }>;
      embedded?: boolean;
    }
  >["onSubmit"];
  prompt: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "exit-plan" }>;
      embedded?: boolean;
    }
  >["prompt"];
  showSpinner: boolean;
};

type TextareaBinding5Props = {
  feedback: string;
  labels: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "exit-plan" }>;
      embedded?: boolean;
    }
  >["labels"];
  controlsDisabled: boolean;
  setFeedback: import("react").Dispatch<import("react").SetStateAction<string>>;
};

type ButtonBinding6Props = {
  controlsDisabled: boolean;
  onSubmit: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "exit-plan" }>;
      embedded?: boolean;
    }
  >["onSubmit"];
  prompt: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "exit-plan" }>;
      embedded?: boolean;
    }
  >["prompt"];
  trimmed: string;
  continueLabel: string;
};

type ButtonBinding7Props = {
  controlsDisabled: boolean;
  onSubmit: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "exit-plan" }>;
      embedded?: boolean;
    }
  >["onSubmit"];
  prompt: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "exit-plan" }>;
      embedded?: boolean;
    }
  >["prompt"];
  labels: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<AgentConversationPromptVM, { kind: "exit-plan" }>;
      embedded?: boolean;
    }
  >["labels"];
};

type TextareaBinding8Props = {
  feedback: string;
  labels: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<
        AgentConversationPromptVM,
        { kind: "plan-implementation" }
      >;
      embedded?: boolean;
    }
  >["labels"];
  controlsDisabled: boolean;
  setFeedback: import("react").Dispatch<import("react").SetStateAction<string>>;
};

type ButtonBinding9Props = {
  controlsDisabled: boolean;
  onSubmit: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<
        AgentConversationPromptVM,
        { kind: "plan-implementation" }
      >;
      embedded?: boolean;
    }
  >["onSubmit"];
  prompt: NonNullable<
    AgentInteractivePromptSurfaceProps & {
      prompt: Extract<
        AgentConversationPromptVM,
        { kind: "plan-implementation" }
      >;
      embedded?: boolean;
    }
  >["prompt"];
  trimmed: string;
  continueLabel: string;
};
