import { observer } from "mobx-react-lite";
import { useState, type JSX } from "react";
import type { AgentToolCallCardProps } from "./AgentToolCallCard";
import { AgentToolCallHeader } from "./AgentToolCallHeader";
import { CollapsibleReveal } from "./CollapsibleReveal";
import { AgentExpandedToolContent } from "./tool-renderers/AgentExpandedToolContent";
import { hasAgentToolContent } from "./tool-renderers/agentToolContentShared";

export const AgentAskUserQuestionCard = observer(
  function AgentAskUserQuestionCard({
    call,
    onLinkClick,
    defaultExpanded,
    nonCollapsible
  }: AgentToolCallCardProps): JSX.Element {
    "use memo";
    const waitingInput =
      (call.askUserQuestion?.status ?? call.status ?? "")
        .trim()
        .toLowerCase() === "waiting_input";
    const hasDetail = hasAgentToolContent(call);
    const pinned = nonCollapsible ?? waitingInput;
    const [expanded, setExpanded] = useState(defaultExpanded ?? waitingInput);

    return (
      <div className="workspace-agents-status-panel__detail-tool-row workspace-agents-status-panel__detail-tool-row--ask-user">
        {hasDetail && !pinned ? (
          <button
            type="button"
            className="workspace-agents-status-panel__detail-tool-row-head workspace-agents-status-panel__detail-tool-row-head--button"
            aria-expanded={expanded}
            onClick={() => setExpanded((value) => !value)}
          >
            <AgentToolCallHeader call={call} expanded={expanded} hasDetail />
          </button>
        ) : (
          <div className="workspace-agents-status-panel__detail-tool-row-head">
            <AgentToolCallHeader
              call={call}
              expanded={false}
              hasDetail={false}
            />
          </div>
        )}
        {!hasDetail && call.summary ? (
          <div className="workspace-agents-status-panel__detail-tool-summary">
            {call.summary}
          </div>
        ) : null}
        {hasDetail && pinned ? (
          <AgentExpandedToolContent call={call} onLinkClick={onLinkClick} />
        ) : null}
        {hasDetail && !pinned ? (
          <CollapsibleReveal expanded={expanded}>
            <AgentExpandedToolContent call={call} onLinkClick={onLinkClick} />
          </CollapsibleReveal>
        ) : null}
      </div>
    );
  }
);
