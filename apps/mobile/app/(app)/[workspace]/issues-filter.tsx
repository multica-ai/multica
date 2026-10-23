/**
 * Status + Priority filter sheet — presented as a formSheet by the parent
 * Stack. Shared by My Issues, the workspace-wide Issues page and a project's
 * issue list; which view-store to read/write is selected by the `scope` URL
 * param.
 *
 * Routes that open this sheet:
 *   - /[workspace]/issues-filter?scope=my   →  useMyIssuesViewStore
 *   - /[workspace]/issues-filter?scope=all  →  useIssuesViewStore
 *   - /[workspace]/issues-filter?scope=project&projectId=<id>
 *                                           →  useProjectIssuesViewStore
 *
 * Self-contained: reads/writes the store directly, no callback passing.
 */
import { Pressable, ScrollView, View } from "react-native";
import { useLocalSearchParams } from "expo-router";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Switch } from "@/components/ui/switch";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { useIssuesViewStore } from "@/data/stores/issues-view-store";
import { useMyIssuesViewStore } from "@/data/stores/my-issues-view-store";
import {
  DEFAULT_PROJECT_ISSUES_VIEW,
  useProjectIssuesViewStore,
} from "@/data/stores/project-issues-view-store";
import type { IssueFilterPrefs } from "@/data/stores/issue-view-prefs";
import { statusOptions } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

// Mirrors PRIORITY_ORDER in packages/core/issues/config/priority.ts.
const PRIORITY_ORDER: IssuePriority[] = [
  "urgent",
  "high",
  "medium",
  "low",
  "none",
];

const PRIORITY_LABEL: Record<IssuePriority, string> = {
  urgent: "issues:priority.urgent",
  high: "issues:priority.high",
  medium: "issues:priority.medium",
  low: "issues:priority.low",
  none: "issues:priority.none",
};

type Scope = "my" | "all" | "project";

export default function IssuesFilterRoute() {
  const { scope, projectId } = useLocalSearchParams<{
    scope?: string;
    projectId?: string;
  }>();
  const { t } = useT("issues");
  const resolvedScope: Scope =
    scope === "all" ? "all" : scope === "project" && projectId ? "project" : "my";

  const target = useFilterTarget(resolvedScope, projectId ?? "");
  const { statusFilters, priorityFilters, showCompleted } = target;
  // Same option list the status picker offers, so every status a user can set
  // is also a status they can filter by. (MUL-6243)
  const catalog = useIssueStatuses();
  const statusChoices = statusOptions(catalog);

  const onToggleStatus = target.toggleStatusFilter;
  const onTogglePriority = target.togglePriorityFilter;
  const onToggleShowCompleted = () => target.setShowCompleted(!showCompleted);
  const onClearFilters = target.clearFilters;

  const hasActive = statusFilters.length > 0 || priorityFilters.length > 0;

  return (
    <View className="flex-1">
      <View className="flex-row items-center justify-between px-4 pt-4 pb-3">
        <Text className="text-base font-semibold text-foreground">
          {t("filters.title")}
        </Text>
        {hasActive ? (
          <Pressable
            onPress={onClearFilters}
            hitSlop={8}
            className="px-2 py-1 active:opacity-60"
          >
            <Text className="text-sm text-primary font-medium">
              {t("filters.clear")}
            </Text>
          </Pressable>
        ) : null}
      </View>
      <ScrollView className="flex-1" showsVerticalScrollIndicator={false}>
        <SectionLabel>{t("filters.status")}</SectionLabel>
        {/* The whole row is the control so the target spans the sheet width;
            the switch only mirrors its state. */}
        <Pressable
          onPress={onToggleShowCompleted}
          accessibilityRole="switch"
          accessibilityState={{ checked: showCompleted }}
          className="flex-row items-center gap-3 px-4 py-2.5 active:bg-secondary"
        >
          <Text className="flex-1 text-sm text-foreground">
            {t("filters.show_completed")}
          </Text>
          <View
            pointerEvents="none"
            accessibilityElementsHidden
            importantForAccessibility="no-hide-descendants"
          >
            <Switch checked={showCompleted} onCheckedChange={() => {}} />
          </View>
        </Pressable>
        {statusChoices.map((option) => {
          const checked = statusFilters.includes(option.key);
          return (
            <Pressable
              key={option.key}
              onPress={() => onToggleStatus(option.key)}
              className={cn(
                "flex-row items-center gap-3 px-4 py-2.5 active:bg-secondary",
                checked && "bg-secondary/60",
              )}
            >
              <StatusIcon
                status={option.key}
                category={option.category}
                icon={option.icon} color={option.color}
                size={16}
              />
              <Text className="flex-1 text-sm text-foreground">
                {catalog.labelOf(option.key)}
              </Text>
              <CheckMark checked={checked} />
            </Pressable>
          );
        })}

        <SectionLabel>{t("filters.priority")}</SectionLabel>
        {PRIORITY_ORDER.map((priority) => {
          const checked = priorityFilters.includes(priority);
          return (
            <Pressable
              key={priority}
              onPress={() => onTogglePriority(priority)}
              className={cn(
                "flex-row items-center gap-3 px-4 py-2.5 active:bg-secondary",
                checked && "bg-secondary/60",
              )}
            >
              <PriorityIcon priority={priority} />
              <Text className="flex-1 text-sm text-foreground">
                {t(PRIORITY_LABEL[priority])}
              </Text>
              <CheckMark checked={checked} />
            </Pressable>
          );
        })}
      </ScrollView>
    </View>
  );
}

