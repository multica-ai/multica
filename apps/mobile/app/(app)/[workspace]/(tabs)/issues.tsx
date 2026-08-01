import { useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  useWindowDimensions,
  View,
} from "react-native";
import { Image as ExpoImage } from "expo-image";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { SafeAreaView } from "react-native-safe-area-context";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { TextField } from "@/components/ui/text-field";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { IssueDetailPane } from "@/components/issue/issue-detail-pane";
import { issueListOptions } from "@/data/queries/issues";
import { useActorLookup } from "@/data/use-actor-name";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  useIssuesViewStore,
  type IssuesScope,
} from "@/data/stores/issues-view-store";
import { useClearFiltersOnWorkspaceChange } from "@/lib/use-clear-filters-on-workspace-change";
import { BOARD_STATUSES, STATUS_LABEL } from "@/lib/issue-status";
import { filterIssues } from "@/lib/filter-issues";
import { timeAgo } from "@/lib/time-ago";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";

const SCOPES: { value: IssuesScope; label: string }[] = [
  { value: "all", label: "All" },
  { value: "members", label: "Members" },
  { value: "agents", label: "Agents" },
];

/**
 * iPad workspace-wide issue surface. Counts, filters, scope semantics, and
 * realtime cache updates mirror `more/issues.tsx`; the tablet-only change is
 * keeping list selection and detail visible together.
 */
export default function TabletIssuesWorkbench() {
  const { width } = useWindowDimensions();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const scope = useIssuesViewStore((s) => s.scope);
  const setScope = useIssuesViewStore((s) => s.setScope);
  const statusFilters = useIssuesViewStore((s) => s.statusFilters);
  const priorityFilters = useIssuesViewStore((s) => s.priorityFilters);
  const { getName } = useActorLookup();
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const [query, setQuery] = useState("");
  const [selectedIssueId, setSelectedIssueId] = useState<string | null>(null);

  useClearFiltersOnWorkspaceChange(
    useIssuesViewStore.getState().clearFilters,
    wsId,
  );

  const issuesQuery = useQuery(issueListOptions(wsId));

  const visibleIssues = useMemo(() => {
    const allIssues = issuesQuery.data ?? [];
    const scoped = allIssues.filter((issue) => {
      if (!BOARD_STATUSES.includes(issue.status)) return false;
      if (scope === "members") return issue.assignee_type === "member";
      if (scope === "agents") {
        return issue.assignee_type === "agent" || issue.assignee_type === "squad";
      }
      return true;
    });
    const filtered = filterIssues(scoped, statusFilters, priorityFilters);
    const normalizedQuery = query.trim().toLocaleLowerCase();
    const searched = normalizedQuery
      ? filtered.filter(
          (issue) =>
            issue.identifier.toLocaleLowerCase().includes(normalizedQuery) ||
            issue.title.toLocaleLowerCase().includes(normalizedQuery) ||
            issue.description?.toLocaleLowerCase().includes(normalizedQuery),
        )
      : filtered;
    return [...searched].sort(
      (a, b) =>
        new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
    );
  }, [issuesQuery.data, priorityFilters, query, scope, statusFilters]);

  useEffect(() => {
    if (
      selectedIssueId &&
      visibleIssues.some((issue) => issue.id === selectedIssueId)
    ) {
      return;
    }
    setSelectedIssueId(visibleIssues[0]?.id ?? null);
  }, [selectedIssueId, visibleIssues]);

  const openFilter = () => {
    if (!wsSlug) return;
    router.push({
      pathname: "/[workspace]/issues-filter",
      params: { workspace: wsSlug, scope: "all" },
    });
  };

  const openNewIssue = () => {
    if (wsSlug) router.push(`/${wsSlug}/new-issue`);
  };

  const activeStatus =
    statusFilters.length === 1 ? STATUS_LABEL[statusFilters[0]] : null;
  const hasActiveFilters =
    statusFilters.length > 0 || priorityFilters.length > 0;
  const listWidth = Math.max(272, Math.min(336, width * 0.31));

  return (
    <SafeAreaView edges={["top"]} className="flex-1 flex-row bg-background">
      <View
        className="border-r border-border bg-background"
        style={{ width: listWidth }}
      >
        <View className="h-14 flex-row items-center border-b border-border px-3">
          <View className="min-w-0 flex-1 flex-row items-center gap-2">
            {statusFilters.length === 1 ? (
              <StatusIcon status={statusFilters[0]} size={16} />
            ) : null}
            <Text className="text-base font-semibold" numberOfLines={1}>
              {activeStatus ?? "Issues"}
            </Text>
            <Text className="text-sm text-muted-foreground">
              {visibleIssues.length}
            </Text>
          </View>
          <IconButton
            name="options-outline"
            onPress={openFilter}
            accessibilityLabel="Filter issues"
            color={hasActiveFilters ? theme.brand : undefined}
          />
          <IconButton
            name="add"
            onPress={openNewIssue}
            accessibilityLabel="New issue"
          />
        </View>

        <View className="gap-2 border-b border-border px-3 py-3">
          <TextField
            value={query}
            onChangeText={setQuery}
            placeholder="Search issues"
            clearButtonMode="while-editing"
            accessibilityLabel="Search issues"
          />
          <View className="flex-row gap-1">
            {SCOPES.map((item) => {
              const active = scope === item.value;
              return (
                <Button
                  key={item.value}
                  variant="outline"
                  size="sm"
                  onPress={() => setScope(item.value)}
                  className={cn("flex-1 px-1", active && "bg-accent")}
                  accessibilityState={{ selected: active }}
                >
                  <Text
                    className={active ? "text-accent-foreground" : "text-muted-foreground"}
                    numberOfLines={1}
                  >
                    {item.label}
                  </Text>
                </Button>
              );
            })}
          </View>
        </View>

        {issuesQuery.isLoading ? (
          <View className="flex-1 items-center justify-center">
            <ActivityIndicator />
          </View>
        ) : issuesQuery.error ? (
          <View className="gap-3 px-4 pt-6">
            <Text className="text-sm text-destructive">
              Failed to load issues: {issuesQuery.error instanceof Error ? issuesQuery.error.message : "unknown error"}
            </Text>
            <Button variant="outline" onPress={() => issuesQuery.refetch()}>
              <Text>Retry</Text>
            </Button>
          </View>
        ) : visibleIssues.length === 0 ? (
          <View className="flex-1 items-center justify-center gap-2 px-8">
            <ExpoImage
              source="sf:checklist"
              tintColor={theme.mutedForeground}
              style={{ width: 32, height: 32 }}
            />
            <Text className="text-sm font-medium text-center">No matching issues</Text>
            <Text className="text-xs text-muted-foreground text-center">
              Try another search, scope, or filter.
            </Text>
          </View>
        ) : (
          <FlatList
            data={visibleIssues}
            keyExtractor={(issue) => issue.id}
            contentContainerClassName="p-2 pb-6 gap-2"
            renderItem={({ item }) => (
              <TabletIssueListItem
                issue={item}
                assigneeName={getName(item.assignee_type, item.assignee_id)}
                selected={item.id === selectedIssueId}
                onPress={() => setSelectedIssueId(item.id)}
              />
            )}
            refreshing={issuesQuery.isRefetching}
            onRefresh={issuesQuery.refetch}
          />
        )}

        <View className="h-11 flex-row items-center justify-between border-t border-border px-4">
          <Text className="text-xs text-muted-foreground">
            {visibleIssues.length} {visibleIssues.length === 1 ? "issue" : "issues"}
          </Text>
          <IconButton
            name="refresh"
            iconSize={17}
            onPress={() => issuesQuery.refetch()}
            accessibilityLabel="Refresh issues"
          />
        </View>
      </View>

      <View className="flex-1 bg-background">
        {selectedIssueId && wsSlug ? (
          <IssueDetailPane
            key={selectedIssueId}
            issueId={selectedIssueId}
            workspaceSlug={wsSlug}
            onDeleted={() => setSelectedIssueId(null)}
          />
        ) : (
          <View className="flex-1 items-center justify-center gap-3 px-10">
            <ExpoImage
              source="sf:sidebar.right"
              tintColor={theme.mutedForeground}
              style={{ width: 36, height: 36 }}
            />
            <Text className="text-base font-semibold">Select an issue</Text>
            <Text className="max-w-sm text-sm text-muted-foreground text-center">
              Review details, agent results, and activity without leaving the workspace list.
            </Text>
          </View>
        )}
      </View>
    </SafeAreaView>
  );
}

