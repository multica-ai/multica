import {
  ArrowRightLeft,
  ChevronDown,
  ChevronUp,
  GitFork,
  LoaderCircle,
  MessageCircleQuestion,
  Users
} from "lucide-react";
import { observer } from "mobx-react-lite";
import { useState, type JSX } from "react";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import type { WorkspaceLinkAction } from "../../../contexts/workspace/presentation/renderer/actions/workspaceLinkActions";
import { translate } from "../../../i18n/index";
import {
  AgentMessageMarkdown,
  type AgentMessageMarkdownWorkspaceAppIcon
} from "../../AgentMessageMarkdown";
import type { AgentCollaborationVM } from "../contracts/agentCollaborationVM";

/**
 * Transcript card for one durable collaboration run (model consult, fork,
 * delegate, or handoff). The daemon updates the same message in place as the
 * run progresses, so all durable state comes from the projected
 * `AgentCollaborationVM`; the card owns only UI-local interaction state
 * (result disclosure).
 */
export const AgentCollaborationRow = observer(function AgentCollaborationRow({
  collaboration,
  workspaceRoot,
  basePath,
  onLinkAction,
  workspaceAppIcons
}: {
  collaboration: AgentCollaborationVM;
  workspaceRoot: string | null;
  basePath: string;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
}): JSX.Element {
  "use memo";
  const [resultExpanded, setResultExpanded] = useState(false);
  const adoption = collaboration.adoption;
  const resultText = collaboration.resultText?.trim() ?? "";

  const ModeIcon = collaborationModeIcon(collaboration.mode);
  const planText = collaboration.modelPlanName ?? collaboration.modelPlanId;

  return (
    <section
      data-testid="agent-collaboration-row"
      data-collaboration-run-id={collaboration.runId}
      data-collaboration-mode={collaboration.mode}
      data-collaboration-status={collaboration.status}
      className="box-border w-full min-w-0 rounded-[8px] border border-[var(--line-2)] bg-[var(--background-fronted)] p-3 text-[13px] leading-5 text-[var(--text-primary)]"
    >
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <span className="inline-flex min-w-0 items-center gap-1.5 font-medium">
          <ModeIcon
            size={14}
            strokeWidth={2}
            aria-hidden="true"
            className="shrink-0 text-[var(--text-secondary)]"
          />
          <span className="truncate">
            {collaborationModeLabel(collaboration.mode)}
          </span>
        </span>
        <span
          data-testid="agent-collaboration-trigger"
          className="shrink-0 rounded-full border border-[var(--line-2)] px-1.5 text-[11px] leading-4 text-[var(--text-tertiary)]"
        >
          {collaborationTriggerLabel(collaboration.triggerSource)}
        </span>
        {planText || collaboration.model ? (
          <span
            data-testid="agent-collaboration-model"
            className="min-w-0 truncate text-[12px] text-[var(--text-tertiary)]"
          >
            {planText
              ? translate("agentHost.agentGui.collaborationPlanLabel", {
                  name: planText
                })
              : null}
            {planText && collaboration.model ? " · " : null}
            {collaboration.model}
          </span>
        ) : null}
        <span className="min-w-0 flex-1" />
        <span
          data-testid="agent-collaboration-status"
          data-status={collaboration.status}
          className={`inline-flex shrink-0 items-center gap-1 rounded-full px-2 text-[11px] leading-[18px] ${collaborationStatusClassName(collaboration.status)}`}
        >
          {collaboration.status === "running" ? (
            <LoaderCircle
              size={11}
              strokeWidth={2.2}
              aria-hidden="true"
              className="animate-spin"
            />
          ) : null}
          {collaborationStatusLabel(collaboration.status)}
          {collaboration.durationMs !== null &&
          collaboration.status !== "running"
            ? ` · ${formatCollaborationDuration(collaboration.durationMs)}`
            : null}
        </span>
      </div>
      {collaboration.usage ? (
        <div
          data-testid="agent-collaboration-usage"
          className="mt-1 text-[11px] text-[var(--text-tertiary)]"
        >
          {translate("agentHost.agentGui.collaborationUsageTokens", {
            input: String(collaboration.usage.inputTokens),
            output: String(collaboration.usage.outputTokens)
          })}
        </div>
      ) : null}
      {collaboration.failureReason ? (
        <div
          data-testid="agent-collaboration-failure"
          className="mt-1.5 text-[12px] text-[var(--state-danger)]"
        >
          {translate("agentHost.agentGui.collaborationFailureReason", {
            reason: collaboration.failureReason
          })}
        </div>
      ) : null}
      {collaboration.mode === "consult" && resultText ? (
        <div className="mt-2">
          <button
            type="button"
            data-testid="agent-collaboration-result-toggle"
            aria-expanded={resultExpanded}
            className="inline-flex items-center gap-1 text-[12px] text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)]"
            onClick={() => setResultExpanded((value) => !value)}
          >
            {translate(
              resultExpanded
                ? "agentHost.agentGui.collaborationResultHide"
                : "agentHost.agentGui.collaborationResultShow"
            )}
            {resultExpanded ? (
              <ChevronUp size={13} strokeWidth={2} aria-hidden="true" />
            ) : (
              <ChevronDown size={13} strokeWidth={2} aria-hidden="true" />
            )}
          </button>
          {resultExpanded ? (
            <div
              data-testid="agent-collaboration-result"
              className="mt-1.5 min-w-0"
            >
              <AgentMessageMarkdown
                content={resultText}
                className={styles.assistantMarkdown}
                onLinkAction={onLinkAction}
                workspaceLinkContext={{
                  workspaceRoot,
                  basePath,
                  source: "agent-markdown"
                }}
                workspaceAppIcons={workspaceAppIcons}
                enableImageZoom
              />
            </div>
          ) : null}
        </div>
      ) : null}
      {adoption === "adopted" || adoption === "rejected" ? (
        <div
          data-testid="agent-collaboration-adoption"
          data-adoption={adoption}
          className="mt-2 text-[11px] text-[var(--text-tertiary)]"
        >
          {translate(
            adoption === "adopted"
              ? "agentHost.agentGui.collaborationAdopted"
              : "agentHost.agentGui.collaborationRejected"
          )}
        </div>
      ) : null}
    </section>
  );
});

