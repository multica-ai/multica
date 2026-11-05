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

function LoadingEllipsis(): JSX.Element {
  "use memo";
  return (
    <span className="tsh-inline-loading-ellipsis" aria-hidden="true">
      <span />
      <span />
      <span />
    </span>
  );
}

export function AgentChromeNotice({
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
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => onAuthLogin()}
                >
                  {labels.authLogin ?? labels.retryActivation}
                </Button>
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
                <button
                  key={option.id}
                  type="button"
                  className={
                    approvalDisabledReason?.trim()
                      ? "pointer-events-none"
                      : undefined
                  }
                  disabled={isRespondingApproval}
                  onClick={() =>
                    onSubmitApprovalOption({
                      ...(chrome.approval?.agentSessionId
                        ? { agentSessionId: chrome.approval.agentSessionId }
                        : {}),
                      optionId: option.id,
                      requestId: chrome.approval?.requestId ?? "",
                      ...(chrome.approval?.turnId
                        ? { turnId: chrome.approval.turnId }
                        : {})
                    })
                  }
                >
                  {approvalOptionDisplayLabel(option)}
                </button>
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
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className={styles.chromeSuccessGhostButton}
                  onClick={() => onContinueInNewConversation()}
                >
                  {labels.continueInNewConversation}
                </Button>
              ) : visibleRecovery.kind === "failed" &&
                visibleRecovery.canRetry !== false ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className={styles.chromeDangerGhostButton}
                  onClick={() => onRetryActivation()}
                >
                  {labels.retryActivation}
                </Button>
              ) : null}
            </div>
          </div>
        </section>
      ) : null}
    </div>
  );
});

function DisabledApprovalActions({
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
