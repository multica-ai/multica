/**
 * HarmonyOS port of apps/mobile/components/issue/run-row.tsx — single row
 * inside the agent-runs screen. Same component for active and past tasks —
 * the trailing Cancel button is conditional on `status in {queued,
 * dispatched, running}`, and the status badge / colour swaps based on the
 * AgentTask.status enum.
 *
 * Tapping a past row is a no-op in v1 — the transcript-detail screen is
 * explicitly out of scope (mirrors the iOS deferral).
 */
import React from "react";
import { Alert, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { AgentTask } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { useCancelTask } from "@/data/mutations/issues";
import { useActorLookup } from "@/data/use-actor-name";
import { runFailureBadgeLabel } from "@/lib/run-failure-badge";
import { timeAgo } from "@/lib/time-ago";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  task: AgentTask;
  issueId: string;
}

const ACTIVE_STATUSES: readonly AgentTask["status"][] = [
  "queued",
  "dispatched",
  "running",
];

export function RunRow({ task, issueId }: Props) {
  const c = useThemeColors();
  const { getName } = useActorLookup();
  const isActive = ACTIVE_STATUSES.includes(task.status);
  const summary = task.trigger_summary?.trim() || fallbackSummary(task);
  // Past tasks use completed_at when present (server fills it for terminal
  // statuses); active tasks fall back to created_at so the user sees how
  // long it's been waiting.
  const timestamp = task.completed_at || task.created_at;

  return (
    <View style={styles.row}>
      <ActorAvatar type="agent" id={task.agent_id} size={28} />
      <View style={styles.main}>
        <Text style={[styles.summary, { color: c.foreground }]} numberOfLines={2}>
          <Text style={styles.summaryName}>{getName("agent", task.agent_id)}</Text>
          <Text style={{ color: c.mutedForeground }}> · {summary}</Text>
        </Text>
        <View style={styles.metaRow}>
          <StatusBadge task={task} />
          <Text style={[styles.timestamp, { color: c.mutedForeground }]}>
            {timestamp ? timeAgo(timestamp) : ""}
          </Text>
        </View>
      </View>
      {isActive ? <CancelButton taskId={task.id} issueId={issueId} /> : null}
    </View>
  );
}

function StatusBadge({ task }: { task: AgentTask }) {
  const c = useThemeColors();
  const label = STATUS_LABEL[task.status] ?? task.status;
  // iOS drew these with text-muted-foreground / text-brand /
  // text-destructive; resolve the same tokens from the active theme.
  const color =
    STATUS_COLOR[task.status] === "destructive"
      ? c.destructive
      : STATUS_COLOR[task.status] === "brand"
        ? c.brand
        : c.mutedForeground;
  // For failed tasks, surface the failure_reason inline so users don't have
  // to drill in. Missing / empty / unrecognised stays as just "Failed".
  if (task.status === "failed") {
    const reasonLabel = runFailureBadgeLabel(task.failure_reason);
    if (reasonLabel) {
      return (
        <Text style={[styles.badgeText, { color }]}>
          {label} · {reasonLabel}
        </Text>
      );
    }
  }
  return <Text style={[styles.badgeText, { color }]}>{label}</Text>;
}

function CancelButton({
  taskId,
  issueId,
}: {
  taskId: string;
  issueId: string;
}) {
  const c = useThemeColors();
  const mutation = useCancelTask(issueId);

  const onPress = () => {
    Alert.alert(
      "Cancel task?",
      "The agent will stop after the current step.",
      [
        { text: "Keep running", style: "cancel" },
        {
          text: "Cancel task",
          style: "destructive",
          onPress: () => mutation.mutate(taskId),
        },
      ],
    );
  };

  return (
    <Pressable
      onPress={onPress}
      disabled={mutation.isPending}
      accessibilityRole="button"
      accessibilityLabel="Cancel task"
      style={({ pressed }) => [
        // px-3 py-1.5 rounded-md bg-secondary
        styles.cancel,
        { backgroundColor: c.secondary },
        pressed ? { opacity: 0.7 } : null,
      ]}
    >
      <Text style={[styles.cancelLabel, { color: c.foreground }]}>Cancel</Text>
    </Pressable>
  );
}

function fallbackSummary(task: AgentTask): string {
  switch (task.kind) {
    case "comment":
      return "Comment task";
    case "autopilot":
      return "Autopilot run";
    case "chat":
      return "Chat task";
    case "quick_create":
      return "Quick create";
    case "direct":
    default:
      return "Task";
  }
}

const STATUS_LABEL: Record<AgentTask["status"], string> = {
  queued: "Queued",
  deferred: "Queued",
  dispatched: "Starting",
  waiting_local_directory: "Waiting for directory",
  running: "Running",
  completed: "Done",
  failed: "Failed",
  cancelled: "Cancelled",
};

// Token key per status — iOS used text-muted-foreground / text-brand /
// text-destructive; resolved against the active theme in StatusBadge.
const STATUS_COLOR: Record<AgentTask["status"], "muted" | "brand" | "destructive"> = {
  queued: "muted",
  deferred: "muted",
  dispatched: "brand",
  waiting_local_directory: "muted",
  running: "brand",
  completed: "muted",
  failed: "destructive",
  cancelled: "muted",
};

const styles = StyleSheet.create({
  // flex-row items-start gap-3 py-2
  row: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 12,
    paddingVertical: 8,
  },
  // flex-1 gap-1
  main: { flex: 1, gap: 4 },
  // text-sm
  summary: { fontSize: 14 },
  // font-medium
  summaryName: { fontWeight: "500" },
  // flex-row items-center gap-2
  metaRow: { flexDirection: "row", alignItems: "center", gap: 8 },
  // text-xs
  badgeText: { fontSize: 12 },
  // text-xs
  timestamp: { fontSize: 12 },
  // px-3 py-1.5 rounded-md
  cancel: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 6,
  },
  // text-xs font-medium
  cancelLabel: { fontSize: 12, fontWeight: "500" },
});
