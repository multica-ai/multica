/**
 * HarmonyOS port of apps/mobile/components/project/project-header-card.tsx.
 * Header card for the project detail screen. Large emoji icon centered above
 * the title, with the description shown in full (no truncation) below.
 *
 * Progress section mirrors web `packages/views/projects/components/project-detail.tsx`:
 * horizontal bar driven by `Project.done_count / Project.issue_count` plus a
 * "X / Y" label and a percentage. Hidden when there are zero issues — empty
 * bar gives no information and creates a divide-by-zero hazard.
 */
import { StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { Project } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ProjectIcon } from "@/components/ui/project-icon";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  project: Project;
  onEdit?: () => void;
}

export function ProjectHeaderCard({ project, onEdit }: Props) {
  const c = useThemeColors();
  return (
    <Pressable
      onPress={onEdit}
      disabled={!onEdit}
      style={({ pressed }) => [
        styles.card,
        pressed && onEdit ? { backgroundColor: withAlpha(c.secondary, 0.4) } : null,
      ]}
    >
      <View style={styles.inner}>
        <ProjectIcon icon={project.icon} size="lg" />
        <Text style={[styles.title, { color: c.foreground }]} selectable>
          {project.title}
        </Text>
        {project.description ? (
          <Text style={[styles.description, { color: c.mutedForeground }]} selectable>
            {project.description}
          </Text>
        ) : onEdit ? (
          <Text
            style={[
              styles.description,
              styles.descriptionPlaceholder,
              { color: withAlpha(c.mutedForeground, 0.6) },
            ]}
          >
            Add a description
          </Text>
        ) : null}
        {project.issue_count > 0 ? (
          <ProgressSection
            done={project.done_count}
            total={project.issue_count}
          />
        ) : null}
      </View>
    </Pressable>
  );
}

function ProgressSection({ done, total }: { done: number; total: number }) {
  const c = useThemeColors();
  const pct = Math.round((done / total) * 100);
  return (
    <View style={styles.progress}>
      <View style={styles.progressLabels}>
        <Text
          style={[styles.progressTitle, { color: c.mutedForeground }]}
        >
          Progress
        </Text>
        <Text style={[styles.progressMeta, { color: c.mutedForeground }]}>
          {done} / {total} · {pct}%
        </Text>
      </View>
      <View style={[styles.progressTrack, { backgroundColor: c.secondary }]}>
        <View
          style={[
            styles.progressFill,
            { backgroundColor: c.brand, width: `${pct}%` },
          ]}
        />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  // px-4 pt-4 pb-3
  card: { paddingHorizontal: 16, paddingTop: 16, paddingBottom: 12 },
  // items-start gap-2
  inner: { alignItems: "flex-start", gap: 8 },
  // text-2xl font-bold
  title: { fontSize: 24, fontWeight: "700" },
  // text-sm
  description: { fontSize: 14 },
  // italic (placeholder variant)
  descriptionPlaceholder: { fontStyle: "italic" },
  // w-full pt-2 gap-1.5
  progress: { width: "100%", paddingTop: 8, gap: 6 },
  // flex-row items-center justify-between
  progressLabels: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
  },
  // text-xs uppercase tracking-wider
  progressTitle: {
    fontSize: 12,
    textTransform: "uppercase",
    letterSpacing: 0.6,
  },
  progressMeta: { fontSize: 12 },
  // h-1.5 rounded-full overflow-hidden
  progressTrack: { height: 6, borderRadius: 3, overflow: "hidden" },
  // h-full rounded-full
  progressFill: { height: "100%", borderRadius: 3 },
});
