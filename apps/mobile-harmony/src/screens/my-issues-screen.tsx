/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/(tabs)/my-issues.tsx
 * (className → StyleSheet translation; NativeWind is not part of this app).
 *
 * "My Issues" tab. Three scopes — assigned / created / agents — mirroring
 * web's `packages/views/my-issues/components/my-issues-page.tsx:48-65`. The
 * `agents` scope label is "Agents" (short form; the full "Agents and Squads"
 * label blows past the pill-row width on small phones) because the backend
 * predicate (`involves_user_id`, MUL-2397) surfaces both the user's owned
 * agents and squads they're involved in (member / leader / has an owned agent
 * inside).
 *
 * Issues are grouped by concrete status key; empty sections are omitted.
 * Category controls lifecycle behavior and ordering, not section identity.
 *
 * Status + Priority filters mirror web's MyIssuesHeader filter sub-menus.
 * Filter state lives in `useMyIssuesViewStore` and is cleared on workspace
 * change via the shared `useClearFiltersOnWorkspaceChange` hook. The filter
 * UI is the ported `IssuesFilterSheet` hosted directly in the screen — the
 * harmony BottomSheet stands in for the iOS formSheet route.
 *
 * Deltas from the iOS file, both platform seams (see apps/mobile-harmony/
 * AGENTS.md):
 *   - Navigation goes through the `onOpenIssue` / `onOpenSearch` /
 *     `onCreateIssue` callbacks instead of expo-router pushes; the header
 *     actions render only when the shell wired the corresponding callback.
 *   - `useIsFocused` (@react-navigation/native) is unavailable; the pull-to-
 *     refresh spinner keys on `isRefetching` alone (background refetches of
 *     hidden tabs are invisible — inactive tabs stay mounted but display:none).
 */
import React, { useMemo, useState } from "react";
import { SectionList, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type {
  Issue,
  IssuePriority,
  IssueStatus,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Header } from "@/components/ui/header";
import { IconButton } from "@/components/ui/icon-button";
import { Icon } from "@/components/ui/icon";
import { StatusIcon } from "@/components/ui/status-icon";
import { IssueRow } from "@/components/issue/issue-row";
import { IssuesLoading } from "@/components/issue/issues-loading";
import {
  buildMyIssuesFilter,
  myIssueListOptions,
} from "@/data/queries/my-issues";
import type { MyIssuesScope } from "@/data/queries/issue-keys";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useMyIssuesViewStore } from "@/data/stores/my-issues-view-store";
import { useMyIssuesRealtime } from "@/data/realtime/use-my-issues-realtime";
import { useClearFiltersOnWorkspaceChange } from "@/lib/use-clear-filters-on-workspace-change";
import { PRIORITY_LABEL } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { groupIssuesByStatus } from "@/lib/group-issues-by-status";
import { filterIssues } from "@/lib/filter-issues";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { IssuesFilterSheet } from "@/src/screens/issues-filter-screen";

// Mobile pill row has tight width on small phones (iOS SE3: 375pt). Three
// pills + Filter icon must fit the usable space, so the agents scope renders
// "Agents" — the full "Agents and Squads" label blows past safe limits and
// breaks under Dynamic Type. Semantics unchanged: same backend predicate
// (`involves_user_id`, MUL-2397) covers owned agents + related squads; the
// empty state copy still says "agents or squads".
const SCOPES: { value: MyIssuesScope; label: string }[] = [
  { value: "assigned", label: "Assigned" },
  { value: "created", label: "Created" },
  { value: "agents", label: "Agents" },
];

interface Props {
  /** Open an issue (shell pushes its issue route). No-op when unwired. */
  onOpenIssue?: (issueId: string) => void;
  /** Header search action; the button renders only when wired. */
  onOpenSearch?: () => void;
  /** Header create-issue action; the button renders only when wired. */
  onCreateIssue?: () => void;
}

