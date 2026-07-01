/**
 * Workspace-wide Issues tab. Moved from more/issues.tsx; header is now
 * the in-body <Header> component since tab roots have headerShown: false.
 *
 * Mirrors web `packages/views/issues/components/issues-page.tsx:32-94`:
 * fetch every issue in the workspace, expose `all / members / agents`
 * scope tabs, group by status, allow status + priority filtering.
 */
import { useMemo } from "react";
import { Pressable, SectionList, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import type { Issue, IssuePriority, IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Header } from "@/components/ui/header";
import { HeaderActions } from "@/components/ui/app-header-actions";
import { StatusIcon } from "@/components/ui/status-icon";
import { IssueRow } from "@/components/issue/issue-row";
import { IssuesLoading } from "@/components/issue/issues-loading";
import { issueListOptions } from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  useIssuesViewStore,
  type IssuesScope,
} from "@/data/stores/issues-view-store";
import { useClearFiltersOnWorkspaceChange } from "@/lib/use-clear-filters-on-workspace-change";
import {
  BOARD_STATUSES,
  PRIORITY_LABEL,
  STATUS_LABEL,
} from "@/lib/issue-status";
import { filterIssues } from "@/lib/filter-issues";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

type IssueSection = { status: IssueStatus; data: Issue[]; count: number };

const SCOPES: { value: IssuesScope; label: string }[] = [
  { value: "all", label: "All" },
  { value: "members", label: "Members" },
  { value: "agents", label: "Agents" },
];

export default function IssuesTab() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const scope = useIssuesViewStore((s) => s.scope);
  const setScope = useIssuesViewStore((s) => s.setScope);
  const statusFilters = useIssuesViewStore((s) => s.statusFilters);
  const priorityFilters = useIssuesViewStore((s) => s.priorityFilters);
  const sortByLastEdited = useIssuesViewStore((s) => s.sortByLastEdited);
  const collapsedStatuses = useIssuesViewStore((s) => s.collapsedStatuses);
  const toggleStatusCollapse = useIssuesViewStore((s) => s.toggleStatusCollapse);

  const openFilter = () => {
    if (!wsSlug) return;
    router.push({
      pathname: "/[workspace]/issues-filter",
      params: { workspace: wsSlug, scope: "all" },
    });
  };

  useClearFiltersOnWorkspaceChange(
    useIssuesViewStore.getState().clearFilters,
    wsId,
  );

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    issueListOptions(wsId),
  );

  const allIssues = data ?? [];

  const scopedIssues = useMemo(() => {
    if (scope === "members") {
      return allIssues.filter((i) => i.assignee_type === "member");
    }
    if (scope === "agents") {
      return allIssues.filter(
        (i) => i.assignee_type === "agent" || i.assignee_type === "squad",
      );
    }
    return allIssues;
  }, [allIssues, scope]);

  const filtered = useMemo(() => {
    const f = filterIssues(scopedIssues, statusFilters, priorityFilters);
    if (!sortByLastEdited) return f;
    return [...f].sort((a, b) => b.updated_at.localeCompare(a.updated_at));
  }, [scopedIssues, statusFilters, priorityFilters, sortByLastEdited]);

  const sections = useMemo<IssueSection[]>(() => {
    if (filtered.length === 0) return [];
    const byStatus = new Map<IssueStatus, Issue[]>();
    for (const issue of filtered) {
      const list = byStatus.get(issue.status);
      if (list) list.push(issue);
      else byStatus.set(issue.status, [issue]);
    }
    const visibleStatuses =
      statusFilters.length > 0
        ? BOARD_STATUSES.filter((s) => statusFilters.includes(s))
        : BOARD_STATUSES;
    return visibleStatuses
      .map((status) => {
        const data = byStatus.get(status) ?? [];
        return { status, data, count: data.length };
      })
      .filter((s) => s.count > 0);
  }, [filtered, statusFilters]);

  const displaySections = useMemo(
    () =>
      sections.map((s) => ({
        ...s,
        data: collapsedStatuses.includes(s.status) ? ([] as Issue[]) : s.data,
      })),
    [sections, collapsedStatuses],
  );

  const hasActiveFilters =
    statusFilters.length > 0 || priorityFilters.length > 0 || sortByLastEdited;

  const showEmptyState = !isLoading && !error && filtered.length === 0;

  return (
    <View className="flex-1 bg-background">
      <Header title="Issues" right={<HeaderActions />} />
      <ScopeToolbar
        scopes={SCOPES}
        scope={scope}
        onChange={(v) => setScope(v)}
        onOpenFilter={openFilter}
        hasActiveFilters={hasActiveFilters}
      />
      {hasActiveFilters ? (
        <ActiveFilterChips
          statusFilters={statusFilters}
          priorityFilters={priorityFilters}
          onClearStatus={(s) =>
            useIssuesViewStore.getState().toggleStatusFilter(s)
          }
          onClearPriority={(p) =>
            useIssuesViewStore.getState().togglePriorityFilter(p)
          }
        />
      ) : null}
      {isLoading ? (
        <IssuesLoading />
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Failed to load issues:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : showEmptyState ? (
        <EmptyState
          message={
            hasActiveFilters
              ? "No issues match the current filters."
              : emptyMessageForScope(scope)
          }
        />
      ) : (
        <SectionList
          sections={displaySections}
          keyExtractor={(item) => item.id}
          stickySectionHeadersEnabled={false}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-4" />
          )}
          renderSectionHeader={({ section }) => (
            <SectionHeader
              status={section.status}
              count={section.count}
              collapsed={collapsedStatuses.includes(section.status)}
              onToggle={() => toggleStatusCollapse(section.status)}
            />
          )}
          contentContainerClassName="pb-6"
          renderItem={({ item }) => (
            <IssueRow
              issue={item}
              onPress={() => {
                if (wsSlug) router.push(`/${wsSlug}/issue/${item.id}`);
              }}
            />
          )}
          refreshing={isRefetching}
          onRefresh={refetch}
        />
      )}
    </View>
  );
}

