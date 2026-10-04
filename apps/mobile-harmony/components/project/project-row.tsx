/**
 * HarmonyOS port of apps/mobile/components/project/project-row.tsx.
 * Mirrors the IssueRow layout shape from `(tabs)/my-issues.tsx` (left icon +
 * flex title + right column for counts + time) — row's right-side elements
 * stack vertically into a column.
 *
 * Layout:
 *   [📦 icon]  Project title          [3/12]
 *              [● in progress] [▍▍ high]   2d ago
 *
 * NativeWind className styling is not part of this app (AGENTS.md), so the
 * iOS classes are expressed as explicit StyleSheet styles with colors from
 * useThemeColors().
 */
import { StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { Project } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ProjectIcon } from "@/components/ui/project-icon";
import { ProjectStatusIcon } from "@/components/ui/project-status-icon";
import { ProjectPriorityIcon } from "@/components/ui/project-priority-icon";
import {
  projectPriorityLabel,
  projectStatusLabel,
} from "@/lib/project-status";
import { timeAgo } from "@/lib/time-ago";
import { useThemeColors } from "@/lib/use-theme-colors";
import { withAlpha } from "@/lib/theme";

interface Props {
  project: Project;
  onPress: () => void;
}

export function ProjectRow({ project, onPress }: Props) {
  const c = useThemeColors();
  const totalIssues = project.issue_count;
  const showCount = totalIssues > 0;

  return (
    <Pressable
      onPress={onPress}
      style={({ pressed }) => [
        styles.row,
        pressed ? { backgroundColor: c.secondary } : null,
      ]}
    >
      <View style={styles.inner}>
        <ProjectIcon icon={project.icon} size="lg" />
        <View style={styles.main}>
          <Text
            style={[styles.title, { color: c.foreground }]}
            numberOfLines={1}
          >
            {project.title}
          </Text>
          <View style={styles.metaRow}>
            <View style={styles.metaItem}>
              <ProjectStatusIcon status={project.status} size={12} />
              <Text style={[styles.metaText, { color: c.mutedForeground }]}>
                {projectStatusLabel(project.status)}
              </Text>
            </View>
            {project.priority !== "none" ? (
              <View style={styles.metaItem}>
                <ProjectPriorityIcon priority={project.priority} size={12} />
                <Text style={[styles.metaText, { color: c.mutedForeground }]}>
                  {projectPriorityLabel(project.priority)}
                </Text>
              </View>
            ) : null}
          </View>
        </View>
        <View style={styles.side}>
          {showCount ? (
            <Text style={[styles.count, { color: c.mutedForeground }]}>
              {project.done_count}/{totalIssues}
            </Text>
          ) : (
            <Text
              style={[styles.count, { color: withAlpha(c.mutedForeground, 0.6) }]}
            >
              —
            </Text>
          )}
          <Text style={[styles.time, { color: withAlpha(c.mutedForeground, 0.7) }]}>
            {timeAgo(project.updated_at)}
          </Text>
        </View>
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  // px-4 py-3
  row: { paddingHorizontal: 16, paddingVertical: 12 },
  // flex-row items-start gap-3
  inner: { flexDirection: "row", alignItems: "flex-start", gap: 12 },
  // flex-1 gap-1
  main: { flex: 1, gap: 4 },
  // text-base font-medium
  title: { fontSize: 16, fontWeight: "500" },
  // flex-row items-center gap-3
  metaRow: { flexDirection: "row", alignItems: "center", gap: 12 },
  // flex-row items-center gap-1.5
  metaItem: { flexDirection: "row", alignItems: "center", gap: 6 },
  // text-xs
  metaText: { fontSize: 12 },
  // items-end gap-1
  side: { alignItems: "flex-end", gap: 4 },
  // text-xs tabular-nums
  count: { fontSize: 12, fontVariant: ["tabular-nums"] },
  // text-[11px]
  time: { fontSize: 11 },
});
