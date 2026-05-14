/**
 * Project picker — single-select over workspace projects. Tap a row to set,
 * tap the "No project" row to clear. Mirrors web's `ProjectPicker`
 * (`packages/views/projects/components/project-picker.tsx`) in behavior;
 * sheet shell is copied from `label-picker-sheet.tsx` and trimmed for
 * single-select.
 *
 * Phase 1 does NOT support inline project creation — mobile users who want
 * a new project create it on web.
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, FlatList, Modal, Pressable, TextInput, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import type { Project } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ProjectIcon } from "@/components/ui/project-icon";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { cn } from "@/lib/utils";

interface Props {
  visible: boolean;
  value: string | null;
  onChange: (next: string | null) => void;
  onClose: () => void;
}

export function ProjectPickerSheet({ visible, value, onChange, onClose }: Props) {
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

  const pick = (next: string | null) => {
    onChange(next);
    onClose();
  };

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onClose}
    >
      <Pressable className="flex-1 bg-black/40" onPress={onClose}>
        <View className="flex-1 items-center justify-center px-6">
          <Pressable onPress={() => {}} className="w-full max-w-sm">
            <View className="bg-popover rounded-2xl overflow-hidden">
              <View className="px-3 pt-3 pb-2 border-b border-border">
                <TextInput
                  value={query}
                  onChangeText={setQuery}
                  placeholder="Search projects"
                  placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
                  className="text-sm text-foreground bg-secondary/50 rounded-md px-3 py-2"
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
                  keyExtractor={(item) => item.id}
                  style={{ maxHeight: 380 }}
                  ListHeaderComponent={
                    <NoProjectRow
                      checked={value === null}
                      onPress={() => pick(null)}
                    />
                  }
                  renderItem={({ item }) => (
                    <ProjectRow
                      project={item}
                      checked={item.id === value}
                      onPress={() => pick(item.id)}
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
          </Pressable>
        </View>
      </Pressable>
    </Modal>
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
      <Ionicons name="close-circle-outline" size={16} color={MOBILE_PLACEHOLDER_COLOR} />
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
