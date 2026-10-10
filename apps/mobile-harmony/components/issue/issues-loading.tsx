/**
 * HarmonyOS port of apps/mobile/components/issue/issues-loading.tsx
 * (className → StyleSheet translation; NativeWind is not part of this app).
 *
 * Loading skeleton for issue-list surfaces — My Issues
 * (`src/screens/my-issues-screen.tsx`) and the workspace Issues page. Both
 * group issues by status via SectionList; this skeleton mirrors that shape so
 * the eye immediately sees a list-like structure instead of a centered
 * spinner. Mirrors the "perceived perf wins over centered spinner" pattern
 * from InboxLoading.
 *
 * Row skeleton mirrors `IssueRow` layout (px-4 py-3, priority dot +
 * identifier slot + title flex + trailing assignee circle), and the section
 * header skeleton mirrors the page's `SectionHeader` (px-4 py-2 with a
 * status-icon-shaped dot and a short label band).
 */
import React from "react";
import { StyleSheet, View } from "react-native";
import { Skeleton } from "@/components/ui/skeleton";

export function IssuesLoading() {
  return (
    <View style={styles.wrap}>
      {Array.from({ length: 2 }).map((_, sectionIdx) => (
        <View key={sectionIdx} style={styles.section}>
          <View style={styles.sectionHeader}>
            <Skeleton style={styles.dotIcon} />
            <Skeleton style={styles.labelBand} />
          </View>
          {Array.from({ length: 3 }).map((_, rowIdx) => (
            <View key={rowIdx} style={styles.row}>
              <Skeleton style={styles.dotIcon} />
              <Skeleton style={styles.identifierBand} />
              <Skeleton style={styles.titleBand} />
              <Skeleton style={styles.avatar} />
            </View>
          ))}
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  // pt-2
  wrap: { paddingTop: 8 },
  // pb-2
  section: { paddingBottom: 8 },
  // px-4 py-2 flex-row items-center gap-2
  sectionHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 8,
  },
  // size-3.5 rounded-full
  dotIcon: { width: 14, height: 14, borderRadius: 9999 },
  // h-3 w-20
  labelBand: { height: 12, width: 80 },
  // flex-row items-center gap-3 px-4 py-3
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  // h-3 w-14
  identifierBand: { height: 12, width: 56 },
  // h-3.5 flex-1
  titleBand: { height: 14, flex: 1 },
  // size-6 rounded-full
  avatar: { width: 24, height: 24, borderRadius: 9999 },
});
