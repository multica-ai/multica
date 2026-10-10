/**
 * HarmonyOS port of apps/mobile/components/issue/pickers/status-picker-body.tsx
 * (+ its formSheet route, `issue/[id]/picker/status.tsx`). Single-select over
 * the workspace's status catalog; the sheet reads the issue from the TanStack
 * Query detail cache and fires `useUpdateIssue` on selection — the same
 * self-contained contract as the iOS route.
 *
 * Options come from `statusOptions()` — the same list the status filter
 * reads, so a status offered in one and missing from the other can't happen.
 * (MUL-6243)
 */
import React from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { Icon } from "@/components/ui/icon";
import { statusOptions } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { useThemeColors } from "@/lib/use-theme-colors";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { issueDetailOptions } from "@/data/queries/issues";
import { useUpdateIssue } from "@/data/mutations/issues";
import { useWorkspaceStore } from "@/data/workspace-store";

interface Props {
  value: IssueStatus;
  onChange: (next: IssueStatus) => void;
}

export function StatusPickerBody({ value, onChange }: Props) {
  const c = useThemeColors();
  const catalog = useIssueStatuses();
  const options = statusOptions(catalog);

  return (
    <ScrollView showsVerticalScrollIndicator={false}>
      <View style={styles.titleRow}>
        <Text style={[styles.title, { color: c.foreground }]}>Status</Text>
      </View>
      <View style={styles.list}>
        {options.map((option) => {
          const selected = option.key === value;
          return (
            <Pressable
              key={option.key}
              onPress={() => onChange(option.key)}
              accessibilityRole="button"
              accessibilityLabel={option.label}
              style={({ pressed }) => [
                styles.row,
                pressed ? { backgroundColor: c.secondary } : null,
              ]}
            >
              <StatusIcon
                status={option.key}
                category={option.category}
                icon={option.icon}
                color={option.color}
                size={18}
              />
              <Text style={[styles.rowLabel, { color: c.foreground }]}>
                {option.label}
              </Text>
              {selected ? (
                <Icon name="checkmark" size={20} color={c.primary} />
              ) : null}
            </Pressable>
          );
        })}
      </View>
    </ScrollView>
  );
}

interface SheetProps {
  issueId: string;
  visible: boolean;
  onClose: () => void;
}

/**
 * The iOS formSheet route (`issue/[id]/picker/status.tsx`): self-contained —
 * reads the issue from the detail cache, calls `useUpdateIssue` directly on
 * selection, then dismisses. If the cache is cold the picker still renders
 * against the current `todo` default and the optimistic mutation patches
 * the cache on pick.
 */
export function IssueStatusPickerSheet({ issueId, visible, onClose }: SheetProps) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const updateIssue = useUpdateIssue(issueId);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <StatusPickerBody
        value={issue?.status ?? "todo"}
        onChange={(next) => {
          updateIssue.mutate({ status: next });
          onClose();
        }}
      />
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  // px-4 pt-3 pb-2
  titleRow: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 8 },
  // text-lg font-semibold
  title: { fontSize: 18, fontWeight: "600" },
  // px-2
  list: { paddingHorizontal: 8 },
  // flex-row items-center gap-3 rounded-lg px-3 py-3
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 12,
  },
  // flex-1 text-base
  rowLabel: { flex: 1, fontSize: 16 },
});
