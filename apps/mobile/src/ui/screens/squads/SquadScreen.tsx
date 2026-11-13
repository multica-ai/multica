import { useMemo } from "react";
import { Alert, Platform, Share } from "react-native";
import { useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { paths } from "@multica/core/paths";
import { useDeleteSquad, useRemoveSquadMember, useUpdateSquad, useUpdateSquadMemberRole } from "@multica/core/squads/mutations";
import { squadDetailOptions, squadMembersOptions, squadRecentIssuesOptions } from "@multica/core/squads/queries";
import { endpoints } from "../../../platform/env";
import { confirmDestructive, pick, promptText } from "../../components/action-sheet";
import { issueRowModel } from "../../model/issues";
import { useActorResolver } from "../../model/people";
import { parseRosterValue, rosterValue, useRoster } from "../../model/roster";
import { useStatusPresentation } from "../../model/status";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useStrings } from "../../strings";
import { SquadView, type SquadMemberModel } from "./SquadView";

const RECENT = 20;

export function SquadScreen() {
  const navigation = useNavigation();
  const { squadId } = useRoute<RouteProp<RootStackParamList, "Squad">>().params;
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const resolveActor = useActorResolver();
  const status = useStatusPresentation(workspace.id);
  const roster = useRoster(workspace.id);
  const detail = useQuery(squadDetailOptions(workspace.id, squadId));
  const members = useQuery(squadMembersOptions(workspace.id, squadId));
  const recent = useQuery(squadRecentIssuesOptions(workspace.id, squadId, RECENT));
  const updateSquad = useUpdateSquad(squadId);
  const removeMember = useRemoveSquadMember(squadId);
  const updateRole = useUpdateSquadMemberRole(squadId);
  const archiveSquad = useDeleteSquad();

  const squad = detail.data;
  const canManage = Boolean(squad && !squad.archived_at && (roster.isAdmin || squad.creator_id === roster.userId));
  const memberModels = useMemo<SquadMemberModel[]>(
    () =>
      (members.data ?? [])
        .map((member) => ({
          key: rosterValue(member.member_type, member.member_id),
          actor: resolveActor(member.member_type, member.member_id),
          role: member.role,
          leader: member.member_type === "agent" && member.member_id === squad?.leader_id,
        }))
        .sort((a, b) => Number(b.leader) - Number(a.leader)),
    [members.data, resolveActor, squad?.leader_id],
  );
  const issues = useMemo(() => (recent.data?.issues ?? []).map((issue) => issueRowModel(issue, status, resolveActor)), [recent.data, status, resolveActor]);
  const failed = () => Alert.alert(strings.squad.failed);
  const webUrl = `${endpoints.webUrl}${paths.workspace(workspace.slug).squadDetail(squadId)}`;

  return (
    <SquadView
      state={squad && members.data ? "ready" : detail.isError || members.isError ? "error" : "loading"}
      squad={squad ? { name: squad.name, description: squad.description, instructions: squad.instructions, leader: resolveActor("agent", squad.leader_id), archived: Boolean(squad.archived_at) } : null}
      members={memberModels}
      recent={{ state: recent.data ? "ready" : recent.isError ? "error" : "loading", issues }}
      canManage={canManage}
      refreshing={detail.isRefetching || members.isRefetching}
      onRefresh={() => {
        void detail.refetch();
        void members.refetch();
        void recent.refetch();
      }}
      onBack={() => navigation.goBack()}
      onRetry={() => {
        void detail.refetch();
        void members.refetch();
        void recent.refetch();
      }}
      onAddMember={() => navigation.navigate("SquadEditor", { squadId, addMembers: true })}
      onMember={async (member) => {
        const who = parseRosterValue(member.key);
        if (!who) return;
        const action = await pick(
          member.actor.name,
          [
            ...(who.type === "agent" && !member.leader ? [{ value: "leader" as const, label: strings.squad.setLeader }] : []),
            { value: "role" as const, label: strings.squad.editRole },
            ...(!member.leader ? [{ value: "remove" as const, label: strings.squad.remove, destructive: true }] : []),
          ],
          strings.common.cancel,
        );
        if (action === "leader") updateSquad.mutate({ leader_id: who.id }, { onError: failed });
        if (action === "remove") removeMember.mutate({ member_type: who.type, member_id: who.id }, { onError: failed });
        if (action === "role") {
          const role = await promptText(strings.squad.role, member.role, strings.form.save, strings.common.cancel);
          if (role !== null && role.trim() !== member.role) updateRole.mutate({ member_type: who.type, member_id: who.id, role: role.trim() }, { onError: failed });
        }
      }}
      onMore={async () => {
        const action = await pick(
          undefined,
          [
            ...(canManage ? [{ value: "edit" as const, label: strings.squad.edit }] : []),
            { value: "share" as const, label: strings.form.share },
            ...(canManage ? [{ value: "archive" as const, label: strings.squad.archive, destructive: true }] : []),
          ],
          strings.common.cancel,
        );
        if (action === "edit") navigation.navigate("SquadEditor", { squadId });
        if (action === "share") void Share.share({ url: webUrl, message: Platform.OS === "ios" ? undefined : webUrl });
        if (action === "archive" && (await confirmDestructive(strings.squad.archiveConfirm, strings.squad.archiveDetail, strings.squad.archive, strings.common.cancel))) {
          archiveSquad.mutate(squadId, { onSuccess: () => navigation.goBack(), onError: failed });
        }
      }}
      onOpenIssue={(issue) => navigation.navigate("Issue", { issueId: issue.id })}
    />
  );
}
