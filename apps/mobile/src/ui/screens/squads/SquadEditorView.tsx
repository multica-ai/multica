import { Avatar } from "../../components/Avatar";
import { ChoiceList, Field, FormBody, SheetHeader, type Choice } from "../../components/Form";
import { Group, GroupNote, GroupRow } from "../../components/Group";
import { Screen } from "../../components/Screen";
import { rosterValue, type RosterEntry } from "../../model/roster";
import { useStrings } from "../../strings";

export interface SquadDraft {
  name: string;
  description: string;
  instructions: string;
  /** Agent id. */
  leader: string | null;
  /** Roster values of the members to add. */
  members: readonly string[];
}

export type SquadEditorPage = "form" | "leader" | "members";

interface SquadEditorViewProps {
  mode: "create" | "edit" | "addMembers";
  draft: SquadDraft;
  onChange: (patch: Partial<SquadDraft>) => void;
  page: SquadEditorPage;
  onPage: (page: SquadEditorPage) => void;
  /** Agents that can lead. */
  agents: readonly RosterEntry[];
  /** People and agents that can join. */
  candidates: readonly RosterEntry[];
  busy: boolean;
  onCancel: () => void;
  onSave: () => void;
}

const choiceOf = (entry: RosterEntry): Choice<string> => ({ value: entry.value, label: entry.actor.name, detail: entry.detail, leading: <Avatar actor={entry.actor} size={24} /> });

export function SquadEditorView({ mode, draft, onChange, page, onPage, agents, candidates, busy, onCancel, onSave }: SquadEditorViewProps) {
  const strings = useStrings();
  const back = { label: strings.common.back, onPress: () => onPage("form"), back: true };
  const leader = agents.find((entry) => entry.id === draft.leader) ?? null;
  const joinable = candidates.filter((entry) => entry.value !== (draft.leader ? rosterValue("agent", draft.leader) : null));
  const toggleMember = (value: string) =>
    onChange({ members: draft.members.includes(value) ? draft.members.filter((other) => other !== value) : [...draft.members, value] });

  if (page === "leader") {
    return (
      <Screen>
        <SheetHeader title={strings.squad.pickLeader} leading={back} />
        <ChoiceList
          choices={agents.map(choiceOf)}
          selected={leader ? [leader.value] : []}
          searchPlaceholder={strings.form.searchPlaceholder}
          onToggle={(value) => {
            const id = agents.find((entry) => entry.value === value)?.id ?? null;
            onChange({ leader: id, members: draft.members.filter((other) => other !== value) });
            onPage("form");
          }}
        />
      </Screen>
    );
  }

  if (page === "members" || mode === "addMembers") {
    return (
      <Screen>
        <SheetHeader
          title={mode === "addMembers" ? strings.squad.addMembers : strings.squad.pickMembers}
          leading={mode === "addMembers" ? { label: strings.common.cancel, onPress: onCancel } : back}
          trailing={
            mode === "addMembers"
              ? { label: strings.form.add, onPress: onSave, disabled: draft.members.length === 0, busy }
              : { label: strings.form.done, onPress: () => onPage("form") }
          }
        />
        <ChoiceList
          choices={joinable.map(choiceOf)}
          selected={draft.members}
          searchPlaceholder={strings.form.searchPlaceholder}
          emptyLabel={strings.squad.noMembersLeft}
          onToggle={toggleMember}
        />
      </Screen>
    );
  }

  return (
    <Screen>
      <SheetHeader
        title={mode === "create" ? strings.squad.new : strings.squad.edit}
        leading={{ label: strings.common.cancel, onPress: onCancel }}
        trailing={{ label: mode === "create" ? strings.form.create : strings.form.save, onPress: onSave, disabled: draft.name.trim() === "" || !draft.leader, busy }}
      />
      <FormBody>
        <Field value={draft.name} onChangeText={(name) => onChange({ name })} placeholder={strings.squad.namePlaceholder} autoFocus={mode === "create"} large />
        <Field value={draft.description} onChangeText={(description) => onChange({ description })} placeholder={strings.squad.descriptionPlaceholder} multiline />
        {mode === "edit" ? (
          <>
            <GroupNote above>{strings.squad.instructions}</GroupNote>
            <Field value={draft.instructions} onChangeText={(instructions) => onChange({ instructions })} placeholder={strings.squad.instructionsPlaceholder} multiline />
          </>
        ) : null}
        <Group>
          <GroupRow
            leading={leader ? <Avatar actor={leader.actor} size={20} /> : undefined}
            icon={leader ? undefined : "Crown"}
            label={strings.squad.leader}
            value={leader?.actor.name ?? strings.squad.pickLeader}
            chevron
            onPress={() => onPage("leader")}
            last={mode !== "create"}
          />
          {mode === "create" ? (
            <GroupRow icon="Users" label={strings.squad.members} value={draft.members.length > 0 ? strings.squad.memberCount(draft.members.length) : strings.form.none} chevron onPress={() => onPage("members")} last />
          ) : null}
        </Group>
        <GroupNote>{strings.squad.leaderHint}</GroupNote>
      </FormBody>
    </Screen>
  );
}
