/**
 * Pure picker body for issue assignee — polymorphic single-select over
 * members + agents + squads, plus an "Unassigned" option. See
 * status-picker-body.tsx for the split rationale.
 *
 * Mirrors web `packages/views/issues/components/pickers/assignee-picker.tsx`
 * (mobile skips frequency-sort; alphabetical instead).
 */
import { useMemo, useState } from "react";
import { FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type {
  Agent,
  IssueAssigneeType,
  MemberWithUser,
  Squad,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { TextField } from "@/components/ui/text-field";
import { memberListOptions } from "@/data/queries/members";
import { agentListOptions } from "@/data/queries/agents";
import { squadListOptions } from "@/data/queries/squads";
import { useWorkspaceStore } from "@/data/workspace-store";
import { cn } from "@/lib/utils";

export type AssigneeValue = {
  type: IssueAssigneeType;
  id: string;
} | null;

interface Props {
  value: AssigneeValue;
  onChange: (next: AssigneeValue) => void;
}

type Row =
  | { kind: "unassigned" }
  | { kind: "member"; member: MemberWithUser }
  | { kind: "agent"; agent: Agent }
  | { kind: "squad"; squad: Squad };

export function AssigneePickerBody({ value, onChange }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const [query, setQuery] = useState("");

  const rows = useMemo<Row[]>(() => {
    const q = query.trim().toLowerCase();
    const matchName = (name: string) => !q || name.toLowerCase().includes(q);

    const memberRows: Row[] = [...members]
      .filter((m) => matchName(m.name))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((m) => ({ kind: "member" as const, member: m }));
    const agentRows: Row[] = [...agents]
      .filter((a) => matchName(a.name))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((a) => ({ kind: "agent" as const, agent: a }));
    const squadRows: Row[] = [...squads]
      .filter((s) => !s.archived_at && matchName(s.name))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((s) => ({ kind: "squad" as const, squad: s }));

    if (q) return [...memberRows, ...agentRows, ...squadRows];
    return [
      { kind: "unassigned" },
      ...memberRows,
      ...agentRows,
      ...squadRows,
    ];
  }, [members, agents, squads, query]);

  const isSelected = (row: Row): boolean => {
    if (row.kind === "unassigned") return value === null;
    if (value === null) return false;
    if (row.kind === "member")
      return value.type === "member" && value.id === row.member.user_id;
    if (row.kind === "agent")
      return value.type === "agent" && value.id === row.agent.id;
    return value.type === "squad" && value.id === row.squad.id;
  };

  const select = (row: Row) => {
    if (row.kind === "unassigned") onChange(null);
    else if (row.kind === "member")
      onChange({ type: "member", id: row.member.user_id });
    else if (row.kind === "agent")
      onChange({ type: "agent", id: row.agent.id });
    else onChange({ type: "squad", id: row.squad.id });
  };

  return (
    <View className="flex-1">
      <View className="px-4 pt-3 pb-2">
        <Text className="text-lg font-semibold text-foreground">Assignee</Text>
      </View>
      <View className="px-3 pb-2 border-b border-border">
        <TextField
          value={query}
          onChangeText={setQuery}
          placeholder="Search people"
          autoCapitalize="none"
          autoCorrect={false}
        />
      </View>
      <FlatList
        data={rows}
        className="flex-1"
        keyboardShouldPersistTaps="handled"
        automaticallyAdjustKeyboardInsets
        keyExtractor={(row) => {
          if (row.kind === "unassigned") return "unassigned";
          if (row.kind === "member") return `m:${row.member.user_id}`;
          if (row.kind === "agent") return `a:${row.agent.id}`;
          return `s:${row.squad.id}`;
        }}
        renderItem={({ item }) => (
          <Pressable
            onPress={() => select(item)}
            className={cn(
              "flex-row items-center gap-3 px-3 py-2.5 active:bg-secondary",
              isSelected(item) && "bg-secondary",
            )}
          >
            {item.kind === "unassigned" ? (
              <View className="size-7 rounded-full border border-dashed border-muted-foreground/40 items-center justify-center">
                <Text className="text-xs text-muted-foreground">∅</Text>
              </View>
            ) : item.kind === "member" ? (
              <ActorAvatar
                type="member"
                id={item.member.user_id}
                size={28}
              />
            ) : item.kind === "agent" ? (
              <ActorAvatar type="agent" id={item.agent.id} size={28} />
            ) : (
              <ActorAvatar type="squad" id={item.squad.id} size={28} />
            )}
            <Text className="flex-1 text-sm text-foreground">
              {item.kind === "unassigned"
                ? "Unassigned"
                : item.kind === "member"
                  ? item.member.name
                  : item.kind === "agent"
                    ? item.agent.name
                    : item.squad.name}
            </Text>
            {isSelected(item) ? (
              <Text className="text-xs text-muted-foreground">✓</Text>
            ) : null}
          </Pressable>
        )}
        ListEmptyComponent={
          <View className="px-3 py-8 items-center">
            <Text className="text-xs text-muted-foreground">No matches.</Text>
          </View>
        }
      />
    </View>
  );
}
