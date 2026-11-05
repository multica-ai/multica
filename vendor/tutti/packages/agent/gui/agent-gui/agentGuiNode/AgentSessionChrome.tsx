import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  Button,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import { type JSX, type ReactNode } from "react";
import { CastIcon } from "../../app/renderer/components/icons/CastIcon";
import { cn } from "../../app/renderer/lib/utils";
import { approvalOptionDisplayLabel } from "../../shared/agentConversation/approvalOptionPresentation";
import type { AgentInteractionResponseInput } from "../../shared/agentConversation/contracts/agentConversationVM";
import styles from "./AgentGUIChrome.styles";
import type { AgentGUISessionChrome } from "./model/agentGuiNodeTypes";
const LoadingEllipsis = observer(LoadingEllipsisRender);
export const AgentChromeNotice = observer(AgentChromeNoticeRender);
const DisabledApprovalActions = observer(DisabledApprovalActionsRender);

interface AgentChromeNoticeProps {
  tone: "warning" | "danger" | "muted";
  title: string;
  description?: string;
  icon?: ReactNode;
  role?: "alert" | "status";
  testId?: string;
}

export interface AgentSessionChromeSource {
  readonly chrome: AgentGUISessionChrome | null;
  readonly approvalDisabledReason?: string | null;
  readonly isRespondingApproval: boolean;
}
interface AgentSessionChromeProps {
  source: AgentSessionChromeSource;
  onSubmitApprovalOption: (input: AgentInteractionResponseInput) => boolean;
  onAuthLogin?: () => void;
  onRetryActivation: () => void;
  onContinueInNewConversation: () => void;
  labels: {
    approvalRequired: string;
    authLogin?: string;
    authRequired: string;
    activatingSession: string;
    retryActivation: string;
    continueInNewConversation: string;
  };
}

function splitTrailingEllipsis(message: string): {
  label: string;
  ellipsis: string | null;
} {
  const match = message.match(/^(.*?)(\.{3}|…)\s*$/);
  if (!match) {
    return { label: message, ellipsis: null };
  }

  return {
    label: match[1] ?? message,
    ellipsis: match[2] ?? null
  };
}

function LoadingEllipsisRender(): JSX.Element {
  "use memo";
  return (
    <span className="tsh-inline-loading-ellipsis" aria-hidden="true">
      <span />
      <span />
      <span />
    </span>
  );
}

function AgentChromeNoticeRender({
  tone,
  title,
  description,
  icon,
  role,
  testId
}: AgentChromeNoticeProps): JSX.Element {
  "use memo";
  const fullMessage = description ? `${title} ${description}` : title;
  const toneClassName =
    tone === "danger"
      ? styles.chromeCardDanger
      : tone === "muted"
        ? styles.chromeCardMuted
        : styles.chromeCardWarning;
  return (
    <div className={styles.sessionChrome}>
      <section
        className={cn(styles.chromeCard, toneClassName)}
        role={role}
        data-testid={testId}
      >
        <div className={styles.chromeMetaRow}>
          <div className={styles.chromeMessageSlot}>
            {icon ? <span className={styles.chromeIcon}>{icon}</span> : null}
            <p
              className={cn(styles.chromeMessage, styles.chromeNoticeMessage)}
              title={fullMessage}
            >
              <span className={styles.chromeNoticeTitle}>{title}</span>
              {description ? (
                <span className={styles.chromeNoticeDescription}>
                  {description}
                </span>
              ) : null}
            </p>
          </div>
        </div>
      </section>
    </div>
  );
}

