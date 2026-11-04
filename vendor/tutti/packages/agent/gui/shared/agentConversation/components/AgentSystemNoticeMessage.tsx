import type { JSX } from "react";
import { SubagentTranscript } from "@multica/tutti-agent-gui-host/subagent-transcript";
import { translate } from "../../../i18n/index";
import type { AgentMessageContentVM } from "../contracts/agentMessageRowVM";
import { AgentMessageDetailsDisclosure } from "./AgentMessageDetailsDisclosure";
import agentSystemNoticeStyles from "./agentSystemNoticeStyles";
import { useElapsedSeconds } from "./useElapsedSeconds";

const TRANSPORT_RETRY_PROGRESS_PATTERN =
  /\b(reconnect(?:ing)?(?:\s*(?:\.\.\.|…|[.。]+|:|-))?\s*\(?\d+\s*\/\s*\d+\)?)/i;

export function AgentSystemNoticeMessage({
  message
}: {
  message: AgentMessageContentVM;
}): JSX.Element {
  "use memo";
  const notice = message.systemNotice;
  const detail = notice?.detail?.trim() ?? "";
  const title = systemNoticeTitle(message);
  if (notice?.noticeKind === "transport_retry") {
    const retryText = transportRetryNoticeText(message);
    return (
      <div
        role="status"
        className="box-border w-full min-w-0 py-1 text-[13px] leading-5 text-[var(--text-secondary)]"
      >
        {retryText}
      </div>
    );
  }
  if (isContextCompactionProgressNotice(message)) {
    return (
      <ContextCompactionProgressDivider
        startedAtUnixMs={message.occurredAtUnixMs}
      />
    );
  }
  if (isContextCompactionNotice(message)) {
    return (
      <ContextCompactionDivider
        text={translate("agentHost.agentGui.contextCompactionCompleted")}
      />
    );
  }
  if (isContextHandoffRequiredNotice(message)) {
    return (
      <section
        role="alert"
        className={`box-border w-full min-w-0 rounded-[8px] border p-3 text-[13px] leading-5 text-[var(--text-primary)] ${agentSystemNoticeStyles.contextHandoff}`}
      >
        <div className="font-medium text-[var(--state-danger)]">
          {translate("agentHost.agentGui.contextHandoffRequired")}
        </div>
        <div className="mt-1 text-[var(--text-secondary)]">
          {translate("agentHost.agentGui.contextHandoffRequiredDetail")}
        </div>
        {detail ? (
          <AgentMessageDetailsDisclosure detail={detail} className="mt-2" />
        ) : null}
      </section>
    );
  }
  if (isContextCompactionInterruptedNotice(message)) {
    return (
      <ContextCompactionDivider
        text={translate("agentHost.agentGui.contextCompactionInterrupted")}
        detail={detail || null}
      />
    );
  }
  const isStatusNotice = systemNoticeIsStatus(message);
  return (
    <section
      role={isStatusNotice ? "status" : undefined}
      className={`box-border w-full min-w-0 rounded-[8px] border p-3 text-[13px] leading-5 text-[var(--text-secondary)] ${agentSystemNoticeStyles.routine}`}
    >
      <div className="min-w-0">
        <div className="font-medium text-[var(--text-secondary)]">{title}</div>
        {notice?.noticeKind === "background_task" && message.body.trim() !== title ? <div role="status" className="mt-1 whitespace-pre-line break-words text-[12px] leading-5">{message.body}</div> : null}
        {notice?.noticeKind === "background_task" ? <SubagentTranscript message={message} /> : null}
        {detail ? (
          <AgentMessageDetailsDisclosure
            detail={detail}
            className="mt-1"
            tone="muted"
          />
        ) : null}
      </div>
    </section>
  );
}

function systemNoticeIsStatus(message: AgentMessageContentVM): boolean {
  const notice = message.systemNotice;
  return (
    notice?.severity === "warning" ||
    notice?.severity === "error" ||
    notice?.noticeKind === "transport_fallback" ||
    isTransportFallbackNotice(message)
  );
}

function isTransportFallbackNotice(message: AgentMessageContentVM): boolean {
  const notice = message.systemNotice;
  const text = [notice?.title, notice?.detail, message.body]
    .filter(Boolean)
    .join("\n")
    .toLowerCase();
  return (
    text.includes("falling back from websockets") ||
    text.includes("https transport")
  );
}

