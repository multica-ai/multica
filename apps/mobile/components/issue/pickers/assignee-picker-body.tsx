/**
 * Pure picker body for issue assignee — polymorphic single-select over
 * members + agents + squads, plus an "Unassigned" option. See
 * status-picker-body.tsx for the split rationale.
 *
 * Mirrors web `packages/views/issues/components/pickers/assignee-picker.tsx`
 * (mobile skips frequency-sort; alphabetical instead).
 *
 * Header + search bar are owned by the iOS native nav header registered in
 * `app/(app)/[workspace]/_layout.tsx` (assignee Stack.Screen sets
 * `headerShown: true` + `title`); the route file wires
 * `headerSearchBarOptions.onChangeText` to a local `query` state and passes
 * it in as the `query` prop. This body is just a FlatList — no chrome.
 */
import { useMemo } from "react";
import { FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import { useColorScheme } from "nativewind";
import type {
  Agent,
  IssueAssigneeType,
  MemberWithUser,
  Squad,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { memberListOptions } from "@/data/queries/members";
import { agentListOptions } from "@/data/queries/agents";
import { squadListOptions } from "@/data/queries/squads";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useScrollToTopOnChange } from "@/lib/use-scroll-to-top-on-change";
import { THEME } from "@/lib/theme";

const AVATAR_SIZE = 36;

export type AssigneeValue = {
  type: IssueAssigneeType;
  id: string;
} | null;

interface Props {
  value: AssigneeValue;
  query: string;
  onChange: (next: AssigneeValue) => void;
}

type Row =
  | { kind: "unassigned" }
  | { kind: "member"; member: MemberWithUser }
  | { kind: "agent"; agent: Agent }
  | { kind: "squad"; squad: Squad };

function isRowSelected(value: AssigneeValue, row: Row): boolean {
  if (row.kind === "unassigned") return value === null;
  if (value === null) return false;
  if (row.kind === "member")
    return value.type === "member" && value.id === row.member.user_id;
  if (row.kind === "agent")
    return value.type === "agent" && value.id === row.agent.id;
  return value.type === "squad" && value.id === row.squad.id;
}

export function AssigneePickerBody({ value, query, onChange }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const listRef = useScrollToTopOnChange(query);
  const { colorScheme } = useColorScheme();
  // Tint color for the checkmark accessory. Project uses a monochrome
  // shadcn palette where `primary` is the canonical tint (near-black light /
  // near-white dark); matches Apple HIG's "tintColor" semantics for
  // selection accessories.
  const checkColor =
    colorScheme === "dark" ? THEME.dark.primary : THEME.light.primary;

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

    // Pin the currently-selected actor right below Unassigned and remove it
    // from its own section so it doesn't render twice. Apple HIG doesn't
    // require this — it's a product UX choice that speeds up the common
    // "see who's assigned + reassign nearby" path. Skipped when query is
    // active because search-result order should reflect matches, not state.
    const all = [...memberRows, ...agentRows, ...squadRows];
    const selectedRow = all.find((r) => isRowSelected(value, r));
    return [
      { kind: "unassigned" },
      ...(selectedRow ? [selectedRow] : []),
      ...memberRows.filter((r) => !isRowSelected(value, r)),
      ...agentRows.filter((r) => !isRowSelected(value, r)),
      ...squadRows.filter((r) => !isRowSelected(value, r)),
    ];
  }, [members, agents, squads, query, value]);

  const isSelected = (row: Row) => isRowSelected(value, row);

  const select = (row: Row) => {
    if (row.kind === "unassigned") onChange(null);
    else if (row.kind === "member")
      onChange({ type: "member", id: row.member.user_id });
    else if (row.kind === "agent")
      onChange({ type: "agent", id: row.agent.id });
    else onChange({ type: "squad", id: row.squad.id });
  };

  // FlatList is returned as the route's direct child so RNSScreenContentWrapper
  // can find it as a direct subview and apply the iOS formSheet header offset.
  // See react-native-screens#3634 — wrapping in a parent <View> hides the list
  // from the native search and the rows render at y=0, overlapping the header.
  return (
    <FlatList
      ref={listRef}
      data={rows}
      className="flex-1"
      keyboardShouldPersistTaps="handled"
      automaticallyAdjustKeyboardInsets
      contentInsetAdjustmentBehavior="automatic"
      keyExtractor={(row) => {
        if (row.kind === "unassigned") return "unassigned";
        if (row.kind === "member") return `m:${row.member.user_id}`;
        if (row.kind === "agent") return `a:${row.agent.id}`;
        return `s:${row.squad.id}`;
      }}
      renderItem={({ item }) => (
        <Pressable
          onPress={() => select(item)}
          className="flex-row items-center gap-3 px-4 py-3 active:bg-secondary"
        >
          {item.kind === "unassigned" ? (
            <View
              className="rounded-full border border-dashed border-muted-foreground/40 items-center justify-center"
              style={{ width: AVATAR_SIZE, height: AVATAR_SIZE }}
            >
              <Text className="text-sm text-muted-foreground">∅</Text>
            </View>
          ) : item.kind === "member" ? (
            <ActorAvatar
              type="member"
              id={item.member.user_id}
              size={AVATAR_SIZE}
            />
          ) : item.kind === "agent" ? (
            <ActorAvatar type="agent" id={item.agent.id} size={AVATAR_SIZE} />
          ) : (
            <ActorAvatar type="squad" id={item.squad.id} size={AVATAR_SIZE} />
          )}
          <Text className="flex-1 text-base text-foreground">
            {item.kind === "unassigned"
              ? "Unassigned"
              : item.kind === "member"
                ? item.member.name
                : item.kind === "agent"
                  ? item.agent.name
                  : item.squad.name}
          </Text>
          {/* Right-aligned secondary label. Mirrors Apple's
              UITableViewCellStyleValue1 / UIListContentConfiguration.valueCell
              pattern used throughout iOS Settings — type tag in lighter font on
              the same row. Members carry no tag (they're the default actor). */}
          {item.kind === "agent" ? (
            <Text className="text-sm text-muted-foreground">Agent</Text>
          ) : item.kind === "squad" ? (
            <Text className="text-sm text-muted-foreground">Squad</Text>
          ) : null}
          {isSelected(item) ? (
            <Ionicons name="checkmark" size={20} color={checkColor} />
          ) : null}
        </Pressable>
      )}
      ListEmptyComponent={
        <View className="px-3 py-8 items-center">
          <Text className="text-sm text-muted-foreground">No matches.</Text>
        </View>
      }
    />
  );
}
