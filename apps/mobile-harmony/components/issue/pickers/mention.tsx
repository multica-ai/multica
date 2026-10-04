/**
 * Pure picker body for the comment composer's @mention chips, plus the
 * `MentionPickerSheet` that hosts it.
 *
 * Mirrors `LabelPickerBody`: multi-select with tap-to-toggle, sheet stays
 * open across toggles, user dismisses via grabber drag-down, backdrop tap
 * or Back. The composer's chip row reflects the store live (sheet is
 * presented over the composer; the row is partly visible behind the sheet).
 *
 * Sections (alphabetical within each):
 *   1. `@all` (pinned top, filtered by query)
 *   2. People
 *   3. Agents
 *   4. Squads (archived hidden)
 *   5. Issues (server-side `api.searchIssues`, debounced 200ms; empty
 *      query → no issues section, matching web's mention-suggestion.tsx)
 *
 * The iOS app hosted this body in a formSheet route driven by the native
 * UISearchController (`useNativeSearchBar`). This platform has no formSheet
 * routes; `MentionPickerSheet` mounts the body inside the hand-rolled
 * `BottomSheet` with a plain search `TextField` on top — the seam the
 * composer and any future picker entry point attach to.
 */
import { useEffect, useMemo, useState } from "react";
import { FlatList, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type {
  Agent,
  Issue,
  MemberWithUser,
  Squad,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { Icon } from "@/components/ui/icon";
import { StatusIcon } from "@/components/ui/status-icon";
import { TextField } from "@/components/ui/text-field";
import { issueColumnCategory } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { memberListOptions } from "@/data/queries/members";
import { agentListOptions } from "@/data/queries/agents";
import { squadListOptions } from "@/data/queries/squads";
import { api } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  useMentionDraftStore,
  type MentionChipDraft,
  type MentionTargetType,
} from "@/data/stores/mention-draft-store";
import { useScrollToTopOnChange } from "@/lib/use-scroll-to-top-on-change";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { isAgentRuntimeBound } from "@/lib/is-agent-runtime-bound";
import { BottomSheet } from "@/src/navigation/bottom-sheet";

const AVATAR_SIZE = 36;

type Row =
  | { kind: "section"; label: string }
  | { kind: "all" }
  | { kind: "member"; member: MemberWithUser }
  | { kind: "agent"; agent: Agent }
  | { kind: "squad"; squad: Squad }
  | { kind: "issue"; issue: Issue };

interface BodyProps {
  query: string;
  /** "comment" (default) renders @all + People + Agents + Squads + Issues.
   *  "chat" hides the people-style sections (member / agent / squad /
   *  @all) because chat is user ↔ single agent — mentioning a person
   *  there generates unintended notifications. Only Issues remain useful
   *  in chat as "reference this ticket for context". */
  mode?: "comment" | "chat";
}

export function MentionPickerBody({ query, mode = "comment" }: BodyProps) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  // Rows are icon-only here too — colour is what names a custom status.
  const catalog = useIssueStatuses();
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
  const checkColor = c.primary;

  const selected = useMentionDraftStore((s) => s.mentions);
  const toggle = useMentionDraftStore((s) => s.toggle);

  // Server-side issue search (mirrors web's mention-suggestion.tsx). Empty
  // query → no fetch + no issues section. Debounced 200ms; in-flight
  // cancelled on every keystroke via AbortController.
  const [issueResults, setIssueResults] = useState<Issue[]>([]);
  useEffect(() => {
    const trimmed = query.trim();
    if (!trimmed) {
      setIssueResults([]);
      return;
    }
    const ac = new AbortController();
    const timer = setTimeout(() => {
      void api
        .searchIssues(
          { q: trimmed, limit: 8, include_closed: false },
          { signal: ac.signal },
        )
        .then((res) => setIssueResults(res.issues))
        .catch(() => setIssueResults([]));
    }, 200);
    return () => {
      ac.abort();
      clearTimeout(timer);
    };
  }, [query]);

  const isSelectedKey = (type: MentionTargetType, id: string) =>
    selected.some((m) => m.type === type && m.id === id);

  const isSelected = (row: Row): boolean => {
    if (row.kind === "section") return false;
    if (row.kind === "all") return isSelectedKey("all", "all");
    if (row.kind === "member")
      return isSelectedKey("member", row.member.user_id);
    if (row.kind === "agent") return isSelectedKey("agent", row.agent.id);
    if (row.kind === "squad") return isSelectedKey("squad", row.squad.id);
    return isSelectedKey("issue", row.issue.id);
  };

  const rows = useMemo<Row[]>(() => {
    const q = query.trim().toLowerCase();
    const matchName = (name: string) => !q || name.toLowerCase().includes(q);

    const out: Row[] = [];

    // People-style sections only render in comment mode. Chat is single-
    // agent; @张三 / @squad / @all there are noise + notify the wrong
    // people. The Issues section IS useful in chat ("reference ticket
    // for context"), so it stays for both modes.
    if (mode === "comment") {
      if (!q || "all".includes(q)) {
        out.push({ kind: "all" });
      }
      const memberRows = [...members]
        .filter((m) => matchName(m.name))
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((m): Row => ({ kind: "member", member: m }));
      if (memberRows.length > 0) {
        out.push({ kind: "section", label: "People" }, ...memberRows);
      }
      const agentRows = [...agents]
        .filter((a) => matchName(a.name))
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((a): Row => ({ kind: "agent", agent: a }));
      if (agentRows.length > 0) {
        out.push({ kind: "section", label: "Agents" }, ...agentRows);
      }
      const squadRows = [...squads]
        .filter((s) => !s.archived_at && matchName(s.name))
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((s): Row => ({ kind: "squad", squad: s }));
      if (squadRows.length > 0) {
        out.push({ kind: "section", label: "Squads" }, ...squadRows);
      }
    }

    if (issueResults.length > 0) {
      out.push({ kind: "section", label: "Issues" });
      for (const i of issueResults) {
        out.push({ kind: "issue", issue: i });
      }
    }
    return out;
  }, [mode, members, agents, squads, issueResults, query]);

  const pick = (row: Row) => {
    let chip: MentionChipDraft | null = null;
    if (row.kind === "all") chip = { type: "all", id: "all", name: "all" };
    else if (row.kind === "member")
      chip = {
        type: "member",
        id: row.member.user_id,
        name: row.member.name,
      };
    else if (row.kind === "agent")
      chip = { type: "agent", id: row.agent.id, name: row.agent.name };
    else if (row.kind === "squad")
      chip = { type: "squad", id: row.squad.id, name: row.squad.name };
    else if (row.kind === "issue")
      chip = {
        type: "issue",
        id: row.issue.id,
        name: row.issue.identifier,
      };
    if (chip) toggle(chip);
  };

  return (
    <FlatList
      ref={listRef}
      data={rows}
      style={styles.list}
      keyboardShouldPersistTaps="handled"
      keyExtractor={(row, idx) => {
        if (row.kind === "section") return `section:${row.label}:${idx}`;
        if (row.kind === "all") return "all";
        if (row.kind === "member") return `m:${row.member.user_id}`;
        if (row.kind === "agent") return `a:${row.agent.id}`;
        if (row.kind === "squad") return `s:${row.squad.id}`;
        return `i:${row.issue.id}`;
      }}
      renderItem={({ item }) => {
        if (item.kind === "section") {
          return (
            <View style={styles.sectionRow}>
              <Text style={[styles.sectionLabel, { color: c.mutedForeground }]}>
                {item.label}
              </Text>
            </View>
          );
        }
        const needsRuntime =
          (item.kind === "agent" && !isAgentRuntimeBound(item.agent)) ||
          (item.kind === "squad" &&
            !runnableAgentIds.has(item.squad.leader_id));
        return (
          <Pressable
            disabled={needsRuntime}
            onPress={() => pick(item)}
            style={({ pressed }) => [
              styles.row,
              pressed && { backgroundColor: c.secondary },
              needsRuntime && styles.rowDisabled,
            ]}
          >
            {item.kind === "all" ? (
              <View
                style={[
                  styles.avatarBox,
                  { backgroundColor: withAlpha(c.primary, 0.1) },
                ]}
              >
                <Icon name="people" size={20} color={checkColor} />
              </View>
            ) : item.kind === "member" ? (
              <ActorAvatar
                type="member"
                id={item.member.user_id}
                name={item.member.name}
                size={AVATAR_SIZE}
              />
            ) : item.kind === "agent" ? (
              <ActorAvatar
                type="agent"
                id={item.agent.id}
                name={item.agent.name}
                avatarUrl={item.agent.avatar_url ?? null}
                size={AVATAR_SIZE}
              />
            ) : item.kind === "squad" ? (
              <ActorAvatar
                type="squad"
                id={item.squad.id}
                name={item.squad.name}
                avatarUrl={item.squad.avatar_url ?? null}
                size={AVATAR_SIZE}
              />
            ) : (
              <View style={styles.avatarBox}>
                <StatusIcon
                  status={item.issue.status}
                  category={issueColumnCategory(item.issue)}
                  icon={catalog.iconOf(item.issue.status)}
                  color={catalog.colorOf(item.issue.status)}
                  size={22}
                />
              </View>
            )}
            {item.kind === "issue" ? (
              <View style={styles.issueLine}>
                <Text
                  style={[styles.issueIdentifier, { color: c.mutedForeground }]}
                >
                  {item.issue.identifier}
                </Text>
                <Text
                  style={[styles.issueTitle, { color: c.foreground }]}
                  numberOfLines={1}
                >
                  {item.issue.title}
                </Text>
              </View>
            ) : (
              <Text style={[styles.name, { color: c.foreground }]}>
                {item.kind === "all"
                  ? "Everyone (@all)"
                  : item.kind === "member"
                    ? item.member.name
                    : item.kind === "agent"
                      ? item.agent.name
                      : item.squad.name}
              </Text>
            )}
            {item.kind === "agent" ? (
              <Text style={[styles.rowMeta, { color: c.mutedForeground }]}>
                {isAgentRuntimeBound(item.agent) ? "Agent" : "Needs runtime"}
              </Text>
            ) : item.kind === "squad" ? (
              <Text style={[styles.rowMeta, { color: c.mutedForeground }]}>
                {needsRuntime ? "Leader needs runtime" : "Squad"}
              </Text>
            ) : null}
            {isSelected(item) ? (
              <Icon name="checkmark" size={20} color={checkColor} />
            ) : null}
          </Pressable>
        );
      }}
      ListEmptyComponent={
        <View style={styles.emptyWrap}>
          <Text style={[styles.emptyLabel, { color: c.mutedForeground }]}>
            No matches.
          </Text>
        </View>
      }
    />
  );
}

