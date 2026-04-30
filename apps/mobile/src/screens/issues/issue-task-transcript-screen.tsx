import { useMemo } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { useIssueTaskRuns, useTaskMessages } from "@multica/core/issues/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";
import { TaskMessageRow } from "./task-transcript-components";

type Props = NativeStackScreenProps<RootStackParamList, "IssueTaskTranscript">;

export function IssueTaskTranscriptScreen({ navigation, route }: Props) {
  const { issueId, taskId } = route.params;
  const { workspace } = useMobileWorkspace();
  const { getActorName } = useActorName();
  const { data: taskRuns = [], isLoading: taskRunsLoading } = useIssueTaskRuns(workspace.id, issueId);
  const { data: messages = [], isError, isLoading } = useTaskMessages(workspace.id, taskId);

  const task = useMemo(
    () => taskRuns.find((candidate) => candidate.id === taskId),
    [taskId, taskRuns],
  );

  if (taskRunsLoading || isLoading) return <LoadingState />;
  if (isError) return <EmptyState title="Unable to load transcript" />;

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title={`Run ${taskId.slice(0, 8)}`} />
      <ScrollView contentContainerStyle={styles.content}>
        <View style={styles.summaryCard}>
          <View style={styles.summaryHeader}>
            <View style={styles.summaryTitleGroup}>
              <Text style={styles.summaryTitle}>Run {taskId.slice(0, 8)}</Text>
              {task ? (
                <Text style={styles.summaryMeta}>
                  {getActorName("agent", task.agent_id)}
                </Text>
              ) : null}
            </View>
            <Text style={styles.status}>{task?.status ?? "unknown"}</Text>
          </View>
          {task?.error ? <Text style={styles.errorText}>{task.error}</Text> : null}
        </View>

        {messages.length === 0 ? (
          <Text style={styles.emptyText}>No transcript messages</Text>
        ) : (
          messages.map((message) => (
            <TaskMessageRow key={`${taskId}-${message.seq}`} message={message} />
          ))
        )}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  content: {
    gap: spacing.md,
    padding: spacing.lg,
    paddingBottom: 48,
  },
  summaryCard: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    padding: spacing.md,
  },
  summaryHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  summaryTitleGroup: {
    flex: 1,
    gap: spacing.xs,
  },
  summaryTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "700",
  },
  summaryMeta: {
    color: colors.mutedForeground,
    fontSize: 13,
  },
  status: {
    color: colors.mutedForeground,
    fontSize: 13,
    fontWeight: "600",
  },
  emptyText: {
    color: colors.mutedForeground,
    fontSize: 14,
  },
  errorText: {
    color: colors.destructive,
    fontSize: 14,
    lineHeight: 20,
  },
});
