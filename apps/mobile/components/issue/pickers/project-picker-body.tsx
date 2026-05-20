/**
 * Pure picker body for an issue's project — single-select. See
 * status-picker-body.tsx for the split rationale.
 *
 * Phase 1 does not support inline project creation; mobile users who want a
 * new project create it on web.
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import type { Project } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ProjectIcon } from "@/components/ui/project-icon";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { TextField } from "@/components/ui/text-field";
import { projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { cn } from "@/lib/utils";

interface Props {
  value: Project | null;
  onChange: (next: Project | null) => void;
}

export function ProjectPickerBody({ value, onChange }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: projects, isLoading } = useQuery(projectListOptions(wsId));
  const [query, setQuery] = useState("");

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const all = projects ?? [];
    const sorted = [...all].sort((a, b) => a.title.localeCompare(b.title));
    if (!q) return sorted;
    return sorted.filter((p) => p.title.toLowerCase().includes(q));
  }, [projects, query]);

  return (
    <View className="flex-1">
      <View className="px-4 pt-3 pb-2">
        <Text className="text-lg font-semibold text-foreground">Project</Text>
      </View>
      <View className="px-3 pb-2 border-b border-border">
        <TextField
          value={query}
          onChangeText={setQuery}
          placeholder="Search projects"
          autoCapitalize="none"
          autoCorrect={false}
        />
      </View>
      {isLoading ? (
        <View className="px-3 py-8 items-center">
          <ActivityIndicator />
        </View>
      ) : (
        <FlatList
          data={filtered}
          className="flex-1"
          keyExtractor={(item) => item.id}
          keyboardShouldPersistTaps="handled"
          automaticallyAdjustKeyboardInsets
          ListHeaderComponent={
            <NoProjectRow
              checked={value === null}
              onPress={() => onChange(null)}
            />
          }
          renderItem={({ item }) => (
            <ProjectRow
              project={item}
              checked={item.id === value?.id}
              onPress={() => onChange(item)}
            />
          )}
          ListEmptyComponent={
            <View className="px-3 py-6 items-center">
              <Text className="text-xs text-muted-foreground text-center">
                {query
                  ? "No matches."
                  : "No projects in this workspace yet.\nCreate them on web."}
              </Text>
            </View>
          }
        />
      )}
    </View>
  );
}

function NoProjectRow({
  checked,
  onPress,
}: {
  checked: boolean;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      className={cn(
        "flex-row items-center gap-3 px-3 py-2.5 border-b border-border active:bg-secondary",
        checked && "bg-secondary",
      )}
    >
      <Ionicons
        name="close-circle-outline"
        size={16}
        color={MOBILE_PLACEHOLDER_COLOR}
      />
      <Text className="flex-1 text-sm text-muted-foreground">No project</Text>
      {checked ? <Text className="text-xs text-muted-foreground">✓</Text> : null}
    </Pressable>
  );
}

function ProjectRow({
  project,
  checked,
  onPress,
}: {
  project: Project;
  checked: boolean;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      className={cn(
        "flex-row items-center gap-3 px-3 py-2.5 active:bg-secondary",
        checked && "bg-secondary",
      )}
    >
      <ProjectIcon icon={project.icon} size="md" />
      <Text className="flex-1 text-sm text-foreground" numberOfLines={1}>
        {project.title}
      </Text>
      {checked ? <Text className="text-xs text-muted-foreground">✓</Text> : null}
    </Pressable>
  );
}
