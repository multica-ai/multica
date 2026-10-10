/**
 * HarmonyOS port of apps/mobile/components/project/project-related-issues.tsx.
 * Project issues use concrete status sections, like the other issue lists.
 *
 * Navigation delta vs iOS: expo-router's `router.push` is replaced by an
 * `onOpenIssue` callback prop so this component never imports the app
 * shell's route type (avoiding an import cycle) — the caller (project
 * detail screen) supplies the navigation.
 */
import { useMemo } from "react";
import { StyleSheet, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type { IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { StatusIcon } from "@/components/ui/status-icon";
import { IssueRow } from "@/components/issue/issue-row";
import { IssuesLoading } from "@/components/issue/issues-loading";
import { projectIssuesOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { groupIssuesByStatus } from "@/lib/group-issues-by-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { useThemeColors } from "@/lib/use-theme-colors";
import { withAlpha } from "@/lib/theme";

interface Props {
  projectId: string;
  /** Opens an issue; wired by the caller to the app shell's issue route. */
  onOpenIssue: (id: string) => void;
}

export function ProjectRelatedIssues({ projectId, onOpenIssue }: Props) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data, isLoading, error, refetch } = useQuery(
    projectIssuesOptions(wsId, projectId),
  );

  const catalog = useIssueStatuses();
  const sections = useMemo(
    () => groupIssuesByStatus(data ?? [], catalog.statuses),
    [data, catalog.statuses],
  );

  if (isLoading) return <IssuesLoading />;

  if (error) {
    return (
      <View style={styles.error}>
        <Text style={{ fontSize: 14, color: c.destructive }}>
          Failed to load issues:{" "}
          {error instanceof Error ? error.message : "unknown error"}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  if ((data?.length ?? 0) === 0) {
    return (
      <View style={styles.empty}>
        <Text style={{ fontSize: 14, color: c.mutedForeground }}>
          No issues yet.
        </Text>
      </View>
    );
  }

  return (
    <View>
      {sections.map(({ status, data: issues }) => {
        if (issues.length === 0) return null;
        return (
          <View key={status}>
            <SectionHeader status={status} count={issues.length} />
            {issues.map((issue) => (
              <IssueRow
                key={issue.id}
                issue={issue}
                onPress={() => onOpenIssue(issue.id)}
              />
            ))}
          </View>
        );
      })}
    </View>
  );
}

function SectionHeader({
  status,
  count,
}: {
  status: IssueStatus;
  count: number;
}) {
  const c = useThemeColors();
  const catalog = useIssueStatuses();
  return (
    <View style={[styles.header, { backgroundColor: c.background }]}>
      <StatusIcon
        status={status}
        category={catalog.categoryOf(status)}
        icon={catalog.iconOf(status)}
        color={catalog.colorOf(status)}
        size={14}
      />
      <Text style={[styles.headerLabel, { color: c.mutedForeground }]}>
        {catalog.labelOf(status)}
      </Text>
      <Text style={{ fontSize: 12, color: withAlpha(c.mutedForeground, 0.6) }}>
        {count}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  // px-4 py-6 gap-3
  error: { paddingHorizontal: 16, paddingVertical: 24, gap: 12 },
  // px-4 py-6
  empty: { paddingHorizontal: 16, paddingVertical: 24 },
  // flex-row items-center gap-2 px-4 py-2
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 8,
  },
  // text-xs uppercase tracking-wider font-medium
  headerLabel: {
    fontSize: 12,
    textTransform: "uppercase",
    letterSpacing: 0.6,
    fontWeight: "500",
  },
});
