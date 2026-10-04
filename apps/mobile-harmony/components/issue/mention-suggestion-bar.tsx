/**
 * Above-input suggestion bar for @mentions.
 *
 * Two modes — `comment` (the default, used in issue comment composer
 * and new-issue body) and `chat` (used in chat composer).
 *
 * `comment` sections:
 *   1. `@all` (single static row; visible when query matches "all"
 *      prefix or is empty)
 *   2. Members — sorted alphabetically
 *   3. Agents — sorted alphabetically
 *   4. Squads — sorted alphabetically (archived hidden). Selecting a squad
 *      emits `mention://squad/<uuid>`; backend wakes the squad's leader
 *      agent (server/internal/handler/comment.go:444).
 *
 * `chat` sections (chat is user ↔ single agent — `@member`/`@agent` are
 * noise; `@` here means "reference a resource for the agent"):
 *   1. Recent — issues the user opened most recently (from the in-memory
 *      viewed-issues store), max 5
 *   2. My issues — assigned-to-me, deduped against Recent, max 10
 *
 * Closed issues (status `done` / `cancelled`) are dimmed but selectable,
 * matching web's behaviour (mention-suggestion.tsx).
 */
import { useMemo } from "react";
import { FlatList, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQueries, useQuery } from "@tanstack/react-query";
import type { Agent, Issue, MemberWithUser, Squad } from "@multica/core/types";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { StatusIcon } from "@/components/ui/status-icon";
import {
  CLOSED_CATEGORIES,
  issueBehavesAsAny,
  issueColumnCategory,
} from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { memberListOptions } from "@/data/queries/members";
import { agentListOptions } from "@/data/queries/agents";
import { squadListOptions } from "@/data/queries/squads";
import { issueDetailOptions } from "@/data/queries/issues";
import { myIssueListOptions } from "@/data/queries/my-issues";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  selectViewedIssueIds,
  useViewedIssuesStore,
} from "@/data/viewed-issues-store";
import type { MentionMarker } from "@/lib/mention-serialize";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { isAgentRuntimeBound } from "@/lib/is-agent-runtime-bound";

type Mode = "comment" | "chat";

type Row =
  | { kind: "all" }
  | { kind: "section"; label: string }
  | { kind: "member"; member: MemberWithUser }
  | { kind: "agent"; agent: Agent }
  | { kind: "squad"; squad: Squad }
  | { kind: "issue"; issue: Issue }
  | { kind: "empty" };

interface Props {
  visible: boolean;
  query: string;
  onSelect: (mention: MentionMarker) => void;
  /** Default `"comment"` to preserve existing comment-composer and
   *  new-issue behaviour. `"chat"` switches the bar to issue mode. */
  mode?: Mode;
}

const RECENT_LIMIT = 5;
const MY_ISSUES_LIMIT = 10;

