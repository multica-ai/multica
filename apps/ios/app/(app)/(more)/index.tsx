import {
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { useRouter } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions } from "@multica/core/issues/queries";
import {
  deduplicateInboxItems,
  inboxListOptions,
} from "@multica/core/inbox/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useAuthStore, useWorkspaceStore } from "@/lib/providers";
import { Avatar } from "@/components/Avatar";
import { MetricTile } from "@/components/MetricTile";
import { ScreenHeader } from "@/components/ScreenHeader";
import { colors } from "@/lib/theme";

const ITEMS = [
  {
    title: "Projects",
    subtitle: "Status, progress, and linked issues",
    route: "/(app)/(projects)" as const,
    icon: "P",
  },
  {
    title: "Agents",
    subtitle: "Runtime status, ownership, and visibility",
    route: "/(app)/(agents)" as const,
    icon: "A",
  },
  {
    title: "Settings",
    subtitle: "Workspace switcher and account actions",
    route: "/(app)/(settings)" as const,
    icon: "S",
  },
];

export default function MoreScreen() {
  const router = useRouter();
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const { data: rawInbox = [] } = useQuery(inboxListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const openIssues = issues.filter(
    (issue) => issue.status !== "done" && issue.status !== "cancelled",
  ).length;
  const unreadInbox = deduplicateInboxItems(rawInbox).filter(
    (item) => !item.read,
  ).length;
  const activeProjects = projects.filter(
    (project) =>
      project.status !== "completed" && project.status !== "cancelled",
  ).length;
  const workingAgents = agents.filter(
    (agent) => agent.status === "working",
  ).length;

  return (
    <View style={styles.container}>
      <ScreenHeader
        title="More"
        subtitle={workspace?.name ?? "Workspace"}
        right={
          <Avatar name={user?.name} avatarUrl={user?.avatar_url} size={36} />
        }
      />
      <ScrollView contentContainerStyle={styles.content}>
        <View style={styles.metrics}>
          <View style={styles.metricRow}>
            <MetricTile label="Open issues" value={openIssues} />
            <MetricTile label="Unread" value={unreadInbox} tone="blue" />
          </View>
          <View style={styles.metricRow}>
            <MetricTile label="Projects" value={activeProjects} />
            <MetricTile
              label="Agents working"
              value={workingAgents}
              tone="green"
            />
          </View>
        </View>

        <View style={styles.profileStrip}>
          <Avatar name={user?.name} avatarUrl={user?.avatar_url} size={42} />
          <View style={styles.profileText}>
            <Text style={styles.userName}>{user?.name ?? "User"}</Text>
            <Text style={styles.workspaceName}>
              {user?.email ?? workspace?.name ?? "Workspace"}
            </Text>
          </View>
        </View>

        <View style={styles.section}>
          <Text style={styles.sectionTitle}>Workspace</Text>
          {ITEMS.map((item) => (
            <TouchableOpacity
              key={item.title}
              style={styles.row}
              onPress={() => router.push(item.route)}
              activeOpacity={0.7}
            >
              <View style={styles.iconBox}>
                <Text style={styles.icon}>{item.icon}</Text>
              </View>
              <View style={styles.rowText}>
                <Text style={styles.rowTitle}>{item.title}</Text>
                <Text style={styles.rowSubtitle}>{item.subtitle}</Text>
              </View>
              <Text style={styles.arrow}>›</Text>
            </TouchableOpacity>
          ))}
        </View>

        <View style={styles.note}>
          <Text style={styles.noteTitle}>Operational flow</Text>
          <Text style={styles.noteText}>
            Start with active work, check updates, then open workspace pages
            when you need broader delivery context.
          </Text>
        </View>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  content: {
    paddingBottom: 32,
    paddingTop: 12,
  },
  metrics: {
    gap: 8,
    paddingHorizontal: 16,
  },
  metricRow: {
    flexDirection: "row",
    gap: 8,
  },
  profileStrip: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    marginHorizontal: 16,
    marginTop: 16,
    padding: 14,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: colors.border,
  },
  profileText: {
    flex: 1,
  },
  userName: {
    fontSize: 17,
    fontWeight: "700",
    color: colors.foreground,
  },
  workspaceName: {
    marginTop: 2,
    fontSize: 13,
    color: colors.mutedForeground,
  },
  section: {
    paddingTop: 22,
  },
  sectionTitle: {
    paddingHorizontal: 16,
    marginBottom: 8,
    fontSize: 13,
    fontWeight: "600",
    color: colors.mutedForeground,
    textTransform: "uppercase",
    letterSpacing: 0.5,
  },
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 20,
    paddingVertical: 14,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  iconBox: {
    width: 36,
    height: 36,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.secondary,
  },
  icon: {
    fontSize: 16,
    fontWeight: "700",
    color: colors.foreground,
  },
  rowText: {
    flex: 1,
  },
  rowTitle: {
    fontSize: 15,
    fontWeight: "600",
    color: colors.foreground,
  },
  rowSubtitle: {
    marginTop: 2,
    fontSize: 13,
    color: colors.mutedForeground,
  },
  arrow: {
    fontSize: 22,
    color: colors.mutedForeground,
  },
  note: {
    marginHorizontal: 16,
    marginTop: 24,
    padding: 14,
    borderRadius: 8,
    backgroundColor: colors.secondary,
  },
  noteTitle: {
    fontSize: 14,
    fontWeight: "700",
    color: colors.foreground,
  },
  noteText: {
    marginTop: 6,
    fontSize: 14,
    lineHeight: 20,
    color: colors.mutedForeground,
  },
});