/**
 * Bottom-sheet host for {@link MentionPickerBody} — this platform's
 * replacement for the iOS `mention-picker` formSheet route. Stands in for
 * `useNativeSearchBar` with a plain autofocus `TextField`; selections flow
 * through `useMentionDraftStore`, identical to the iOS route.
 */
export function MentionPickerSheet({
  visible,
  onClose,
  mode = "comment",
}: {
  visible: boolean;
  onClose: () => void;
  mode?: "comment" | "chat";
}) {
  const [query, setQuery] = useState("");
  const placeholder =
    mode === "chat" ? "Reference an issue" : "Search people or issues";

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View style={styles.sheetContent}>
        <TextField
          value={query}
          onChangeText={setQuery}
          placeholder={placeholder}
          returnKeyType="search"
          autoFocus
          style={styles.search}
        />
        <MentionPickerBody query={query} mode={mode} />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  // flex-1 — bounded by the sheet's fixed-height content on this platform
  // (iOS bounded it via the formSheet route itself)
  list: { flex: 1 },
  // sheet: search field + bounded list area
  sheetContent: { height: 460 },
  search: { marginHorizontal: 16, marginTop: 8 },
  // px-4 pt-4 pb-1
  sectionRow: { paddingHorizontal: 16, paddingTop: 16, paddingBottom: 4 },
  // text-[11px] font-medium uppercase tracking-wide
  sectionLabel: {
    fontSize: 11,
    fontWeight: "500",
    textTransform: "uppercase",
    letterSpacing: 0.3,
  },
  // flex-row items-center gap-3 px-4 py-3
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  // needs-runtime rows dim to 50%
  rowDisabled: { opacity: 0.5 },
  // rounded-full bg-primary/10 size-9 center
  avatarBox: {
    width: AVATAR_SIZE,
    height: AVATAR_SIZE,
    borderRadius: AVATAR_SIZE / 2,
    alignItems: "center",
    justifyContent: "center",
  },
  // flex-1 flex-row items-center gap-2
  issueLine: { flex: 1, flexDirection: "row", alignItems: "center", gap: 8 },
  // text-sm font-medium text-muted-foreground
  issueIdentifier: { fontSize: 14, fontWeight: "500" },
  // flex-1 text-base
  issueTitle: { flex: 1, fontSize: 16 },
  // flex-1 text-base
  name: { flex: 1, fontSize: 16 },
  // text-sm text-muted-foreground
  rowMeta: { fontSize: 14 },
  // px-3 py-8 items-center
  emptyWrap: { paddingHorizontal: 12, paddingVertical: 32, alignItems: "center" },
  // text-sm
  emptyLabel: { fontSize: 14 },
});