export function MyIssuesScreen({ onOpenIssue, onOpenSearch, onCreateIssue }: Props) {
  const c = useThemeColors();
  // Listing-level WS subscriptions patch the my-issues caches in place; the
  // iOS app mounts the same hook at the workspace-layout level, but tabs stay
  // mounted here, so per-screen mounting delivers the same freshness.
  useMyIssuesRealtime();

  const userId = useAuthStore((s) => s.user?.id ?? null);
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const scope = useMyIssuesViewStore((s) => s.scope);
  const setScope = useMyIssuesViewStore((s) => s.setScope);
  const statusFilters = useMyIssuesViewStore((s) => s.statusFilters);
  const priorityFilters = useMyIssuesViewStore((s) => s.priorityFilters);

  // The iOS app pushes the /[workspace]/issues-filter formSheet route; the
  // harmony BottomSheet is hosted in-screen instead.
  const [filterOpen, setFilterOpen] = useState(false);
  const openFilter = () => {
    if (!wsSlug) return;
    setFilterOpen(true);
  };

  useClearFiltersOnWorkspaceChange(
    useMyIssuesViewStore.getState().clearFilters,
    wsId,
  );

  const filter = useMemo(
    () => (userId ? buildMyIssuesFilter(scope, userId) : { assignee_id: "" }),
    [scope, userId],
  );

  const { data, isLoading, error, refetch, isRefetching } = useQuery({
    ...myIssueListOptions(wsId, scope, filter),
    enabled: !!wsId && !!userId,
  });

  // Catalog labels and ordering enhance exact-key sections without blocking rows.
  const catalog = useIssueStatuses();

  // Apply client-side status + priority filter. Mirrors the predicate at
  // packages/views/issues/utils/filter.ts:30-34 via filterIssues().
  const filtered = useMemo(
    () => filterIssues(data ?? [], statusFilters, priorityFilters),
    [data, statusFilters, priorityFilters],
  );

  const sections = useMemo(
    () => groupIssuesByStatus(filtered, catalog.statuses),
    [filtered, catalog.statuses],
  );

  const hasActiveFilters =
    statusFilters.length > 0 || priorityFilters.length > 0;

  const showEmptyState =
    !isLoading && !error && filtered.length === 0;

  const headerActions =
    onOpenSearch || onCreateIssue ? (
      <>
        {onOpenSearch ? (
          <IconButton
            name="search"
            onPress={onOpenSearch}
            accessibilityLabel="Search"
          />
        ) : null}
        {onCreateIssue ? (
          <IconButton
            name="add"
            iconSize={24}
            onPress={onCreateIssue}
            accessibilityLabel="New issue"
          />
        ) : null}
      </>
    ) : undefined;

  return (
    <View style={[styles.screen, { backgroundColor: c.background }]}>
      <Header title="My Issues" right={headerActions} />
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
          statusLabelOf={catalog.labelOf}
          onClearStatus={(s) =>
            useMyIssuesViewStore.getState().toggleStatusFilter(s)
          }
          onClearPriority={(p) =>
            useMyIssuesViewStore.getState().togglePriorityFilter(p)
          }
        />
      ) : null}
      {isLoading ? (
        <IssuesLoading />
      ) : error ? (
        <View style={styles.errorWrap}>
          <Text style={[styles.errorText, { color: c.destructive }]}>
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
        <SectionList<Issue, (typeof sections)[number]>
          sections={sections}
          keyExtractor={(item) => item.id}
          stickySectionHeadersEnabled={false}
          ItemSeparatorComponent={ItemSeparator}
          renderSectionHeader={({ section }) => (
            <SectionHeader
              status={section.status}
              count={section.data.length}
            />
          )}
          contentContainerStyle={styles.listContent}
          renderItem={({ item }) => (
            <IssueRow issue={item} onPress={() => onOpenIssue?.(item.id)} />
          )}
          refreshing={isRefetching}
          onRefresh={() => refetch()}
        />
      )}

      <IssuesFilterSheet
        visible={filterOpen}
        onClose={() => setFilterOpen(false)}
        scope="my"
      />
    </View>
  );
}

/** `h-px bg-border ml-4` — one hairline between grouped rows. */
function ItemSeparator() {
  const c = useThemeColors();
  return (
    <View
      style={[staticStyles.separator, { backgroundColor: c.border }]}
    />
  );
}

/**
 * Outline icon button matching the pill height so the toolbar row reads as
 * one visual group. Mirrors web `IssuesHeader` / `MyIssuesHeader` filter
 * trigger (`packages/views/my-issues/components/my-issues-header.tsx:174`),
 * which is also `variant="outline"` + icon-sized — NOT the ghost-style we'd
 * get from <IconButton>. Square (w-9) with px-0 to suppress the sm default
 * horizontal padding.
 */
function FilterButton({
  onPress,
  hasActiveFilters,
}: {
  onPress: () => void;
  hasActiveFilters: boolean;
}) {
  const c = useThemeColors();
  return (
    <View style={styles.filterWrap}>
      <Button
        variant="outline"
        size="sm"
        onPress={onPress}
        accessibilityLabel="Filter"
        style={styles.filterButton}
      >
        <Icon
          name="options-outline"
          size={16}
          color={c.mutedForeground}
        />
      </Button>
      {hasActiveFilters ? (
        <View
          pointerEvents="none"
          style={[styles.filterDot, { backgroundColor: c.brand }]}
        />
      ) : null}
    </View>
  );
}

