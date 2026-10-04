/**
 * HarmonyOS port of apps/mobile/components/issue/pickers/priority-picker-body.tsx
 * (+ `issue/[id]/picker/priority.tsx`). Single-select over the 5 priority
 * enum values.
 */
import React from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { IssuePriority } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { Icon } from "@/components/ui/icon";
import { PRIORITY_LABEL } from "@/lib/issue-status";
import { useThemeColors } from "@/lib/use-theme-colors";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { issueDetailOptions } from "@/data/queries/issues";
import { useUpdateIssue } from "@/data/mutations/issues";
import { useWorkspaceStore } from "@/data/workspace-store";

// Display order: severity descending (urgent → none).
const PRIORITY_OPTIONS: IssuePriority[] = [
  "urgent",
  "high",
  "medium",
  "low",
  "none",
];

interface Props {
  value: IssuePriority;
  onChange: (next: IssuePriority) => void;
}

export function PriorityPickerBody({ value, onChange }: Props) {
  const c = useThemeColors();

  return (
    <ScrollView showsVerticalScrollIndicator={false}>
      <View style={styles.titleRow}>
        <Text style={[styles.title, { color: c.foreground }]}>Priority</Text>
      </View>
      <View style={styles.list}>
        {PRIORITY_OPTIONS.map((v) => {
          const selected = v === value;
          return (
            <Pressable
              key={v}
              onPress={() => onChange(v)}
              accessibilityRole="button"
              accessibilityLabel={PRIORITY_LABEL[v]}
              style={({ pressed }) => [
                styles.row,
                pressed ? { backgroundColor: c.secondary } : null,
              ]}
            >
              <PriorityIcon priority={v} size={16} />
              <Text style={[styles.rowLabel, { color: c.foreground }]}>
                {PRIORITY_LABEL[v]}
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

/** The iOS formSheet route — see status.tsx for the self-contained pattern. */
export function IssuePriorityPickerSheet({ issueId, visible, onClose }: SheetProps) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const updateIssue = useUpdateIssue(issueId);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <PriorityPickerBody
        value={issue?.priority ?? "none"}
        onChange={(next) => {
          updateIssue.mutate({ priority: next });
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
