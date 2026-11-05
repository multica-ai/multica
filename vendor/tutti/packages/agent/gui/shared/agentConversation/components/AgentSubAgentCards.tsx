import { AlertCircle, ChevronDown, ChevronRight } from "lucide-react";
import { observer } from "mobx-react-lite";
import { memo, useState, type JSX } from "react";
import { AgentLinedIcon } from "../../../app/renderer/components/icons/AgentLinedIcon";
import { translate } from "../../../i18n/index";
import { AgentMessageMarkdown } from "../../AgentMessageMarkdown";
import type { AgentTaskSubAgentVM } from "../contracts/agentTaskItemVM";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import { useAgentConversationNowUnixMs } from "./AgentConversationClock";
import { CollapsibleReveal } from "./CollapsibleReveal";
import { formatAgentToolDurationMs } from "./tool-renderers/render-data/agentToolRenderData";

// A delegated sub-agent renders as a first-class row aligned with tool rows:
// header = "Sub-agent · <name> · <elapsed> · <status>" with a chevron, body =
// the indented detail panel (TASK / PROGRESS). Identity and status are the
// sub-agent's own (child thread nickname + lifecycle), never the spawn tool's.
export const AgentSubAgentCards = observer(function AgentSubAgentCards({
  call,
  onLinkClick
}: {
  call: AgentToolCallVM;
  onLinkClick?: (href: string) => void;
}): JSX.Element | null {
  "use memo";
  const subAgents = call.task?.subAgents ?? [];
  if (subAgents.length === 0) {
    return null;
  }
  // No wrapper element: the transcript row's spacing-compression selectors
  // match direct .detail-tool-row children, keeping gaps identical to tools.
  return (
    <>
      {subAgents.map((subAgent) => (
        <AgentSubAgentCard
          key={subAgent.childSessionId}
          subAgent={subAgent}
          onLinkClick={onLinkClick}
        />
      ))}
    </>
  );
});

// Field-level memo: streaming updates elsewhere in the transcript must not
// re-render settled cards; a collapsed card renders only its header row
// (CollapsibleReveal lazy-mounts the body on first expand).
export const AgentSubAgentCard = memo(
  AgentSubAgentCardImpl,
  (prev, next) =>
    prev.onLinkClick === next.onLinkClick &&
    subAgentVMEquals(prev.subAgent, next.subAgent)
);

function subAgentVMEquals(
  left: AgentTaskSubAgentVM,
  right: AgentTaskSubAgentVM
): boolean {
  return (
    left.childSessionId === right.childSessionId &&
    left.parentToolCallId === right.parentToolCallId &&
    left.status === right.status &&
    left.name === right.name &&
    left.task === right.task &&
    left.laneIndex === right.laneIndex &&
    left.laneCount === right.laneCount &&
    left.latestActivity === right.latestActivity &&
    left.assistantMarkdown === right.assistantMarkdown &&
    left.failureDetail === right.failureDetail &&
    left.queued === right.queued &&
    left.startedAtUnixMs === right.startedAtUnixMs &&
    left.latestActivityAtUnixMs === right.latestActivityAtUnixMs &&
    left.terminalAtUnixMs === right.terminalAtUnixMs &&
    left.childSessions === right.childSessions
  );
}

function AgentSubAgentCardImpl({
  subAgent,
  onLinkClick
}: {
  subAgent: AgentTaskSubAgentVM;
  onLinkClick?: (href: string) => void;
}): JSX.Element {
  "use memo";
  // Expanded by default while running; always collapsible via the header.
  const [expanded, setExpanded] = useState(subAgent.status === "running");

  return (
    <div
      className="workspace-agents-status-panel__detail-tool-row workspace-agents-status-panel__detail-tool-row--subagent"
      data-status={subAgent.status}
    >
      <button
        type="button"
        className="workspace-agents-status-panel__detail-tool-row-head workspace-agents-status-panel__detail-tool-row-head--button"
        aria-expanded={expanded}
        aria-label={subAgentAriaLabel(subAgent)}
        onClick={() => setExpanded((value) => !value)}
      >
        <SubAgentHeader subAgent={subAgent} expanded={expanded} />
      </button>
      <CollapsibleReveal expanded={expanded}>
        <div className="workspace-agents-status-panel__detail-subagent-reveal">
          <SubAgentBody subAgent={subAgent} onLinkClick={onLinkClick} />
        </div>
      </CollapsibleReveal>
    </div>
  );
}

