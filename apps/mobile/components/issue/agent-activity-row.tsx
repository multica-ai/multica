/**
 * Double-state row that lives inside `IssueHeaderCard`. Dispatches to
 * `useRunsSheetStore` to open the page-level `<RunsSheet>`.
 *
 *   ≥1 active task        → [agent avatars] (pulse) Working           ›
 *   0 active, ≥1 past     → 🕓 Runs · N                                ›
 *   never run             → null (zero space)
 *
 * This row is the "discovery" surface (visible only when timeline isn't
 * scrolled). The Stack-header `<AgentHeaderBadge>` is the "ambient"
 * surface (always visible during active tasks). Both open the same
 * `<RunsSheet>` instance via the shared store — see plan
 * /Users/qingnaiyuan/.claude/plans/ok-plan-linked-taco.md.
 */
import { useMemo } from "react";
import { Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import { Text } from "@/components/ui/text";
import { AvatarStack, type StackActor } from "@/components/ui/avatar-stack";
import { PulseDot } from "@/components/ui/pulse-dot";
import {
  issueActiveTasksOptions,
  issueTasksOptions,
} from "@/data/queries/issues";
import { useRunsSheetStore } from "@/data/runs-sheet-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

interface Props {
  issueId: string;
}

export function AgentActivityRow({ issueId }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const open = useRunsSheetStore((s) => s.open);
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const { data: activeTasks = [] } = useQuery(
    issueActiveTasksOptions(wsId, issueId),
  );
  const { data: allTasks = [] } = useQuery(issueTasksOptions(wsId, issueId));

  const activeCount = activeTasks.length;
  // "Past" = tasks not currently active. The /task-runs endpoint returns the
  // full list, so we filter rather than fetching a separate past-only query.
  const pastCount = useMemo(
    () =>
      allTasks.filter(
        (t) =>
          t.status === "completed" ||
          t.status === "failed" ||
          t.status === "cancelled",
      ).length,
    [allTasks],
  );

  if (activeCount === 0 && pastCount === 0) {
    return null;
  }

  return (
    <Pressable
      onPress={() => open(issueId)}
      className="flex-row items-center gap-2 -mx-2 px-2 py-2 rounded-lg active:bg-secondary"
    >
      {activeCount > 0 ? (
        <ActiveContent
          actors={activeTasks.map<StackActor>((t) => ({
            type: "agent",
            id: t.agent_id,
          }))}
        />
      ) : (
        <IdleContent count={pastCount} mutedFg={mutedFg} />
      )}
      <Ionicons name="chevron-forward" size={16} color={mutedFg} />
    </Pressable>
  );
}

function ActiveContent({ actors }: { actors: StackActor[] }) {
  return (
    <View className="flex-1 flex-row items-center gap-2">
      <AvatarStack actors={actors} max={3} size={24} />
      <PulseDot />
      <Text className="text-sm font-medium text-foreground">Working</Text>
    </View>
  );
}

function IdleContent({ count, mutedFg }: { count: number; mutedFg: string }) {
  return (
    <View className="flex-1 flex-row items-center gap-2">
      <Ionicons name="time-outline" size={16} color={mutedFg} />
      <Text className="text-sm text-foreground">Runs · {count}</Text>
    </View>
  );
}
