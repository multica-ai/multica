/**
 * HarmonyOS port of apps/mobile/components/issue/custom-status-chip.tsx
 * (className → StyleSheet translation; NativeWind is not part of this app).
 *
 * Names a custom status on a compact issue row, including ungrouped lists.
 * Built-ins stay silent to keep the default row compact.
 *
 * Divergence from web: the catalog arrives as a PROP rather than from a hook
 * inside. Every mobile caller is a virtualized list row that already resolved
 * the catalog for its status icon's colour, and subscribing twice per row is
 * free on the network (React Query dedupes) but not on re-renders.
 */
import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { IssueStatus } from "@multica/core/types";
import { StatusIcon } from "@/components/ui/status-icon";
import { isCustomStatus, type IssueStatusCatalog } from "@/lib/issue-status";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

export function CustomStatusChip({
  status,
  catalog,
}: {
  status: IssueStatus;
  catalog: IssueStatusCatalog;
}) {
  const c = useThemeColors();
  const entry = catalog.entryOf(status);
  if (!isCustomStatus(catalog, status) || !entry) return null;

  return (
    // bg-secondary/60 → withAlpha; Tailwind: max-w-[120px] pl-1 pr-1.5 py-0.5.
    <View style={[styles.chip, { backgroundColor: withAlpha(c.secondary, 0.6) }]}>
      <StatusIcon
        status={status}
        category={catalog.categoryOf(status)}
        icon={entry.icon}
        color={entry.color}
        size={10}
      />
      <Text style={[styles.label, { color: c.mutedForeground }]} numberOfLines={1}>
        {entry.name}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  // flex-row items-center gap-1 shrink-0 max-w-[120px] rounded-full pl-1 pr-1.5 py-0.5
  chip: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    flexShrink: 0,
    maxWidth: 120,
    borderRadius: 9999,
    paddingLeft: 4,
    paddingRight: 6,
    paddingTop: 2,
    paddingBottom: 2,
  },
  // text-[10px] shrink
  label: { fontSize: 10, flexShrink: 1 },
});