function collaborationModeIcon(
  mode: AgentCollaborationVM["mode"]
): typeof MessageCircleQuestion {
  switch (mode) {
    case "fork":
      return GitFork;
    case "delegate":
      return Users;
    case "handoff":
      return ArrowRightLeft;
    default:
      return MessageCircleQuestion;
  }
}

function collaborationModeLabel(mode: AgentCollaborationVM["mode"]): string {
  switch (mode) {
    case "consult":
      return translate("agentHost.agentGui.collaborationModeConsult");
    case "fork":
      return translate("agentHost.agentGui.collaborationModeFork");
    case "delegate":
      return translate("agentHost.agentGui.collaborationModeDelegate");
    case "handoff":
      return translate("agentHost.agentGui.collaborationModeHandoff");
    default:
      return mode;
  }
}

function collaborationTriggerLabel(
  triggerSource: AgentCollaborationVM["triggerSource"]
): string {
  switch (triggerSource) {
    case "user":
      return translate("agentHost.agentGui.collaborationTriggerUser");
    case "agent":
      return translate("agentHost.agentGui.collaborationTriggerAgent");
    case "policy":
      return translate("agentHost.agentGui.collaborationTriggerPolicy");
    default:
      return triggerSource;
  }
}

function collaborationStatusLabel(
  status: AgentCollaborationVM["status"]
): string {
  switch (status) {
    case "running":
      return translate("agentHost.agentGui.collaborationStatusRunning");
    case "completed":
      return translate("agentHost.agentGui.collaborationStatusCompleted");
    case "failed":
      return translate("agentHost.agentGui.collaborationStatusFailed");
    case "canceled":
      return translate("agentHost.agentGui.collaborationStatusCanceled");
    default:
      return status;
  }
}

function collaborationStatusClassName(
  status: AgentCollaborationVM["status"]
): string {
  switch (status) {
    case "failed":
      return "bg-[var(--on-danger)] text-[var(--state-danger)]";
    case "running":
      return "bg-[color-mix(in_srgb,var(--tutti-purple)_12%,transparent)] text-[var(--tutti-purple)]";
    default:
      return "bg-[var(--transparency-hover)] text-[var(--text-secondary)]";
  }
}

function formatCollaborationDuration(durationMs: number): string {
  const totalSeconds = Math.max(0, Math.round(durationMs / 1000));
  if (totalSeconds < 60) {
    return `${totalSeconds}s`;
  }
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}m ${seconds}s`;
}