interface FilterTarget extends IssueFilterPrefs {
  toggleStatusFilter: (status: IssueStatus) => void;
  togglePriorityFilter: (priority: IssuePriority) => void;
  setShowCompleted: (showCompleted: boolean) => void;
  clearFilters: () => void;
}

function useFilterTarget(scope: Scope, projectId: string): FilterTarget {
  const allStatus = useIssuesViewStore((s) => s.statusFilters);
  const allPriority = useIssuesViewStore((s) => s.priorityFilters);
  const allCompleted = useIssuesViewStore((s) => s.showCompleted);
  const myStatus = useMyIssuesViewStore((s) => s.statusFilters);
  const myPriority = useMyIssuesViewStore((s) => s.priorityFilters);
  const myCompleted = useMyIssuesViewStore((s) => s.showCompleted);
  const project =
    useProjectIssuesViewStore((s) => s.byProject[projectId]) ??
    DEFAULT_PROJECT_ISSUES_VIEW;

  if (scope === "project") {
    const store = useProjectIssuesViewStore.getState();
    return {
      ...project,
      toggleStatusFilter: (status) => store.toggleStatusFilter(projectId, status),
      togglePriorityFilter: (priority) =>
        store.togglePriorityFilter(projectId, priority),
      setShowCompleted: (value) => store.setShowCompleted(projectId, value),
      clearFilters: () => store.clearFilters(projectId),
    };
  }
  const store =
    scope === "all"
      ? useIssuesViewStore.getState()
      : useMyIssuesViewStore.getState();
  return {
    statusFilters: scope === "all" ? allStatus : myStatus,
    priorityFilters: scope === "all" ? allPriority : myPriority,
    showCompleted: scope === "all" ? allCompleted : myCompleted,
    toggleStatusFilter: store.toggleStatusFilter,
    togglePriorityFilter: store.togglePriorityFilter,
    setShowCompleted: store.setShowCompleted,
    clearFilters: store.clearFilters,
  };
}

function SectionLabel({ children }: { children: string }) {
  return (
    <View className="px-4 pt-3 pb-1.5">
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {children}
      </Text>
    </View>
  );
}

function CheckMark({ checked }: { checked: boolean }) {
  if (!checked) return null;
  return <Text className="text-sm text-primary font-semibold">✓</Text>;
}
