/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/more/issues.tsx.
 * Workspace-wide Issues page. Mirrors web `packages/views/issues/components/
 * issues-page.tsx`: fetch every issue in the workspace, expose
 * `all / members / agents` scope tabs, group by status, allow status +
 * priority filtering.
 *
 * Scope is a **client-side** filter on `assignee_type` — matches web
 * `issues-page.tsx`. This keeps `issueListOptions(wsId)` workspace-scoped
 * (no scope param on the wire), so `issueKeys.list(wsId)` and
 * `useIssuesRealtime` need no changes.
 *
 * Differences vs My Issues (`(tabs)/my-issues.tsx`):
 *   - Workspace-wide list (all issues), not user-scoped.
 *   - Three scopes are `all / members / agents` (assignee_type pre-filter),
 *     not `assigned / created / agents` (per-user predicates).
 *   - Independent filter store (`useIssuesViewStore`) so workspace-level
 *     filters don't bleed into the per-user view.
 *
 * Filters beyond status/priority (assignee / project / label / creator)
 * are deferred — power-user features with non-trivial picker cost; ship
 * after the parity-critical scope tabs land.
 *
 * Navigation deltas vs iOS: row taps go through `onOpenIssue` and the
 * filter affordance through `onOpenFilter` — both are callback props owned
 * by the route registry (import-cycle rule); the filter sheet itself is a
 * separate route on the iOS side too.
 */
import { useMemo } from "react";
import { SectionList, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type {
  IssuePriority,
  IssueStatus,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Icon } from "@/components/ui/icon";
import { StatusIcon } from "@/components/ui/status-icon";
import { ScreenHeader } from "@/src/screens/screen-header";
import { useNav } from "@/src/navigation/navigator";
import { IssueRow } from "@/components/issue/issue-row";
import { IssuesLoading } from "@/components/issue/issues-loading";
import { issueListOptions } from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  useIssuesViewStore,
  type IssuesScope,
} from "@/data/stores/issues-view-store";
import { useClearFiltersOnWorkspaceChange } from "@/lib/use-clear-filters-on-workspace-change";
import { PRIORITY_LABEL } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { groupIssuesByStatus } from "@/lib/group-issues-by-status";
import { filterIssues } from "@/lib/filter-issues";
import { useThemeColors } from "@/lib/use-theme-colors";
import { withAlpha } from "@/lib/theme";

// Scope tab definitions. Mirrors web `issuesScopeStore`. Counts are NOT
// rendered on the pill labels — web's `IssuesHeader` doesn't show them
// either, and on narrow phones "(123)" appended to each label pushes the
// row past the safe width when the filter icon shares it. Per-status
// counts still appear on the SectionList headers below.
const SCOPES: { value: IssuesScope; label: string }[] = [
  { value: "all", label: "All" },
  { value: "members", label: "Members" },
  { value: "agents", label: "Agents" },
];

export function MoreIssuesScreen({
  onOpenIssue,
  onOpenFilter,
}: {
  onOpenIssue: (issueId: string) => void;
  /** Opens the shared filter sheet; the route lives in the registry. */
  onOpenFilter?: () => void;
}) {
  const nav = useNav<{ pop: () => void }>();
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  const scope = useIssuesViewStore((s) => s.scope);
  const setScope = useIssuesViewStore((s) => s.setScope);
  const statusFilters = useIssuesViewStore((s) => s.statusFilters);
  const priorityFilters = useIssuesViewStore((s) => s.priorityFilters);

  useClearFiltersOnWorkspaceChange(
    useIssuesViewStore.getState().clearFilters,
    wsId,
  );

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    issueListOptions(wsId),
  );

  // Only the active-filter chips need the catalog — sections group on the
  // category the server already resolved onto each issue. (MUL-6243)
  const catalog = useIssueStatuses();

  const allIssues = data ?? [];

  // Scope pre-filter — mirrors web `issues-page.tsx`. Applied before
  // status/priority filtering so chip filters operate on the visible slice.
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

  const filtered = useMemo(
    () => filterIssues(scopedIssues, statusFilters, priorityFilters),
    [scopedIssues, statusFilters, priorityFilters],
  );

  const sections = useMemo(
    () => groupIssuesByStatus(filtered, catalog.statuses),
    [filtered, catalog.statuses],
  );

  const hasActiveFilters =
    statusFilters.length > 0 || priorityFilters.length > 0;

  const showEmptyState = !isLoading && !error && filtered.length === 0;

  return (
    <View style={[styles.screen, { backgroundColor: c.background }]}>
      <ScreenHeader title="Issues" onBack={() => nav.pop()} />
      <ScopeToolbar
        scopes={SCOPES}
        scope={scope}
        onChange={(v) => setScope(v)}
        onOpenFilter={() => onOpenFilter?.()}
        hasActiveFilters={hasActiveFilters}
      />
      {hasActiveFilters ? (
        <ActiveFilterChips
          statusFilters={statusFilters}
          priorityFilters={priorityFilters}
          statusLabelOf={catalog.labelOf}
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
        <SectionList
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
            <IssueRow issue={item} onPress={() => onOpenIssue(item.id)} />
          )}
          refreshing={isRefetching}
          onRefresh={refetch}
        />
      )}
    </View>
  );
}

