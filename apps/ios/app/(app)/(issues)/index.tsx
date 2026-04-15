import { useState, useMemo, useCallback } from "react";
import {
  View,
  SectionList,
  RefreshControl,
  StyleSheet,
  TouchableOpacity,
  Text,
} from "react-native";
import { useRouter } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import {
  issueListOptions,
  myIssueListOptions,
  issueKeys,
} from "@multica/core/issues/queries";
import { STATUS_ORDER } from "@multica/core/issues/config/status";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Issue, IssueStatus } from "@multica/core/types";
import { IssueRow } from "@/components/IssueRow";
import { SectionHeader } from "@/components/SectionHeader";
import { TabBar } from "@/components/TabBar";
import { createActorResolver } from "@/lib/actors";
import { colors } from "@/lib/theme";

type Scope = "assigned" | "created" | "agents";
type IssueTab = "all" | Scope;

const TABS: { key: IssueTab; label: string }[] = [
  { key: "assigned", label: "Mine" },
  { key: "all", label: "All" },
  { key: "created", label: "Created" },
  { key: "agents", label: "Agents" },
];

/** Filter that excludes done and cancelled from the default view */
const OPEN_STATUSES: IssueStatus[] = STATUS_ORDER.filter(
  (s) => s !== "done" && s !== "cancelled",
);

export default function MyIssuesScreen() {
  const [activeTab, setActiveTab] = useState<IssueTab>("assigned");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const qc = useQueryClient();
  const router = useRouter();
  const isAllIssues = activeTab === "all";
  const scope: Scope = isAllIssues ? "assigned" : activeTab;
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const activeAgentIds = useMemo(
    () => agents.filter((agent) => !agent.archived_at).map((agent) => agent.id),
    [agents],
  );

  const filter = useMemo(() => {
    if (!user || isAllIssues) return {};
    switch (scope) {
      case "assigned":
        return { assignee_id: user.id };
      case "created":
        return { creator_id: user.id };
      case "agents":
        return { assignee_ids: activeAgentIds };
    }
  }, [activeAgentIds, isAllIssues, scope, user]);

  const shouldFetchScopedIssues =
    !isAllIssues && !(scope === "agents" && activeAgentIds.length === 0);

  const allIssuesQuery = useQuery({
    ...issueListOptions(wsId),
    enabled: isAllIssues,
  });
  const myIssuesQuery = useQuery({
    ...myIssueListOptions(wsId, scope, filter),
    enabled: shouldFetchScopedIssues,
  });
  const issues = isAllIssues
    ? allIssuesQuery.data ?? []
    : shouldFetchScopedIssues
      ? myIssuesQuery.data ?? []
      : [];
  const isLoading = isAllIssues
    ? allIssuesQuery.isLoading
    : myIssuesQuery.isLoading;

  const resolveActor = useMemo(
    () => createActorResolver(members, agents),
    [members, agents],
  );

  const projectMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const project of projects) map.set(project.id, project.title);
    return map;
  }, [projects]);

  const sections = useMemo(() => {
    const grouped = new Map<IssueStatus, Issue[]>();
    for (const issue of issues) {
      const list = grouped.get(issue.status) ?? [];
      list.push(issue);
      grouped.set(issue.status, list);
    }
    const visibleStatuses = isAllIssues ? STATUS_ORDER : OPEN_STATUSES;
    return visibleStatuses.filter((s) => grouped.has(s)).map((status) => ({
      status,
      data: grouped.get(status) ?? [],
    }));
  }, [isAllIssues, issues]);

  const openCount = issues.filter(
    (issue) => issue.status !== "done" && issue.status !== "cancelled",
  ).length;
  const workingCount = issues.filter(
    (issue) => issue.assignee_type === "agent" && issue.status === "in_progress",
  ).length;
  const blockedCount = issues.filter((issue) => issue.status === "blocked").length;

  const handleRefresh = useCallback(() => {
    qc.invalidateQueries({
      queryKey: isAllIssues ? issueKeys.list(wsId) : issueKeys.myAll(wsId),
    });
  }, [isAllIssues, qc, wsId]);

  return (
    <View style={styles.container}>
      <TabBar tabs={TABS} activeTab={activeTab} onTabChange={setActiveTab} />
      <View style={styles.summary}>
        <View style={styles.summaryItem}>
          <Text style={styles.summaryValue}>{openCount}</Text>
          <Text style={styles.summaryLabel}>Open</Text>
        </View>
        <View style={styles.summaryItem}>
          <Text style={styles.summaryValue}>{workingCount}</Text>
          <Text style={styles.summaryLabel}>Agent work</Text>
        </View>
        <View style={styles.summaryItem}>
          <Text
            style={[
              styles.summaryValue,
              blockedCount > 0 && styles.summaryValueWarning,
            ]}
          >
            {blockedCount}
          </Text>
          <Text style={styles.summaryLabel}>Blocked</Text>
        </View>
      </View>
      <SectionList
        sections={sections}
        keyExtractor={(item) => item.id}
        renderItem={({ item }) => {
          const assignee = resolveActor(item.assignee_type, item.assignee_id);
          return (
            <IssueRow
              issue={item}
              assigneeName={assignee?.name}
              projectTitle={
                item.project_id ? projectMap.get(item.project_id) : undefined
              }
            />
          );
        }}
        renderSectionHeader={({ section }) => (
          <SectionHeader status={section.status} count={section.data.length} />
        )}
        ItemSeparatorComponent={Separator}
        stickySectionHeadersEnabled
        refreshControl={
          <RefreshControl refreshing={isLoading} onRefresh={handleRefresh} />
        }
        contentContainerStyle={
          sections.length === 0 ? styles.empty : undefined
        }
        ListEmptyComponent={
          <View style={styles.emptyContainer}>
            <Text style={styles.emptyTitle}>
              {isLoading ? "Loading..." : emptyTitle(activeTab)}
            </Text>
            {!isLoading ? (
              <Text style={styles.emptyText}>{emptyBody(activeTab)}</Text>
            ) : null}
          </View>
        }
      />
      <TouchableOpacity
        style={styles.fab}
        onPress={() => router.push("/(app)/(issues)/create")}
        activeOpacity={0.8}
      >
        <Text style={styles.fabText}>+</Text>
      </TouchableOpacity>
    </View>
  );
}

