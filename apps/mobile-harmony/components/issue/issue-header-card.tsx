/**
 * HarmonyOS port of apps/mobile/components/issue/issue-header-card.tsx —
 * slim header for the issue detail screen.
 *
 * Linear iOS-inspired layout:
 *   - identifier (MUL-NN) above as a small muted label
 *   - title in a large bold treatment
 *   - attribute chip row below (status / priority / assignee / labels /
 *     project / due date) — tappable, opens picker sheets
 *
 * Navigation delta vs iOS: the runs push goes through the `onOpenRuns`
 * callback prop.
 */
import React from "react";
import { StyleSheet, View } from "react-native";
import type { Issue } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { AttributeRow, type IssuePickerField } from "./attribute-row";
import { AgentActivityRow } from "./agent-activity-row";
import { useThemeColors } from "@/lib/use-theme-colors";

export function IssueHeaderCard({
  issue,
  onOpenPicker,
  onOpenRuns,
}: {
  issue: Issue;
  onOpenPicker: (field: IssuePickerField) => void;
  onOpenRuns: (issueId: string) => void;
}) {
  const c = useThemeColors();
  return (
    <View style={styles.wrap}>
      <Text style={[styles.identifier, { color: c.mutedForeground }]}>
        {issue.identifier}
      </Text>
      <Text style={[styles.title, { color: c.foreground }]}>{issue.title}</Text>
      {/* Activity row sits between title and attributes — it represents
       *  "who's doing this issue right now / who has done it" (dynamic),
       *  which is higher-IA than the static property chips below.
       *  Conditionally renders null when there are no tasks at all. */}
      <AgentActivityRow issueId={issue.id} onOpenRuns={onOpenRuns} />
      <AttributeRow issue={issue} onOpenPicker={onOpenPicker} />
    </View>
  );
}

const styles = StyleSheet.create({
  // px-4 pt-4 pb-3 gap-3
  wrap: {
    paddingHorizontal: 16,
    paddingTop: 16,
    paddingBottom: 12,
    gap: 12,
  },
  // text-xs
  identifier: { fontSize: 12 },
  // text-2xl font-bold
  title: { fontSize: 24, fontWeight: "700" },
});
