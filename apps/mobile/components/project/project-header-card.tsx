/**
 * Header card for the project detail screen. Large emoji icon centered above
 * the title, with the description shown in full (no truncation) below.
 *
 * Mirrors the visual emphasis of web's `project-header.tsx` but in a single
 * vertical stack instead of the web sidebar layout — phones don't have the
 * horizontal real estate for a side-by-side header + properties layout.
 *
 * Progress section mirrors web `packages/views/projects/components/project-detail.tsx:596-620`:
 * horizontal bar driven by `Project.done_count / Project.issue_count` plus a
 * "X / Y" label and a percentage. Hidden when there are zero issues — empty
 * bar gives no information and creates a divide-by-zero hazard.
 */
import { Pressable, View } from "react-native";
import type { Project } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ProjectIcon } from "@/components/ui/project-icon";

interface Props {
  project: Project;
  onEdit?: () => void;
}

export function ProjectHeaderCard({ project, onEdit }: Props) {
  return (
    <Pressable
      onPress={onEdit}
      disabled={!onEdit}
      className="px-4 pt-4 pb-3 active:bg-secondary/40"
    >
      <View className="items-start gap-2">
        <ProjectIcon icon={project.icon} size="lg" />
        <Text
          className="text-2xl font-bold text-foreground"
          selectable
        >
          {project.title}
        </Text>
        {project.description ? (
          <Text
            className="text-sm text-muted-foreground"
            selectable
          >
            {project.description}
          </Text>
        ) : onEdit ? (
          <Text className="text-sm text-muted-foreground/60 italic">
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
  const pct = Math.round((done / total) * 100);
  return (
    <View className="w-full pt-2 gap-1.5">
      <View className="flex-row items-center justify-between">
        <Text className="text-xs uppercase tracking-wider text-muted-foreground">
          Progress
        </Text>
        <Text className="text-xs text-muted-foreground">
          {done} / {total} · {pct}%
        </Text>
      </View>
      <View className="h-1.5 bg-secondary rounded-full overflow-hidden">
        <View
          className="h-full bg-brand rounded-full"
          style={{ width: `${pct}%` }}
        />
      </View>
    </View>
  );
}
