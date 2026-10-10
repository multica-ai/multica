/**
 * HarmonyOS port of apps/mobile/components/project/pickers/project-lead-picker-body.tsx.
 * Pure picker body for project lead — single-select over members + agents
 * with an Unassigned row. On iOS the header + search bar are owned by the
 * native nav header (useNativeSearchBar); here the search input is a plain
 * TextField rendered by the owning BottomSheet
 * (pickers/project-picker-sheets.tsx) and passed in as `query`.
 *
 * Flat list with inline "Agent" right-aligned tag — matches Apple's
 * UITableViewCellStyleValue1 pattern (used throughout Settings); at this
 * row count (~10–30) inline tag beats SectionList headers (which would
 * eat ~8% of the sheet height).
 *
 * Actor avatars take `name`/`avatarUrl` explicitly on this side (the
 * harmony ActorAvatar has no built-in directory lookup), so the values come
 * straight off the member/agent rows.
 */
import { useMemo } from "react";
import { FlatList, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { Agent, MemberWithUser } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { Icon } from "@/components/ui/icon";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { agentListOptions } from "@/data/queries/agents";
import { memberListOptions } from "@/data/queries/members";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useScrollToTopOnChange } from "@/lib/use-scroll-to-top-on-change";
import { useThemeColors } from "@/lib/use-theme-colors";
import { withAlpha } from "@/lib/theme";

const AVATAR_SIZE = 36;

export interface LeadValue {
  type: "member" | "agent";
  id: string;
}

interface Props {
  value: LeadValue | null;
  query: string;
  onChange: (next: LeadValue | null) => void;
}

type Row =
  | { kind: "unassigned" }
  | { kind: "member"; member: MemberWithUser }
  | { kind: "agent"; agent: Agent };

function isRowSelected(value: LeadValue | null, row: Row): boolean {
  if (row.kind === "unassigned") return value === null;
  if (value === null) return false;
  if (row.kind === "member")
    return value.type === "member" && value.id === row.member.user_id;
  return value.type === "agent" && value.id === row.agent.id;
}

export function ProjectLeadPickerBody({ value, query, onChange }: Props) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const listRef = useScrollToTopOnChange(query);

  const rows = useMemo<Row[]>(() => {
    const q = query.trim().toLowerCase();
    const matchName = (n: string) => !q || n.toLowerCase().includes(q);

    const memberRows: Row[] = [...members]
      .filter((m) => matchName(m.name))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((m) => ({ kind: "member" as const, member: m }));
    const agentRows: Row[] = [...agents]
      .filter((a) => matchName(a.name))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((a) => ({ kind: "agent" as const, agent: a }));

    if (q) return [...memberRows, ...agentRows];

    const all = [...memberRows, ...agentRows];
    const selectedRow = all.find((r) => isRowSelected(value, r));
    return [
      { kind: "unassigned" },
      ...(selectedRow ? [selectedRow] : []),
      ...memberRows.filter((r) => !isRowSelected(value, r)),
      ...agentRows.filter((r) => !isRowSelected(value, r)),
    ];
  }, [members, agents, query, value]);

  const select = (row: Row) => {
    if (row.kind === "unassigned") onChange(null);
    else if (row.kind === "member")
      onChange({ type: "member", id: row.member.user_id });
    else onChange({ type: "agent", id: row.agent.id });
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
        return `a:${row.agent.id}`;
      }}
      renderItem={({ item }) => (
        <Pressable
          onPress={() => select(item)}
          style={({ pressed }) => [
            // flex-row items-center gap-3 px-4 py-3
            styles.row,
            pressed ? { backgroundColor: c.secondary } : null,
          ]}
        >
          {item.kind === "unassigned" ? (
            <View
              style={[
                styles.unassignedRing,
                {
                  width: AVATAR_SIZE,
                  height: AVATAR_SIZE,
                  // border-muted-foreground/40 — translucent theme shade.
                  borderColor: withAlpha(c.mutedForeground, 0.4),
                },
              ]}
            >
              <Icon
                name="close-circle-outline"
                size={20}
                color={MOBILE_PLACEHOLDER_COLOR}
              />
            </View>
          ) : item.kind === "member" ? (
            <ActorAvatar
              type="member"
              id={item.member.user_id}
              name={item.member.name}
              avatarUrl={item.member.avatar_url}
              size={AVATAR_SIZE}
            />
          ) : (
            <ActorAvatar
              type="agent"
              id={item.agent.id}
              name={item.agent.name}
              avatarUrl={item.agent.avatar_url}
              size={AVATAR_SIZE}
            />
          )}
          <Text style={[styles.rowLabel, { color: c.foreground }]} numberOfLines={1}>
            {item.kind === "unassigned"
              ? "Unassigned"
              : item.kind === "member"
                ? item.member.name
                : item.agent.name}
          </Text>
          {/* Inline type tag — Apple UITableViewCellStyleValue1. */}
          {item.kind === "agent" ? (
            <Text style={[styles.agentTag, { color: c.mutedForeground }]}>
              Agent
            </Text>
          ) : null}
          {isRowSelected(value, item) ? (
            <Icon name="checkmark" size={20} color={c.primary} />
          ) : null}
        </Pressable>
      )}
      ListEmptyComponent={
        <View style={styles.empty}>
          <Text style={[styles.emptyText, { color: c.mutedForeground }]}>
            {query
              ? "No matches."
              : "No members or agents in this workspace yet."}
          </Text>
        </View>
      }
    />
  );
}

// border-muted-foreground/40 — translucent theme shade (lib/theme withAlpha).
const styles = StyleSheet.create({
  // flex-1
  list: { flexGrow: 1 },
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  // rounded-full border border-dashed items-center justify-center
  unassignedRing: {
    borderRadius: AVATAR_SIZE / 2,
    borderWidth: 1,
    borderStyle: "dashed",
    alignItems: "center",
    justifyContent: "center",
  },
  // flex-1 text-base
  rowLabel: { flex: 1, fontSize: 16 },
  // text-sm
  agentTag: { fontSize: 14 },
  // px-3 py-8 items-center
  empty: { paddingHorizontal: 12, paddingVertical: 32, alignItems: "center" },
  // text-sm text-center
  emptyText: { fontSize: 14, textAlign: "center" },
});
