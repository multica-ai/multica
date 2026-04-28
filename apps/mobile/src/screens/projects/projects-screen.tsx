import { FlatList, StyleSheet, Text, View } from "react-native";
import { useProjectList } from "@multica/core/projects/hooks";
import type { Project } from "@multica/core/types";
import { EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { WorkspaceHeader } from "../../components/ui/workspace-header";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

export function ProjectsScreen() {
  const { workspace } = useMobileWorkspace();
  const { data: projects = [], isLoading, isError } = useProjectList(workspace.id);

  return (
    <Screen>
      <WorkspaceHeader title="Projects" />
      {isLoading ? <LoadingState /> : null}
      {isError ? <EmptyState title="Unable to load projects" /> : null}
      {!isLoading && !isError ? (
        <FlatList
          contentContainerStyle={styles.list}
          data={projects}
          keyExtractor={(item) => item.id}
          ListEmptyComponent={<EmptyState title="No projects yet" />}
          renderItem={({ item }) => <ProjectCard project={item} />}
        />
      ) : null}
    </Screen>
  );
}

function ProjectCard({ project }: { project: Project }) {
  const progress = project.issue_count > 0
    ? Math.round((project.done_count / project.issue_count) * 100)
    : 0;

  return (
    <View style={styles.card}>
      <View style={styles.cardHeader}>
        <Text style={styles.projectTitle}>{project.title}</Text>
        <Text style={styles.status}>{project.status.replace("_", " ")}</Text>
      </View>
      {project.description ? <Text style={styles.description}>{project.description}</Text> : null}
      <Text style={styles.meta}>
        {project.done_count}/{project.issue_count} issues done · {progress}%
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  list: {
    gap: spacing.sm,
    paddingBottom: spacing.xl,
  },
  card: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    padding: spacing.md,
  },
  cardHeader: {
    alignItems: "flex-start",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  projectTitle: {
    color: colors.foreground,
    flex: 1,
    fontSize: 16,
    fontWeight: "500",
  },
  status: {
    color: colors.info,
    fontSize: 12,
    fontWeight: "500",
    textTransform: "capitalize",
  },
  description: {
    color: colors.mutedForeground,
    fontSize: 14,
    lineHeight: 20,
  },
  meta: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
});
