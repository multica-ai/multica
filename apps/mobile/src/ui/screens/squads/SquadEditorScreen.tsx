import { useState } from "react";
import { Alert } from "react-native";
import { StackActions, useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { useAddSquadMember, useCreateSquad, useUpdateSquad, type NewSquadMember } from "@multica/core/squads/mutations";
import { squadDetailOptions, squadMembersOptions } from "@multica/core/squads/queries";
import type { Squad, SquadMember } from "@multica/core/types";
import { Failure, Loading } from "../../components/States";
import { parseRosterValue, rosterValue, useRoster } from "../../model/roster";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useStrings } from "../../strings";
import { SquadEditorView, type SquadDraft, type SquadEditorPage } from "./SquadEditorView";

const EMPTY_DRAFT: SquadDraft = { name: "", description: "", instructions: "", leader: null, members: [] };

function toNewMembers(values: readonly string[]): NewSquadMember[] {
  return values.flatMap((value) => {
    const who = parseRosterValue(value);
    return who ? [{ member_type: who.type, member_id: who.id }] : [];
  });
}

export function SquadEditorScreen() {
  const { squadId, addMembers = false } = useRoute<RouteProp<RootStackParamList, "SquadEditor">>().params;
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const squad = useQuery({ ...squadDetailOptions(workspace.id, squadId ?? ""), enabled: Boolean(squadId) });
  const members = useQuery({ ...squadMembersOptions(workspace.id, squadId ?? ""), enabled: Boolean(squadId) });
  if (squadId && (!squad.data || !members.data)) {
    return squad.isError || members.isError ? (
      <Failure
        title={strings.common.loadFailed}
        retryLabel={strings.common.retry}
        onRetry={() => {
          void squad.refetch();
          void members.refetch();
        }}
      />
    ) : (
      <Loading />
    );
  }
  return <SquadEditor squad={squad.data ?? null} members={members.data ?? []} addMembers={addMembers} />;
}

function SquadEditor({ squad, members, addMembers }: { squad: Squad | null; members: readonly SquadMember[]; addMembers: boolean }) {
  const navigation = useNavigation();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const roster = useRoster(workspace.id);
  const createSquad = useCreateSquad();
  const updateSquad = useUpdateSquad(squad?.id ?? "");
  const addMember = useAddSquadMember(squad?.id ?? "");
  const [draft, setDraft] = useState<SquadDraft>(() =>
    squad ? { name: squad.name, description: squad.description, instructions: squad.instructions, leader: squad.leader_id, members: [] } : EMPTY_DRAFT,
  );
  const [page, setPage] = useState<SquadEditorPage>("form");
  const [busy, setBusy] = useState(false);
  const existing = new Set(members.map((member) => rosterValue(member.member_type, member.member_id)));
  const mode = addMembers ? "addMembers" : squad ? "edit" : "create";

  const save = async () => {
    setBusy(true);
    try {
      if (mode === "addMembers") {
        const results = await Promise.allSettled(toNewMembers(draft.members).map((member) => addMember.mutateAsync(member)));
        const failed = results.filter((result) => result.status === "rejected").length;
        if (failed > 0) Alert.alert(strings.squad.membersFailed(failed));
        navigation.goBack();
      } else if (squad) {
        await updateSquad.mutateAsync({
          name: draft.name.trim(),
          description: draft.description.trim(),
          instructions: draft.instructions.trim(),
          ...(draft.leader && draft.leader !== squad.leader_id ? { leader_id: draft.leader } : {}),
        });
        navigation.goBack();
      } else if (draft.leader) {
        const result = await createSquad.mutateAsync({
          name: draft.name.trim(),
          description: draft.description.trim() || undefined,
          leader_id: draft.leader,
          members: toNewMembers(draft.members),
        });
        if (result.failedMembers.length > 0) Alert.alert(strings.squad.membersFailed(result.failedMembers.length));
        navigation.dispatch(StackActions.replace("Squad", { squadId: result.squad.id }));
      }
    } catch {
      Alert.alert(strings.squad.failed);
    } finally {
      setBusy(false);
    }
  };

  return (
    <SquadEditorView
      mode={mode}
      draft={draft}
      onChange={(patch) => setDraft((current) => ({ ...current, ...patch }))}
      page={page}
      onPage={setPage}
      agents={roster.activeAgents}
      candidates={[...roster.members, ...roster.activeAgents].filter((entry) => !existing.has(entry.value))}
      busy={busy}
      onCancel={() => navigation.goBack()}
      onSave={() => void save()}
    />
  );
}
