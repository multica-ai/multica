import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useState, type JSX } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { BrainIcon } from "../app/renderer/components/icons/BrainIcon";
import { translate } from "../i18n/index";
import { CollapsibleReveal } from "./agentConversation/components/CollapsibleReveal";
import { AgentMessageMarkdown } from "./AgentMessageMarkdown";
import type { WorkspaceAgentSessionDetailThinking } from "./workspaceAgentSessionDetailViewModel";
import { ToolActivityKindIcon } from "./toolActivityKindIcons";
import { observer } from "mobx-react-lite";
export const WorkspaceAgentSessionThinkingDisclosure = observer(
  WorkspaceAgentSessionThinkingDisclosureRender
);
const LoadingEllipsis = observer(LoadingEllipsisRender);

interface WorkspaceAgentSessionThinkingDisclosureProps {
  thinking: WorkspaceAgentSessionDetailThinking;
  label: string;
  onLinkClick?: (href: string) => void;
}

function WorkspaceAgentSessionThinkingDisclosureRender({
  thinking,
  label,
  onLinkClick
}: WorkspaceAgentSessionThinkingDisclosureProps): JSX.Element {
  "use memo";
  const [expanded, setExpanded] = useState(false);
  const isActive =
    thinking.statusKind === "working" || thinking.statusKind === "waiting";
  const statusLabel =
    thinking.statusKind === "working"
      ? null
      : thinkingStatusLabel(thinking.statusKind);

  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => {
    setExpanded((value) => !value);
  });
  return (
    <div className="workspace-agents-status-panel__detail-thinking-disclosure flex w-full min-w-0 flex-col items-start gap-[7px]">
      <button
        type="button"
        className={[
          "group/thinking-disclosure inline-flex w-fit max-w-full min-w-0 items-center gap-1.5 border-0 bg-transparent p-0 text-left font-[var(--tsh-font-ui)] text-[13px] leading-[1.3] font-normal text-[var(--text-secondary)] transition-colors duration-150 hover:text-[var(--tsh-shell-body)]",
          isActive ? "tsh-inline-scanlight-group" : ""
        ]
          .filter(Boolean)
          .join(" ")}
        aria-expanded={expanded}
        data-active={isActive ? "true" : undefined}
        onClick={onClickProp1}
      >
        {isActive ? (
          <BrainIcon
            size={16}
            active
            aria-hidden="true"
            className="tsh-inline-scanlight-icon shrink-0 opacity-[0.86]"
          />
        ) : (
          <ToolActivityKindIcon
            kind="thinking"
            width={16}
            height={16}
            className="tsh-inline-scanlight-icon shrink-0 opacity-[0.72]"
          />
        )}
        <span className={isActive ? "tsh-inline-scanlight-line" : undefined}>
          <span className="font-semibold">{label}</span>
          {statusLabel ? (
            <span className="ml-1.5">
              {formatInlineStatusLabel(statusLabel)}
            </span>
          ) : null}
          {isActive ? <LoadingEllipsis /> : null}
        </span>
        {expanded ? (
          <ChevronDown
            size={12}
            strokeWidth={2.2}
            aria-hidden="true"
            className="shrink-0 text-[var(--text-tertiary)] opacity-0 transition-opacity duration-150 group-hover/thinking-disclosure:opacity-100 group-focus-visible/thinking-disclosure:opacity-100"
          />
        ) : (
          <ChevronRight
            size={12}
            strokeWidth={2.2}
            aria-hidden="true"
            className="shrink-0 text-[var(--text-tertiary)] opacity-0 transition-opacity duration-150 group-hover/thinking-disclosure:opacity-100 group-focus-visible/thinking-disclosure:opacity-100"
          />
        )}
      </button>

      <CollapsibleReveal expanded={expanded} className="w-full">
        <div className="box-border w-full min-w-0 rounded-[6px] bg-[var(--transparency-block)] p-3">
          <AgentMessageMarkdown
            content={thinking.body}
            onLinkClick={onLinkClick}
            className="text-[11px] text-[var(--text-secondary)] opacity-92 [&_p]:mb-1"
            enableImageZoom
          />
        </div>
      </CollapsibleReveal>
    </div>
  );
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

function thinkingStatusLabel(
  statusKind: WorkspaceAgentSessionDetailThinking["statusKind"]
): string | null {
  switch (statusKind) {
    case "working":
      return translate("agentHost.agentTool.statusWorking");
    case "completed":
      return translate("agentHost.agentTool.statusCompleted");
    case "failed":
      return translate("agentHost.agentTool.statusFailed");
    case "canceled":
      return translate("agentHost.agentTool.statusCanceled");
    case "waiting":
      return translate("agentHost.agentTool.statusWaiting");
    default:
      return null;
  }
}

function formatInlineStatusLabel(label: string): string {
  return /^[A-Z][a-z]+$/.test(label) ? label.toLowerCase() : label;
}
