import { View, Text, TouchableOpacity, ScrollView, StyleSheet } from "react-native";
import { useRouter } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions } from "@multica/core/issues/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { inboxListOptions, deduplicateInboxItems } from "@multica/core/inbox/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { useWorkspaceStore, useAuthStore } from "@/lib/providers";
import { Avatar } from "@/components/Avatar";
import { MetricTile } from "@/components/MetricTile";
import { ScreenHeader } from "@/components/ScreenHeader";
import { colors } from "@/lib/theme";

export default function HomeScreen() {
  const router = useRouter();
  const wsId = useWorkspaceId();
  const workspace = useWorkspaceStore((s) => s.workspace);
  const user = useAuthStore((s) => s.user);
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: rawInbox = [] } = useQuery(inboxListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));

  const openIssues = issues.filter(
    (issue) => issue.status !== "done" && issue.status !== "cancelled",
  );
  const blockedIssues = issues.filter((issue) => issue.status === "blocked");
  const workingAgents = agents.filter((agent) => agent.status === "working");
  const inboxItems = deduplicateInboxItems(rawInbox).filter((item) => !item.read);
  const activeProjects = projects.filter(
    (project) => project.status !== "completed" && project.status !== "cancelled",
  );

  return (
    <View style={styles.safeArea}>
      <ScreenHeader
        title={workspace?.name ?? "Workspace"}
        subtitle={`Today${user?.name ? ` for ${user.name.split(" ")[0]}` : ""}`}
        right={<Avatar name={user?.name} avatarUrl={user?.avatar_url} size={36} />}
      />
      <ScrollView style={styles.container} contentContainerStyle={styles.content}>
        <View style={styles.metrics}>
          <View style={styles.metricRow}>
            <MetricTile label="Open issues" value={openIssues.length} />
            <MetricTile label="Agents working" value={workingAgents.length} tone="green" />
          </View>
          <View style={styles.metricRow}>
            <MetricTile label="Unread inbox" value={inboxItems.length} tone="blue" />
            <MetricTile label="Blocked" value={blockedIssues.length} tone={blockedIssues.length > 0 ? "red" : "default"} />
          </View>
        </View>

        <View style={styles.focusPanel}>
          <Text style={styles.focusTitle}>Focus</Text>
          <Text style={styles.focusText}>
            {workingAgents.length > 0
              ? `${workingAgents.length} agent${workingAgents.length === 1 ? "" : "s"} running now. Open Issues to follow the live log.`
              : openIssues.length > 0
                ? `${openIssues.length} open issue${openIssues.length === 1 ? "" : "s"} across ${activeProjects.length} active project${activeProjects.length === 1 ? "" : "s"}.`
                : "No open issues. Inbox and projects are clear."}
          </Text>
        </View>

        <View style={styles.section}>
          <Text style={styles.sectionTitle}>Go to</Text>

          <TouchableOpacity
            style={styles.actionCard}
            onPress={() => router.push("/(app)/(issues)")}
            activeOpacity={0.7}
          >
            <Text style={styles.actionIcon}>☰</Text>
            <View style={styles.actionContent}>
              <Text style={styles.actionTitle}>My Issues</Text>
              <Text style={styles.actionSubtitle}>
                Assigned, agent work, and all open tasks
              </Text>
            </View>
            <Text style={styles.actionArrow}>→</Text>
          </TouchableOpacity>

          <TouchableOpacity
            style={styles.actionCard}
            onPress={() => router.push("/(app)/(projects)")}
            activeOpacity={0.7}
          >
            <Text style={styles.actionIcon}>P</Text>
            <View style={styles.actionContent}>
              <Text style={styles.actionTitle}>Projects</Text>
              <Text style={styles.actionSubtitle}>
                Progress and linked issues
              </Text>
            </View>
            <Text style={styles.actionArrow}>→</Text>
          </TouchableOpacity>

          <TouchableOpacity
            style={styles.actionCard}
            onPress={() => router.push("/(app)/(agents)")}
            activeOpacity={0.7}
          >
            <Text style={styles.actionIcon}>A</Text>
            <View style={styles.actionContent}>
              <Text style={styles.actionTitle}>Agents</Text>
              <Text style={styles.actionSubtitle}>
                Runtime, ownership, and status
              </Text>
            </View>
            <Text style={styles.actionArrow}>→</Text>
          </TouchableOpacity>

          <TouchableOpacity
            style={styles.actionCard}
            onPress={() => router.push("/(app)/(inbox)")}
            activeOpacity={0.7}
          >
            <Text style={styles.actionIcon}>✉</Text>
            <View style={styles.actionContent}>
              <Text style={styles.actionTitle}>Inbox</Text>
              <Text style={styles.actionSubtitle}>
                Notifications and updates
              </Text>
            </View>
            <Text style={styles.actionArrow}>→</Text>
          </TouchableOpacity>

          <TouchableOpacity
            style={styles.actionCard}
            onPress={() => router.push("/(app)/(issues)/create")}
            activeOpacity={0.7}
          >
            <Text style={styles.actionIcon}>+</Text>
            <View style={styles.actionContent}>
              <Text style={styles.actionTitle}>Create Issue</Text>
              <Text style={styles.actionSubtitle}>
                Start a new task
              </Text>
            </View>
            <Text style={styles.actionArrow}>→</Text>
          </TouchableOpacity>
        </View>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  safeArea: {
    flex: 1,
    backgroundColor: colors.background,
  },
  container: {
    flex: 1,
  },
  content: {
    paddingBottom: 96,
    paddingTop: 16,
  },
  metrics: {
    paddingHorizontal: 16,
    gap: 8,
  },
  metricRow: {
    flexDirection: "row",
    gap: 8,
  },
  focusPanel: {
    marginHorizontal: 16,
    marginTop: 16,
    marginBottom: 22,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: colors.border,
    padding: 14,
  },
  focusTitle: {
    fontSize: 13,
    fontWeight: "700",
    color: colors.foreground,
    textTransform: "uppercase",
  },
  focusText: {
    marginTop: 6,
    fontSize: 15,
    lineHeight: 21,
    color: colors.foreground,
  },
  section: {
    paddingHorizontal: 16,
  },
  sectionTitle: {
    fontSize: 13,
    fontWeight: "600",
    color: colors.mutedForeground,
    textTransform: "uppercase",
    letterSpacing: 0.5,
    marginBottom: 12,
  },
  actionCard: {
    flexDirection: "row",
    alignItems: "center",
    padding: 16,
    backgroundColor: colors.secondary,
    borderRadius: 8,
    marginBottom: 8,
    gap: 14,
  },
  actionIcon: {
    fontSize: 20,
    width: 28,
    textAlign: "center",
  },
  actionContent: {
    flex: 1,
  },
  actionTitle: {
    fontSize: 15,
    fontWeight: "600",
    color: colors.foreground,
    marginBottom: 2,
  },
  actionSubtitle: {
    fontSize: 13,
    color: colors.mutedForeground,
  },
  actionArrow: {
    fontSize: 16,
    color: colors.mutedForeground,
  },
});
