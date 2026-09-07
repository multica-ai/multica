/**
 * Names an issue's status when the surface around it only shows the CATEGORY
 * (MUL-6243).
 *
 * List sections are categories, so two issues sitting in the same "In Review"
 * section can be on different statuses — "Code Review" and "QA" — with nothing
 * on the row to tell them apart. This chip is that missing signal, mirroring
 * web's `packages/views/issues/components/custom-status-chip.tsx`.
 *
 * It renders NOTHING for a status that already is its category's built-in: the
 * section header says "In Review" and a chip repeating it is pure noise. So a
 * workspace that never defined a custom status sees no visual change at all.
 *
 * Divergence from web: the catalog arrives as a PROP rather than from a hook
 * inside. Every mobile caller is a virtualized list row that already resolved
 * the catalog for its status icon's colour, and subscribing twice per row is
 * free on the network (React Query dedupes) but not on re-renders.
 */
import { View } from "react-native";
import type { IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { isCustomStatus, type IssueStatusCatalog } from "@/lib/issue-status";

export function CustomStatusChip({
  status,
  catalog,
}: {
  status: IssueStatus;
  catalog: IssueStatusCatalog;
}) {
  const entry = catalog.entryOf(status);
  if (!isCustomStatus(catalog, status) || !entry) return null;

  return (
    <View className="flex-row items-center gap-1 shrink-0 max-w-[120px] rounded-full bg-secondary/60 pl-1 pr-1.5 py-0.5">
      <StatusIcon status={status} category={entry.category} color={entry.color} size={10} />
      <Text className="text-[10px] text-muted-foreground shrink" numberOfLines={1}>
        {entry.name}
      </Text>
    </View>
  );
}
