/**
 * Pure picker body for an issue's project — single-select. Mirrors the
 * assignee picker pattern: header + search bar are the iOS native nav
 * header (registered in `app/(app)/[workspace]/_layout.tsx`); the route
 * wires `headerSearchBarOptions.onChangeText` to a local `query` state
 * via `useNativeSearchBar` and passes it in as `query`. Body is a pure
 * FlatList — no chrome.
 */
import { useMemo } from "react";
import { FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import { useColorScheme } from "nativewind";
import type { Project } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ProjectIcon } from "@/components/ui/project-icon";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useScrollToTopOnChange } from "@/lib/use-scroll-to-top-on-change";
import { THEME } from "@/lib/theme";

type Row = { kind: "none" } | { kind: "project"; project: Project };

interface Props {
  value: Project | null;
  query: string;
  onChange: (next: Project | null) => void;
}

export function ProjectPickerBody({ value, query, onChange }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const listRef = useScrollToTopOnChange(query);
  const { colorScheme } = useColorScheme();
  const checkColor =
    colorScheme === "dark" ? THEME.dark.primary : THEME.light.primary;

  const rows = useMemo<Row[]>(() => {
    const q = query.trim().toLowerCase();
    const matchName = (n: string) => !q || n.toLowerCase().includes(q);
    const projectRows: Row[] = [...projects]
      .filter((p) => matchName(p.title))
      .sort((a, b) => a.title.localeCompare(b.title))
      .map((p) => ({ kind: "project" as const, project: p }));

    if (q) return projectRows;

    // Pin selected project to the top (below "No project"). Apple HIG
    // doesn't require this — product UX choice that mirrors assignee.
    const selected = projectRows.find(
      (r) => r.kind === "project" && r.project.id === value?.id,
    );
    return [
      { kind: "none" },
      ...(selected ? [selected] : []),
      ...projectRows.filter(
        (r) => !(r.kind === "project" && r.project.id === value?.id),
      ),
    ];
  }, [projects, query, value]);

  const isSelected = (row: Row) => {
    if (row.kind === "none") return value === null;
    return value !== null && row.project.id === value.id;
  };

  return (
    <FlatList
      ref={listRef}
      data={rows}
      className="flex-1"
      keyboardShouldPersistTaps="handled"
      automaticallyAdjustKeyboardInsets
      contentInsetAdjustmentBehavior="automatic"
      keyExtractor={(row) =>
        row.kind === "none" ? "none" : `p:${row.project.id}`
      }
      renderItem={({ item }) => (
        <Pressable
          onPress={() =>
            item.kind === "none" ? onChange(null) : onChange(item.project)
          }
          className="flex-row items-center gap-3 px-4 py-3 active:bg-secondary"
        >
          {item.kind === "none" ? (
            <Ionicons
              name="close-circle-outline"
              size={28}
              color={MOBILE_PLACEHOLDER_COLOR}
            />
          ) : (
            <ProjectIcon icon={item.project.icon} size="md" />
          )}
          <Text
            className="flex-1 text-base text-foreground"
            numberOfLines={1}
          >
            {item.kind === "none" ? "No project" : item.project.title}
          </Text>
          {isSelected(item) ? (
            <Ionicons name="checkmark" size={20} color={checkColor} />
          ) : null}
        </Pressable>
      )}
      ListEmptyComponent={
        <View className="px-3 py-8 items-center">
          <Text className="text-sm text-muted-foreground text-center">
            {query
              ? "No matches."
              : "No projects in this workspace yet.\nCreate them on web."}
          </Text>
        </View>
      }
    />
  );
}
