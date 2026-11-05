import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { observer } from "mobx-react-lite";
import { useState, type JSX } from "react";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import { AgentToolCallHeader } from "./AgentToolCallHeader";
import { CollapsibleReveal } from "./CollapsibleReveal";
import { AgentExpandedToolContent } from "./tool-renderers/AgentExpandedToolContent";
import { hasAgentToolContent } from "./tool-renderers/agentToolContentShared";

export interface AgentToolCallCardProps {
  call: AgentToolCallVM;
  onLinkClick?: (href: string) => void;
  defaultExpanded?: boolean;
  nonCollapsible?: boolean;
  variantClassName?: string;
}

export const AgentToolCallCard = observer(function AgentToolCallCard({
  call,
  onLinkClick,
  defaultExpanded = false,
  nonCollapsible = false,
  variantClassName
}: AgentToolCallCardProps): JSX.Element {
  "use memo";
  const [expanded, setExpanded] = useState(defaultExpanded);
  const hasDetail = hasAgentToolContent(call);
  const canCollapse = hasDetail && !nonCollapsible;
  const ariaLabel = toolCallAriaLabel(call);

  return (
    <div
      className={[
        "workspace-agents-status-panel__detail-tool-row",
        variantClassName ?? ""
      ]
        .filter(Boolean)
        .join(" ")}
    >
      {canCollapse ? (
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
      {hasDetail && nonCollapsible ? (
        <AgentExpandedToolContent call={call} onLinkClick={onLinkClick} />
      ) : null}
      {hasDetail && !nonCollapsible ? (
        <CollapsibleReveal expanded={expanded}>
          <AgentExpandedToolContent call={call} onLinkClick={onLinkClick} />
        </CollapsibleReveal>
      ) : null}
    </div>
  );
});

function toolCallAriaLabel(call: AgentToolCallVM): string {
  return [
    call.name,
    call.status,
    call.compactSummary?.trim() || call.summary.trim()
  ]
    .filter(Boolean)
    .join(" ");
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
  call: NonNullable<AgentToolCallCardProps>["call"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    expanded: boolean;
    ariaLabel: string;
    setExpanded: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
    call: NonNullable<AgentToolCallCardProps>["call"];
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
