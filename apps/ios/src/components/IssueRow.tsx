import { View, Text, TouchableOpacity, StyleSheet } from "react-native";
import { useRouter } from "expo-router";
import type { Issue } from "@multica/core/types";
import { StatusIcon } from "./StatusIcon";
import { PriorityIcon } from "./PriorityIcon";
import { colors } from "@/lib/theme";

export function IssueRow({
  issue,
  assigneeName,
  projectTitle,
  returnTo,
  returnLabel,
}: {
  issue: Issue;
  assigneeName?: string;
  projectTitle?: string;
  returnTo?: string;
  returnLabel?: string;
}) {
  const router = useRouter();
  const metaParts = [
    assigneeName ? `${issue.assignee_type === "agent" ? "Agent" : "Assignee"}: ${assigneeName}` : null,
    projectTitle ? `Project: ${projectTitle}` : null,
  ].filter(Boolean);

  return (
    <TouchableOpacity
      style={styles.container}
      onPress={() =>
        router.push({
          pathname: "/(app)/(issues)/[id]",
          params: {
            id: issue.id,
            ...(returnTo ? { returnTo } : {}),
            ...(returnLabel ? { returnLabel } : {}),
          },
        })
      }
      activeOpacity={0.6}
    >
      <StatusIcon status={issue.status} size={16} />
      <View style={styles.content}>
        <View style={styles.titleRow}>
          <Text style={styles.identifier}>{issue.identifier}</Text>
          <Text style={styles.title} numberOfLines={1}>
            {issue.title}
          </Text>
        </View>
        {metaParts.length > 0 && (
          <Text style={styles.meta} numberOfLines={1}>
            {metaParts.join("  |  ")}
          </Text>
        )}
      </View>
      {issue.priority !== "none" && (
        <PriorityIcon priority={issue.priority} size={14} />
      )}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 16,
    paddingVertical: 12,
    backgroundColor: colors.background,
    gap: 10,
  },
  content: {
    flex: 1,
    gap: 3,
  },
  titleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  identifier: {
    fontSize: 13,
    color: colors.mutedForeground,
    fontWeight: "500",
  },
  title: {
    flex: 1,
    fontSize: 15,
    color: colors.foreground,
  },
  meta: {
    fontSize: 12,
    color: colors.mutedForeground,
  },
});