function ItemSeparator() {
  const c = useThemeColors();
  // h-px bg-border ml-4
  return <View style={[styles.itemSeparator, { backgroundColor: c.border }]} />;
}

/**
 * Outline icon button matching the pill height. Identical to the helper in
 * `(tabs)/my-issues.tsx` for the same reason ScopeToolbar is duplicated:
 * two callers don't justify a shared primitive yet.
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
        <Icon name="options-outline" size={16} color={c.mutedForeground} />
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
 * Toolbar row mirroring web `IssuesHeader`
 * (`packages/views/issues/components/issues-header.tsx`): left-aligned
 * scope pill group + right-side Filter icon (red dot on active filters).
 * Identical to the equivalent in `(tabs)/my-issues.tsx` — kept duplicated
 * because the threshold for a shared `components/ui/` primitive is 3 callers,
 * and two callers don't justify the abstraction yet.
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
      <View style={styles.scopeRow}>
        {scopes.map((s) => {
          const active = scope === s.value;
          return (
            <Button
              key={s.value}
              variant="outline"
              size="sm"
              onPress={() => onChange(s.value)}
              style={active ? { backgroundColor: c.accent } : null}
              accessibilityState={{ selected: active }}
            >
              <Text
                numberOfLines={1}
                style={[
                  styles.scopeLabel,
                  { color: active ? c.accentForeground : c.mutedForeground },
                ]}
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
    <View style={styles.chipsWrap}>
      {statusFilters.map((s) => (
        <Chip
          key={`s-${s}`}
          label={statusLabelOf(s)}
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
  const c = useThemeColors();
  return (
    <Pressable
      onPress={onClear}
      style={({ pressed }) => [
        // flex-row items-center gap-1 pl-2.5 pr-2 py-1 rounded-full border
        styles.chip,
        { borderColor: c.border, backgroundColor: withAlpha(c.secondary, 0.4) },
        pressed ? { backgroundColor: c.secondary } : null,
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
      <Text style={[styles.sectionLabel, { color: c.mutedForeground }]}>
        {catalog.labelOf(status)}
      </Text>
      <Text style={{ fontSize: 12, color: withAlpha(c.mutedForeground, 0.6) }}>
        {count}
      </Text>
    </View>
  );
}

function EmptyState({ message }: { message: string }) {
  const c = useThemeColors();
  return (
    <View style={styles.emptyWrap}>
      <Text style={[styles.emptyText, { color: c.mutedForeground }]}>
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

const styles = StyleSheet.create({
  screen: { flex: 1 },
  // flex-row items-center justify-between px-4 pt-2 pb-2
  toolbar: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 8,
  },
  // flex-row items-center gap-1 flex-shrink min-w-0
  scopeRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    flexShrink: 1,
    minWidth: 0,
  },
  scopeLabel: { fontSize: 14 },
  // relative ml-2
  filterWrap: {
    position: "relative",
    marginLeft: 8,
  },
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
  chipsWrap: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 6,
    paddingHorizontal: 16,
    paddingBottom: 8,
  },
  // pl-2.5 pr-2 py-1 rounded-full border
  chip: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    paddingLeft: 10,
    paddingRight: 8,
    paddingVertical: 4,
    borderRadius: 999,
    borderWidth: 1,
  },
  // text-xs
  chipLabel: { fontSize: 12 },
  listContent: { paddingBottom: 24 },
  itemSeparator: { height: 1, marginLeft: 16 },
  // flex-row items-center gap-2 px-4 py-2
  sectionHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 8,
  },
  // text-xs uppercase tracking-wider font-medium
  sectionLabel: {
    fontSize: 12,
    textTransform: "uppercase",
    letterSpacing: 0.6,
    fontWeight: "500",
  },
  // px-4 gap-3 pt-4
  errorWrap: { paddingHorizontal: 16, gap: 12, paddingTop: 16 },
  errorText: { fontSize: 14 },
  // flex-1 items-center justify-center px-6
  emptyWrap: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  // text-sm text-center
  emptyText: { fontSize: 14, textAlign: "center" },
});
