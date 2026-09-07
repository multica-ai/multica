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
 *     categories, same 6 visible groups as web — only the layout
 *     differs. UI may diverge when semantics agree.
 *
 * Groups are status CATEGORIES (`BOARD_CATEGORIES`, cancelled excluded) to
 * match web `packages/views/projects/components/project-detail.tsx`, NOT status
 * keys: a workspace's custom statuses live inside their category's group rather
 * than adding one of their own, and grouping by key dropped them from the list
 * entirely (MUL-6457). The earlier mobile-only "Open / Done" two-bucket layout
 * was a parity violation: the same status would appear in different visible
 * groups on mobile vs web. Cancelled is omitted on both clients.
 */
import { useMemo } from "react";
import { View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import type { Issue, IssueStatusCategory } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { StatusIcon } from "@/components/ui/status-icon";
import { IssueRow } from "@/components/issue/issue-row";
import { IssuesLoading } from "@/components/issue/issues-loading";
import { projectIssuesOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  BOARD_CATEGORIES,
  STATUS_LABEL,
  issueColumnCategory,
} from "@/lib/issue-status";

interface Props {
  projectId: string;
}

export function ProjectRelatedIssues({ projectId }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data, isLoading, error, refetch } = useQuery(
    projectIssuesOptions(wsId, projectId),
  );

  const byCategory = useMemo(() => {
    const m = new Map<IssueStatusCategory, Issue[]>();
    for (const category of BOARD_CATEGORIES) m.set(category, []);
    for (const issue of data ?? []) {
      // Issues in the cancelled category have no group here, same as before.
      m.get(issueColumnCategory(issue))?.push(issue);
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
      {BOARD_CATEGORIES.map((category) => {
        const issues = byCategory.get(category) ?? [];
        if (issues.length === 0) return null;
        return (
          <View key={category}>
            <SectionHeader category={category} count={issues.length} />
            {issues.map((issue) => (
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
  category,
  count,
}: {
  category: IssueStatusCategory;
  count: number;
}) {
  return (
    <View className="flex-row items-center gap-2 px-4 py-2 bg-background">
      {/* A category IS a built-in status key, so it resolves to its own glyph. */}
      <StatusIcon status={category} size={14} />
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {STATUS_LABEL[category]}
      </Text>
      <Text className="text-xs text-muted-foreground/60">{count}</Text>
    </View>
  );
}
