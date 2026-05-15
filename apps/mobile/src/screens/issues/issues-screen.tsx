import { useMemo, useRef, useState, type ReactNode } from "react";
import { ActivityIndicator, FlatList, Modal, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from "react-native";
import type { GestureResponderEvent } from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { ChevronDown, Filter, Menu, Plus, Search as SearchIcon } from "lucide-react-native";
import { BOARD_STATUSES } from "@multica/core/issues/config/status";
import { PRIORITY_ORDER } from "@multica/core/issues/config/priority";
import { useIssueList } from "@multica/core/issues/hooks";
import { useLoadMoreByStatusForWorkspace } from "@multica/core/issues/mutations";
import {
  useMobileIssuesFilterStore,
  useRehydrateMobileIssuesFilters,
} from "@multica/core/issues/stores";
import { filterIssues, type ActorFilterValue } from "@multica/core/issues/utils/filter";
import { useAuthStore } from "@multica/core/auth";
import { useMemberList, useAgentList } from "@multica/core/workspace/hooks";
import { useProjectList } from "@multica/core/projects/hooks";
import type { Issue, IssuePriority, IssueStatus } from "@multica/core/types";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { FloatingActionMenu } from "../../components/ui/floating-action-menu";
import { EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { WorkspaceHeader } from "../../components/ui/workspace-header";
import { formatIssuePriority, formatIssueStatus } from "../../i18n/format";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

export function IssuesScreen() {
  const { t } = useTranslation();
  const navigation = useNavigation<NativeStackNavigationProp<RootStackParamList>>();
  const { workspace } = useMobileWorkspace();
  const userId = useAuthStore((state) => state.user?.id);
  useRehydrateMobileIssuesFilters(userId);
  const statusListRef = useRef<FlatList<IssueStatus>>(null);
  const [status, setStatus] = useState<IssueStatus>("todo");
  const [filterOpen, setFilterOpen] = useState(false);
  const {
    data: issues = [],
    isLoading,
    isError,
    isRefetching,
    refetch,
  } = useIssueList(workspace.id);
  const {
    hasMore,
    isLoading: isLoadingMore,
    loadMore,
  } = useLoadMoreByStatusForWorkspace(workspace.id, status);
  const { data: members = [] } = useMemberList(workspace.id);
  const { data: agents = [] } = useAgentList(workspace.id);
  const { data: projects = [] } = useProjectList(workspace.id);

  const priorityFilters = useMobileIssuesFilterStore((s) => s.priorityFilters);
  const assigneeFilters = useMobileIssuesFilterStore((s) => s.assigneeFilters);
  const includeNoAssignee = useMobileIssuesFilterStore((s) => s.includeNoAssignee);
  const creatorFilters = useMobileIssuesFilterStore((s) => s.creatorFilters);
  const projectFilters = useMobileIssuesFilterStore((s) => s.projectFilters);
  const includeNoProject = useMobileIssuesFilterStore((s) => s.includeNoProject);
  const clearFilters = useMobileIssuesFilterStore((s) => s.clearFilters);

  const activeFilterCount = getActiveFilterCount({
    priorityFilters,
    assigneeFilters,
    includeNoAssignee,
    creatorFilters,
    projectFilters,
    includeNoProject,
  });
  const filteredIssues = useMemo(
    () =>
      filterIssues(issues.filter((issue) => issue.status === status), {
        statusFilters: [],
        priorityFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
        projectFilters,
        includeNoProject,
        labelFilters: [],
      }),
    [
      issues,
      status,
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
    ],
  );

  return (
    <Screen>
      <View style={styles.headerRow}>
        <WorkspaceHeader centered />
      </View>
      <View style={styles.statusToolbar}>
        <FlatList
          contentContainerStyle={styles.statusTabsContent}
          data={BOARD_STATUSES}
          horizontal
          initialNumToRender={BOARD_STATUSES.length}
          keyExtractor={(item) => item}
          onScrollToIndexFailed={({ averageItemLength, index }) => {
            statusListRef.current?.scrollToOffset({
              animated: true,
              offset: Math.max(0, averageItemLength * index),
            });
          }}
          ref={statusListRef}
          renderItem={({ item, index }) => (
            <Pressable
              accessibilityRole="button"
              onPress={() => {
                setStatus(item);
                statusListRef.current?.scrollToIndex({
                  animated: true,
                  index,
                  viewPosition: 0.5,
                });
              }}
              style={({ pressed }) => [
                styles.statusTab,
                item === status && styles.statusTabActive,
                pressed && styles.pressed,
              ]}
            >
              <Text style={[styles.statusText, item === status && styles.statusTextActive]}>
                {formatIssueStatus(t, item)}
              </Text>
            </Pressable>
          )}
          showsHorizontalScrollIndicator={false}
          style={styles.statusTabsList}
        />
        <Pressable
          accessibilityLabel={t("issues.filter")}
          accessibilityRole="button"
          onPress={(event: GestureResponderEvent) => {
            event.stopPropagation();
            setFilterOpen(true);
          }}
          style={({ pressed }) => [styles.tabsFilterButton, pressed && styles.pressed]}
        >
          <Filter color={colors.foreground} size={18} strokeWidth={2} />
          {activeFilterCount > 0 ? (
            <View style={styles.tabsFilterBadge}>
              <Text style={styles.tabsFilterBadgeText}>{activeFilterCount}</Text>
            </View>
          ) : null}
        </Pressable>
      </View>
      {isLoading ? <LoadingState /> : null}
      {isError ? (
        <EmptyState detail={t("common.pull_to_retry")} title={t("issues.unable_to_load")} />
      ) : null}
      {!isLoading && !isError ? (
        <FlatList
          contentContainerStyle={styles.list}
          data={filteredIssues}
          keyExtractor={(item) => item.id}
          ListFooterComponent={
            <IssueListFooter
              hasMore={hasMore}
              loading={isLoadingMore}
            />
          }
          ListEmptyComponent={
            <IssueListEmpty
              hasFilters={activeFilterCount > 0}
              onClear={clearFilters}
            />
          }
          onEndReached={() => {
            if (hasMore && !isLoadingMore) void loadMore();
          }}
          onEndReachedThreshold={0.4}
          onRefresh={() => {
            void refetch();
          }}
          refreshing={isRefetching && !isLoading}
          renderItem={({ item }) => (
            <IssueCard
              issue={item}
              onPress={() => navigation.navigate("IssueDetail", { issueId: item.id })}
            />
          )}
        />
      ) : null}
      <FloatingActionMenu
        mainIcon={<Menu color={colors.primaryForeground} size={25} strokeWidth={2.3} />}
        actions={[
          {
            key: "create",
            label: t("issues.new_issue"),
            icon: <Plus color={colors.primaryForeground} size={21} strokeWidth={2.4} />,
            onPress: () => navigation.navigate("CreateIssue"),
          },
          {
            key: "search",
            label: t("common.search"),
            icon: <SearchIcon color={colors.primaryForeground} size={20} strokeWidth={2.3} />,
            onPress: () => navigation.navigate("Search"),
          },
        ]}
      />
      <IssueFilterSheet
        agents={agents}
        includeNoAssignee={includeNoAssignee}
        includeNoProject={includeNoProject}
        members={members}
        onClose={() => setFilterOpen(false)}
        projects={projects}
        visible={filterOpen}
      />
    </Screen>
  );
}

function getActiveFilterCount(state: {
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
}) {
  let count = 0;
  if (state.priorityFilters.length > 0) count++;
  if (state.assigneeFilters.length > 0 || state.includeNoAssignee) count++;
  if (state.creatorFilters.length > 0) count++;
  if (state.projectFilters.length > 0 || state.includeNoProject) count++;
  return count;
}

function actorKey(actor: ActorFilterValue) {
  return `${actor.type}:${actor.id}`;
}

function fuzzyMatch(label: string, query: string) {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return true;
  let index = 0;
  for (const char of label.toLowerCase()) {
    if (char === normalized[index]) index += 1;
    if (index === normalized.length) return true;
  }
  return false;
}

function IssueListEmpty({
  hasFilters,
  onClear,
}: {
  hasFilters: boolean;
  onClear: () => void;
}) {
  const { t } = useTranslation();
  if (!hasFilters) return <EmptyState title={t("issues.no_in_status")} />;
  return (
    <View style={styles.emptyFiltered}>
      <Text style={styles.emptyTitle}>{t("issues.no_matching")}</Text>
      <Text style={styles.emptyDetail}>{t("issues.try_clear_filters")}</Text>
      <Pressable
        accessibilityRole="button"
        onPress={onClear}
        style={({ pressed }) => [styles.emptyClearButton, pressed && styles.pressed]}
      >
        <Text style={styles.emptyClearText}>{t("issues.clear_filters")}</Text>
      </Pressable>
    </View>
  );
}

function IssueListFooter({
  hasMore,
  loading,
}: {
  hasMore: boolean;
  loading: boolean;
}) {
  const { t } = useTranslation();
  if (!hasMore && !loading) return null;
  return (
    <View style={styles.listFooter}>
      {loading ? (
        <>
          <ActivityIndicator color={colors.mutedForeground} />
          <Text style={styles.listFooterText}>{t("issues.loading_more")}</Text>
        </>
      ) : null}
    </View>
  );
}

function IssueFilterSheet({
  agents,
  includeNoAssignee,
  includeNoProject,
  members,
  onClose,
  projects,
  visible,
}: {
  agents: Array<{ id: string; name: string; archived_at?: string | null }>;
  includeNoAssignee: boolean;
  includeNoProject: boolean;
  members: Array<{ user_id: string; name: string }>;
  onClose: () => void;
  projects: Array<{ id: string; title: string }>;
  visible: boolean;
}) {
  const { t } = useTranslation();
  const priorityFilters = useMobileIssuesFilterStore((s) => s.priorityFilters);
  const assigneeFilters = useMobileIssuesFilterStore((s) => s.assigneeFilters);
  const creatorFilters = useMobileIssuesFilterStore((s) => s.creatorFilters);
  const projectFilters = useMobileIssuesFilterStore((s) => s.projectFilters);
  const togglePriorityFilter = useMobileIssuesFilterStore((s) => s.togglePriorityFilter);
  const toggleAssigneeFilter = useMobileIssuesFilterStore((s) => s.toggleAssigneeFilter);
  const toggleNoAssignee = useMobileIssuesFilterStore((s) => s.toggleNoAssignee);
  const toggleCreatorFilter = useMobileIssuesFilterStore((s) => s.toggleCreatorFilter);
  const toggleProjectFilter = useMobileIssuesFilterStore((s) => s.toggleProjectFilter);
  const toggleNoProject = useMobileIssuesFilterStore((s) => s.toggleNoProject);
  const clearFilters = useMobileIssuesFilterStore((s) => s.clearFilters);
  const activeAgents = agents.filter((agent) => !agent.archived_at);
  const assigneeSelectedKeys = useMemo(
    () => new Set(assigneeFilters.map(actorKey)),
    [assigneeFilters],
  );
  const creatorSelectedKeys = useMemo(
    () => new Set(creatorFilters.map(actorKey)),
    [creatorFilters],
  );
  const projectSelectedKeys = useMemo(
    () => new Set(projectFilters),
    [projectFilters],
  );
  const assigneeOptions = useMemo(
    () => [
      ...members.map((member) => ({
        id: member.user_id,
        type: "member" as const,
        label: member.name,
        meta: t("issues.member"),
      })),
      ...activeAgents.map((agent) => ({
        id: agent.id,
        type: "agent" as const,
        label: agent.name,
        meta: t("issues.agent"),
      })),
    ],
    [activeAgents, members, t],
  );
  const projectOptions = useMemo(
    () => projects.map((project) => ({ id: project.id, label: project.title })),
    [projects],
  );

  return (
    <Modal animationType="slide" transparent visible={visible} onRequestClose={onClose}>
      <Pressable style={styles.sheetBackdrop} onPress={onClose} />
      <View style={styles.sheet}>
        <View style={styles.sheetHeader}>
          <Text style={styles.sheetTitle}>{t("issues.filter")}</Text>
          <Pressable accessibilityRole="button" onPress={onClose} style={styles.sheetDoneButton}>
            <Text style={styles.sheetDoneText}>{t("common.done")}</Text>
          </Pressable>
        </View>
        <ScrollView contentContainerStyle={styles.sheetContent}>
          <FilterSection title={t("issues.priority")}>
            {PRIORITY_ORDER.map((priority) => (
              <FilterOption
                key={priority}
                label={formatIssuePriority(t, priority)}
                onPress={() => togglePriorityFilter(priority)}
                selected={priorityFilters.includes(priority)}
              />
            ))}
          </FilterSection>

          <FilterDroplistSection
            emptyLabel={t("issues.any_assignee")}
            footer={
              <FilterOption
                label={t("issues.no_assignee")}
                onPress={toggleNoAssignee}
                selected={includeNoAssignee}
              />
            }
            options={assigneeOptions}
            placeholder={t("issues.search_assignees")}
            selectedKeys={assigneeSelectedKeys}
            title={t("issues.assignee")}
            toKey={(value) => actorKey(value)}
            onToggle={(value) => toggleAssigneeFilter({ type: value.type, id: value.id })}
          />

          <FilterDroplistSection
            emptyLabel={t("issues.any_creator")}
            options={assigneeOptions}
            placeholder={t("issues.search_creators")}
            selectedKeys={creatorSelectedKeys}
            title={t("issues.creator")}
            toKey={(value) => actorKey(value)}
            onToggle={(value) => toggleCreatorFilter({ type: value.type, id: value.id })}
          />

          <FilterDroplistSection
            emptyLabel={t("issues.any_project")}
            footer={
              <FilterOption
                label={t("issues.no_project")}
                onPress={toggleNoProject}
                selected={includeNoProject}
              />
            }
            options={projectOptions}
            placeholder={t("issues.search_projects")}
            selectedKeys={projectSelectedKeys}
            title={t("issues.project")}
            toKey={(value) => value.id}
            onToggle={(value) => toggleProjectFilter(value.id)}
          />
        </ScrollView>
        <View style={styles.sheetFooter}>
          <Pressable accessibilityRole="button" onPress={clearFilters} style={styles.resetButton}>
            <Text style={styles.resetButtonText}>{t("common.reset")}</Text>
          </Pressable>
        </View>
      </View>
    </Modal>
  );
}

function FilterSection({ children, title }: { children: ReactNode; title: string }) {
  return (
    <View style={styles.filterSection}>
      <Text style={styles.filterSectionTitle}>{title}</Text>
      <View style={styles.filterOptions}>{children}</View>
    </View>
  );
}

function FilterDroplistSection<T extends { id: string; label: string; meta?: string }>({
  emptyLabel,
  footer,
  options,
  placeholder,
  selectedKeys,
  title,
  toKey,
  onToggle,
}: {
  emptyLabel: string;
  footer?: ReactNode;
  options: T[];
  placeholder: string;
  selectedKeys: Set<string>;
  title: string;
  toKey: (value: T) => string;
  onToggle: (value: T) => void;
}) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const [query, setQuery] = useState("");
  const visibleOptions = useMemo(() => {
    const matched = options.filter((option) => fuzzyMatch(option.label, query));
    return matched.sort((a, b) => {
      const aSelected = selectedKeys.has(toKey(a));
      const bSelected = selectedKeys.has(toKey(b));
      if (aSelected === bSelected) return a.label.localeCompare(b.label);
      return aSelected ? -1 : 1;
    });
  }, [options, query, selectedKeys, toKey]);
  const summary = selectedKeys.size > 0 ? String(selectedKeys.size) : emptyLabel;

  return (
    <View style={styles.filterSection}>
      <Pressable
        accessibilityRole="button"
        onPress={() => setExpanded((value) => !value)}
        style={({ pressed }) => [styles.droplistHeader, pressed && styles.pressed]}
      >
        <View style={styles.optionTextGroup}>
          <Text style={styles.filterSectionTitle}>{title}</Text>
          <Text style={styles.droplistSummary}>{summary}</Text>
        </View>
        <ChevronDown
          color={colors.mutedForeground}
          size={18}
          style={expanded ? styles.droplistChevronOpen : undefined}
        />
      </Pressable>
      {expanded ? (
        <View style={styles.filterOptions}>
          {footer}
          <View style={styles.searchWrap}>
            <TextInput
              autoCapitalize="none"
              autoCorrect={false}
              onChangeText={setQuery}
              placeholder={placeholder}
              placeholderTextColor={colors.mutedForeground}
              style={styles.searchInput}
              value={query}
            />
          </View>
          {visibleOptions.length > 0 ? (
            visibleOptions.map((option) => {
              const key = toKey(option);
              return (
                <FilterOption
                  key={key}
                  label={option.label}
                  meta={option.meta}
                  onPress={() => onToggle(option)}
                  selected={selectedKeys.has(key)}
                />
              );
            })
          ) : (
            <Text style={styles.noResultsText}>{t("common.no_results")}</Text>
          )}
        </View>
      ) : null}
    </View>
  );
}

function FilterOption({
  label,
  meta,
  onPress,
  selected,
}: {
  label: string;
  meta?: string;
  onPress: () => void;
  selected: boolean;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.filterOption, pressed && styles.pressed]}
    >
      <View style={styles.optionTextGroup}>
        <Text numberOfLines={1} style={styles.filterOptionLabel}>{label}</Text>
        {meta ? <Text style={styles.filterOptionMeta}>{meta}</Text> : null}
      </View>
      <View style={[styles.checkbox, selected && styles.checkboxSelected]}>
        {selected ? <View style={styles.checkboxDot} /> : null}
      </View>
    </Pressable>
  );
}

