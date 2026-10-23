/**
 * Project issues use concrete status sections, like the other issue lists.
 *
 * Status, priority and "Show completed" filters are kept per project in
 * `useProjectIssuesViewStore` (web keeps a view per `project:<id>` surface)
 * and edited in the shared `issues-filter` sheet. As on the other lists,
 * completed issues are hidden until "Show completed" is on.
 */
import { useEffect, useMemo } from "react";
import { Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { StatusIcon } from "@/components/ui/status-icon";
import { IssueRow } from "@/components/issue/issue-row";
import { IssuesLoading } from "@/components/issue/issues-loading";
import { projectIssuesOptions } from "@/data/queries/projects";
import {
  DEFAULT_PROJECT_ISSUES_VIEW,
  loadProjectIssuesView,
  useProjectIssuesViewStore,
} from "@/data/stores/project-issues-view-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { filterIssues, hideCompletedIssues } from "@/lib/filter-issues";
import { groupIssuesByStatus } from "@/lib/group-issues-by-status";
import { PRIORITY_LABEL } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { useT } from "@/lib/i18n";

interface Props {
  projectId: string;
}

export function ProjectRelatedIssues({ projectId }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { t } = useT("issues");
  const { data, isLoading, error, refetch } = useQuery(
    projectIssuesOptions(wsId, projectId),
  );

  const view =
    useProjectIssuesViewStore((s) => s.byProject[projectId]) ??
    DEFAULT_PROJECT_ISSUES_VIEW;
  const { statusFilters, priorityFilters, showCompleted } = view;

  const generation = useProjectIssuesViewStore((s) => s.generation);
  useEffect(() => {
    void loadProjectIssuesView(projectId);
  }, [projectId, generation]);

  const catalog = useIssueStatuses();
  const matching = useMemo(
    () => filterIssues(data ?? [], statusFilters, priorityFilters),
    [data, statusFilters, priorityFilters],
  );
  const visible = useMemo(
    () =>
      showCompleted ? matching : hideCompletedIssues(matching, statusFilters),
    [matching, showCompleted, statusFilters],
  );
  const sections = useMemo(() => groupIssuesByStatus(visible, catalog.statuses), [visible, catalog.statuses]);

  const hasActiveFilters =
    statusFilters.length > 0 || priorityFilters.length > 0;

  const navigateToIssue = (id: string) => {
    if (wsSlug) router.push(`/${wsSlug}/issue/${id}`);
  };

  const openFilter = () => {
    if (!wsSlug) return;
    router.push({
      pathname: "/[workspace]/issues-filter",
      params: { workspace: wsSlug, scope: "project", projectId },
    });
  };

  const header = (
    <>
      <View className="flex-row items-center justify-between pl-4 pr-2">
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          {t("projects:detail.related_issues")}
        </Text>
        <FilterButton onPress={openFilter} active={hasActiveFilters} />
      </View>
      {hasActiveFilters ? (
        <ActiveFilterChips
          statusFilters={statusFilters}
          priorityFilters={priorityFilters}
          statusLabelOf={catalog.labelOf}
          onClearStatus={(s) =>
            useProjectIssuesViewStore.getState().toggleStatusFilter(projectId, s)
          }
          onClearPriority={(p) =>
            useProjectIssuesViewStore.getState().togglePriorityFilter(projectId, p)
          }
        />
      ) : null}
    </>
  );

  if (isLoading) {
    return (
      <View>
        {header}
        <IssuesLoading />
      </View>
    );
  }

  if (error) {
    return (
      <View>
        {header}
        <View className="px-4 py-6 gap-3">
          <Text className="text-sm text-destructive">
            {t("errors.load_failed", {
              message: error instanceof Error ? error.message : "unknown",
            })}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>{t("common:actions.retry")}</Text>
          </Button>
        </View>
      </View>
    );
  }

  if (visible.length === 0) {
    const completedOnly = matching.length > 0;
    return (
      <View>
        {header}
        <View className="px-4 py-6 gap-3 items-start">
          <Text className="text-sm text-muted-foreground">
            {completedOnly
              ? t("empty.completed_hidden")
              : hasActiveFilters
                ? t("empty.filtered")
                : t("list.empty")}
          </Text>
          {completedOnly ? (
            <Button
              variant="outline"
              size="sm"
              onPress={() =>
                useProjectIssuesViewStore
                  .getState()
                  .setShowCompleted(projectId, true)
              }
            >
              <Text>{t("filters.show_completed")}</Text>
            </Button>
          ) : null}
        </View>
      </View>
    );
  }

  return (
    <View>
      {header}
      {sections.map(({ status, data: issues }) => {
        if (issues.length === 0) return null;
        return (
          <View key={status}>
            <SectionHeader status={status} count={issues.length} />
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

function FilterButton({
  onPress,
  active,
}: {
  onPress: () => void;
  active: boolean;
}) {
  const { t } = useT("issues");
  const { colorScheme } = useColorScheme();
  return (
    <View>
      <IconButton
        name="options-outline"
        iconSize={16}
        color={THEME[colorScheme].mutedForeground}
        onPress={onPress}
        accessibilityLabel={t("header_filter")}
      />
      {active ? (
        <View
          pointerEvents="none"
          className="absolute top-1.5 right-1.5 size-1.5 rounded-full bg-brand"
        />
      ) : null}
    </View>
  );
}

function ActiveFilterChips({
  statusFilters,
  priorityFilters,
  statusLabelOf,
  onClearStatus,
  onClearPriority,
}: {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  /** Resolves a status KEY — which can be a custom one — to its label. */
  statusLabelOf: (statusKey: string) => string;
  onClearStatus: (s: IssueStatus) => void;
  onClearPriority: (p: IssuePriority) => void;
}) {
  const { t } = useT("issues");
  return (
    <View className="flex-row flex-wrap gap-1.5 px-4 pb-2">
      {statusFilters.map((s) => (
        <Chip key={`s-${s}`} label={statusLabelOf(s)} onClear={() => onClearStatus(s)} />
      ))}
      {priorityFilters.map((p) => (
        <Chip key={`p-${p}`} label={t(PRIORITY_LABEL[p])} onClear={() => onClearPriority(p)} />
      ))}
    </View>
  );
}

function Chip({ label, onClear }: { label: string; onClear: () => void }) {
  const { colorScheme } = useColorScheme();
  return (
    <Pressable
      onPress={onClear}
      accessibilityRole="button"
      className="flex-row items-center gap-1 pl-2.5 pr-2 py-1 rounded-full border border-border bg-secondary/40 active:bg-secondary"
    >
      <Text className="text-xs text-foreground">{label}</Text>
      <Ionicons
        name="close"
        size={12}
        color={THEME[colorScheme].mutedForeground}
      />
    </Pressable>
  );
}

function SectionHeader({
  status,
  count,
}: {
  status: IssueStatus;
  count: number;
}) {
  const catalog = useIssueStatuses();
  return (
    <View className="flex-row items-center gap-2 px-4 py-2 bg-background">
      <StatusIcon status={status} category={catalog.categoryOf(status)} icon={catalog.iconOf(status)} color={catalog.colorOf(status)} size={14} />
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {catalog.labelOf(status)}
      </Text>
      <Text className="text-xs text-muted-foreground/60">{count}</Text>
    </View>
  );
}