export const AgentSessionChrome = observer(function AgentSessionChrome({
  source,
  onSubmitApprovalOption,
  onAuthLogin,
  onRetryActivation,
  onContinueInNewConversation,
  labels
}: AgentSessionChromeProps): JSX.Element | null {
  const {
    chrome,
    approvalDisabledReason = null,
    isRespondingApproval
  } = source;
  if (!chrome) return null;
  const visibleAuth =
    chrome.recovery?.kind === "activating" ||
    chrome.recovery?.kind === "agent-sharing-revoked" ||
    chrome.recovery?.kind === "transport-connecting" ||
    chrome.recovery?.kind === "transport-unavailable"
      ? null
      : chrome.auth;
  const visibleRecovery = chrome.recovery;
  const recoveryMessage =
    visibleRecovery?.kind === "activating"
      ? labels.activatingSession
      : (visibleRecovery?.message ?? "");
  const recoveryHasInlineAction =
    visibleRecovery?.kind === "resume-unavailable" ||
    (visibleRecovery?.kind === "failed" && visibleRecovery.canRetry !== false);
  const activatingMessage = splitTrailingEllipsis(recoveryMessage);
  const hasContent =
    visibleAuth !== null ||
    chrome.approval !== null ||
    visibleRecovery !== null;

  if (!hasContent) {
    return null;
  }

  return (
    <div className={styles.sessionChrome}>
      {visibleAuth ? (
        <section className={cn(styles.chromeCard, styles.chromeCardWarning)}>
          <div className={styles.chromeMetaRow}>
            <div className={styles.chromeMessageSlot}>
              <p
                className={styles.chromeMessage}
                tabIndex={0}
                title={visibleAuth.message}
              >
                {visibleAuth.message}
              </p>
            </div>
            <div className={styles.chromeInlineActions}>
              {onAuthLogin ? (
                <ButtonBinding1 onAuthLogin={onAuthLogin} labels={labels} />
              ) : null}
            </div>
          </div>
        </section>
      ) : null}

      {chrome.approval ? (
        <section className={cn(styles.chromeCard, styles.chromeCardAction)}>
          <div className={styles.chromeTitle}>{labels.approvalRequired}</div>
          <p className={styles.chromeMessage}>{chrome.approval.title}</p>
          <DisabledApprovalActions reason={approvalDisabledReason}>
            <div className={styles.chromeActions}>
              {chrome.approval.options.map((option) => (
                <ButtonBinding2
                  key={option.id}
                  option={option}
                  approvalDisabledReason={approvalDisabledReason}
                  isRespondingApproval={isRespondingApproval}
                  onSubmitApprovalOption={onSubmitApprovalOption}
                  chrome={chrome}
                />
              ))}
            </div>
          </DisabledApprovalActions>
        </section>
      ) : null}

      {visibleRecovery ? (
        <section
          role={
            visibleRecovery.kind === "failed" ||
            visibleRecovery.kind === "agent-sharing-revoked" ||
            visibleRecovery.kind === "transport-unavailable"
              ? "alert"
              : visibleRecovery.kind === "resume-unavailable" ||
                  visibleRecovery.kind === "transport-connecting"
                ? "status"
                : undefined
          }
          aria-live={
            visibleRecovery.kind === "failed" ||
            visibleRecovery.kind === "agent-sharing-revoked" ||
            visibleRecovery.kind === "transport-unavailable"
              ? "assertive"
              : visibleRecovery.kind === "transport-connecting"
                ? "polite"
                : visibleRecovery.kind === "resume-unavailable"
                  ? "polite"
                  : undefined
          }
          data-has-inline-actions={recoveryHasInlineAction ? "true" : "false"}
          className={cn(
            styles.chromeCard,
            visibleRecovery.kind === "failed" ||
              visibleRecovery.kind === "agent-sharing-revoked" ||
              visibleRecovery.kind === "transport-unavailable"
              ? styles.chromeCardDanger
              : visibleRecovery.kind === "resume-unavailable"
                ? styles.chromeCardSuccess
                : visibleRecovery.kind === "warning"
                  ? styles.chromeCardDanger
                  : visibleRecovery.kind === "activating" ||
                      visibleRecovery.kind === "transport-connecting"
                    ? styles.chromeCardConnecting
                    : styles.chromeCardMuted
          )}
        >
          <div className={styles.chromeMetaRow}>
            <div className={styles.chromeMessageSlot}>
              {visibleRecovery.kind === "activating" ||
              visibleRecovery.kind === "transport-connecting" ? (
                <CastIcon
                  active
                  aria-hidden="true"
                  className={styles.chromeIcon}
                  data-testid="agent-session-chrome-connecting-icon"
                  size={16}
                />
              ) : null}
              <p
                className={styles.chromeMessage}
                aria-label={
                  visibleRecovery.kind === "activating" ||
                  visibleRecovery.kind === "transport-connecting"
                    ? recoveryMessage
                    : undefined
                }
                tabIndex={0}
                title={recoveryMessage}
              >
                {visibleRecovery.kind === "activating" ||
                visibleRecovery.kind === "transport-connecting" ? (
                  <>
                    <span className="tsh-inline-loading-label">
                      {activatingMessage.label}
                    </span>
                    {activatingMessage.ellipsis ? <LoadingEllipsis /> : null}
                  </>
                ) : (
                  recoveryMessage
                )}
              </p>
            </div>
            <div className={styles.chromeInlineActions}>
              {/* followupAction is required on this variant, so the kind
                  check alone guarantees the continue action. */}
              {visibleRecovery.kind === "resume-unavailable" ? (
                <ButtonBinding3
                  onContinueInNewConversation={onContinueInNewConversation}
                  labels={labels}
                />
              ) : visibleRecovery.kind === "failed" &&
                visibleRecovery.canRetry !== false ? (
                <ButtonBinding4
                  onRetryActivation={onRetryActivation}
                  labels={labels}
                />
              ) : null}
            </div>
          </div>
        </section>
      ) : null}
    </div>
  );
});

