import { useCallback, useMemo } from "react";
import {
  ActivityIndicator,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { useLocalSearchParams } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueKeys, issueListOptions } from "@multica/core/issues/queries";
import {
  PROJECT_PRIORITY_CONFIG,
  PROJECT_STATUS_CONFIG,
} from "@multica/core/projects/config";
import {
  projectDetailOptions,
  projectKeys,
} from "@multica/core/projects/queries";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import type { Issue } from "@multica/core/types";
import { Avatar } from "@/components/Avatar";
import { IssueRow } from "@/components/IssueRow";
import { MarkdownText } from "@/components/MarkdownText";
import { ScreenHeader } from "@/components/ScreenHeader";
import { createActorResolver } from "@/lib/actors";
import { colors, priorityColors } from "@/lib/theme";

export default function ProjectDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const { data: project, isLoading } = useQuery(projectDetailOptions(wsId, id!));
  const { data: issues = [], isLoading: issuesLoading } = useQuery(
    issueListOptions(wsId),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const resolveActor = useMemo(
    () => createActorResolver(members, agents),
    [members, agents],
  );

  const linkedIssues = useMemo(
    () => issues.filter((issue: Issue) => issue.project_id === id),
    [id, issues],
  );

  const handleRefresh = useCallback(() => {
    qc.invalidateQueries({ queryKey: projectKeys.detail(wsId, id!) });
    qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
  }, [id, qc, wsId]);

  if (isLoading || !project) {
    return (
      <>
        <ScreenHeader
          title="Project"
          backLabel="Projects"
          fallback="/(app)/(projects)"
        />
        <View style={styles.loadingContainer}>
          <ActivityIndicator size="large" />
        </View>
      </>
    );
  }

  const lead = resolveActor(project.lead_type, project.lead_id);
  const done = project.done_count ?? 0;
  const total = project.issue_count ?? 0;
  const progress = total > 0 ? Math.round((done / total) * 100) : 0;

  return (
    <>
      <ScreenHeader
        title="Project"
        subtitle={project.title}
        backLabel="Projects"
        fallback="/(app)/(projects)"
      />
      <ScrollView
        style={styles.container}
        contentContainerStyle={styles.content}
        refreshControl={
          <RefreshControl
            refreshing={isLoading || issuesLoading}
            onRefresh={handleRefresh}
          />
        }
      >
        <View style={styles.header}>
          <View style={styles.iconBox}>
            <Text style={styles.iconText}>{project.icon || "P"}</Text>
          </View>
          <Text style={styles.title}>{project.title}</Text>
          {project.description ? (
            <View style={styles.description}>
              <MarkdownText
                content={project.description}
                returnTo={`/(app)/(projects)/${project.id}`}
                returnLabel="Project"
              />
            </View>
          ) : null}
        </View>

        <View style={styles.metaSection}>
          <View style={styles.metaRow}>
            <Text style={styles.metaLabel}>Status</Text>
            <Text style={styles.metaValue}>
              {PROJECT_STATUS_CONFIG[project.status].label}
            </Text>
          </View>
          <View style={styles.metaRow}>
            <Text style={styles.metaLabel}>Priority</Text>
            <Text
              style={[
                styles.metaValue,
                { color: priorityColors[project.priority] },
              ]}
            >
              {PROJECT_PRIORITY_CONFIG[project.priority].label}
            </Text>
          </View>
          <View style={styles.metaRow}>
            <Text style={styles.metaLabel}>Lead</Text>
            <View style={styles.leadValue}>
              {lead ? (
                <>
                  <Avatar
                    name={lead.name}
                    avatarUrl={lead.avatarUrl}
                    size={20}
                    isAgent={lead.isAgent}
                  />
                  <Text style={styles.metaValue}>{lead.name}</Text>
                </>
              ) : (
                <Text style={styles.metaMuted}>No lead</Text>
              )}
            </View>
          </View>
          <View style={styles.progressTrack}>
            <View style={[styles.progressFill, { width: `${progress}%` }]} />
          </View>
          <Text style={styles.progressText}>
            {done}/{total} issues done ({progress}%)
          </Text>
        </View>

        <View style={styles.section}>
          <Text style={styles.sectionTitle}>
            Issues ({linkedIssues.length})
          </Text>
          {linkedIssues.length > 0 ? (
            linkedIssues.map((issue) => {
              const assignee = resolveActor(
                issue.assignee_type,
                issue.assignee_id,
              );
              return (
                <View key={issue.id} style={styles.issueWrapper}>
                  <IssueRow
                    issue={issue}
                    assigneeName={assignee?.name}
                    projectTitle={project.title}
                    returnTo={`/(app)/(projects)/${project.id}`}
                    returnLabel="Project"
                  />
                </View>
              );
            })
          ) : (
            <Text style={styles.emptyText}>No linked issues</Text>
          )}
        </View>
      </ScrollView>
    </>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  content: {
    paddingBottom: 32,
  },
  loadingContainer: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.background,
  },
  header: {
    alignItems: "center",
    paddingHorizontal: 20,
    paddingTop: 20,
    paddingBottom: 18,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  iconBox: {
    width: 52,
    height: 52,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.secondary,
    marginBottom: 12,
  },
  iconText: {
    fontSize: 24,
    color: colors.foreground,
    fontWeight: "700",
  },
  title: {
    fontSize: 22,
    fontWeight: "700",
    color: colors.foreground,
    textAlign: "center",
  },
  description: {
    marginTop: 8,
    alignSelf: "stretch",
  },
  metaSection: {
    paddingHorizontal: 16,
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  metaRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingVertical: 8,
  },
  metaLabel: {
    fontSize: 14,
    color: colors.mutedForeground,
  },
  metaValue: {
    fontSize: 14,
    color: colors.foreground,
    fontWeight: "500",
  },
  metaMuted: {
    fontSize: 14,
    color: colors.mutedForeground,
  },
  leadValue: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  progressTrack: {
    height: 8,
    borderRadius: 4,
    backgroundColor: colors.secondary,
    marginTop: 10,
    overflow: "hidden",
  },
  progressFill: {
    height: 8,
    borderRadius: 4,
    backgroundColor: colors.foreground,
  },
  progressText: {
    marginTop: 8,
    fontSize: 12,
    color: colors.mutedForeground,
  },
  section: {
    paddingTop: 16,
  },
  sectionTitle: {
    paddingHorizontal: 16,
    marginBottom: 8,
    fontSize: 14,
    fontWeight: "600",
    color: colors.mutedForeground,
    textTransform: "uppercase",
    letterSpacing: 0.5,
  },
  issueWrapper: {
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.border,
  },
  emptyText: {
    paddingHorizontal: 16,
    paddingTop: 16,
    fontSize: 15,
    color: colors.mutedForeground,
  },
});