export function MentionSuggestionBar({
  visible,
  query,
  onSelect,
  mode = "comment",
}: Props) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const isChat = mode === "chat";
  // Rows are icon-only, so colour is the only thing that can carry a custom
  // status's identity here. (MUL-6243)
  const catalog = useIssueStatuses();

  // Comment-mode data — disabled in chat mode to avoid wasted fetches.
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !isChat && !!wsId,
  });
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled: !isChat && !!wsId,
  });
  const { data: squads = [] } = useQuery({
    ...squadListOptions(wsId),
    enabled: !isChat && !!wsId,
  });

  // Chat-mode data.
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const viewedIds = useViewedIssuesStore(selectViewedIssueIds(wsId));
  const recentIds = useMemo(
    () => viewedIds.slice(0, RECENT_LIMIT),
    [viewedIds],
  );
  const recentQueries = useQueries({
    queries: recentIds.map((id) => ({
      ...issueDetailOptions(wsId, id),
      enabled: isChat && !!wsId,
    })),
  });
  const recentIssues = useMemo<Issue[]>(
    () =>
      recentQueries
        .map((q) => q.data)
        .filter((i): i is Issue => !!i),
    [recentQueries],
  );

  const myFilter = useMemo(
    () => (userId ? { assignee_id: userId } : { assignee_id: "" }),
    [userId],
  );
  const { data: myIssuesAll = [] } = useQuery({
    ...myIssueListOptions(wsId, "assigned", myFilter),
    enabled: isChat && !!wsId && !!userId,
  });

  const rows = useMemo<Row[]>(() => {
    const q = query.trim().toLowerCase();

    if (isChat) {
      const issueMatches = (i: Issue) =>
        !q ||
        i.identifier.toLowerCase().includes(q) ||
        i.title.toLowerCase().includes(q);

      const matchedRecent = recentIssues.filter(issueMatches);
      const recentIdSet = new Set(matchedRecent.map((i) => i.id));
      const matchedMine = myIssuesAll
        .filter((i) => !recentIdSet.has(i.id) && issueMatches(i))
        .slice(0, MY_ISSUES_LIMIT);

      const out: Row[] = [];
      if (matchedRecent.length > 0) {
        out.push({ kind: "section", label: "Recent" });
        for (const i of matchedRecent) out.push({ kind: "issue", issue: i });
      }
      if (matchedMine.length > 0) {
        out.push({ kind: "section", label: "My issues" });
        for (const i of matchedMine) out.push({ kind: "issue", issue: i });
      }
      if (out.length === 0) out.push({ kind: "empty" });
      return out;
    }

    // Comment mode.
    const showAll = !q || "all".startsWith(q);
    const matchedMembers = [...members]
      .filter((m) => !q || m.name.toLowerCase().includes(q))
      .sort((a, b) => a.name.localeCompare(b.name));
    // Agents: filter archived + drop ones the current user can't assign —
    // mirrors web (packages/views/editor/extensions/mention-suggestion.tsx).
    // A private agent shown in the suggestion list would create a mention the
    // assignee can never act on; web hides them, mobile must too.
    const myRole =
      members.find((m) => m.user_id === userId)?.role ?? null;
    const runnableAgentIds = new Set(
      agents
        .filter(
          (agent) =>
            !agent.archived_at && isAgentRuntimeBound(agent),
        )
        .map((agent) => agent.id),
    );
    const matchedAgents = [...agents]
      .filter(
        (a) =>
          !a.archived_at &&
          isAgentRuntimeBound(a) &&
          (!q || a.name.toLowerCase().includes(q)) &&
          canAssignAgentToIssue(a, { userId, role: myRole }).allowed,
      )
      .sort((a, b) => a.name.localeCompare(b.name));
    // Archived squads are filtered out — matching web (mention-suggestion.tsx).
    // A re-activated squad re-appears on the next list refetch.
    const matchedSquads = [...squads]
      .filter(
        (s) =>
          !s.archived_at &&
          runnableAgentIds.has(s.leader_id) &&
          (!q || s.name.toLowerCase().includes(q)),
      )
      .sort((a, b) => a.name.localeCompare(b.name));

    const out: Row[] = [];
    if (showAll) out.push({ kind: "all" });
    if (matchedMembers.length > 0) {
      out.push({ kind: "section", label: "Members" });
      for (const m of matchedMembers) out.push({ kind: "member", member: m });
    }
    if (matchedAgents.length > 0) {
      out.push({ kind: "section", label: "Agents" });
      for (const a of matchedAgents) out.push({ kind: "agent", agent: a });
    }
    if (matchedSquads.length > 0) {
      out.push({ kind: "section", label: "Squads" });
      for (const s of matchedSquads) out.push({ kind: "squad", squad: s });
    }
    if (out.length === 0) out.push({ kind: "empty" });
    return out;
  }, [isChat, query, recentIssues, myIssuesAll, members, agents, squads, userId]);

  if (!visible) return null;

  return (
    // Plain View — the iOS comment explains why the entrance animation was
    // dropped (Animated.View + NativeWind interplay); this port has no
    // NativeWind at all, and animation parity is not worth the reanimated
    // dependency here. Behavior is identical: bar appears above the input.
    <View
      style={[
        styles.bar,
        { backgroundColor: c.background, borderBottomColor: c.border },
      ]}
    >
      <FlatList
        data={rows}
        keyboardShouldPersistTaps="handled"
        style={styles.list}
        keyExtractor={(row, i) =>
          row.kind === "all"
            ? "row:all"
            : row.kind === "section"
              ? `row:section:${row.label}`
              : row.kind === "member"
                ? `row:m:${row.member.user_id}`
                : row.kind === "agent"
                  ? `row:a:${row.agent.id}`
                  : row.kind === "issue"
                    ? `row:i:${row.issue.id}`
                    : `row:empty:${i}`
        }
        renderItem={({ item, index }) => {
          if (item.kind === "section") {
            return (
              <View
                style={[
                  styles.sectionRow,
                  index > 0 && {
                    borderTopWidth: 1,
                    borderTopColor: withAlpha(c.border, 0.6),
                    marginTop: 4,
                  },
                ]}
              >
                <Text
                  style={[
                    styles.sectionLabel,
                    { color: withAlpha(c.mutedForeground, 0.8) },
                  ]}
                >
                  {item.label}
                </Text>
              </View>
            );
          }
          if (item.kind === "empty") {
            return (
              <View style={styles.emptyRow}>
                <Text style={[styles.emptyLabel, { color: c.mutedForeground }]}>
                  No matches.
                </Text>
              </View>
            );
          }
          if (item.kind === "all") {
            return (
              <Pressable
                onPress={() =>
                  onSelect({ type: "all", id: "all", name: "all" })
                }
                style={({ pressed }) => [
                  styles.row,
                  pressed && { backgroundColor: c.secondary },
                ]}
              >
                <View
                  style={[
                    styles.allAvatar,
                    { backgroundColor: withAlpha(c.brand, 0.15) },
                  ]}
                >
                  <Text style={[styles.allGlyph, { color: c.brand }]}>@</Text>
                </View>
                <Text style={[styles.name, { color: c.foreground }]}>
                  Everyone
                </Text>
                <Badge label="All" />
              </Pressable>
            );
          }
          if (item.kind === "member") {
            return (
              <Pressable
                onPress={() =>
                  onSelect({
                    type: "member",
                    id: item.member.user_id,
                    name: item.member.name,
                  })
                }
                style={({ pressed }) => [
                  styles.row,
                  pressed && { backgroundColor: c.secondary },
                ]}
              >
                <ActorAvatar
                  type="member"
                  id={item.member.user_id}
                  name={item.member.name}
                  avatarUrl={item.member.avatar_url ?? null}
                  size={28}
                />
                <Text style={[styles.name, { color: c.foreground }]}>
                  {item.member.name}
                </Text>
                <Badge label="Member" />
              </Pressable>
            );
          }
          if (item.kind === "agent") {
            const runtimeBound = isAgentRuntimeBound(item.agent);
            return (
              <Pressable
                disabled={!runtimeBound}
                onPress={() =>
                  onSelect({
                    type: "agent",
                    id: item.agent.id,
                    name: item.agent.name,
                  })
                }
                style={({ pressed }) => [
                  styles.row,
                  pressed && runtimeBound && { backgroundColor: c.secondary },
                  !runtimeBound && styles.rowDimmed,
                ]}
              >
                <ActorAvatar
                  type="agent"
                  id={item.agent.id}
                  name={item.agent.name}
                  avatarUrl={item.agent.avatar_url}
                  size={28}
                />
                <Text style={[styles.name, { color: c.foreground }]}>
                  {item.agent.name}
                </Text>
                <Badge
                  label={runtimeBound ? "Agent" : "Needs runtime"}
                  tone={runtimeBound ? "brand" : "outline"}
                />
              </Pressable>
            );
          }
          if (item.kind === "squad") {
            return (
              <Pressable
                onPress={() =>
                  onSelect({
                    type: "squad",
                    id: item.squad.id,
                    name: item.squad.name,
                  })
                }
                style={({ pressed }) => [
                  styles.row,
                  pressed && { backgroundColor: c.secondary },
                ]}
              >
                <ActorAvatar
                  type="squad"
                  id={item.squad.id}
                  name={item.squad.name}
                  avatarUrl={item.squad.avatar_url}
                  size={28}
                />
                <Text style={[styles.name, { color: c.foreground }]}>
                  {item.squad.name}
                </Text>
                <Badge label="Squad" tone="outline" />
              </Pressable>
            );
          }
          // issue
          // By CATEGORY, not by key: a custom status in the completed category is
          // done, and `status === "done"` silently disagrees — the row would
          // render at full opacity as though the work were still open.
          // (MUL-6243)
          const closed = issueBehavesAsAny(item.issue, CLOSED_CATEGORIES);
          return (
            <Pressable
              onPress={() =>
                onSelect({
                  type: "issue",
                  id: item.issue.id,
                  name: item.issue.identifier,
                })
              }
              style={({ pressed }) => [
                styles.row,
                pressed && { backgroundColor: c.secondary },
                closed && styles.rowDimmed,
              ]}
            >
              <View style={styles.issueIconBox}>
                <StatusIcon
                  status={item.issue.status}
                  category={issueColumnCategory(item.issue)}
                  icon={catalog.iconOf(item.issue.status)}
                  color={catalog.colorOf(item.issue.status)}
                  size={16}
                />
              </View>
              <Text style={[styles.issueIdentifier, { color: c.foreground }]}>
                {item.issue.identifier}
              </Text>
              <Text
                style={[styles.issueTitle, { color: c.mutedForeground }]}
                numberOfLines={1}
              >
                {item.issue.title}
              </Text>
            </Pressable>
          );
        }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  // bg-background border-b border-border + maxHeight 220
  bar: { borderBottomWidth: 1, maxHeight: 220 },
  list: { flexGrow: 0 },
  // px-3 pt-2 pb-1
  sectionRow: { paddingHorizontal: 12, paddingTop: 8, paddingBottom: 4 },
  // text-[10px] uppercase tracking-wider font-medium
  sectionLabel: {
    fontSize: 10,
    textTransform: "uppercase",
    letterSpacing: 0.5,
    fontWeight: "500",
  },
  // px-3 py-3
  emptyRow: { paddingHorizontal: 12, paddingVertical: 12 },
  // text-xs
  emptyLabel: { fontSize: 12 },
  // flex-row items-center gap-3 px-3 py-2
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  // opacity-50 / opacity-60 dimmed rows
  rowDimmed: { opacity: 0.6 },
  // size-7 rounded-full bg-brand/15 center
  allAvatar: {
    width: 28,
    height: 28,
    borderRadius: 14,
    alignItems: "center",
    justifyContent: "center",
  },
  // text-xs font-medium
  allGlyph: { fontSize: 12, fontWeight: "500" },
  // flex-1 text-sm
  name: { flex: 1, fontSize: 14 },
  // size-7 center (issue icon box)
  issueIconBox: {
    width: 28,
    height: 28,
    alignItems: "center",
    justifyContent: "center",
  },
  // text-sm font-medium
  issueIdentifier: { fontSize: 14, fontWeight: "500" },
  // flex-1 text-sm
  issueTitle: { flex: 1, fontSize: 14 },
  // px-1.5 py-0.5 rounded-xs
  badge: {
    paddingHorizontal: 6,
    paddingVertical: 2,
    borderRadius: 2,
  },
  // text-[10px] uppercase tracking-wide
  badgeLabel: {
    fontSize: 10,
    textTransform: "uppercase",
    letterSpacing: 0.25,
  },
});

function Badge({
  label,
  tone = "muted",
}: {
  label: string;
  tone?: "muted" | "brand" | "outline";
}) {
  const c = useThemeColors();
  return (
    <View
      style={[
        // px-1.5 py-0.5 rounded-xs
        styles.badge,
        tone === "brand" && { backgroundColor: withAlpha(c.brand, 0.1) },
        tone === "outline" && {
          borderWidth: 1,
          borderColor: c.border,
        },
        tone === "muted" && { backgroundColor: c.secondary },
      ]}
    >
      <Text
        style={[
          // text-[10px] uppercase tracking-wide
          styles.badgeLabel,
          { color: tone === "brand" ? c.brand : c.mutedForeground },
        ]}
      >
        {label}
      </Text>
    </View>
  );
}