function IssueCard({ issue, onPress }: { issue: Issue; onPress: () => void }) {
  return (
    <Pressable onPress={onPress} style={styles.card}>
      <View style={styles.cardHeader}>
        <Text style={styles.identifier}>{issue.identifier}</Text>
        <Text style={styles.priority}>{issue.priority}</Text>
      </View>
      <Text style={styles.issueTitle}>{issue.title}</Text>
      <Text style={styles.meta}>
        Updated {new Date(issue.updated_at).toLocaleDateString()}
      </Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  headerRow: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "center",
    minHeight: 44,
    position: "relative",
    zIndex: 100,
  },
  statusToolbar: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  statusTabsList: {
    flex: 1,
  },
  statusTabsContent: {
    gap: spacing.sm,
    paddingRight: spacing.xs,
  },
  statusTab: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexShrink: 0,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  statusTabActive: {
    backgroundColor: colors.primary,
    borderColor: colors.primary,
  },
  statusText: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  statusTextActive: {
    color: colors.primaryForeground,
  },
  tabsFilterButton: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexShrink: 0,
    height: 36,
    justifyContent: "center",
    width: 36,
  },
  tabsFilterBadge: {
    alignItems: "center",
    backgroundColor: colors.primary,
    borderRadius: 9,
    height: 18,
    justifyContent: "center",
    minWidth: 18,
    paddingHorizontal: 3,
    position: "absolute",
    right: -5,
    top: -5,
  },
  tabsFilterBadgeText: {
    color: colors.primaryForeground,
    fontSize: 10,
    fontWeight: "700",
  },
  pressed: {
    opacity: 0.72,
  },
  list: {
    flexGrow: 1,
    gap: spacing.sm,
    paddingBottom: spacing.xl,
  },
  listFooter: {
    alignItems: "center",
    gap: spacing.sm,
    minHeight: 56,
    justifyContent: "center",
    paddingVertical: spacing.md,
  },
  listFooterText: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  emptyFiltered: {
    alignItems: "center",
    flex: 1,
    gap: spacing.sm,
    justifyContent: "center",
    padding: spacing.xl,
  },
  emptyTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "500",
  },
  emptyDetail: {
    color: colors.mutedForeground,
    fontSize: 14,
    textAlign: "center",
  },
  emptyClearButton: {
    backgroundColor: colors.primary,
    borderRadius: radii.md,
    marginTop: spacing.sm,
    minHeight: 40,
    justifyContent: "center",
    paddingHorizontal: spacing.lg,
  },
  emptyClearText: {
    color: colors.primaryForeground,
    fontSize: 14,
    fontWeight: "500",
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
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
  },
  identifier: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  priority: {
    color: colors.warning,
    fontSize: 12,
    fontWeight: "500",
    textTransform: "capitalize",
  },
  issueTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "500",
    lineHeight: 22,
  },
  meta: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  sheetBackdrop: {
    backgroundColor: "#00000033",
    flex: 1,
  },
  sheet: {
    backgroundColor: colors.background,
    borderTopLeftRadius: 18,
    borderTopRightRadius: 18,
    bottom: 0,
    left: 0,
    maxHeight: "82%",
    position: "absolute",
    right: 0,
  },
  sheetHeader: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    justifyContent: "space-between",
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
  sheetTitle: {
    color: colors.foreground,
    fontSize: 17,
    fontWeight: "600",
  },
  sheetDoneButton: {
    minHeight: 36,
    justifyContent: "center",
    paddingHorizontal: spacing.sm,
  },
  sheetDoneText: {
    color: colors.info,
    fontSize: 15,
    fontWeight: "600",
  },
  sheetContent: {
    gap: spacing.lg,
    padding: spacing.lg,
    paddingBottom: spacing.xl,
  },
  filterSection: {
    gap: spacing.sm,
  },
  filterSectionTitle: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
    textTransform: "uppercase",
  },
  droplistHeader: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    justifyContent: "space-between",
    minHeight: 56,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  droplistSummary: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "500",
    marginTop: 2,
  },
  droplistChevronOpen: {
    transform: [{ rotate: "180deg" }],
  },
  filterOptions: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  searchWrap: {
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    padding: spacing.sm,
  },
  searchInput: {
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    color: colors.foreground,
    fontSize: 15,
    minHeight: 38,
    paddingHorizontal: spacing.md,
    paddingVertical: 0,
  },
  noResultsText: {
    color: colors.mutedForeground,
    fontSize: 14,
    padding: spacing.lg,
    textAlign: "center",
  },
  filterOption: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
    minHeight: 48,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  optionTextGroup: {
    flex: 1,
    minWidth: 0,
  },
  filterOptionLabel: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "500",
  },
  filterOptionMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
    marginTop: 2,
  },
  checkbox: {
    alignItems: "center",
    borderColor: colors.border,
    borderRadius: radii.sm,
    borderWidth: StyleSheet.hairlineWidth,
    height: 22,
    justifyContent: "center",
    width: 22,
  },
  checkboxSelected: {
    backgroundColor: colors.primary,
    borderColor: colors.primary,
  },
  checkboxDot: {
    backgroundColor: colors.primaryForeground,
    borderRadius: 4,
    height: 8,
    width: 8,
  },
  sheetFooter: {
    borderTopColor: colors.border,
    borderTopWidth: StyleSheet.hairlineWidth,
    padding: spacing.lg,
  },
  resetButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    minHeight: 44,
    justifyContent: "center",
  },
  resetButtonText: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "600",
  },
});