function SubAgentHeader({
  subAgent,
  expanded
}: {
  subAgent: AgentTaskSubAgentVM;
  expanded: boolean;
}): JSX.Element {
  "use memo";
  const running = subAgent.status === "running";
  const statusLabel = subAgentStatusLabel(subAgent.status);
  const runningNowUnixMs = useRunningSubAgentNowUnixMs(subAgent);
  const elapsedText = subAgentElapsedText(subAgent, runningNowUnixMs);
  const nameText = subAgentNameText(subAgent);
  return (
    <div
      data-active={running ? "true" : undefined}
      className={[
        "workspace-agents-status-panel__detail-tool-row-header-content",
        running ? "tsh-inline-scanlight-group" : ""
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <div className="workspace-agents-status-panel__detail-tool-row-icon tsh-inline-scanlight-icon">
        {subAgent.status === "failed" ? (
          <AlertCircle size={16} strokeWidth={2} aria-hidden="true" />
        ) : (
          <AgentLinedIcon width={16} height={16} aria-hidden="true" />
        )}
      </div>
      <div className="workspace-agents-status-panel__detail-tool-row-text">
        <strong className="workspace-agents-status-panel__detail-tool-row-title">
          {translate("agentHost.agentTool.details.subAgentFallbackName")}
        </strong>
        {nameText ? (
          <span className="workspace-agents-status-panel__detail-subagent-name">
            {nameText}
          </span>
        ) : null}
        <span className="workspace-agents-status-panel__detail-tool-status">
          {elapsedText ? `${elapsedText} · ` : ""}
          {statusLabel}
        </span>
      </div>
      {expanded ? (
        <ChevronDown
          size={12}
          strokeWidth={2.2}
          aria-hidden="true"
          className="workspace-agents-status-panel__detail-tool-row-chevron"
        />
      ) : (
        <ChevronRight
          size={12}
          strokeWidth={2.2}
          aria-hidden="true"
          className="workspace-agents-status-panel__detail-tool-row-chevron"
        />
      )}
    </div>
  );
}

function SubAgentBody({
  subAgent,
  onLinkClick
}: {
  subAgent: AgentTaskSubAgentVM;
  onLinkClick?: (href: string) => void;
}): JSX.Element {
  "use memo";
  // Bash-block layout: the task is the header strip, live progress renders
  // below it - no section labels.
  return (
    <div className="workspace-agents-status-panel__detail-tool-body workspace-agents-status-panel__detail-tool-body--plain">
      <div className="workspace-agents-status-panel__detail-subagent-terminal">
        {subAgent.task ? (
          <div className="workspace-agents-status-panel__detail-subagent-task-strip">
            {subAgent.task}
          </div>
        ) : null}
        <SubAgentProgress subAgent={subAgent} onLinkClick={onLinkClick} />
        {subAgent.childSessions.length > 0 ? (
          <div className="workspace-agents-status-panel__detail-subagent-children">
            {subAgent.childSessions.map((childSession) => (
              <AgentSubAgentCard
                key={childSession.childSessionId}
                subAgent={childSession}
                onLinkClick={onLinkClick}
              />
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function SubAgentProgress({
  subAgent,
  onLinkClick
}: {
  subAgent: AgentTaskSubAgentVM;
  onLinkClick?: (href: string) => void;
}): JSX.Element {
  "use memo";
  if (subAgent.assistantMarkdown?.trim()) {
    return (
      <div className="workspace-agents-status-panel__detail-subagent-activity workspace-agents-status-panel__detail-subagent-activity--in-terminal">
        <AgentMessageMarkdown
          content={subAgent.assistantMarkdown}
          documentCacheKey={`sub-agent:${subAgent.childSessionId}:assistant`}
          onLinkClick={onLinkClick}
          streaming={subAgent.status === "running"}
          enableImageZoom
          className="workspace-agents-status-panel__detail-tool-markdown [&_ol]:text-[var(--text-secondary)] [&_ul]:text-[var(--text-secondary)]"
        />
        {subAgent.failureDetail ? (
          <div className="workspace-agents-status-panel__detail-subagent-activity--failure">
            {subAgent.failureDetail}
          </div>
        ) : null}
      </div>
    );
  }
  // Tool/reasoning activity remains a short fallback until the child emits an
  // assistant message. Once it does, the complete Markdown above owns both the
  // streaming and settled presentation.
  const text =
    subAgent.failureDetail ??
    (subAgent.status === "running" ? subAgent.latestActivity : null) ??
    translate(
      subAgent.status === "completed"
        ? "agentHost.agentTool.statusCompleted"
        : subAgent.status === "canceled"
          ? "agentHost.agentTool.statusCanceled"
          : subAgent.status === "failed"
            ? "agentHost.agentTool.statusFailed"
            : subAgent.queued
              ? "agentHost.agentTool.details.subAgentQueued"
              : "agentHost.agentTool.details.subAgentStarting"
    );
  return (
    <div
      className={`workspace-agents-status-panel__detail-subagent-activity workspace-agents-status-panel__detail-subagent-activity--in-terminal${
        subAgent.failureDetail
          ? " workspace-agents-status-panel__detail-subagent-activity--failure"
          : ""
      }`}
    >
      {text}
    </div>
  );
}

function subAgentNameText(subAgent: AgentTaskSubAgentVM): string | null {
  if (subAgent.name) {
    return subAgent.name;
  }
  return subAgent.laneCount > 1 ? `#${subAgent.laneIndex}` : null;
}

function subAgentAriaLabel(subAgent: AgentTaskSubAgentVM): string {
  return [
    translate("agentHost.agentTool.details.subAgentFallbackName"),
    subAgentNameText(subAgent),
    subAgentStatusLabel(subAgent.status)
  ]
    .filter(Boolean)
    .join(" ");
}

function subAgentStatusLabel(status: AgentTaskSubAgentVM["status"]): string {
  switch (status) {
    case "completed":
      return translate("agentHost.agentTool.statusCompleted");
    case "failed":
      return translate("agentHost.agentTool.statusFailed");
    case "canceled":
      return translate("agentHost.agentTool.statusCanceled");
    case "running":
    default:
      return translate("agentHost.agentTool.statusWorking");
  }
}

// A sub-agent that is merely `queued` has been accepted but has not been
// dispatched to a provider yet (codex caps concurrent sub-agents and queues
// spawns beyond the cap - see AgentTaskSubAgentVM.queued). Its `status` still
// reads "running" (there is no child lifecycle yet to say otherwise) and its
// `startedAtUnixMs` is stamped from the spawn call itself, i.e. dispatch/
// queued time, not from when the sub-agent actually started running. Ticking
// a live clock off that timestamp would count queued wait time as elapsed
// run time, so the clock must stay off until the sub-agent is no longer
// queued.
function isSubAgentActivelyRunning(subAgent: AgentTaskSubAgentVM): boolean {
  return subAgent.status === "running" && !subAgent.queued;
}

function useRunningSubAgentNowUnixMs(
  subAgent: AgentTaskSubAgentVM
): number | null {
  const shouldTick =
    isSubAgentActivelyRunning(subAgent) &&
    typeof subAgent.startedAtUnixMs === "number";
  return useAgentConversationNowUnixMs(shouldTick);
}

function subAgentElapsedText(
  subAgent: AgentTaskSubAgentVM,
  runningNowUnixMs: number | null
): string | null {
  const started = subAgent.startedAtUnixMs;
  if (typeof started !== "number") {
    return null;
  }

  const terminal = subAgent.terminalAtUnixMs;
  const latest = subAgent.latestActivityAtUnixMs;
  const ended =
    typeof terminal === "number"
      ? terminal
      : isSubAgentActivelyRunning(subAgent) &&
          typeof runningNowUnixMs === "number"
        ? Math.max(latest ?? started, runningNowUnixMs)
        : latest;
  if (typeof ended !== "number" || ended <= started) {
    return null;
  }
  return formatAgentToolDurationMs(ended - started);
}