function TabletIssueListItem({
  issue,
  assigneeName,
  selected,
  onPress,
}: {
  issue: Issue;
  assigneeName: string;
  selected: boolean;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      className={cn(
        "gap-2 rounded-md border bg-card px-3 py-3 active:bg-accent",
        selected ? "border-brand bg-accent/40" : "border-border",
      )}
      accessibilityRole="button"
      accessibilityState={{ selected }}
    >
      <View className="flex-row items-center gap-2">
        <StatusIcon status={issue.status} size={15} />
        <Text className="flex-1 text-xs text-muted-foreground">
          {issue.identifier}
        </Text>
        <PriorityIcon priority={issue.priority} size={13} />
      </View>
      <Text className="text-sm font-semibold leading-5" numberOfLines={2}>
        {issue.title}
      </Text>
      {issue.description ? (
        <Text className="text-xs leading-4 text-muted-foreground" numberOfLines={2}>
          {issue.description}
        </Text>
      ) : null}
      <View className="flex-row items-center gap-2 pt-1">
        {issue.assignee_type && issue.assignee_id ? (
          <ActorAvatar
            type={issue.assignee_type}
            id={issue.assignee_id}
            size={20}
            showPresence
          />
        ) : null}
        <Text className="min-w-0 flex-1 text-xs text-muted-foreground" numberOfLines={1}>
          {issue.assignee_id ? assigneeName : "Unassigned"}
        </Text>
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {timeAgo(issue.updated_at)}
        </Text>
      </View>
    </Pressable>
  );
}
