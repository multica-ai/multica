/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/issues-filter.tsx.
 * The iOS route renders as a formSheet pushed by the parent stack; here the
 * route body is wrapped in the hand-rolled `<BottomSheet>`
 * (src/navigation/bottom-sheet.tsx stands in for formSheet/modal routes), so
 * callers host it with visible/onClose instead of pushing a route.
 *
 * Status + Priority filter sheet — shared by My Issues and the workspace-wide
 * Issues page; which view-store to read/write is selected by the `scope`
 * prop (the iOS version's `scope` URL param).
 *
 * Callers:
 *   - MyIssuesScreen (scope "my")   →  useMyIssuesViewStore
 *   - future workspace Issues page (scope "all") →  useIssuesViewStore
 *
 * Self-contained: reads/writes the store directly, no callback passing.
 */
import React from "react";
import { Dimensions, ScrollView, StyleSheet, Text, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { useIssuesViewStore } from "@/data/stores/issues-view-store";
import { useMyIssuesViewStore } from "@/data/stores/my-issues-view-store";
import { statusOptions } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { withAlpha } from "@/lib/theme";
import { useSafeAreaInsets } from "@/lib/safe-area";
import { useThemeColors } from "@/lib/use-theme-colors";

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

export function IssuesFilterSheet({
  visible,
  onClose,
  scope = "my",
}: {
  visible: boolean;
  onClose: () => void;
  /** Which view-store to read/write. Default "my" (the iOS param's fallback). */
  scope?: Scope;
}) {
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <IssuesFilterBody scope={scope} />
    </BottomSheet>
  );
}

function IssuesFilterBody({ scope }: { scope: Scope }) {
  const c = useThemeColors();
  const insets = useSafeAreaInsets();

  const statusFilters = useScopedFilters(scope, "status");
  const priorityFilters = useScopedFilters(scope, "priority");
  // Same option list the status picker offers, so every status a user can set
  // is also a status they can filter by. (MUL-6243)
  const catalog = useIssueStatuses();
  const statusChoices = statusOptions(catalog);

  const onToggleStatus = (s: IssueStatus) => {
    if (scope === "all") {
      useIssuesViewStore.getState().toggleStatusFilter(s);
    } else {
      useMyIssuesViewStore.getState().toggleStatusFilter(s);
    }
  };
  const onTogglePriority = (p: IssuePriority) => {
    if (scope === "all") {
      useIssuesViewStore.getState().togglePriorityFilter(p);
    } else {
      useMyIssuesViewStore.getState().togglePriorityFilter(p);
    }
  };
  const onClearFilters = () => {
    if (scope === "all") {
      useIssuesViewStore.getState().clearFilters();
    } else {
      useMyIssuesViewStore.getState().clearFilters();
    }
  };

  const hasActive = statusFilters.length > 0 || priorityFilters.length > 0;

  return (
    // Content-sized like the sheet intends, but bounded so the row list
    // scrolls on short screens (the ScrollView shrinks instead of overflowing
    // the sheet's own maxHeight cap).
    <View
      style={{
        maxHeight:
          Math.round(Dimensions.get("window").height * 0.92) -
          insets.top -
          insets.bottom -
          32,
      }}
    >
      <View style={styles.header}>
        <Text style={[styles.title, { color: c.foreground }]}>Filter</Text>
        {hasActive ? (
          <Pressable
            onPress={onClearFilters}
            hitSlop={8}
            style={({ pressed }) => [styles.reset, pressed && { opacity: 0.6 }]}
          >
            <Text style={[styles.resetLabel, { color: c.primary }]}>Reset</Text>
          </Pressable>
        ) : null}
      </View>
      <ScrollView
        style={styles.list}
        contentContainerStyle={styles.listContent}
        showsVerticalScrollIndicator={false}
      >
        <SectionLabel>Status</SectionLabel>
        {statusChoices.map((option) => {
          const checked = statusFilters.includes(option.key);
          return (
            <Pressable
              key={option.key}
              onPress={() => onToggleStatus(option.key)}
              style={({ pressed }) => [
                styles.option,
                checked && { backgroundColor: withAlpha(c.secondary, 0.6) },
                pressed && { backgroundColor: c.secondary },
              ]}
            >
              <StatusIcon
                status={option.key}
                category={option.category}
                icon={option.icon}
                color={option.color}
                size={16}
              />
              <Text style={[styles.optionLabel, { color: c.foreground }]}>
                {option.label}
              </Text>
              <CheckMark checked={checked} color={c.primary} />
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
              style={({ pressed }) => [
                styles.option,
                checked && { backgroundColor: withAlpha(c.secondary, 0.6) },
                pressed && { backgroundColor: c.secondary },
              ]}
            >
              <PriorityIcon priority={priority} />
              <Text style={[styles.optionLabel, { color: c.foreground }]}>
                {PRIORITY_LABEL[priority]}
              </Text>
              <CheckMark checked={checked} color={c.primary} />
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
  const c = useThemeColors();
  return (
    <View style={styles.sectionLabel}>
      <Text style={[styles.sectionLabelText, { color: c.mutedForeground }]}>
        {children}
      </Text>
    </View>
  );
}

function CheckMark({ checked, color }: { checked: boolean; color: string }) {
  if (!checked) return null;
  return <Text style={[styles.check, { color }]}>✓</Text>;
}

const styles = StyleSheet.create({
  // flex-row items-center justify-between px-4 pt-4 pb-3
  header: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingTop: 16,
    paddingBottom: 12,
  },
  // text-base font-semibold
  title: { fontSize: 16, fontWeight: "600" },
  // px-2 py-1
  reset: { paddingHorizontal: 8, paddingVertical: 4 },
  // text-sm font-medium
  resetLabel: { fontSize: 14, fontWeight: "500" },
  list: { flexGrow: 0, flexShrink: 1 },
  listContent: { paddingBottom: 8 },
  // flex-row items-center gap-3 px-4 py-2.5
  option: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 10,
  },
  // flex-1 text-sm
  optionLabel: { flex: 1, fontSize: 14 },
  // text-sm font-semibold
  check: { fontSize: 14, fontWeight: "600" },
  // px-4 pt-3 pb-1.5
  sectionLabel: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 6 },
  // text-xs uppercase tracking-wider font-medium
  sectionLabelText: {
    fontSize: 12,
    textTransform: "uppercase",
    letterSpacing: 0.6,
    fontWeight: "500",
  },
});
