/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/issue/[id]/runs.tsx —
 * Agent Runs screen. Two sections: Active (queued/dispatched/running,
 * created_at desc) and Past (completed_at desc, status rank as tiebreaker).
 * Empty sections hide entirely.
 *
 * Navigation delta vs iOS: the iOS route was a formSheet pushed by the
 * in-card AgentActivityRow and the Stack-header AgentHeaderBadge; here it
 * is a pushed screen in the hand-rolled stack (the route registry maps an
 * `issue-runs` route to this component), so it renders its own ScreenHeader
 * and pops itself.
 *
 * Past-row tap is a no-op in v1 — transcript drilldown is deferred
 * (mirrors the iOS deferral).
 */
import React, { useMemo } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type { AgentTask } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { RunRow } from "@/components/issue/run-row";
import { ScreenHeader } from "./screen-header";
import { useNav } from "@/src/navigation/navigator";
import {
  issueActiveTasksOptions,
  issueTasksOptions,
} from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useThemeColors } from "@/lib/use-theme-colors";

const PAST_STATUS_ORDER: Record<AgentTask["status"], number> = {
  failed: 0,
  cancelled: 1,
  completed: 2,
  queued: 99,
  deferred: 99,
  dispatched: 99,
  waiting_local_directory: 99,
  running: 99,
};

/** Local stack-nav view — pop is all this screen needs from the navigator. */
type RunsNav = { pop: () => void };

export function IssueRunsScreen({ issueId }: { issueId: string }) {
  const c = useThemeColors();
  const nav = useNav<RunsNav>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: activeTasks = [] } = useQuery(
    issueActiveTasksOptions(wsId, issueId),
  );
  const { data: allTasks = [] } = useQuery(issueTasksOptions(wsId, issueId));

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
    <View style={[styles.screen, { backgroundColor: c.background }]}>
      <ScreenHeader
        title="Agent Runs"
        onBack={() => nav.pop()}
      />
      <ScrollView showsVerticalScrollIndicator={false}>
        <View style={styles.content}>
          {active.length > 0 ? (
            <Section title="Active">
              {active.map((task) => (
                <RunRow key={task.id} task={task} issueId={issueId} />
              ))}
            </Section>
          ) : null}
          {past.length > 0 ? (
            <Section title="Past">
              {past.map((task) => (
                <RunRow key={task.id} task={task} issueId={issueId} />
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
  const c = useThemeColors();
  return (
    <View style={styles.section}>
      <Text style={[styles.sectionTitle, { color: c.mutedForeground }]}>
        {title}
      </Text>
      <View>{children}</View>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  // px-4 gap-3 pb-4
  content: { paddingHorizontal: 16, paddingTop: 16, gap: 12, paddingBottom: 16 },
  // gap-1
  section: { gap: 4 },
  // text-[11px] font-medium uppercase tracking-wide
  sectionTitle: {
    fontSize: 11,
    fontWeight: "500",
    textTransform: "uppercase",
    letterSpacing: 0.4,
  },
});