function Separator() {
  return <View style={styles.separator} />;
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  separator: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: colors.border,
    marginLeft: 42,
  },
  summary: {
    flexDirection: "row",
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  summaryItem: {
    flex: 1,
    borderRadius: 8,
    backgroundColor: colors.secondary,
    paddingHorizontal: 10,
    paddingVertical: 10,
  },
  summaryValue: {
    fontSize: 18,
    fontWeight: "800",
    color: colors.foreground,
  },
  summaryValueWarning: {
    color: colors.destructive,
  },
  summaryLabel: {
    marginTop: 2,
    fontSize: 11,
    color: colors.mutedForeground,
  },
  empty: {
    flex: 1,
  },
  emptyContainer: {
    flex: 1,
    justifyContent: "center",
    alignItems: "center",
    paddingTop: 80,
  },
  emptyText: {
    marginTop: 8,
    fontSize: 14,
    color: colors.mutedForeground,
    textAlign: "center",
    paddingHorizontal: 32,
    lineHeight: 20,
  },
  emptyTitle: {
    fontSize: 18,
    color: colors.foreground,
    fontWeight: "700",
  },
  fab: {
    position: "absolute",
    bottom: 24,
    right: 20,
    width: 52,
    height: 52,
    borderRadius: 26,
    backgroundColor: colors.foreground,
    justifyContent: "center",
    alignItems: "center",
    shadowColor: "#000",
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.25,
    shadowRadius: 8,
    elevation: 5,
  },
  fabText: {
    fontSize: 28,
    color: colors.background,
    fontWeight: "300",
    marginTop: -2,
  },
});

function emptyTitle(tab: IssueTab) {
  switch (tab) {
    case "assigned":
      return "Nothing assigned to you";
    case "created":
      return "No issues created by you";
    case "agents":
      return "No agent work yet";
    case "all":
      return "No issues";
  }
}

function emptyBody(tab: IssueTab) {
  switch (tab) {
    case "assigned":
      return "When work lands on your plate, it will show up here first.";
    case "created":
      return "Issues you create will be collected here.";
    case "agents":
      return "Assign an issue to an agent to follow automated work from this view.";
    case "all":
      return "Create the first issue to start tracking work in this workspace.";
  }
}
