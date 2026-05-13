import { memo } from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TaskMessagePayload } from "@multica/core/types";
import { colors, radii, spacing } from "../../theme/tokens";

export const TaskMessageRow = memo(function TaskMessageRow({ message }: { message: TaskMessagePayload }) {
  const content =
    message.content ??
    message.output ??
    (message.input ? JSON.stringify(message.input) : "");

  return (
    <View style={styles.taskMessage}>
      <Text style={styles.taskMessageType}>
        #{message.seq} {message.type}{message.tool ? ` / ${message.tool}` : ""}
      </Text>
      {content ? <Text style={styles.timelineBody}>{content}</Text> : null}
    </View>
  );
});

const styles = StyleSheet.create({
  taskMessage: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    gap: spacing.xs,
    padding: spacing.sm,
  },
  taskMessageType: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  timelineBody: {
    color: colors.foreground,
    fontSize: 14,
    lineHeight: 20,
  },
});
