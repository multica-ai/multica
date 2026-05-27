/**
 * Loading skeleton for issue-list surfaces — My Issues (`(tabs)/my-issues.tsx`)
 * and workspace Issues (`more/issues.tsx`). Both group issues by status via
 * SectionList; this skeleton mirrors that shape so the eye immediately sees
 * a list-like structure instead of a centered spinner. Mirrors the
 * "perceived perf wins over centered spinner" pattern from InboxLoading.
 *
 * Row skeleton mirrors `IssueRow` layout (px-4 py-3, priority dot +
 * identifier slot + title flex + trailing assignee circle), and the section
 * header skeleton mirrors the page's `SectionHeader` (px-4 py-2 with a
 * status-icon-shaped dot and a short label band).
 */
import { View } from "react-native";
import { Skeleton } from "@/components/ui/skeleton";

export function IssuesLoading() {
  return (
    <View className="pt-2">
      {Array.from({ length: 2 }).map((_, sectionIdx) => (
        <View key={sectionIdx} className="pb-2">
          <View className="px-4 py-2 flex-row items-center gap-2">
            <Skeleton className="size-3.5 rounded-full" />
            <Skeleton className="h-3 w-20" />
          </View>
          {Array.from({ length: 3 }).map((_, rowIdx) => (
            <View key={rowIdx} className="flex-row items-center gap-3 px-4 py-3">
              <Skeleton className="size-3.5 rounded-full" />
              <Skeleton className="h-3 w-14" />
              <Skeleton className="h-3.5 flex-1" />
              <Skeleton className="size-6 rounded-full" />
            </View>
          ))}
        </View>
      ))}
    </View>
  );
}
