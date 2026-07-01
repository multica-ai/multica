/**
 * Issues belonging to a project — status-grouped list.
 *
 * Mobile intentionally does NOT implement web's Board (kanban) view, only
 * the List form. Reasons:
 *   - Phone screens are too narrow to show ≥3 status columns at once, so
 *     kanban loses its core "see all-statuses pipeline at a glance" value;
 *     users end up swiping between near-empty columns.
 *   - Major mobile task apps (Linear iOS, Things, Apple Reminders) don't
 *     ship kanban either — list with status grouping is the established
 *     small-screen pattern for the same data.
 *   - This is a UI divergence, NOT semantic divergence (per
 *     mobile/CLAUDE.md "Behavioral parity"): same issues, same status
 *     enum, same 6 BOARD_STATUSES grouping as web — only the layout
 *     differs. UI may diverge when semantics agree.
 *
 * Status grouping uses full `BOARD_STATUSES` (six visible groups, cancelled
 * excluded) to match web `packages/views/projects/components/project-detail.tsx`.
 * The earlier mobile-only "Open / Done" two-bucket layout was a parity
 * violation: same status enum value would appear in different visible
 * groups on mobile vs web. Cancelled is omitted on both clients.
 */
import { useMemo } from "react";
import { Pressable, View } from "react-native";
import { Image as ExpoImage } from "expo-image";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import type { Issue, IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { StatusIcon } from "@/components/ui/status-icon";
import { IssueRow } from "@/components/issue/issue-row";
import { IssuesLoading } from "@/components/issue/issues-loading";
import { projectIssuesOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useProjectCollapseStore } from "@/data/stores/project-collapse-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { BOARD_STATUSES, STATUS_LABEL } from "@/lib/issue-status";

interface Props {
  projectId: string;
}

export function ProjectRelatedIssues({ projectId }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data, isLoading, error, refetch } = useQuery(
    projectIssuesOptions(wsId, projectId),
  );
  const toggle = useProjectCollapseStore((s) => s.toggle);
  const collapsedByProject = useProjectCollapseStore((s) => s.collapsedByProject);

  const byStatus = useMemo(() => {
    const m = new Map<IssueStatus, Issue[]>();
    for (const status of BOARD_STATUSES) m.set(status, []);
    for (const issue of data ?? []) {
      const list = m.get(issue.status);
      if (list) list.push(issue);
    }
    return m;
  }, [data]);

  const navigateToIssue = (id: string) => {
    if (wsSlug) router.push(`/${wsSlug}/issue/${id}`);
  };

  if (isLoading) return <IssuesLoading />;

  if (error) {
    return (
      <View className="px-4 py-6 gap-3">
        <Text className="text-sm text-destructive">
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
      <View className="px-4 py-6">
        <Text className="text-sm text-muted-foreground">No issues yet.</Text>
      </View>
    );
  }

  return (
    <View>
      {BOARD_STATUSES.map((status) => {
        const issues = byStatus.get(status) ?? [];
        if (issues.length === 0) return null;
        const collapsed = (collapsedByProject[projectId] ?? []).includes(status);
        return (
          <View key={status}>
            <SectionHeader
              status={status}
              count={issues.length}
              collapsed={collapsed}
              onToggle={() => toggle(projectId, status)}
            />
            {!collapsed &&
              issues.map((issue) => (
                <IssueRow
                  key={issue.id}
                  issue={issue}
                  onPress={() => navigateToIssue(issue.id)}
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
  collapsed,
  onToggle,
}: {
  status: IssueStatus;
  count: number;
  collapsed: boolean;
  onToggle: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const t = THEME[colorScheme];
  return (
    <Pressable
      onPress={onToggle}
      className="flex-row items-center gap-2 px-4 py-2 bg-background active:bg-secondary"
      accessibilityRole="button"
      accessibilityLabel={`${STATUS_LABEL[status]}, ${count} issues, ${collapsed ? "collapsed" : "expanded"}`}
    >
      <StatusIcon status={status} size={14} />
      <Text className="flex-1 text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {STATUS_LABEL[status]}
      </Text>
      <Text className="text-xs text-muted-foreground/60 mr-1">{count}</Text>
      <ExpoImage
        source="sf:chevron.right"
        tintColor={t.mutedForeground}
        style={{
          width: 12,
          height: 12,
          transform: [{ rotate: collapsed ? "0deg" : "90deg" }],
        }}
      />
    </Pressable>
  );
}
