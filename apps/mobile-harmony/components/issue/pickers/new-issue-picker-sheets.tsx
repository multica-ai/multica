/**
 * HarmonyOS ports of the new-issue draft picker routes
 * (`apps/mobile/app/(app)/[workspace]/new-issue-picker/*.tsx`). They
 * read/write `useNewIssueDraftStore` — the new-issue screen owns the draft
 * and reads the same store, so each sheet only needs to flip the attribute
 * and dismiss (see data/stores/new-issue-draft-store.ts).
 *
 * The iOS route bodies are reused verbatim (the picker bodies in
 * pickers/{status,priority,assignee,project,due-date}.tsx); the iOS
 * native search bars become plain TextFields inside the sheets.
 */
import React, { useMemo, useState } from "react";
import { StyleSheet, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { StatusPickerBody } from "./status";
import { PriorityPickerBody } from "./priority";
import { AssigneePickerBody, type AssigneeValue } from "./assignee";
import { ProjectPickerBody } from "./project";
import { DueDatePickerBody, type DueDatePickerBodyHandle } from "./due-date";
import { useNewIssueDraftStore } from "@/data/stores/new-issue-draft-store";
import { projectListOptions, findProject } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useThemeColors } from "@/lib/use-theme-colors";

interface SheetProps {
  visible: boolean;
  onClose: () => void;
}

export function NewIssueStatusPickerSheet({ visible, onClose }: SheetProps) {
  const status = useNewIssueDraftStore((s) => s.status);
  const setStatus = useNewIssueDraftStore((s) => s.setStatus);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <StatusPickerBody
        value={status}
        onChange={(next) => {
          setStatus(next);
          onClose();
        }}
      />
    </BottomSheet>
  );
}

export function NewIssuePriorityPickerSheet({ visible, onClose }: SheetProps) {
  const priority = useNewIssueDraftStore((s) => s.priority);
  const setPriority = useNewIssueDraftStore((s) => s.setPriority);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <PriorityPickerBody
        value={priority}
        onChange={(next) => {
          setPriority(next);
          onClose();
        }}
      />
    </BottomSheet>
  );
}

export function NewIssueAssigneePickerSheet({ visible, onClose }: SheetProps) {
  const c = useThemeColors();
  const assignee = useNewIssueDraftStore((s) => s.assignee);
  const setAssignee = useNewIssueDraftStore((s) => s.setAssignee);
  const [query, setQuery] = useState("");

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
        <Text style={[styles.sheetTitle, { color: c.foreground }]}>Assignee</Text>
        <TextField
          value={query}
          onChangeText={setQuery}
          placeholder="Search people"
          autoCapitalize="none"
          style={styles.search}
        />
      </View>
      <View style={styles.sheetList}>
        <AssigneePickerBody
          value={assignee}
          query={query}
          onChange={(next) => {
            setAssignee(next);
            onClose();
          }}
        />
      </View>
    </BottomSheet>
  );
}

export function NewIssueProjectPickerSheet({ visible, onClose }: SheetProps) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const project = useNewIssueDraftStore((s) => s.project);
  const setProject = useNewIssueDraftStore((s) => s.setProject);
  const [query, setQuery] = useState("");
  const { data: projects = [] } = useQuery(projectListOptions(wsId));

  // The draft store holds the full Project object, but re-resolve from the
  // live list when available so the pinned-selected row reflects renames.
  const value = useMemo(
    () => findProject(projects, project?.id ?? null) ?? project,
    [projects, project],
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
          value={value ?? null}
          query={query}
          onChange={(next) => {
            setProject(next);
            onClose();
          }}
        />
      </View>
    </BottomSheet>
  );
}

export function NewIssueDueDatePickerSheet({ visible, onClose }: SheetProps) {
  const c = useThemeColors();
  const dueDate = useNewIssueDraftStore((s) => s.dueDate);
  const setDueDate = useNewIssueDraftStore((s) => s.setDueDate);
  const bodyRef = React.useRef<DueDatePickerBodyHandle>(null);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      {/* Same Done / Clear header as the issue-detail due-date variant —
          the calendar doesn't auto-commit. */}
      <View style={styles.dueHeader}>
        <Text style={[styles.sheetTitle, { color: c.foreground }]}>
          Due date
        </Text>
        <View style={styles.dueActions}>
          {dueDate ? (
            <Text
              onPress={() => {
                setDueDate(null);
                onClose();
              }}
              style={[styles.dueClear, { color: c.destructive }]}
            >
              Clear
            </Text>
          ) : null}
          <Text
            onPress={() => {
              const iso = bodyRef.current?.getIso();
              if (iso) setDueDate(iso);
              onClose();
            }}
            style={[styles.dueDone, { color: c.primary }]}
          >
            Done
          </Text>
        </View>
      </View>
      <DueDatePickerBody ref={bodyRef} value={dueDate} />
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  sheetHeader: {
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 8,
    gap: 8,
  },
  // text-lg font-semibold
  sheetTitle: { fontSize: 18, fontWeight: "600" },
  search: { fontSize: 15 },
  sheetList: { height: 420 },
  dueHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 8,
  },
  dueActions: { flexDirection: "row", alignItems: "center", gap: 12 },
  // text-sm
  dueClear: { fontSize: 14 },
  // text-sm font-medium
  dueDone: { fontSize: 14, fontWeight: "500" },
});
