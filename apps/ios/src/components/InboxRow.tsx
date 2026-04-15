import { View, Text, TouchableOpacity, StyleSheet } from "react-native";
import { useRouter } from "expo-router";
import type { InboxItem } from "@multica/core/types";
import { timeAgo } from "@multica/core/utils";
import { useMarkInboxRead } from "@multica/core/inbox/mutations";
import { colors } from "@/lib/theme";

const TYPE_LABELS: Record<string, string> = {
  issue_assigned: "Assigned to you",
  unassigned: "Unassigned",
  assignee_changed: "Assignee changed",
  status_changed: "Status changed",
  priority_changed: "Priority changed",
  due_date_changed: "Due date changed",
  new_comment: "New comment",
  mentioned: "Mentioned you",
  review_requested: "Review requested",
  task_completed: "Task completed",
  task_failed: "Task failed",
  agent_blocked: "Agent blocked",
  agent_completed: "Agent completed",
  reaction_added: "Reaction added",
};

export function InboxRow({ item }: { item: InboxItem }) {
  const router = useRouter();
  const markRead = useMarkInboxRead();

  return (
    <TouchableOpacity
      style={[styles.container, !item.read && styles.unread]}
      onPress={() => {
        if (!item.read) markRead.mutate(item.id);
        if (item.issue_id) {
          router.push({
            pathname: "/(app)/(issues)/[id]",
            params: {
              id: item.issue_id,
              returnTo: "/(app)/(inbox)",
              returnLabel: "Inbox",
            },
          });
        }
      }}
      activeOpacity={0.6}
    >
      <View style={styles.dot}>
        {!item.read && <View style={styles.unreadDot} />}
      </View>
      <View style={styles.content}>
        <Text style={styles.type}>{TYPE_LABELS[item.type] ?? item.type}</Text>
        <Text style={styles.title} numberOfLines={1}>
          {item.title}
        </Text>
        {item.body && (
          <Text style={styles.body} numberOfLines={2}>
            {item.body}
          </Text>
        )}
        <Text style={styles.time}>{timeAgo(item.created_at)}</Text>
      </View>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: "row",
    paddingHorizontal: 16,
    paddingVertical: 12,
    backgroundColor: colors.background,
    gap: 10,
  },
  unread: {
    backgroundColor: colors.secondary,
  },
  dot: {
    width: 8,
    paddingTop: 6,
    alignItems: "center",
  },
  unreadDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: colors.brand,
  },
  content: {
    flex: 1,
  },
  type: {
    fontSize: 12,
    fontWeight: "600",
    color: colors.mutedForeground,
    marginBottom: 2,
    textTransform: "uppercase",
    letterSpacing: 0.3,
  },
  title: {
    fontSize: 15,
    fontWeight: "500",
    color: colors.foreground,
    marginBottom: 2,
  },
  body: {
    fontSize: 13,
    color: colors.mutedForeground,
    lineHeight: 18,
    marginBottom: 4,
  },
  time: {
    fontSize: 12,
    color: colors.mutedForeground,
  },
});
