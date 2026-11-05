import { useMemo } from "react";
import { ReferenceProvenanceFilterControl } from "@tutti-os/workspace-file-reference/ui";
import { translate } from "../../i18n/index";
import type { AgentComposerReferenceProvenanceFilter } from "./composer/AgentComposer.types";
import { observer } from "mobx-react-lite";
export const AgentReferenceProvenanceFilterControl = observer(
  AgentReferenceProvenanceFilterControlRender
);

function AgentReferenceProvenanceFilterControlRender({
  filter,
  popoverElevation = "default"
}: {
  filter: AgentComposerReferenceProvenanceFilter;
  popoverElevation?: "default" | "panel";
}): React.JSX.Element {
  const labelsProp1Value1 = translate(
    "agentHost.agentGui.provenanceFilterAllAgents"
  );
  const labelsProp1Value2 = translate(
    "agentHost.agentGui.provenanceFilterAllMembers"
  );
  const labelsProp1Value3 = translate(
    "agentHost.agentGui.provenanceFilterAllSources"
  );
  const labelsProp1Value4 = translate(
    "agentHost.agentGui.provenanceFilterAgents"
  );
  const labelsProp1Value5 = translate(
    "agentHost.agentGui.provenanceFilterFilteredSources"
  );
  const labelsProp1Value6 = translate(
    "agentHost.agentGui.provenanceFilterMembers"
  );
  const labelsProp1 = useMemo<
    NonNullable<
      React.ComponentProps<typeof ReferenceProvenanceFilterControl>["labels"]
    >
  >(
    () => ({
      allAgents: labelsProp1Value1,
      allMembers: labelsProp1Value2,
      allSources: labelsProp1Value3,
      agents: labelsProp1Value4,
      filteredSources: labelsProp1Value5,
      members: labelsProp1Value6
    }),
    [
      labelsProp1Value1,
      labelsProp1Value2,
      labelsProp1Value3,
      labelsProp1Value4,
      labelsProp1Value5,
      labelsProp1Value6
    ]
  );
  return (
    <ReferenceProvenanceFilterControl
      agentOptions={filter.snapshot.catalog.agentOptions}
      enabledDimensions={filter.snapshot.catalog.enabledDimensions}
      labels={labelsProp1}
      memberOptions={filter.snapshot.catalog.memberOptions}
      popoverElevation={popoverElevation}
      value={filter.snapshot.value}
      onToggle={filter.controller.toggle}
      onToggleAll={filter.controller.toggleAll}
    />
  );
}
