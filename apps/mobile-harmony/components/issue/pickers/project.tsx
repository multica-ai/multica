/**
 * HarmonyOS port of apps/mobile/components/issue/pickers/project-picker-body.tsx
 * (+ `issue/[id]/picker/project.tsx`) — single-select with a "No project"
 * row. The iOS native search bar becomes a plain TextField inside the sheet.
 */
import React, { useMemo } from "react";
import { FlatList, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { Project } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ProjectIcon } from "@/components/ui/project-icon";
import { Icon } from "@/components/ui/icon";
import { TextField } from "@/components/ui/text-field";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import {
  projectListOptions,
  findProject,
} from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useScrollToTopOnChange } from "@/lib/use-scroll-to-top-on-change";
import { useThemeColors } from "@/lib/use-theme-colors";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { issueDetailOptions } from "@/data/queries/issues";
import { useUpdateIssue } from "@/data/mutations/issues";

type Row = { kind: "none" } | { kind: "project"; project: Project };

interface Props {
  value: Project | null;
  query: string;
  onChange: (next: Project | null) => void;
}

export function ProjectPickerBody({ value, query, onChange }: Props) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const listRef = useScrollToTopOnChange(query);

  const rows = useMemo<Row[]>(() => {
    const q = query.trim().toLowerCase();
    const matchName = (n: string) => !q || n.toLowerCase().includes(q);
    const projectRows: Row[] = [...projects]
      .filter((p) => matchName(p.title))
      .sort((a, b) => a.title.localeCompare(b.title))
      .map((p) => ({ kind: "project" as const, project: p }));

    if (q) return projectRows;

    // Pin selected project to the top (below "No project"). Product UX
    // choice that mirrors the assignee picker.
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
      style={styles.list}
      keyboardShouldPersistTaps="handled"
      keyExtractor={(row) =>
        row.kind === "none" ? "none" : `p:${row.project.id}`
      }
      renderItem={({ item }) => (
        <Pressable
          onPress={() =>
            item.kind === "none" ? onChange(null) : onChange(item.project)
          }
          accessibilityRole="button"
          accessibilityLabel={
            item.kind === "none" ? "No project" : item.project.title
          }
          style={({ pressed }) => [
            styles.row,
            pressed ? { backgroundColor: c.secondary } : null,
          ]}
        >
          {item.kind === "none" ? (
            <Icon name="close-circle-outline" size={28} color={MOBILE_PLACEHOLDER_COLOR} />
          ) : (
            <ProjectIcon icon={item.project.icon} size="md" />
          )}
          <Text style={[styles.rowLabel, { color: c.foreground }]} numberOfLines={1}>
            {item.kind === "none" ? "No project" : item.project.title}
          </Text>
          {isSelected(item) ? (
            <Icon name="checkmark" size={20} color={c.primary} />
          ) : null}
        </Pressable>
      )}
      ListEmptyComponent={
        <View style={styles.empty}>
          <Text style={[styles.emptyText, { color: c.mutedForeground }]}>
            {query
              ? "No matches."
              : "No projects in this workspace yet.\nCreate them on web."}
          </Text>
        </View>
      }
    />
  );
}

interface SheetProps {
  issueId: string;
  visible: boolean;
  onClose: () => void;
}

/** The iOS formSheet route — see status.tsx for the contract. */
export function IssueProjectPickerSheet({ issueId, visible, onClose }: SheetProps) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const updateIssue = useUpdateIssue(issueId);
  const [query, setQuery] = React.useState("");

  const project = useMemo(
    () => findProject(projects, issue?.project_id ?? null),
    [projects, issue?.project_id],
  );

  return (
    <BottomSheet
      visible={visible}
      onClose={() => {
        setQuery("");
        onClose();
      }}
      maxHeightRatio={0.7}
    >
      <View style={styles.sheetHeader}>
        <Text style={[styles.sheetTitle, { color: c.foreground }]}>Project</Text>
        <TextField
          value={query}
          onChangeText={setQuery}
          placeholder="Search projects"
          autoCapitalize="none"
          style={styles.search}
        />
      </View>
      <View style={styles.sheetList}>
        <ProjectPickerBody
          value={project ?? null}
          query={query}
          onChange={(next) => {
            updateIssue.mutate({ project_id: next?.id ?? null });
            onClose();
          }}
        />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  // flex-1
  list: { flex: 1 },
  // flex-row items-center gap-3 px-4 py-3
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  // flex-1 text-base
  rowLabel: { flex: 1, fontSize: 16 },
  // px-3 py-8 items-center
  empty: { paddingHorizontal: 12, paddingVertical: 32, alignItems: "center" },
  // text-sm text-center
  emptyText: { fontSize: 14, textAlign: "center" },
  sheetHeader: {
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 8,
    gap: 8,
  },
  sheetTitle: { fontSize: 18, fontWeight: "600" },
  search: { fontSize: 15 },
  sheetList: { height: 420 },
});
