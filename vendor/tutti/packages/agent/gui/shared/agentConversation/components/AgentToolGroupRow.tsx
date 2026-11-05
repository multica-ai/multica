import { ChevronDown, ChevronRight } from "lucide-react";
import { observer } from "mobx-react-lite";
import { memo, useState, type JSX } from "react";
import { ToolActivityKindIcon } from "../../toolActivityKindIcons";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import type { AgentToolGroupRowVM } from "../contracts/agentToolGroupRowVM";
import { AgentAskUserQuestionCard } from "./AgentAskUserQuestionCard";
import { AgentEnterPlanModeCard } from "./AgentEnterPlanModeCard";
import { AgentExitPlanModeCard } from "./AgentExitPlanModeCard";
import { AgentSubAgentCards } from "./AgentSubAgentCards";
import { AgentTaskCallCard } from "./AgentTaskCallCard";
import { AgentThinkingDisclosure } from "./AgentThinkingDisclosure";
import { AgentToolCallCard } from "./AgentToolCallCard";
import { CollapsibleReveal } from "./CollapsibleReveal";
import { RawTimelineJsonDisclosure } from "./RawTimelineJsonDisclosure";

interface AgentToolGroupRowProps {
  row: AgentToolGroupRowVM;
  label: (count: number) => string;
  thinkingLabel: string;
  onLinkClick?: (href: string) => void;
  expanded?: boolean;
  expansionKey?: string;
  onExpandedChange?: (key: string, expanded: boolean) => void;
  showRawTimelineJson?: boolean;
  rawTimelineJsonLabel?: string;
}

export const AgentToolGroupRow = memo(
  observer(function AgentToolGroupRow({
    row,
    label,
    thinkingLabel,
    onLinkClick,
    expanded,
    expansionKey,
    onExpandedChange,
    showRawTimelineJson = false,
    rawTimelineJsonLabel = ""
  }: AgentToolGroupRowProps): JSX.Element {
    "use memo";
    const [localExpanded, setLocalExpanded] = useState(false);
    const hasDetail = row.entries.length > 0;
    const isExpanded = hasDetail && (expanded ?? localExpanded);
    const setNextExpanded = (nextExpanded: boolean) => {
      if (expanded === undefined) {
        setLocalExpanded(nextExpanded);
      }
      if (expansionKey) {
        onExpandedChange?.(expansionKey, nextExpanded);
      }
    };
    const groupLabel = label(row.calls.length);
    const singleCall = row.calls[0];
    if (!row.grouped && singleCall) {
      return renderToolCard(
        singleCall,
        onLinkClick,
        showRawTimelineJson,
        rawTimelineJsonLabel
      );
    }

    return (
      <div
        className="workspace-agents-status-panel__detail-tool-section"
        data-tool-group-expanded={isExpanded ? "true" : undefined}
      >
        {hasDetail ? (
          <button
            type="button"
            className="workspace-agents-status-panel__detail-tool-count"
            aria-expanded={isExpanded}
            aria-label={groupLabel}
            onClick={() => setNextExpanded(!isExpanded)}
          >
            <ToolGroupLabel label={groupLabel} />
            {isExpanded ? (
              <ChevronDown
                size={12}
                strokeWidth={2.2}
                aria-hidden="true"
                className="workspace-agents-status-panel__detail-tool-count-chevron"
              />
            ) : (
              <ChevronRight
                size={12}
                strokeWidth={2.2}
                aria-hidden="true"
                className="workspace-agents-status-panel__detail-tool-count-chevron"
              />
            )}
          </button>
        ) : (
          <div className="workspace-agents-status-panel__detail-tool-count">
            <ToolGroupLabel label={groupLabel} />
          </div>
        )}
        {hasDetail ? (
          <CollapsibleReveal expanded={isExpanded}>
            <div className="workspace-agents-status-panel__detail-tool-list">
              {row.entries.map((entry) =>
                entry.kind === "thinking" ? (
                  <div
                    key={entry.thinking.id}
                    className="workspace-agents-status-panel__detail-tool-list-item workspace-agents-status-panel__detail-tool-row"
                  >
                    <AgentThinkingDisclosure
                      thinking={entry.thinking}
                      label={thinkingLabel}
                      onLinkClick={onLinkClick}
                      showRawTimelineJson={showRawTimelineJson}
                      rawTimelineJsonLabel={rawTimelineJsonLabel}
                    />
                  </div>
                ) : (
                  <div
                    key={entry.call.id}
                    className="workspace-agents-status-panel__detail-tool-list-item"
                  >
                    {renderToolCard(
                      entry.call,
                      onLinkClick,
                      showRawTimelineJson,
                      rawTimelineJsonLabel
                    )}
                  </div>
                )
              )}
            </div>
          </CollapsibleReveal>
        ) : null}
      </div>
    );
  })
);

function ToolGroupLabel({ label }: { label: string }): JSX.Element {
  "use memo";
  return (
    <>
      <ToolActivityKindIcon
        kind="update_todos"
        width={16}
        height={16}
        className="workspace-agents-status-panel__detail-tool-count-icon"
      />
      <span className="workspace-agents-status-panel__detail-tool-count-label">
        {renderToolCountLabel(label)}
      </span>
    </>
  );
}

function renderToolCountLabel(label: string): JSX.Element {
  return (
    <span className="workspace-agents-status-panel__detail-tool-count-primary">
      {label}
    </span>
  );
}

function renderToolCard(
  call: AgentToolCallVM,
  onLinkClick?: (href: string) => void,
  showRawTimelineJson = false,
  rawTimelineJsonLabel = ""
): JSX.Element {
  const props = { call, onLinkClick };
  // A delegated sub-agent renders as its own first-class card - no tool-row
  // chrome, no prompt echo; identity/status belong to the sub-agent itself.
  if (call.task?.subAgents?.length) {
    return <AgentSubAgentCards call={call} onLinkClick={onLinkClick} />;
  }
  let card: JSX.Element;
  switch (call.rendererKind) {
    case "plan-enter":
      card = <AgentEnterPlanModeCard {...props} />;
      break;
    case "plan-exit":
      card = <AgentExitPlanModeCard {...props} />;
      break;
    case "ask-user":
      card = <AgentAskUserQuestionCard {...props} />;
      break;
    case "task":
      card = <AgentTaskCallCard {...props} />;
      break;
    default:
      card = <AgentToolCallCard {...props} />;
  }
  return (
    <>
      {card}
      {showRawTimelineJson && rawTimelineJsonLabel ? (
        <RawTimelineJsonDisclosure
          items={call.sourceTimelineItems}
          label={rawTimelineJsonLabel}
        />
      ) : null}
    </>
  );
}
