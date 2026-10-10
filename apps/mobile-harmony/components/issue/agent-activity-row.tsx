/**
 * HarmonyOS port of apps/mobile/components/issue/agent-activity-row.tsx —
 * double-state row that lives inside IssueHeaderCard. Opens the runs
 * surface — the Stack-header AgentHeaderBadge opens the same destination.
 *
 *   ≥1 active task        → [agent avatars] (pulse) Working           ›
 *   0 active, ≥1 past     → 🕓 Runs · N                                ›
 *   never run             → null (zero space)
 *
 * Navigation delta vs iOS: the push goes through the `onOpenRuns` callback
 * prop (components never import the app shell's route type — import-cycle
 * rule); the screen owns the destination.
 */
import React, { useMemo } from "react";
import { StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Icon } from "@/components/ui/icon";
import { AvatarStack, type StackActor } from "@/components/ui/avatar-stack";
import { PulseDot } from "@/components/ui/pulse-dot";
import {
  issueActiveTasksOptions,
  issueTasksOptions,
} from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  issueId: string;
  /** Opens the issue's runs surface (screen push or sheet — the host picks). */
  onOpenRuns: (issueId: string) => void;
}

export function AgentActivityRow({ issueId, onOpenRuns }: Props) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

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
      onPress={() => onOpenRuns(issueId)}
      accessibilityRole="button"
      accessibilityLabel={activeCount > 0 ? "Agent working — open runs" : "Open runs"}
      style={({ pressed }) => [
        // flex-row items-center gap-2 -mx-2 px-2 py-2 rounded-lg
        styles.row,
        pressed ? { backgroundColor: c.secondary } : null,
      ]}
    >
      {activeCount > 0 ? (
        <View style={styles.activeContent}>
          <AvatarStack
            actors={activeTasks.map<StackActor>((t) => ({
              type: "agent",
              id: t.agent_id,
            }))}
            max={3}
            size={24}
          />
          <PulseDot />
          <Text style={[styles.workingLabel, { color: c.foreground }]}>
            Working
          </Text>
        </View>
      ) : (
        <View style={styles.activeContent}>
          <Icon name="time-outline" size={16} color={c.mutedForeground} />
          <Text style={[styles.idleLabel, { color: c.foreground }]}>
            Runs · {pastCount}
          </Text>
        </View>
      )}
      <Icon name="chevron-forward" size={16} color={c.mutedForeground} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  // flex-row items-center gap-2 -mx-2 px-2 py-2 rounded-lg
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    marginHorizontal: -8,
    paddingHorizontal: 8,
    paddingVertical: 8,
    borderRadius: 8,
  },
  // flex-1 flex-row items-center gap-2
  activeContent: {
    flex: 1,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  // text-sm font-medium
  workingLabel: { fontSize: 14, fontWeight: "500" },
  // text-sm
  idleLabel: { fontSize: 14 },
});
