import { useMemo, useState, type ReactNode } from "react";
import { FlatList, Modal, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { BOARD_STATUSES, STATUS_CONFIG } from "@multica/core/issues/config/status";
import { PRIORITY_CONFIG, PRIORITY_ORDER } from "@multica/core/issues/config/priority";
import { useIssueList } from "@multica/core/issues/hooks";
import {
  useMobileIssuesFilterStore,
  useRehydrateMobileIssuesFilters,
  type ActorFilterValue,
} from "@multica/core/issues/stores";
import { filterIssues } from "@multica/core/issues/utils/filter";
import { useAuthStore } from "@multica/core/auth";
import { useMemberList, useAgentList } from "@multica/core/workspace/hooks";
import { useProjectList } from "@multica/core/projects/hooks";
import type { Issue, IssuePriority, IssueStatus } from "@multica/core/types";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { WorkspaceHeader } from "../../components/ui/workspace-header";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

export function IssuesScreen() {
  const navigation = useNavigation<NativeStackNavigationProp<RootStackParamList>>();
  const { workspace } = useMobileWorkspace();
  const userId = useAuthStore((state) => state.user?.id);
  useRehydrateMobileIssuesFilters(userId);
  const [status, setStatus] = useState<IssueStatus>("todo");
  const [filterOpen, setFilterOpen] = useState(false);
  const { data: issues = [], isLoading, isError } = useIssueList(workspace.id);
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

  const actorLabels = useMemo(() => {
    const labels = new Map<string, string>();
    for (const member of members) labels.set(`member:${member.user_id}`, member.name);
    for (const agent of agents) labels.set(`agent:${agent.id}`, agent.name);
    return labels;
  }, [members, agents]);

  const projectLabels = useMemo(
    () => new Map(projects.map((project) => [project.id, project.title])),
    [projects],
  );

  return (
    <Screen>
      <View style={styles.headerRow}>
        <WorkspaceHeader centered />
      </View>
      <View style={styles.statusTabs}>
        {BOARD_STATUSES.map((item) => (
          <Pressable
            key={item}
            onPress={() => setStatus(item)}
            style={[styles.statusTab, item === status && styles.statusTabActive]}
          >
            <Text style={[styles.statusText, item === status && styles.statusTextActive]}>
              {STATUS_CONFIG[item].label}
            </Text>
          </Pressable>
        ))}
      </View>
      <View style={styles.toolbar}>
        <Pressable
          accessibilityLabel="Filter issues"
          accessibilityRole="button"
          onPress={() => setFilterOpen(true)}
          style={({ pressed }) => [styles.filterButton, pressed && styles.pressed]}
        >
          <Text style={styles.filterButtonText}>Filter</Text>
          {activeFilterCount > 0 ? (
            <View style={styles.filterBadge}>
              <Text style={styles.filterBadgeText}>{activeFilterCount}</Text>
            </View>
          ) : null}
        </Pressable>
        {activeFilterCount > 0 ? (
          <Pressable
            accessibilityLabel="Clear issue filters"
            accessibilityRole="button"
            onPress={clearFilters}
            style={({ pressed }) => [styles.clearButton, pressed && styles.pressed]}
          >
            <Text style={styles.clearButtonText}>Clear</Text>
          </Pressable>
        ) : null}
      </View>
      {activeFilterCount > 0 ? (
        <FilterChips
          actorLabels={actorLabels}
          projectLabels={projectLabels}
        />
      ) : null}
      {isLoading ? <LoadingState /> : null}
      {isError ? (
        <EmptyState detail="Pull to retry once the connection is available." title="Unable to load issues" />
      ) : null}
      {!isLoading && !isError ? (
        <FlatList
          contentContainerStyle={styles.list}
          data={filteredIssues}
          keyExtractor={(item) => item.id}
          ListEmptyComponent={
            <IssueListEmpty
              hasFilters={activeFilterCount > 0}
              onClear={clearFilters}
            />
          }
          renderItem={({ item }) => (
            <IssueCard
              issue={item}
              onPress={() => navigation.navigate("IssueDetail", { issueId: item.id })}
            />
          )}
        />
      ) : null}
      <Pressable
        accessibilityLabel="Create issue"
        accessibilityRole="button"
        onPress={() => navigation.navigate("CreateIssue")}
        style={({ pressed }) => [styles.floatingButton, pressed && styles.floatingButtonPressed]}
      >
        <Text style={styles.floatingButtonText}>+</Text>
      </Pressable>
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

function actorLabel(actor: ActorFilterValue, labels: Map<string, string>) {
  return labels.get(actorKey(actor)) ?? `Unknown ${actor.type}`;
}

function FilterChips({
  actorLabels,
  projectLabels,
}: {
  actorLabels: Map<string, string>;
  projectLabels: Map<string, string>;
}) {
  const priorityFilters = useMobileIssuesFilterStore((s) => s.priorityFilters);
  const assigneeFilters = useMobileIssuesFilterStore((s) => s.assigneeFilters);
  const includeNoAssignee = useMobileIssuesFilterStore((s) => s.includeNoAssignee);
  const creatorFilters = useMobileIssuesFilterStore((s) => s.creatorFilters);
  const projectFilters = useMobileIssuesFilterStore((s) => s.projectFilters);
  const includeNoProject = useMobileIssuesFilterStore((s) => s.includeNoProject);
  const togglePriorityFilter = useMobileIssuesFilterStore((s) => s.togglePriorityFilter);
  const toggleAssigneeFilter = useMobileIssuesFilterStore((s) => s.toggleAssigneeFilter);
  const toggleNoAssignee = useMobileIssuesFilterStore((s) => s.toggleNoAssignee);
  const toggleCreatorFilter = useMobileIssuesFilterStore((s) => s.toggleCreatorFilter);
  const toggleProjectFilter = useMobileIssuesFilterStore((s) => s.toggleProjectFilter);
  const toggleNoProject = useMobileIssuesFilterStore((s) => s.toggleNoProject);

  return (
    <ScrollView
      contentContainerStyle={styles.chips}
      horizontal
      showsHorizontalScrollIndicator={false}
    >
      {priorityFilters.map((priority) => (
        <FilterChip
          key={`priority-${priority}`}
          label={`Priority: ${PRIORITY_CONFIG[priority].label}`}
          onRemove={() => togglePriorityFilter(priority)}
        />
      ))}
      {includeNoAssignee ? (
        <FilterChip label="Assignee: No assignee" onRemove={toggleNoAssignee} />
      ) : null}
      {assigneeFilters.map((assignee) => (
        <FilterChip
          key={`assignee-${actorKey(assignee)}`}
          label={`Assignee: ${actorLabel(assignee, actorLabels)}`}
          onRemove={() => toggleAssigneeFilter(assignee)}
        />
      ))}
      {creatorFilters.map((creator) => (
        <FilterChip
          key={`creator-${actorKey(creator)}`}
          label={`Creator: ${actorLabel(creator, actorLabels)}`}
          onRemove={() => toggleCreatorFilter(creator)}
        />
      ))}
      {includeNoProject ? (
        <FilterChip label="Project: No project" onRemove={toggleNoProject} />
      ) : null}
      {projectFilters.map((projectId) => (
        <FilterChip
          key={`project-${projectId}`}
          label={`Project: ${projectLabels.get(projectId) ?? "Unknown project"}`}
          onRemove={() => toggleProjectFilter(projectId)}
        />
      ))}
    </ScrollView>
  );
}

function FilterChip({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <Pressable
      accessibilityLabel={`Remove ${label} filter`}
      accessibilityRole="button"
      onPress={onRemove}
      style={({ pressed }) => [styles.chip, pressed && styles.pressed]}
    >
      <Text numberOfLines={1} style={styles.chipText}>{label}</Text>
      <Text style={styles.chipRemove}>x</Text>
    </Pressable>
  );
}

function IssueListEmpty({
  hasFilters,
  onClear,
}: {
  hasFilters: boolean;
  onClear: () => void;
}) {
  if (!hasFilters) return <EmptyState title="No issues in this status" />;
  return (
    <View style={styles.emptyFiltered}>
      <Text style={styles.emptyTitle}>No matching issues</Text>
      <Text style={styles.emptyDetail}>Try clearing filters or switch status.</Text>
      <Pressable
        accessibilityRole="button"
        onPress={onClear}
        style={({ pressed }) => [styles.emptyClearButton, pressed && styles.pressed]}
      >
        <Text style={styles.emptyClearText}>Clear filters</Text>
      </Pressable>
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

  return (
    <Modal animationType="slide" transparent visible={visible} onRequestClose={onClose}>
      <Pressable style={styles.sheetBackdrop} onPress={onClose} />
      <View style={styles.sheet}>
        <View style={styles.sheetHeader}>
          <Text style={styles.sheetTitle}>Filter issues</Text>
          <Pressable accessibilityRole="button" onPress={onClose} style={styles.sheetDoneButton}>
            <Text style={styles.sheetDoneText}>Done</Text>
          </Pressable>
        </View>
        <ScrollView contentContainerStyle={styles.sheetContent}>
          <FilterSection title="Priority">
            {PRIORITY_ORDER.map((priority) => (
              <FilterOption
                key={priority}
                label={PRIORITY_CONFIG[priority].label}
                onPress={() => togglePriorityFilter(priority)}
                selected={priorityFilters.includes(priority)}
              />
            ))}
          </FilterSection>

          <FilterSection title="Assignee">
            <FilterOption
              label="No assignee"
              onPress={toggleNoAssignee}
              selected={includeNoAssignee}
            />
            {members.map((member) => (
              <FilterOption
                key={`assignee-member-${member.user_id}`}
                label={member.name}
                meta="Member"
                onPress={() => toggleAssigneeFilter({ type: "member", id: member.user_id })}
                selected={assigneeFilters.some((f) => f.type === "member" && f.id === member.user_id)}
              />
            ))}
            {activeAgents.map((agent) => (
              <FilterOption
                key={`assignee-agent-${agent.id}`}
                label={agent.name}
                meta="Agent"
                onPress={() => toggleAssigneeFilter({ type: "agent", id: agent.id })}
                selected={assigneeFilters.some((f) => f.type === "agent" && f.id === agent.id)}
              />
            ))}
          </FilterSection>

          <FilterSection title="Creator">
            {members.map((member) => (
              <FilterOption
                key={`creator-member-${member.user_id}`}
                label={member.name}
                meta="Member"
                onPress={() => toggleCreatorFilter({ type: "member", id: member.user_id })}
                selected={creatorFilters.some((f) => f.type === "member" && f.id === member.user_id)}
              />
            ))}
            {activeAgents.map((agent) => (
              <FilterOption
                key={`creator-agent-${agent.id}`}
                label={agent.name}
                meta="Agent"
                onPress={() => toggleCreatorFilter({ type: "agent", id: agent.id })}
                selected={creatorFilters.some((f) => f.type === "agent" && f.id === agent.id)}
              />
            ))}
          </FilterSection>

          <FilterSection title="Project">
            <FilterOption
              label="No project"
              onPress={toggleNoProject}
              selected={includeNoProject}
            />
            {projects.map((project) => (
              <FilterOption
                key={project.id}
                label={project.title}
                onPress={() => toggleProjectFilter(project.id)}
                selected={projectFilters.includes(project.id)}
              />
            ))}
          </FilterSection>
        </ScrollView>
        <View style={styles.sheetFooter}>
          <Pressable accessibilityRole="button" onPress={clearFilters} style={styles.resetButton}>
            <Text style={styles.resetButtonText}>Reset</Text>
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
    zIndex: 20,
  },
  floatingButton: {
    alignItems: "center",
    backgroundColor: colors.primary,
    borderRadius: 28,
    bottom: spacing.xl,
    elevation: 8,
    height: 56,
    justifyContent: "center",
    position: "absolute",
    right: spacing.xl,
    shadowColor: "#000000",
    shadowOffset: { height: 4, width: 0 },
    shadowOpacity: 0.18,
    shadowRadius: 10,
    width: 56,
    zIndex: 40,
  },
  floatingButtonPressed: {
    opacity: 0.82,
  },
  floatingButtonText: {
    color: colors.primaryForeground,
    fontSize: 32,
    fontWeight: "400",
    lineHeight: 36,
  },
  statusTabs: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  statusTab: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
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
  toolbar: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    marginBottom: spacing.sm,
  },
  filterButton: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 36,
    paddingHorizontal: spacing.md,
  },
  filterButtonText: {
    color: colors.foreground,
    fontSize: 13,
    fontWeight: "500",
  },
  filterBadge: {
    alignItems: "center",
    backgroundColor: colors.primary,
    borderRadius: 10,
    height: 20,
    justifyContent: "center",
    minWidth: 20,
    paddingHorizontal: spacing.xs,
  },
  filterBadgeText: {
    color: colors.primaryForeground,
    fontSize: 11,
    fontWeight: "600",
  },
  clearButton: {
    minHeight: 36,
    justifyContent: "center",
    paddingHorizontal: spacing.sm,
  },
  clearButtonText: {
    color: colors.mutedForeground,
    fontSize: 13,
    fontWeight: "500",
  },
  pressed: {
    opacity: 0.72,
  },
  chips: {
    gap: spacing.sm,
    paddingBottom: spacing.sm,
  },
  chip: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.xs,
    maxWidth: 220,
    minHeight: 32,
    paddingHorizontal: spacing.sm,
  },
  chipText: {
    color: colors.foreground,
    flexShrink: 1,
    fontSize: 12,
    fontWeight: "500",
  },
  chipRemove: {
    color: colors.mutedForeground,
    fontSize: 16,
    lineHeight: 18,
  },
  list: {
    flexGrow: 1,
    gap: spacing.sm,
    paddingBottom: spacing.xl,
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
  filterOptions: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
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