function DisabledApprovalActionsRender({
  children,
  reason
}: {
  children: ReactNode;
  reason?: string | null;
}): JSX.Element {
  const normalizedReason = reason?.trim() ?? "";
  const actions = (
    <div
      aria-disabled={normalizedReason ? "true" : undefined}
      aria-label={normalizedReason || undefined}
      className={cn(
        normalizedReason && "cursor-not-allowed rounded-md outline-none"
      )}
      data-agent-interaction-disabled={normalizedReason ? "true" : undefined}
      role={normalizedReason ? "group" : undefined}
      tabIndex={normalizedReason ? 0 : undefined}
    >
      {children}
    </div>
  );

  if (!normalizedReason) {
    return actions;
  }

  return (
    <TooltipProvider delayDuration={120} skipDelayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>{actions}</TooltipTrigger>
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

const ButtonBinding1 = observer(function ButtonBinding1({
  onAuthLogin,
  labels,
  ...slotProps
}: {
  onAuthLogin: () => void;
  labels: NonNullable<AgentSessionChromeProps>["labels"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    onAuthLogin: () => void;
    labels: NonNullable<AgentSessionChromeProps>["labels"];
  }
>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => onAuthLogin());
  return (
    <Slot.Root {...slotProps}>
      <Button type="button" variant="ghost" size="sm" onClick={onClickProp1}>
        {labels.authLogin ?? labels.retryActivation}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding2 = observer(function ButtonBinding2({
  option,
  approvalDisabledReason,
  isRespondingApproval,
  onSubmitApprovalOption,
  chrome,
  ...slotProps
}: ButtonBinding2Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding2Props>) {
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() =>
    onSubmitApprovalOption({
      ...(chrome.approval?.agentSessionId
        ? { agentSessionId: chrome.approval.agentSessionId }
        : {}),
      optionId: option.id,
      requestId: chrome.approval?.requestId ?? "",
      ...(chrome.approval?.turnId ? { turnId: chrome.approval.turnId } : {})
    })
  );
  return (
    <Slot.Root {...slotProps}>
      <button
        key={option.id}
        type="button"
        className={
          approvalDisabledReason?.trim() ? "pointer-events-none" : undefined
        }
        disabled={isRespondingApproval}
        onClick={onClickProp2}
      >
        {approvalOptionDisplayLabel(option)}
      </button>
    </Slot.Root>
  );
});

const ButtonBinding3 = observer(function ButtonBinding3({
  onContinueInNewConversation,
  labels,
  ...slotProps
}: {
  onContinueInNewConversation: NonNullable<AgentSessionChromeProps>["onContinueInNewConversation"];
  labels: NonNullable<AgentSessionChromeProps>["labels"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    onContinueInNewConversation: NonNullable<AgentSessionChromeProps>["onContinueInNewConversation"];
    labels: NonNullable<AgentSessionChromeProps>["labels"];
  }
>) {
  const onClickProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => onContinueInNewConversation());
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className={styles.chromeSuccessGhostButton}
        onClick={onClickProp3}
      >
        {labels.continueInNewConversation}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding4 = observer(function ButtonBinding4({
  onRetryActivation,
  labels,
  ...slotProps
}: {
  onRetryActivation: NonNullable<AgentSessionChromeProps>["onRetryActivation"];
  labels: NonNullable<AgentSessionChromeProps>["labels"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    onRetryActivation: NonNullable<AgentSessionChromeProps>["onRetryActivation"];
    labels: NonNullable<AgentSessionChromeProps>["labels"];
  }
>) {
  const onClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => onRetryActivation());
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className={styles.chromeDangerGhostButton}
        onClick={onClickProp4}
      >
        {labels.retryActivation}
      </Button>
    </Slot.Root>
  );
});

type ButtonBinding2Props = {
  option: import("../../shared/agentConversation/contracts/agentApprovalItemVM").AgentApprovalOptionVM;
  approvalDisabledReason: string | null;
  isRespondingApproval: NonNullable<
    NonNullable<AgentSessionChromeProps>["source"]
  >["isRespondingApproval"];
  onSubmitApprovalOption: NonNullable<AgentSessionChromeProps>["onSubmitApprovalOption"];
  chrome: AgentGUISessionChrome;
};