/**
 * Toolbar row mirroring web `MyIssuesHeader` / `IssuesHeader`
 * (`packages/views/my-issues/components/my-issues-header.tsx:138-163`):
 * left-aligned scope pill group + right-side Filter icon (red dot when
 * filters are active). Replaces the previous full-width segmented tabs +
 * Filter-in-title-bar split — keeps scope and the filter affordance in the
 * same row, because they both control the list directly below.
 */
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
  const c = useThemeColors();
  return (
    <View style={styles.toolbar}>
      <View style={styles.pillGroup}>
        {scopes.map((s) => {
          const active = scope === s.value;
          return (
            <Button
              key={s.value}
              variant="outline"
              size="sm"
              onPress={() => onChange(s.value)}
              style={active ? { backgroundColor: c.accent } : undefined}
              accessibilityState={{ selected: active }}
            >
              <Text
                numberOfLines={1}
                style={{
                  color: active ? c.accentForeground : c.mutedForeground,
                }}
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
  return (
    <View style={styles.chipRow}>
      {statusFilters.map((s) => (
        <Chip key={`s-${s}`} label={statusLabelOf(s)} onClear={() => onClearStatus(s)} />
      ))}
      {priorityFilters.map((p) => (
        <Chip key={`p-${p}`} label={PRIORITY_LABEL[p]} onClear={() => onClearPriority(p)} />
      ))}
    </View>
  );
}

function Chip({ label, onClear }: { label: string; onClear: () => void }) {
  const c = useThemeColors();
  return (
    <Pressable
      onPress={onClear}
      style={({ pressed }) => [
        styles.chip,
        {
          borderColor: c.border,
          backgroundColor: withAlpha(c.secondary, 0.4),
        },
        pressed && { backgroundColor: c.secondary },
      ]}
    >
      <Text style={[styles.chipLabel, { color: c.foreground }]}>{label}</Text>
      <Icon name="close" size={12} color={c.mutedForeground} />
    </Pressable>
  );
}

// The section header names its concrete built-in or custom status.
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
    <View style={[styles.sectionHeader, { backgroundColor: c.background }]}>
      {/* Category keys resolve to their canonical lifecycle glyph. */}
      <StatusIcon
        status={status}
        category={catalog.categoryOf(status)}
        icon={catalog.iconOf(status)}
        color={catalog.colorOf(status)}
        size={14}
      />
      <Text style={[styles.sectionTitle, { color: c.mutedForeground }]}>
        {catalog.labelOf(status)}
      </Text>
      <Text style={[styles.sectionCount, { color: withAlpha(c.mutedForeground, 0.6) }]}>
        {count}
      </Text>
    </View>
  );
}

function EmptyState({ message }: { message: string }) {
  const c = useThemeColors();
  return (
    <View style={styles.empty}>
      <Text style={[styles.emptyText, { color: c.mutedForeground }]}>
        {message}
      </Text>
    </View>
  );
}

function emptyMessageForScope(scope: MyIssuesScope): string {
  switch (scope) {
    case "assigned":
      return "No issues assigned to you.";
    case "created":
      return "You haven't created any issues.";
    case "agents":
      return "No issues assigned to your agents or squads yet.";
  }
}

const styles = StyleSheet.create({
  // flex-1 bg-background
  screen: { flex: 1 },
  // px-4 pt-2 pb-2 flex-row items-center justify-between
  toolbar: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 8,
  },
  // flex-row items-center gap-1 flex-shrink min-w-0
  pillGroup: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    flexShrink: 1,
    minWidth: 0,
  },
  // ml-2 + relative
  filterWrap: { marginLeft: 8, position: "relative" },
  // w-9 px-0
  filterButton: { width: 36, paddingHorizontal: 0 },
  // absolute top-1 right-1 size-1.5 rounded-full
  filterDot: {
    position: "absolute",
    top: 4,
    right: 4,
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  // flex-row flex-wrap gap-1.5 px-4 pb-2
  chipRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 6,
    paddingHorizontal: 16,
    paddingBottom: 8,
  },
  // flex-row items-center gap-1 pl-2.5 pr-2 py-1 rounded-full border
  chip: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    paddingLeft: 10,
    paddingRight: 8,
    paddingTop: 4,
    paddingBottom: 4,
    borderRadius: 9999,
    borderWidth: 1,
  },
  // text-xs
  chipLabel: { fontSize: 12 },
  // px-4 py-2 flex-row items-center gap-2 bg-background
  sectionHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 8,
  },
  // text-xs uppercase tracking-wider font-medium
  sectionTitle: {
    fontSize: 12,
    textTransform: "uppercase",
    letterSpacing: 0.6,
    fontWeight: "500",
  },
  // text-xs text-muted-foreground/60
  sectionCount: { fontSize: 12 },
  // pb-6
  listContent: { paddingBottom: 24 },
  // px-4 gap-3 pt-4
  errorWrap: { paddingHorizontal: 16, gap: 12, paddingTop: 16 },
  // text-sm
  errorText: { fontSize: 14 },
  // flex-1 items-center justify-center px-6
  empty: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  // text-sm text-center
  emptyText: { fontSize: 14, textAlign: "center" },
});

const staticStyles = StyleSheet.create({
  // h-px ml-4 (color comes from the theme border token)
  separator: { height: 1, marginLeft: 16 },
});
