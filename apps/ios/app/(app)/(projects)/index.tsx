import { useCallback } from "react";
import {
  FlatList,
  RefreshControl,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { useRouter } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectKeys, projectListOptions } from "@multica/core/projects/queries";
import {
  PROJECT_PRIORITY_CONFIG,
  PROJECT_STATUS_CONFIG,
} from "@multica/core/projects/config";
import type { Project } from "@multica/core/types";
import { EmptyState } from "@/components/EmptyState";
import { MetricTile } from "@/components/MetricTile";
import { ScreenHeader } from "@/components/ScreenHeader";
import { colors, priorityColors } from "@/lib/theme";

export default function ProjectsScreen() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: projects = [], isLoading } = useQuery(projectListOptions(wsId));
  const activeCount = projects.filter(
    (project) =>
      project.status === "planned" || project.status === "in_progress",
  ).length;
  const completedCount = projects.filter(
    (project) => project.status === "completed",
  ).length;
  const priorityCount = projects.filter(
    (project) => project.priority === "urgent" || project.priority === "high",
  ).length;

  const handleRefresh = useCallback(() => {
    qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
  }, [qc, wsId]);

  return (
    <View style={styles.container}>
      <ScreenHeader
        title="Projects"
        subtitle="Progress, priority, and linked work"
        backLabel="More"
        fallback="/(app)/(more)"
      />
      <FlatList
        data={projects}
        keyExtractor={(project) => project.id}
        renderItem={({ item }) => <ProjectRow project={item} />}
        ItemSeparatorComponent={Separator}
        ListHeaderComponent={
          <View style={styles.metrics}>
            <MetricTile label="Active" value={activeCount} tone="blue" />
            <MetricTile label="Completed" value={completedCount} tone="green" />
            <MetricTile
              label="High priority"
              value={priorityCount}
              tone={priorityCount > 0 ? "red" : "default"}
            />
          </View>
        }
        refreshControl={
          <RefreshControl refreshing={isLoading} onRefresh={handleRefresh} />
        }
        contentContainerStyle={
          projects.length === 0 ? styles.empty : styles.listContent
        }
        ListEmptyComponent={
          <EmptyState
            title={isLoading ? "Loading..." : "No projects"}
            body={
              isLoading
                ? undefined
                : "Create a project on web to group related issues and track delivery."
            }
          />
        }
      />
    </View>
  );
}

function ProjectRow({ project }: { project: Project }) {
  const router = useRouter();
  const done = project.done_count ?? 0;
  const total = project.issue_count ?? 0;
  const progress = total > 0 ? Math.round((done / total) * 100) : 0;

  return (
    <TouchableOpacity
      style={styles.row}
      activeOpacity={0.7}
      onPress={() =>
        router.push({
          pathname: "/(app)/(projects)/[id]",
          params: { id: project.id },
        })
      }
    >
      <View style={styles.iconBox}>
        <Text style={styles.iconText}>{project.icon || "P"}</Text>
      </View>
      <View style={styles.rowContent}>
        <View style={styles.titleRow}>
          <Text style={styles.projectTitle} numberOfLines={1}>
            {project.title}
          </Text>
          <Text
            style={[
              styles.priority,
              { color: priorityColors[project.priority] },
            ]}
          >
            {PROJECT_PRIORITY_CONFIG[project.priority].label}
          </Text>
        </View>
        {project.description ? (
          <Text style={styles.description} numberOfLines={2}>
            {project.description}
          </Text>
        ) : null}
        <View style={styles.metaRow}>
          <StatusPill project={project} />
          <Text style={styles.metaText}>
            {done}/{total} done
          </Text>
          <Text style={styles.metaText}>{progress}%</Text>
        </View>
      </View>
    </TouchableOpacity>
  );
}

function StatusPill({ project }: { project: Project }) {
  return (
    <View style={styles.statusPill}>
      <View
        style={[
          styles.statusDot,
          { backgroundColor: projectStatusColor(project.status) },
        ]}
      />
      <Text style={styles.statusText}>
        {PROJECT_STATUS_CONFIG[project.status].label}
      </Text>
    </View>
  );
}

function projectStatusColor(status: Project["status"]) {
  switch (status) {
    case "in_progress":
      return colors.warning;
    case "completed":
      return colors.info;
    case "cancelled":
      return colors.destructive;
    case "planned":
    case "paused":
      return colors.mutedForeground;
  }
}

function Separator() {
  return <View style={styles.separator} />;
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  listContent: {
    paddingBottom: 24,
  },
  metrics: {
    flexDirection: "row",
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  row: {
    flexDirection: "row",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 14,
    backgroundColor: colors.background,
  },
  iconBox: {
    width: 38,
    height: 38,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.secondary,
  },
  iconText: {
    fontSize: 18,
    color: colors.foreground,
    fontWeight: "600",
  },
  rowContent: {
    flex: 1,
    gap: 5,
  },
  titleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  projectTitle: {
    flex: 1,
    fontSize: 16,
    color: colors.foreground,
    fontWeight: "600",
  },
  priority: {
    fontSize: 12,
    fontWeight: "600",
  },
  description: {
    fontSize: 13,
    color: colors.foreground,
    lineHeight: 18,
  },
  metaRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  metaText: {
    fontSize: 12,
    color: colors.mutedForeground,
  },
  statusPill: {
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 8,
    backgroundColor: colors.secondary,
  },
  statusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  statusText: {
    fontSize: 12,
    color: colors.foreground,
    fontWeight: "500",
  },
  separator: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: colors.border,
    marginLeft: 66,
  },
  empty: {
    flexGrow: 1,
  },
});
