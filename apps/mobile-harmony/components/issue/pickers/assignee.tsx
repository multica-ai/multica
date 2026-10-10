/**
 * HarmonyOS port of apps/mobile/components/issue/pickers/assignee-picker-body.tsx
 * (+ `issue/[id]/picker/assignee.tsx`) — polymorphic single-select over
 * members + agents + squads, plus an "Unassigned" option.
 *
 * Mirrors web's assignee picker (mobile skips frequency-sort; alphabetical
 * instead). The iOS native search bar (useNativeSearchBar) becomes a plain
 * TextField above the list inside the sheet; the body keeps its
 * scroll-reset-on-query behavior via useScrollToTopOnChange.
 */
import React, { useMemo } from "react";
import { FlatList, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type {
  Agent,
  MemberWithUser,
  Squad,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { Icon } from "@/components/ui/icon";
import { TextField } from "@/components/ui/text-field";
import { memberListOptions } from "@/data/queries/members";
import { agentListOptions } from "@/data/queries/agents";
import { squadListOptions } from "@/data/queries/squads";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useScrollToTopOnChange } from "@/lib/use-scroll-to-top-on-change";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { isAgentRuntimeBound } from "@/lib/is-agent-runtime-bound";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { issueDetailOptions } from "@/data/queries/issues";
import { useUpdateIssue } from "@/data/mutations/issues";

const AVATAR_SIZE = 36;

export type AssigneeValue = {
  type: "member" | "agent" | "squad";
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
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const runnableAgentIds = useMemo(
    () =>
      new Set(
        agents
          .filter((agent) => !agent.archived_at && isAgentRuntimeBound(agent))
          .map((agent) => agent.id),
      ),
    [agents],
  );
  const listRef = useScrollToTopOnChange(query);

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
    // from its own section so it doesn't render twice. Skipped when query is
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

  return (
    <FlatList
      ref={listRef}
      data={rows}
      style={styles.list}
      keyboardShouldPersistTaps="handled"
      keyExtractor={(row) => {
        if (row.kind === "unassigned") return "unassigned";
        if (row.kind === "member") return `m:${row.member.user_id}`;
        if (row.kind === "agent") return `a:${row.agent.id}`;
        return `s:${row.squad.id}`;
      }}
      renderItem={({ item }) => {
        const needsRuntime =
          (item.kind === "agent" && !isAgentRuntimeBound(item.agent)) ||
          (item.kind === "squad" &&
            !runnableAgentIds.has(item.squad.leader_id));
        return (
          <Pressable
            disabled={needsRuntime}
            onPress={() => select(item)}
            accessibilityRole="button"
            accessibilityLabel={
              item.kind === "unassigned"
                ? "Unassigned"
                : item.kind === "member"
                  ? item.member.name
                  : item.kind === "agent"
                    ? item.agent.name
                    : item.squad.name
            }
            style={({ pressed }) => [
              styles.row,
              needsRuntime && styles.rowDisabled,
              pressed && !needsRuntime ? { backgroundColor: c.secondary } : null,
            ]}
          >
            {item.kind === "unassigned" ? (
              <View
                style={[
                  styles.unassignedAvatar,
                  { borderColor: withAlpha(c.mutedForeground, 0.4) },
                ]}
              >
                <Text style={{ color: c.mutedForeground, fontSize: 14 }}>∅</Text>
              </View>
            ) : item.kind === "member" ? (
              <ActorAvatar
                type="member"
                id={item.member.user_id}
                name={item.member.name}
                avatarUrl={item.member.avatar_url}
                size={AVATAR_SIZE}
              />
            ) : item.kind === "agent" ? (
              <ActorAvatar
                type="agent"
                id={item.agent.id}
                name={item.agent.name}
                size={AVATAR_SIZE}
              />
            ) : (
              <ActorAvatar
                type="squad"
                id={item.squad.id}
                name={item.squad.name}
                size={AVATAR_SIZE}
              />
            )}
            <Text style={[styles.rowLabel, { color: c.foreground }]} numberOfLines={1}>
              {item.kind === "unassigned"
                ? "Unassigned"
                : item.kind === "member"
                  ? item.member.name
                  : item.kind === "agent"
                    ? item.agent.name
                    : item.squad.name}
            </Text>
            {/* Right-aligned secondary label. Mirrors Apple's
                UITableViewCellStyleValue1 pattern — type tag in lighter font
                on the same row. Members carry no tag (they're the default
                actor). */}
            {item.kind === "agent" ? (
              <Text style={[styles.tag, { color: c.mutedForeground }]}>
                {isAgentRuntimeBound(item.agent) ? "Agent" : "Needs runtime"}
              </Text>
            ) : item.kind === "squad" ? (
              <Text style={[styles.tag, { color: c.mutedForeground }]}>
                {needsRuntime ? "Leader needs runtime" : "Squad"}
              </Text>
            ) : null}
            {isSelected(item) ? (
              <Icon name="checkmark" size={20} color={c.primary} />
            ) : null}
          </Pressable>
        );
      }}
      ListEmptyComponent={
        <View style={styles.empty}>
          <Text style={{ color: c.mutedForeground, fontSize: 14 }}>
            No matches.
          </Text>
        </View>
      }
    />
  );
}

interface SheetProps {
  issueId: string;
  visible: boolean;
  onClose: () => void;
}

/**
 * The iOS formSheet route. iOS resets the native search text on dismiss;
  * local query state lives here so reopening starts fresh.
 */
export function IssueAssigneePickerSheet({ issueId, visible, onClose }: SheetProps) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const updateIssue = useUpdateIssue(issueId);
  const [query, setQuery] = React.useState("");

  const handleClose = () => {
    setQuery("");
    onClose();
  };

  const value: AssigneeValue | null =
    issue?.assignee_type && issue?.assignee_id
      ? { type: issue.assignee_type, id: issue.assignee_id }
      : null;

  return (
    <BottomSheet visible={visible} onClose={handleClose} maxHeightRatio={0.7}>
      <View style={styles.sheetHeader}>
        <Text style={[styles.sheetTitle, { color: c.foreground }]}>
          Assignee
        </Text>
        <TextField
          value={query}
          onChangeText={setQuery}
          placeholder="Search people"
          autoCapitalize="none"
          style={styles.search}
        />
      </View>
      <View style={styles.sheetList}>
        <AssigneePickerBody
          value={value}
          query={query}
          onChange={(next) => {
            if (next === null) {
              updateIssue.mutate({ assignee_type: null, assignee_id: null });
            } else {
              updateIssue.mutate({
                assignee_type: next.type,
                assignee_id: next.id,
              });
            }
            handleClose();
          }}
        />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  // flex-1
  list: { flex: 1 },
  // flex-row items-center gap-3 px-4 py-3
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  // opacity-50 on needs-runtime rows
  rowDisabled: { opacity: 0.5 },
  // rounded-full border border-dashed items-center justify-center
  unassignedAvatar: {
    width: AVATAR_SIZE,
    height: AVATAR_SIZE,
    borderRadius: AVATAR_SIZE / 2,
    borderWidth: 1,
    borderStyle: "dashed",
    alignItems: "center",
    justifyContent: "center",
  },
  // flex-1 text-base
  rowLabel: { flex: 1, fontSize: 16 },
  // text-sm
  tag: { fontSize: 14 },
  // px-3 py-8 items-center
  empty: { paddingHorizontal: 12, paddingVertical: 32, alignItems: "center" },
  sheetHeader: {
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 8,
    gap: 8,
  },
  sheetTitle: { fontSize: 18, fontWeight: "600" },
  search: { fontSize: 15 },
  // A max height keeps long directories from pushing past the sheet cap.
  sheetList: { height: 420 },
});
