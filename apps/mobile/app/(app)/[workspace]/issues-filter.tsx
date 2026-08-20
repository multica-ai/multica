/**
 * Status + Priority filter sheet — presented as a formSheet by the parent
 * Stack. Shared by My Issues and the workspace-wide Issues page; which
 * view-store to read/write is selected by the `scope` URL param.
 *
 * Routes that open this sheet:
 *   - /[workspace]/issues-filter?scope=my   →  useMyIssuesViewStore
 *   - /[workspace]/issues-filter?scope=all  →  useIssuesViewStore
 *
 * Self-contained: reads/writes the store directly, no callback passing.
 */
import { Pressable, ScrollView, View } from "react-native";
import { useLocalSearchParams } from "expo-router";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { useIssuesViewStore } from "@/data/stores/issues-view-store";
import { useMyIssuesViewStore } from "@/data/stores/my-issues-view-store";
import { statusOptions } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { cn } from "@/lib/utils";

// Mirrors PRIORITY_ORDER in packages/core/issues/config/priority.ts.
const PRIORITY_ORDER: IssuePriority[] = [
  "urgent",
  "high",
  "medium",
  "low",
  "none",
];

// Label map duplicated across several mobile files — out of scope to
// consolidate per the SheetShell migration plan.
const PRIORITY_LABEL: Record<IssuePriority, string> = {
  urgent: "Urgent",
  high: "High",
  medium: "Medium",
  low: "Low",
  none: "No priority",
};

type Scope = "my" | "all";

export default function IssuesFilterRoute() {
  const { scope } = useLocalSearchParams<{ scope?: string }>();
  const resolvedScope: Scope = scope === "all" ? "all" : "my";

  const statusFilters = useScopedFilters(resolvedScope, "status");
  const priorityFilters = useScopedFilters(resolvedScope, "priority");
  // Same option list the status picker offers, so every status a user can set
  // is also a status they can filter by. (MUL-6243)
  const catalog = useIssueStatuses();
  const statusChoices = statusOptions(catalog);

  const onToggleStatus = (s: IssueStatus) => {
    if (resolvedScope === "all") {
      useIssuesViewStore.getState().toggleStatusFilter(s);
    } else {
      useMyIssuesViewStore.getState().toggleStatusFilter(s);
    }
  };
  const onTogglePriority = (p: IssuePriority) => {
    if (resolvedScope === "all") {
      useIssuesViewStore.getState().togglePriorityFilter(p);
    } else {
      useMyIssuesViewStore.getState().togglePriorityFilter(p);
    }
  };
  const onClearFilters = () => {
    if (resolvedScope === "all") {
      useIssuesViewStore.getState().clearFilters();
    } else {
      useMyIssuesViewStore.getState().clearFilters();
    }
  };

  const hasActive = statusFilters.length > 0 || priorityFilters.length > 0;

  return (
    <View className="flex-1">
      <View className="flex-row items-center justify-between px-4 pt-4 pb-3">
        <Text className="text-base font-semibold text-foreground">Filter</Text>
        {hasActive ? (
          <Pressable
            onPress={onClearFilters}
            hitSlop={8}
            className="px-2 py-1 active:opacity-60"
          >
            <Text className="text-sm text-primary font-medium">Reset</Text>
          </Pressable>
        ) : null}
      </View>
      <ScrollView className="flex-1" showsVerticalScrollIndicator={false}>
        <SectionLabel>Status</SectionLabel>
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
                color={option.color}
                size={16}
              />
              <Text className="flex-1 text-sm text-foreground">
                {option.label}
              </Text>
              <CheckMark checked={checked} />
            </Pressable>
          );
        })}

        <SectionLabel>Priority</SectionLabel>
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
                {PRIORITY_LABEL[priority]}
              </Text>
              <CheckMark checked={checked} />
            </Pressable>
          );
        })}
      </ScrollView>
    </View>
  );
}

function useScopedFilters(
  scope: Scope,
  kind: "status",
): IssueStatus[];
function useScopedFilters(
  scope: Scope,
  kind: "priority",
): IssuePriority[];
function useScopedFilters(
  scope: Scope,
  kind: "status" | "priority",
): IssueStatus[] | IssuePriority[] {
  const allStatus = useIssuesViewStore((s) => s.statusFilters);
  const allPriority = useIssuesViewStore((s) => s.priorityFilters);
  const myStatus = useMyIssuesViewStore((s) => s.statusFilters);
  const myPriority = useMyIssuesViewStore((s) => s.priorityFilters);
  if (scope === "all") {
    return kind === "status" ? allStatus : allPriority;
  }
  return kind === "status" ? myStatus : myPriority;
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
