import { useMemo } from "react";
import { useNavigation } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { autopilotSections } from "../../model/autopilots";
import { useAssigneeResolver } from "../../model/roster";
import { useWorkspace } from "../../model/workspace-context";
import { useStrings } from "../../strings";
import { AutopilotsView } from "./AutopilotsView";

export function AutopilotsScreen() {
  const navigation = useNavigation();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const resolveAssignee = useAssigneeResolver(workspace.id);
  const autopilots = useQuery(autopilotListOptions(workspace.id));
  const sections = useMemo(() => autopilotSections(autopilots.data ?? [], resolveAssignee, strings), [autopilots.data, resolveAssignee, strings]);
  return (
    <AutopilotsView
      state={autopilots.data ? "ready" : autopilots.isError ? "error" : "loading"}
      sections={sections}
      refreshing={autopilots.isRefetching}
      onRefresh={() => void autopilots.refetch()}
      onRetry={() => void autopilots.refetch()}
      onOpen={(autopilotId) => navigation.navigate("Autopilot", { autopilotId })}
      onCreate={() => navigation.navigate("AutopilotEditor", {})}
    />
  );
}
