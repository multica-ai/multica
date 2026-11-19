import { useMemo } from "react";
import { useNavigation } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { squadListOptions } from "@multica/core/workspace/queries";
import { useActorResolver } from "../../model/people";
import { useWorkspace } from "../../model/workspace-context";
import { SquadsView, type SquadRowModel } from "./SquadsView";

const PREVIEW = 5;

export function SquadsScreen() {
  const navigation = useNavigation();
  const { workspace } = useWorkspace();
  const resolveActor = useActorResolver();
  const squads = useQuery(squadListOptions(workspace.id));
  const rows = useMemo<SquadRowModel[]>(
    () =>
      (squads.data ?? [])
        .filter((squad) => !squad.archived_at)
        .map((squad) => ({
          id: squad.id,
          name: squad.name,
          description: squad.description,
          leader: resolveActor("agent", squad.leader_id),
          members: (squad.member_preview ?? []).slice(0, PREVIEW).map((member) => resolveActor(member.member_type, member.member_id)),
          memberCount: squad.member_count ?? squad.member_preview?.length ?? 0,
        })),
    [squads.data, resolveActor],
  );
  return (
    <SquadsView
      state={squads.data ? "ready" : squads.isError ? "error" : "loading"}
      squads={rows}
      refreshing={squads.isRefetching}
      onRefresh={() => void squads.refetch()}
      onRetry={() => void squads.refetch()}
      onOpen={(squadId) => navigation.navigate("Squad", { squadId })}
      onCreate={() => navigation.navigate("SquadEditor", {})}
    />
  );
}
