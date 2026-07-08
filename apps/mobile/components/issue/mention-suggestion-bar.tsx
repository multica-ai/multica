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
import { FlatList, Pressable, View } from "react-native";
import { useQueries, useQuery } from "@tanstack/react-query";
import type { Agent, Issue, MemberWithUser, Squad } from "@multica/core/types";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { StatusIcon } from "@/components/ui/status-icon";
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
import { cn } from "@/lib/utils";

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
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const isChat = mode === "chat";

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
    // mirrors web (packages/views/editor/extensions/mention-suggestion.tsx:418-424).
    // A private agent shown in the suggestion list would create a mention the
    // assignee can never act on; web hides them, mobile must too.
    const myRole =
      members.find((m) => m.user_id === userId)?.role ?? null;
    const matchedAgents = [...agents]
      .filter(
        (a) =>
          !a.archived_at &&
          (!q || a.name.toLowerCase().includes(q)) &&
          canAssignAgentToIssue(a, { userId, role: myRole }).allowed,
      )
      .sort((a, b) => a.name.localeCompare(b.name));
    // Archived squads are filtered out — matching web (mention-suggestion.tsx:428).
    // A re-activated squad re-appears on the next list refetch.
    const matchedSquads = [...squads]
      .filter(
        (s) => !s.archived_at && (!q || s.name.toLowerCase().includes(q)),
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
    // Plain View — wrapping in reanimated `Animated.View` with `className`
    // silently drops every NativeWind utility (no `cssInterop` registered in
    // this repo for Animated.View), and `FadeInDown` left the bar at
    // `opacity:0` when mounted inside `KeyboardStickyView`. Entrance
    // animation is desirable but needs the chat-composer pattern (bare
    // Animated.View outer + className on inner View) — single follow-up.
    <View
      className="bg-background border-b border-border"
      style={{ maxHeight: 220 }}
    >
      <FlatList
        data={rows}
        keyboardShouldPersistTaps="handled"
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
                className={cn(
                  "px-3 pt-2 pb-1",
                  index > 0 && "border-t border-border/60 mt-1",
                )}
              >
                <Text className="text-[10px] uppercase tracking-wider text-muted-foreground/80 font-medium">
                  {item.label}
                </Text>
              </View>
            );
          }
          if (item.kind === "empty") {
            return (
              <View className="px-3 py-3">
                <Text className="text-xs text-muted-foreground">
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
                className="flex-row items-center gap-3 px-3 py-2 active:bg-secondary"
              >
                <View className="size-7 rounded-full bg-brand/15 items-center justify-center">
                  <Text className="text-xs font-medium text-brand">@</Text>
                </View>
                <Text className="flex-1 text-sm text-foreground">
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
                className="flex-row items-center gap-3 px-3 py-2 active:bg-secondary"
              >
                <ActorAvatar
                  type="member"
                  id={item.member.user_id}
                  size={28}
                />
                <Text className="flex-1 text-sm text-foreground">
                  {item.member.name}
                </Text>
                <Badge label="Member" />
              </Pressable>
            );
          }
          if (item.kind === "agent") {
            return (
              <Pressable
                onPress={() =>
                  onSelect({
                    type: "agent",
                    id: item.agent.id,
                    name: item.agent.name,
                  })
                }
                className="flex-row items-center gap-3 px-3 py-2 active:bg-secondary"
              >
                <ActorAvatar type="agent" id={item.agent.id} size={28} showPresence />
                <Text className="flex-1 text-sm text-foreground">
                  {item.agent.name}
                </Text>
                <Badge label="Agent" tone="brand" />
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
                className="flex-row items-center gap-3 px-3 py-2 active:bg-secondary"
              >
                <ActorAvatar type="squad" id={item.squad.id} size={28} />
                <Text className="flex-1 text-sm text-foreground">
                  {item.squad.name}
                </Text>
                <Badge label="Squad" tone="outline" />
              </Pressable>
            );
          }
          // issue
          const closed =
            item.issue.status === "done" ||
            item.issue.status === "cancelled";
          return (
            <Pressable
              onPress={() =>
                onSelect({
                  type: "issue",
                  id: item.issue.id,
                  name: item.issue.identifier,
                })
              }
              className={cn(
                "flex-row items-center gap-3 px-3 py-2 active:bg-secondary",
                closed && "opacity-60",
              )}
            >
              <View className="size-7 items-center justify-center">
                <StatusIcon status={item.issue.status} size={16} />
              </View>
              <Text className="text-sm font-medium text-foreground">
                {item.issue.identifier}
              </Text>
              <Text
                className="flex-1 text-sm text-muted-foreground"
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

function Badge({
  label,
  tone = "muted",
}: {
  label: string;
  tone?: "muted" | "brand" | "outline";
}) {
  return (
    <View
      className={cn(
        "px-1.5 py-0.5 rounded",
        tone === "brand"
          ? "bg-brand/10"
          : tone === "outline"
            ? "border border-border"
            : "bg-secondary",
      )}
    >
      <Text
        className={cn(
          "text-[10px] uppercase tracking-wide",
          tone === "brand" ? "text-brand" : "text-muted-foreground",
        )}
      >
        {label}
      </Text>
    </View>
  );
}