function FilterButton({
  onPress,
  hasActiveFilters,
}: {
  onPress: () => void;
  hasActiveFilters: boolean;
}) {
  const { colorScheme } = useColorScheme();
  return (
    <View style={{ position: "relative" }} className="ml-2">
      <Button
        variant="outline"
        size="sm"
        onPress={onPress}
        accessibilityLabel="Filter"
        className="w-9 px-0"
      >
        <Ionicons
          name="options-outline"
          size={16}
          color={THEME[colorScheme].mutedForeground}
        />
      </Button>
      {hasActiveFilters ? (
        <View
          pointerEvents="none"
          className="absolute top-1 right-1 size-1.5 rounded-full bg-brand"
        />
      ) : null}
    </View>
  );
}

function ScopeToolbar<S extends string>({
  scopes,
  scope,
  onChange,
  onOpenFilter,
  hasActiveFilters,
}: {
  scopes: { value: S; label: string }[];
  scope: S;
  onChange: (value: S) => void;
  onOpenFilter: () => void;
  hasActiveFilters: boolean;
}) {
  return (
    <View className="flex-row items-center justify-between px-4 pt-2 pb-2">
      <View className="flex-row items-center gap-1 flex-shrink min-w-0">
        {scopes.map((s) => {
          const active = scope === s.value;
          return (
            <Button
              key={s.value}
              variant="outline"
              size="sm"
              onPress={() => onChange(s.value)}
              className={active ? "bg-accent" : ""}
              accessibilityState={{ selected: active }}
            >
              <Text
                numberOfLines={1}
                className={active ? "text-accent-foreground" : "text-muted-foreground"}
              >
                {s.label}
              </Text>
            </Button>
          );
        })}
      </View>
      <FilterButton
        onPress={onOpenFilter}
        hasActiveFilters={hasActiveFilters}
      />
    </View>
  );
}

function ActiveFilterChips({
  statusFilters,
  priorityFilters,
  onClearStatus,
  onClearPriority,
}: {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  onClearStatus: (s: IssueStatus) => void;
  onClearPriority: (p: IssuePriority) => void;
}) {
  return (
    <View className="flex-row flex-wrap gap-1.5 px-4 pb-2">
      {statusFilters.map((s) => (
        <Chip
          key={`s-${s}`}
          label={STATUS_LABEL[s]}
          onClear={() => onClearStatus(s)}
        />
      ))}
      {priorityFilters.map((p) => (
        <Chip
          key={`p-${p}`}
          label={PRIORITY_LABEL[p]}
          onClear={() => onClearPriority(p)}
        />
      ))}
    </View>
  );
}

function Chip({ label, onClear }: { label: string; onClear: () => void }) {
  const { colorScheme } = useColorScheme();
  return (
    <Pressable
      onPress={onClear}
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
      <Ionicons
        name={collapsed ? "chevron-forward" : "chevron-down"}
        size={12}
        color={t.mutedForeground}
      />
    </Pressable>
  );
}

function EmptyState({ message }: { message: string }) {
  return (
    <View className="flex-1 items-center justify-center px-6">
      <Text className="text-sm text-muted-foreground text-center">
        {message}
      </Text>
    </View>
  );
}

function emptyMessageForScope(scope: IssuesScope): string {
  switch (scope) {
    case "all":
      return "No issues in this workspace.";
    case "members":
      return "No issues assigned to a member.";
    case "agents":
      return "No issues assigned to agents or squads.";
  }
}
