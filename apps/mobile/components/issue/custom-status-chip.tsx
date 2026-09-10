/**
 * Names a custom status on a compact issue row, including ungrouped lists.
 * Built-ins stay silent to keep the default row compact.
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
      <StatusIcon
        status={status}
        category={catalog.categoryOf(status)}
        color={entry.color}
        size={10}
      />
      <Text className="text-[10px] text-muted-foreground shrink" numberOfLines={1}>
        {entry.name}
      </Text>
    </View>
  );
}