function transportRetryNoticeText(message: AgentMessageContentVM): string {
  const notice = message.systemNotice;
  const detail = notice?.detail?.trim() ?? "";
  const progressText =
    transportRetryProgressText(detail) ??
    transportRetryProgressText(notice?.title ?? "") ??
    transportRetryProgressText(message.body);
  if (progressText) {
    return progressText;
  }
  return (
    notice?.title?.trim() ||
    message.body.trim() ||
    translate("agentHost.agentGui.systemNoticeTransportRetry")
  );
}

function transportRetryProgressText(value: string): string | null {
  const match = TRANSPORT_RETRY_PROGRESS_PATTERN.exec(value.trim());
  return match?.[1]?.replace(/\s+/g, " ").trim() || null;
}

function isContextCompactionNotice(message: AgentMessageContentVM): boolean {
  const notice = message.systemNotice;
  return notice?.command === "compact" && notice.commandStatus === "completed";
}

function isContextCompactionProgressNotice(
  message: AgentMessageContentVM
): boolean {
  const notice = message.systemNotice;
  return notice?.command === "compact" && notice.commandStatus === "running";
}

function isContextCompactionInterruptedNotice(
  message: AgentMessageContentVM
): boolean {
  const notice = message.systemNotice;
  return (
    notice?.command === "compact" &&
    (notice.commandStatus === "failed" || notice.commandStatus === "canceled")
  );
}

function isContextHandoffRequiredNotice(
  message: AgentMessageContentVM
): boolean {
  return message.systemNotice?.semanticKind === "context-handoff-required";
}

function ContextCompactionDivider({
  text,
  detail = null
}: {
  text: string;
  detail?: string | null;
}): JSX.Element {
  "use memo";
  return (
    <div
      role="status"
      className="box-border w-full min-w-0 py-2 text-[12px] leading-4 text-[var(--text-secondary)]"
    >
      <div className="flex min-w-0 items-center gap-3">
        <span
          aria-hidden="true"
          className="h-px min-w-4 flex-1 bg-[var(--line-1)]"
        />
        <span className="shrink-0 whitespace-nowrap">{text}</span>
        <span
          aria-hidden="true"
          className="h-px min-w-4 flex-1 bg-[var(--line-1)]"
        />
      </div>
      {detail ? (
        <div className="mt-1 min-w-0 whitespace-pre-wrap break-words text-center leading-5">
          {detail}
        </div>
      ) : null}
    </div>
  );
}

// Live compaction banner: the daemon replaces this notice in place with the
// "Context compacted." notice once the provider finishes, so the timer only
// runs while compaction is actually in flight.
function ContextCompactionProgressDivider({
  startedAtUnixMs
}: {
  startedAtUnixMs: number | null;
}): JSX.Element {
  "use memo";
  const elapsedSeconds = useElapsedSeconds(startedAtUnixMs);
  const label = translate("agentHost.agentGui.contextCompactionInProgress");
  const text =
    elapsedSeconds === null
      ? label
      : `${label} · ${formatElapsedSeconds(elapsedSeconds)}`;
  return <ContextCompactionDivider text={text} />;
}

function formatElapsedSeconds(seconds: number): string {
  if (seconds < 60) {
    return `${seconds}s`;
  }
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

function systemNoticeTitle(message: AgentMessageContentVM): string {
  const notice = message.systemNotice;
  switch (notice?.noticeKind) {
    case "transport_retry":
      return translate("agentHost.agentGui.systemNoticeTransportRetry");
    case "transport_fallback":
      return translate("agentHost.agentGui.systemNoticeTransportFallback");
    case "plan_implementation_pending_confirmation":
      return translate(
        "agentHost.agentGui.systemNoticePlanImplementationPendingConfirmation"
      );
    case "plan_implementation_completed":
      return translate(
        "agentHost.agentGui.systemNoticePlanImplementationCompleted"
      );
    case "warning":
      return (
        notice.title || translate("agentHost.agentGui.systemNoticeWarning")
      );
    default:
      return (
        notice?.title ||
        message.body ||
        translate("agentHost.agentGui.systemNoticeDefault")
      );
  }
}
