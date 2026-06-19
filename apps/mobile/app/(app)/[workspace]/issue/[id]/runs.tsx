/**
 * Agent Runs sheet — presented as a formSheet by the parent Stack. Two
 * sections: Active (queued/dispatched/running, created_at desc) and Past
 * (completed_at desc, status rank as tiebreaker). Empty
 * sections hide entirely.
 *
 * Both entry points (the in-card AgentActivityRow and the Stack-header
 * AgentHeaderBadge) now `router.push("/[workspace]/issue/[id]/runs")` —
 * the legacy `useRunsSheetStore` is gone since the route system is the
 * single source of truth for what's open.
 *
 * Past-row tap is a no-op in v1 — transcript drilldown is deferred.
 */
import { useMemo } from "react";
import { ScrollView, View } from "react-native";
import { useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type { AgentTask } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { RunRow } from "@/components/issue/run-row";
import {
  issueActiveTasksOptions,
  issueTasksOptions,
} from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";

const PAST_STATUS_ORDER: Record<AgentTask["status"], number> = {
  failed: 0,
  cancelled: 1,
  completed: 2,
  queued: 99,
  dispatched: 99,
  waiting_local_directory: 99,
  running: 99,
};

export default function IssueRunsRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: activeTasks = [] } = useQuery(
    issueActiveTasksOptions(wsId, id),
  );
  const { data: allTasks = [] } = useQuery(issueTasksOptions(wsId, id));

  const active = useMemo(
    () =>
      [...activeTasks].sort((a, b) =>
        (b.created_at ?? "").localeCompare(a.created_at ?? ""),
      ),
    [activeTasks],
  );

  const past = useMemo(() => {
    const filtered = allTasks.filter(
      (t) =>
        t.status === "completed" ||
        t.status === "failed" ||
        t.status === "cancelled",
    );
    return filtered.sort((a, b) => {
      const timeDiff = (b.completed_at ?? "").localeCompare(a.completed_at ?? "");
      if (timeDiff !== 0) return timeDiff;
      return PAST_STATUS_ORDER[a.status] - PAST_STATUS_ORDER[b.status];
    });
  }, [allTasks]);

  return (
    <View className="flex-1">
      <View className="px-4 pt-4 pb-3">
        <Text className="text-base font-semibold text-foreground">
          Agent Runs
        </Text>
      </View>
      <ScrollView showsVerticalScrollIndicator={false}>
        <View className="px-4 gap-3 pb-4">
          {active.length > 0 ? (
            <Section title="Active">
              {active.map((task) => (
                <RunRow key={task.id} task={task} issueId={id} />
              ))}
            </Section>
          ) : null}
          {past.length > 0 ? (
            <Section title="Past">
              {past.map((task) => (
                <RunRow key={task.id} task={task} issueId={id} />
              ))}
            </Section>
          ) : null}
        </View>
      </ScrollView>
    </View>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <View className="gap-1">
      <Text className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide">
        {title}
      </Text>
      <View>{children}</View>
    </View>
  );
}
