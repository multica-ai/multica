import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
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
          <ButtonBinding1
            expanded={expanded}
            setExpanded={setExpanded}
            call={call}
          />
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

const ButtonBinding1 = observer(function ButtonBinding1({
  expanded,
  setExpanded,
  call,
  ...slotProps
}: {
  expanded: boolean;
  setExpanded: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
  call: NonNullable<AgentToolCallCardProps>["call"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    expanded: boolean;
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
        onClick={onClickProp1}
      >
        <AgentToolCallHeader call={call} expanded={expanded} hasDetail />
      </button>
    </Slot.Root>
  );
});
