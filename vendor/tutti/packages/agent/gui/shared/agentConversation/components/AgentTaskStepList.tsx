import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { observer } from "mobx-react-lite";
import { useState, type JSX } from "react";
import type { AgentTaskStepVM } from "../contracts/agentTaskItemVM";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import { projectAgentTaskStepTool } from "../projection/agentTaskProjection";
import { AgentToolCallHeader } from "./AgentToolCallHeader";
import { AgentExpandedToolContent } from "./tool-renderers/AgentExpandedToolContent";
import { hasAgentToolContent } from "./tool-renderers/agentToolContentShared";
const AgentTaskStepRow = observer(AgentTaskStepRowRender);

interface AgentTaskStepListProps {
  steps: AgentTaskStepVM[];
  onLinkClick?: (href: string) => void;
}

export const AgentTaskStepList = observer(function AgentTaskStepList({
  steps,
  onLinkClick
}: AgentTaskStepListProps): JSX.Element {
  "use memo";
  return (
    <div className="workspace-agents-status-panel__detail-tool-stack">
      {steps.map((step) => (
        <AgentTaskStepRow key={step.id} step={step} onLinkClick={onLinkClick} />
      ))}
    </div>
  );
});

function AgentTaskStepRowRender({
  step,
  onLinkClick
}: {
  step: AgentTaskStepVM;
  onLinkClick?: (href: string) => void;
}): JSX.Element {
  "use memo";
  const [expanded, setExpanded] = useState(false);
  const call = step.tool ?? projectTaskStepCall(step);
  const hasDetail = hasAgentToolContent(call);
  const ariaLabel = taskStepAriaLabel(call);

  return (
    <div className="workspace-agents-status-panel__detail-tool-row">
      {hasDetail ? (
        <ButtonBinding1
          expanded={expanded}
          ariaLabel={ariaLabel}
          setExpanded={setExpanded}
          call={call}
        />
      ) : (
        <div className="workspace-agents-status-panel__detail-tool-row-head">
          <AgentToolCallHeader call={call} expanded={false} hasDetail={false} />
        </div>
      )}
      {!hasDetail && step.summary ? (
        <div className="workspace-agents-status-panel__detail-tool-summary">
          {step.summary}
        </div>
      ) : null}
      {hasDetail && expanded ? (
        <AgentExpandedToolContent call={call} onLinkClick={onLinkClick} />
      ) : null}
    </div>
  );
}

function taskStepAriaLabel(call: AgentToolCallVM): string {
  return [
    call.name,
    call.status,
    call.compactSummary?.trim() || call.summary.trim()
  ]
    .filter(Boolean)
    .join(" ");
}

function projectTaskStepCall(step: AgentTaskStepVM): AgentToolCallVM {
  return projectAgentTaskStepTool({
    id: step.id,
    turnId: step.turnId,
    toolName: step.toolName,
    name: step.name,
    callType: null,
    status: step.status,
    summary: step.summary,
    payload: null,
    metadata: null,
    input:
      step.payload &&
      typeof step.payload.input === "object" &&
      step.payload.input !== null
        ? (step.payload.input as Record<string, unknown>)
        : null,
    output:
      step.payload &&
      typeof step.payload.output === "object" &&
      step.payload.output !== null
        ? (step.payload.output as Record<string, unknown>)
        : null,
    error:
      step.payload &&
      typeof step.payload.error === "object" &&
      step.payload.error !== null
        ? (step.payload.error as Record<string, unknown>)
        : null,
    locations: null,
    occurredAtUnixMs: step.occurredAtUnixMs ?? null
  });
}

const ButtonBinding1 = observer(function ButtonBinding1({
  expanded,
  ariaLabel,
  setExpanded,
  call,
  ...slotProps
}: {
  expanded: boolean;
  ariaLabel: string;
  setExpanded: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
  call: AgentToolCallVM;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    expanded: boolean;
    ariaLabel: string;
    setExpanded: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
    call: AgentToolCallVM;
  }
>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => setExpanded((value) => !value));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className="workspace-agents-status-panel__detail-tool-row-head workspace-agents-status-panel__detail-tool-row-head--button"
        aria-expanded={expanded}
        aria-label={ariaLabel}
        onClick={onClickProp1}
      >
        <AgentToolCallHeader call={call} expanded={expanded} hasDetail />
      </button>
    </Slot.Root>
  );
});
