import { View, Text, StyleSheet } from "react-native";
import type { IssueStatus } from "@multica/core/types";
import { STATUS_CONFIG } from "@multica/core/issues/config/status";
import { StatusIcon } from "./StatusIcon";
import { colors } from "@/lib/theme";

export function SectionHeader({
  status,
  count,
}: {
  status: IssueStatus;
  count: number;
}) {
  const config = STATUS_CONFIG[status];

  return (
    <View style={styles.container}>
      <StatusIcon status={status} size={14} />
      <Text style={styles.label}>{config.label}</Text>
      <Text style={styles.count}>{count}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 16,
    paddingVertical: 10,
    backgroundColor: colors.background,
    gap: 8,
  },
  label: {
    fontSize: 14,
    fontWeight: "600",
    color: colors.foreground,
  },
  count: {
    fontSize: 13,
    color: colors.mutedForeground,
  },
});
